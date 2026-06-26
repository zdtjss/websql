// Package monitor: ai_analyze.go — 监控变量/状态 AI 分析（SSE 流式）
//
// 在数据库监控对话框的"服务器变量""状态指标"页签下提供 AI 分析入口，
// 将前端当前展示的变量/指标列表发给 AI，由 AI 输出风险点、性能瓶颈与优化建议。
// 复用 agent.BuildChatModel + StreamChunk 流式协议，与 SQL 优化建议体验一致。
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	agentv2 "websql/internal/ai/agent"
	"websql/internal/app/system"
	"websql/internal/logger"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

// AIAnalyzeRequest 前端提交的 AI 分析请求
type AIAnalyzeRequest struct {
	ConnID  string    `json:"connId"`
	Kind    string    `json:"kind"`    // variables | status
	DbType  string    `json:"dbType"`  // mysql | mariadb | oracle | sqlite
	Version string    `json:"version"` // 数据库版本号，用于提示词上下文
	Data    []VarInfo `json:"data"`    // 当前展示的变量/指标列表（已过滤）
}

// aiAnalyzeMaxItems 单次分析的最大条目数，防止 token 超限
const aiAnalyzeMaxItems = 300

// AIAnalyze POST /api/monitor/aiAnalyze
//
// 请求体: AIAnalyzeRequest（JSON）
// 响应: SSE 流式，data 字段为 StreamChunk JSON：
//
//	{type:"thinking",content:"..."}  AI 推理过程
//	{type:"content",content:"..."}   Markdown 分析结果
//	{type:"error",content:"..."}     错误信息
//	{type:"done"}                    结束标记
//
// 设计要点：
//   - AI 未配置时返回明确错误，前端展示"请管理员配置 AI"
//   - 传入数据超过 300 条时截断，避免 token 超限
//   - 不调用工具，直接基于传入数据分析，响应快、风险低
//   - 客户端断开连接时通过 context cancel 中止生成
func AIAnalyze(c *gin.Context) {
	var req AIAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.WriteErr(c, 200, 500, "参数解析失败："+err.Error())
		return
	}

	// connId 兜底：优先用请求体里的，再用 appctx 取（兼容 query/form）
	if req.ConnID == "" {
		req.ConnID = appctx.Ctx.GetConnID(c)
	}

	if req.Kind != "variables" && req.Kind != "status" {
		response.WriteErr(c, 200, 500, "非法的分析类型： "+req.Kind)
		return
	}
	if len(req.Data) == 0 {
		response.WriteErr(c, 200, 500, "当前没有可分析的数据")
		return
	}

	// dbType/version 兜底：前端未传时从 t_conn 表读取（连接建立时已保存）
	if req.DbType == "" || req.Version == "" {
		dbType, _, version := getConnDbTypeAndVersion(req.ConnID)
		if req.DbType == "" {
			req.DbType = dbType
		}
		if req.Version == "" {
			req.Version = version
		}
	}

	// 截断超长数据，避免 token 超限
	truncated := false
	if len(req.Data) > aiAnalyzeMaxItems {
		req.Data = req.Data[:aiAnalyzeMaxItems]
		truncated = true
	}

	aiCfg := system.GetSelectedModelConfig("")
	if aiCfg == nil || aiCfg.ApiKey == "" || aiCfg.BaseURL == "" {
		response.WriteErr(c, 200, 500, "AI 模型未配置，请联系管理员在系统设置中配置 AI 模型")
		return
	}

	// SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	var mu sync.Mutex
	dead := false

	writeSSE := func(data string) {
		mu.Lock()
		defer mu.Unlock()
		if dead {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}

	flush := func(chunk agentv2.StreamChunk) {
		b, _ := json.Marshal(chunk)
		writeSSE(string(b))
	}

	// 心跳：每 5 秒发送空行，防止代理/浏览器断开空闲连接
	kaStop := make(chan struct{})
	defer close(kaStop)
	safego.GoWithName("monitor-ai-keepalive", func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-kaStop:
				return
			case <-ticker.C:
				mu.Lock()
				if !dead {
					c.Writer.WriteString("data: \n\n")
					c.Writer.Flush()
				}
				mu.Unlock()
			}
		}
	})

	// 总超时 5 分钟，覆盖大多数模型响应
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 客户端断开时中止生成
	safego.GoWithName("monitor-ai-ctx-watch", func() {
		<-c.Request.Context().Done()
		mu.Lock()
		dead = true
		mu.Unlock()
		cancel()
	})

	cm, err := agentv2.BuildChatModel(ctx, aiCfg)
	if err != nil {
		logger.PrintErrf("创建监控分析模型失败", err)
		flush(agentv2.StreamChunk{Type: "error", Content: "AI 模型初始化失败: " + err.Error()})
		flush(agentv2.StreamChunk{Type: "done"})
		return
	}

	sysPrompt := buildAIAnalyzeSystemPrompt(req.Kind, req.DbType, req.Version, truncated)
	userPrompt := buildAIAnalyzeUserPrompt(req.Data)

	msgs := []*schema.Message{
		{Role: schema.System, Content: sysPrompt},
		{Role: schema.User, Content: userPrompt},
	}

	sr, err := cm.Stream(ctx, msgs)
	if err != nil {
		logger.PrintErrf("监控分析流式调用失败", err)
		flush(agentv2.StreamChunk{Type: "error", Content: "AI 调用失败: " + err.Error()})
		flush(agentv2.StreamChunk{Type: "done"})
		return
	}

	for {
		chunk, recvErr := sr.Recv()
		if recvErr != nil {
			break
		}
		if chunk.ReasoningContent != "" {
			flush(agentv2.StreamChunk{Type: "thinking", Content: chunk.ReasoningContent})
		}
		if chunk.Content != "" {
			flush(agentv2.StreamChunk{Type: "content", Content: chunk.Content})
		}
	}

	flush(agentv2.StreamChunk{Type: "done"})
}

// buildAIAnalyzeSystemPrompt 构造系统提示词，明确 AI 角色、输出格式与分析重点。
func buildAIAnalyzeSystemPrompt(kind, dbType, version string, truncated bool) string {
	var sb strings.Builder

	sb.WriteString("你是一名资深 DBA，擅长通过服务器变量与状态指标诊断数据库健康度。\n")
	sb.WriteString("请基于用户提供的真实数据进行分析，禁止凭猜测给出结论，所有判断需引用具体变量名和数值。\n\n")

	fmt.Fprintf(&sb, "## 环境信息\n- 数据库类型：%s\n", dbType)
	if version != "" {
		fmt.Fprintf(&sb, "- 数据库版本：%s\n", version)
	}
	if kind == "variables" {
		sb.WriteString("- 数据来源：服务器变量（初始化参数）\n")
	} else {
		sb.WriteString("- 数据来源：运行时状态指标（累计计数器）\n")
	}
	if truncated {
		fmt.Fprintf(&sb, "- 注意：数据已截断为前 %d 条，分析时请说明仅覆盖部分数据\n", aiAnalyzeMaxItems)
	}
	sb.WriteString("\n")

	sb.WriteString("## 分析重点\n")
	if kind == "variables" {
		sb.WriteString("- **风险配置**：识别存在安全/稳定性风险的参数（如 max_connections 过低/过高、innodb_buffer_pool_size 不合理、character_set_server 非 utf8mb4 等）\n")
		sb.WriteString("- **性能相关**：关注 buffer pool、连接池、日志、超时等性能参数是否合理\n")
		sb.WriteString("- **推荐值对比**：给出关键参数的推荐值或计算公式，并与当前值对比\n")
	} else {
		sb.WriteString("- **性能瓶颈信号**：识别命中率低、等待多、慢查询多、锁等待多等异常信号\n")
		sb.WriteString("- **资源使用**：关注连接数、线程、IO、临时表、无索引查询等累计指标\n")
		sb.WriteString("- **趋势推断**：基于累计值推断可能的性能压力（如 Slow_queries 增长、Innodb_buffer_pool_reads 偏高等）\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## 输出格式（Markdown）\n")
	sb.WriteString("### 概览\n")
	sb.WriteString("用 2-3 句话总结数据库整体健康度\n\n")
	sb.WriteString("### 风险/异常项\n")
	sb.WriteString("逐条列出有问题的变量/指标，格式：`变量名=当前值` — 问题描述 + 推荐\n\n")
	sb.WriteString("### 性能优化建议\n")
	sb.WriteString("给出可执行的优化动作，每条带预期收益\n\n")
	sb.WriteString("### 正常项（可选）\n")
	sb.WriteString("简要列举关键正常指标，给出健康基线\n\n")

	sb.WriteString("## 约束\n")
	sb.WriteString("- 仅基于用户提供的数据分析，不要假设未提供的数据\n")
	sb.WriteString("- 不要输出与监控分析无关的内容（如代码风格、命名规范）\n")
	sb.WriteString("- 如果数据全部正常，直接说明无需优化，不要强行编造问题\n")
	sb.WriteString("- 输出简洁专业，避免冗余铺垫\n")
	return sb.String()
}

// buildAIAnalyzeUserPrompt 构造用户消息，紧凑展示变量/指标列表以节省 token。
func buildAIAnalyzeUserPrompt(data []VarInfo) string {
	var sb strings.Builder
	sb.WriteString("请分析以下")
	if len(data) == aiAnalyzeMaxItems {
		fmt.Fprintf(&sb, "（前 %d 条）", aiAnalyzeMaxItems)
	}
	sb.WriteString("数据：\n\n")
	for _, d := range data {
		fmt.Fprintf(&sb, "%s = %s\n", d.Name, d.Value)
	}
	sb.WriteString("\n请按系统提示要求的格式输出分析结果。")
	return sb.String()
}
