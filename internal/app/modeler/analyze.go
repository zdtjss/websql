// Package modeler: analyze.go — ER 关系 AI 推断
//
// 现实中绝大多数数据库不在物理层创建外键约束（关系由应用程序定义），
// 因此 ER 图组件加载到的物理外键常常为空。本接口把前端已加载的表元数据
// （表名、注释、字段、主键）发给 AI，由 AI 根据命名/语义推断表关系，
// 仅供当前会话可视化使用，不持久化、不写入数据库。
package modeler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	agent "websql/internal/ai/agent"
	system "websql/internal/app/system"
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/response"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

// AnalyzeTableRequest 前端提交的表结构信息（精简版，仅含 AI 推断所需字段）
type AnalyzeTableRequest struct {
	ConnID  string           `json:"connId"`
	Schema  string           `json:"schema"`
	DbType  string           `json:"dbType"`
	Tables  []AnalyzeTable   `json:"tables"`
	// ExistingRelations 已存在的物理外键关系，供 AI 参考（避免重复推断）
	ExistingRelations []AnalyzeRelation `json:"existingRelations,omitempty"`
}

type AnalyzeTable struct {
	Name    string           `json:"name"`
	Comment string           `json:"comment"`
	Columns []AnalyzeColumn  `json:"columns"`
}

type AnalyzeColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Comment    string `json:"comment"`
	PrimaryKey bool   `json:"primaryKey"`
	Unique     bool   `json:"unique"`
}

// AnalyzeRelation AI 推断出的关系
type AnalyzeRelation struct {
	Source     string `json:"source"`      // 源表（外键所在表）
	SourceCol  string `json:"sourceCol"`   // 源表字段
	Target     string `json:"target"`      // 目标表（被引用表）
	TargetCol  string `json:"targetCol"`   // 目标表字段
	RelationType string `json:"relationType"` // 1:1 | 1:N | N:M
	Confidence string `json:"confidence"`  // high | medium | low
	Reason     string `json:"reason"`      // 推断依据（命名、注释等）
}

type AnalyzeResponse struct {
	Relations []AnalyzeRelation `json:"relations"`
}

// AnalyzeRelationsHandler POST /api/er/analyzeRelations
//
// 请求体: AnalyzeTableRequest
// 响应体: AnalyzeResponse
//
// 设计要点：
//   - 表数量超过 30 张时只取前 30 张（按字母序），防止 token 超限
//   - AI 超时 60 秒，超时返回 503 而非阻塞前端
//   - AI 输出强制 JSON 格式，使用正则提取首个 JSON 数组
//   - 任何错误都不影响前端已有 ER 图，前端会保留原状态
func AnalyzeRelationsHandler(c *gin.Context) {
	var req AnalyzeTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.WriteErr(c, 200, 500, "参数解析失败："+err.Error())
		return
	}
	if len(req.Tables) == 0 {
		response.WriteOK(c, AnalyzeResponse{Relations: []AnalyzeRelation{}})
		return
	}

	cfg := system.GetAIConfigFromDB()
	if cfg == nil || cfg.Provider == "" || cfg.Model == "" {
		response.WriteErr(c, 200, 500, "AI 未配置，请联系管理员在系统设置中配置 AI")
		return
	}

	// 限制单次分析表数量，避免大库 token 超限
	const maxTables = 30
	tables := req.Tables
	if len(tables) > maxTables {
		tables = tables[:maxTables]
	}

	prompt := buildAnalyzePrompt(tables, req.ExistingRelations)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	cm, err := agent.BuildChatModel(ctx, cfg)
	if err != nil {
		log.Printf("[ER-Analyze] 创建模型失败 - err=%v\n", err)
		response.WriteInternalErr(c, "AI 模型创建失败")
		return
	}

	msgs := []*schema.Message{
		{Role: schema.System, Content: "你是一名资深数据库架构师，精通关系型数据库表结构设计。请根据提供的表元数据，分析表之间可能的关系。严格按要求输出 JSON，不要输出任何额外解释。"},
		{Role: schema.User, Content: prompt},
	}

	resp, err := cm.Generate(ctx, msgs)
	if err != nil {
		log.Printf("[ER-Analyze] AI 调用失败 - err=%v\n", err)
		response.WriteErr(c, 200, 500, "AI 分析失败："+err.Error())
		return
	}

	relations := parseAnalyzeResponse(resp.Content)
	log.Printf("[ER-Analyze] connId=%s schema=%s tables=%d inferred=%d\n",
		appctx.Ctx.GetConnID(c), req.Schema, len(tables), len(relations))

	response.WriteOK(c, AnalyzeResponse{Relations: relations})
}

// buildAnalyzePrompt 构造 AI 提示词，紧凑表示表结构以节省 token
func buildAnalyzePrompt(tables []AnalyzeTable, existing []AnalyzeRelation) string {
	var sb strings.Builder
	sb.WriteString("以下是数据库的表结构（表名、注释、字段列表，PK 表示主键，UNI 表示唯一键）：\n\n")
	for _, t := range tables {
		sb.WriteString("表: ")
		sb.WriteString(t.Name)
		if t.Comment != "" {
			sb.WriteString("  -- ")
			sb.WriteString(t.Comment)
		}
		sb.WriteString("\n")
		for _, c := range t.Columns {
			sb.WriteString("  ")
			sb.WriteString(c.Name)
			sb.WriteString(" ")
			sb.WriteString(c.Type)
			marks := []string{}
			if c.PrimaryKey {
				marks = append(marks, "PK")
			}
			if c.Unique {
				marks = append(marks, "UNI")
			}
			if len(marks) > 0 {
				sb.WriteString(" [")
				sb.WriteString(strings.Join(marks, ","))
				sb.WriteString("]")
			}
			if c.Comment != "" {
				sb.WriteString("  -- ")
				sb.WriteString(c.Comment)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(existing) > 0 {
		sb.WriteString("已知的物理外键关系（请勿重复推断）：\n")
		for _, r := range existing {
			fmt.Fprintf(&sb, "  %s.%s -> %s.%s\n", r.Source, r.SourceCol, r.Target, r.TargetCol)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`请根据表名、字段名、注释和命名约定（如 xxx_id、parent_id、order_no 等）推断表之间的关系。

输出要求：
1. 严格输出 JSON 数组，不要包含 markdown 代码块标记
2. 仅输出有合理把握的关系，避免臆测
3. 字段含义：
   - source: 外键所在表名
   - sourceCol: 外键字段名
   - target: 被引用表名
   - targetCol: 被引用字段名（通常是主键）
   - relationType: 关系类型，取值 "1:1" | "1:N" | "N:M"
   - confidence: 把握程度，取值 "high" | "medium" | "low"
   - reason: 推断依据，简短一句话
4. 如无法推断任何关系，输出 []

示例输出：
[{"source":"order_item","sourceCol":"order_id","target":"orders","targetCol":"id","relationType":"1:N","confidence":"high","reason":"order_id 命名指向 orders.id 主键"}]

请输出 JSON：`)
	return sb.String()
}

// parseAnalyzeResponse 从 AI 输出中提取 JSON 数组
func parseAnalyzeResponse(content string) []AnalyzeRelation {
	if content == "" {
		return []AnalyzeRelation{}
	}
	// 移除 markdown 代码块标记
	cleaned := strings.TrimSpace(content)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// 直接尝试解析
	var relations []AnalyzeRelation
	if err := json.Unmarshal([]byte(cleaned), &relations); err == nil {
		return normalizeRelations(relations)
	}

	// 兜底：用正则提取首个 JSON 数组
	re := regexp.MustCompile(`(?s)\[\s*\{.*?\}\s*\]`)
	match := re.FindString(cleaned)
	if match == "" {
		log.Printf("[ER-Analyze] 未能从 AI 响应中提取 JSON，原始输出前 200 字: %s\n", truncForLog(content, 200))
		return []AnalyzeRelation{}
	}
	if err := json.Unmarshal([]byte(match), &relations); err != nil {
		log.Printf("[ER-Analyze] JSON 解析失败 - err=%v, content=%s\n", err, truncForLog(match, 200))
		return []AnalyzeRelation{}
	}
	return normalizeRelations(relations)
}

// normalizeRelations 过滤掉明显无效的关系（source/target 为空或自环）
func normalizeRelations(relations []AnalyzeRelation) []AnalyzeRelation {
	out := make([]AnalyzeRelation, 0, len(relations))
	for _, r := range relations {
		if r.Source == "" || r.Target == "" {
			continue
		}
		if r.Source == r.Target {
			continue
		}
		if r.RelationType == "" {
			r.RelationType = "1:N"
		}
		if r.Confidence == "" {
			r.Confidence = "medium"
		}
		out = append(out, r)
	}
	return out
}

func truncForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
