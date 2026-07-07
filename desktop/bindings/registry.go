//go:build desktop

package bindings

import (
	"context"
	"fmt"
	"sync"

	"websql/internal/app"
	"websql/internal/pkg/rpc"
)

// EmitFunc 是流式响应回调，对应 Wails runtime.EventsEmit。
// 事件名格式: sse:<sessionId>:data / sse:<sessionId>:done / sse:<sessionId>:error
type EmitFunc func(eventName string, data interface{})

// BlobResult 是可下载文件描述，由 BlobHandler 返回。
// Path 是 Go 端临时文件路径，前端通过 SaveFileDialog 选保存位置后调用 ReadFile 读取。
type BlobResult struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
}

// StreamRequest 是流式响应入参，由前端 StartStream 调用传入。
type StreamRequest struct {
	SessionID     string                 `json:"sessionId"`
	Module        string                 `json:"module"`
	Method        string                 `json:"method"`
	Authorization string                 `json:"authorization"`
	ConnID        string                 `json:"connId,omitempty"`
	Params        map[string]interface{} `json:"params,omitempty"`
	Body          interface{}            `json:"body,omitempty"`
}

// Handler 是普通 RPC 调用的统一签名。
type Handler func(req rpc.Request) rpc.Response

// BlobHandler 是文件下载类调用的统一签名。
type BlobHandler func(req rpc.Request) (BlobResult, error)

// StreamHandler 是流式调用的统一签名，emit 由调用方注入。
type StreamHandler func(ctx context.Context, req StreamRequest, emit EmitFunc)

// Registry 是模块路由表，负责把 RPC 请求分发到对应业务函数。
// 所有 binding 在 NewRegistry 时通过 registerAll 集中注册。
type Registry struct {
	container *app.Container
	ctx       context.Context
	mu        sync.RWMutex

	handlers       map[string]map[string]Handler       // module -> method -> handler
	blobHandlers   map[string]map[string]BlobHandler   // module -> method -> blob handler
	streamHandlers map[string]map[string]StreamHandler  // module -> method -> stream handler
}

// NewRegistry 创建路由表并注册所有模块。
func NewRegistry(container *app.Container) *Registry {
	r := &Registry{
		container:      container,
		handlers:       make(map[string]map[string]Handler),
		blobHandlers:   make(map[string]map[string]BlobHandler),
		streamHandlers: make(map[string]map[string]StreamHandler),
	}
	registerAll(r)
	return r
}

// SetContext 在 Wails OnStartup 时注入 ctx。
func (r *Registry) SetContext(ctx context.Context) {
	r.ctx = ctx
}

func (r *Registry) register(module, method string, h Handler) {
	if r.handlers[module] == nil {
		r.handlers[module] = make(map[string]Handler)
	}
	r.handlers[module][method] = h
}

func (r *Registry) registerBlob(module, method string, h BlobHandler) {
	if r.blobHandlers[module] == nil {
		r.blobHandlers[module] = make(map[string]BlobHandler)
	}
	r.blobHandlers[module][method] = h
}

func (r *Registry) registerStream(module, method string, h StreamHandler) {
	if r.streamHandlers[module] == nil {
		r.streamHandlers[module] = make(map[string]StreamHandler)
	}
	r.streamHandlers[module][method] = h
}

// Dispatch 把普通 RPC 请求路由到对应 handler。
func (r *Registry) Dispatch(req rpc.Request) rpc.Response {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.handlers[req.Module]; ok {
		if h, ok := m[req.Method]; ok {
			return h(req)
		}
	}
	return rpc.Err(404, fmt.Sprintf("未知方法: %s.%s", req.Module, req.Method))
}

// DispatchBlob 把文件下载请求路由到对应 BlobHandler。
func (r *Registry) DispatchBlob(req rpc.Request) (BlobResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.blobHandlers[req.Module]; ok {
		if h, ok := m[req.Method]; ok {
			return h(req)
		}
	}
	return BlobResult{}, fmt.Errorf("未知 blob 方法: %s.%s", req.Module, req.Method)
}

// DispatchStream 把流式请求路由到对应 StreamHandler。
// 找不到时通过 emit 推送 error 事件，与前端 adapter-sse-wails.ts 协议对齐。
func (r *Registry) DispatchStream(ctx context.Context, req StreamRequest, emit EmitFunc) {
	r.mu.RLock()
	m, ok := r.streamHandlers[req.Module]
	if !ok {
		r.mu.RUnlock()
		emit("sse:"+req.SessionID+":error", "未知流模块: "+req.Module)
		return
	}
	h, ok := m[req.Method]
	r.mu.RUnlock()
	if !ok {
		emit("sse:"+req.SessionID+":error", "未知流方法: "+req.Module+"."+req.Method)
		return
	}
	h(ctx, req, emit)
}

// registerAll 集中注册所有模块的入口。
// 新增模块时在此追加一行 registerXxx(r)，CI 脚本会校验完整性（见 scripts/check_bindings.go）。
func registerAll(r *Registry) {
	registerConn(r)
	registerAdmin(r)
	registerSql(r)
	registerTree(r)
	registerDbops(r)
	registerSystem(r)
	registerPermission(r)
	registerSnippet(r)
	registerBackup(r)
	registerMonitor(r)
	registerSearch(r)
	registerDatadict(r)
	registerModeler(r)
	registerSyncdb(r)
	registerAudit(r)
	registerSqlopt(r)
	registerAi(r)
	registerAgent(r)
	registerFileio(r)
}
