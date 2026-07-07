//go:build desktop

package bindings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	agent "websql/internal/ai/agent"
	"websql/internal/pkg/rpc"
	"websql/internal/pkg/safego"
)

// registerAgent 注册 agent 模块的 binding。
//
// 对应 HTTP 路由 (internal/app/router.go) 中 agent 模块的方法:
//   - POST /ai/agent/chatStream       → agentHandler.ChatStream (StreamHandler)
//   - POST /ai/agent/uploadExcel      → agent.HandleUploadExcel
//   - POST /ai/agent/preMatchColumns  → agent.HandlePreMatchColumns
//   - GET  /ai/agent/sessions         → agentHandler.HandleGetSessions
//   - GET  /ai/agent/session           → agentHandler.HandleGetSession
//   - POST /ai/agent/session/delete    → agentHandler.HandleDeleteSession
//
// 调用 service: internal/ai/agent/binding_delegates.go
func registerAgent(r *Registry) {
	h, err := agent.NewHandler()
	if err != nil {
		// Agent 初始化失败时注册占位 handler，避免 binding 加载失败导致整个 registry 不可用。
		// 前端调用时会收到 "AI 功能不可用" 错误。
		registerAgentPlaceholder(r, fmt.Sprintf("AI Agent 不可用: %v", err))
		return
	}

	// ChatStream: SSE 流式
	r.registerStream("agent", "ChatStream", func(ctx context.Context, req StreamRequest, emit EmitFunc) {
		chatStreamHandler(ctx, req, emit, h)
	})

	// UploadExcel: 文件上传
	// 桌面 binding 接收 fileID（前端已通过 Wails runtime 选择文件路径），
	// 然后打开文件路径并传给 service。
	r.register("agent", "UploadExcel", func(req rpc.Request) rpc.Response {
		filePath := req.StringBody("filePath")
		if filePath == "" {
			filePath = req.StringParam("filePath")
		}
		filename := req.StringBody("fileName")
		if filename == "" {
			filename = req.StringParam("fileName")
		}
		if filePath == "" {
			return rpc.Err(400, "缺少文件路径参数 filePath")
		}

		f, err := os.Open(filePath)
		if err != nil {
			return rpc.Err(500, "打开文件失败: "+err.Error())
		}
		defer f.Close()

		result, err := agent.UploadExcelByService(f, filename)
		if err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse(result)
	})

	r.register("agent", "PreMatchColumns", func(req rpc.Request) rpc.Response {
		var preq agent.PreMatchColumnsRequest
		if err := decodeBody(req.Body, &preq); err != nil {
			return rpc.Err(400, "参数格式错误")
		}
		result, err := agent.PreMatchColumnsByService(&preq, req.Authorization)
		if err != nil {
			return rpc.Err(400, err.Error())
		}
		return okResponse(result)
	})

	r.register("agent", "GetSessions", func(req rpc.Request) rpc.Response {
		keyword := req.StringParam("keyword")
		page, pageSize := agent.ParseSessionsPageParams(
			req.StringParam("page"),
			req.StringParam("pageSize"),
		)
		result, err := h.GetSessionsByService(req.Authorization, keyword, page, pageSize)
		if err != nil {
			return rpc.Err(500, err.Error())
		}
		return okResponse(result)
	})

	r.register("agent", "GetSession", func(req rpc.Request) rpc.Response {
		sessionID := req.StringParam("sessionId")
		if sessionID == "" {
			sessionID = req.StringBody("sessionId")
		}
		detail, err := h.GetSessionByService(sessionID, req.Authorization)
		if err != nil {
			return rpc.Err(500, err.Error())
		}
		return okResponse(map[string]any{"session": detail})
	})

	r.register("agent", "DeleteSession", func(req rpc.Request) rpc.Response {
		sessionID := req.StringBody("sessionId")
		if sessionID == "" {
			sessionID = req.StringParam("sessionId")
		}
		if err := h.DeleteSessionByService(sessionID, req.Authorization); err != nil {
			return rpc.Err(500, err.Error())
		}
		return okResponse(map[string]string{"message": "会话已删除"})
	})
}

// chatStreamHandler 处理 ChatStream 流式调用。
// 提取为独立函数以保持 register 函数简洁。
func chatStreamHandler(ctx context.Context, req StreamRequest, emit EmitFunc, h *agent.Handler) {
	// 30 分钟超时，与 HTTP 模式一致
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// 解析 ChatRequest：Body 是前端传来的 JSON
	var chatReq agent.ChatRequest
	if req.Body != nil {
		if b, err := json.Marshal(req.Body); err == nil {
			json.Unmarshal(b, &chatReq)
		}
	}

	// 如果 SessionID 为空，从 Params 补充
	if chatReq.SessionID == "" {
		if sid, ok := req.Params["sessionId"].(string); ok {
			chatReq.SessionID = sid
		}
	}

	dataName := "sse:" + req.SessionID + ":data"
	doneName := "sse:" + req.SessionID + ":done"
	errName := "sse:" + req.SessionID + ":error"

	var mu sync.Mutex
	chunkEmit := func(chunk agent.StreamChunk) {
		mu.Lock()
		defer mu.Unlock()
		data, _ := json.Marshal(chunk)
		emit(dataName, string(data))
	}

	// 监听 ctx 取消，确保 emit 正常结束
	safego.GoWithName("agent-stream-ctx-watch", func() {
		<-ctx.Done()
	})

	h.ChatStreamByService(ctx, &chatReq, req.Authorization, chunkEmit)
	emit(doneName, nil)
	_ = errName
}

// registerAgentPlaceholder 注册占位 handler，在 Agent 初始化失败时使用。
func registerAgentPlaceholder(r *Registry, msg string) {
	placeholder := func(req rpc.Request) rpc.Response {
		return rpc.Err(500, msg)
	}
	r.register("agent", "UploadExcel", placeholder)
	r.register("agent", "PreMatchColumns", placeholder)
	r.register("agent", "GetSessions", placeholder)
	r.register("agent", "GetSession", placeholder)
	r.register("agent", "DeleteSession", placeholder)
	r.registerStream("agent", "ChatStream", func(ctx context.Context, req StreamRequest, emit EmitFunc) {
		emit("sse:"+req.SessionID+":error", msg)
		emit("sse:"+req.SessionID+":done", nil)
	})
}
