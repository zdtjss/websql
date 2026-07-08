//go:build desktop

package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"websql/desktop/bindings"
	"websql/internal/app"
	"websql/internal/app/monitor"
	sqlhand "websql/internal/app/sql"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/pkg/rpc"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:web-src/dist
var frontendAssets embed.FS

//go:embed sqlite3-init.sql
var sqliteInitSQL []byte

//go:embed config.json
var defaultConfigJSON []byte

// DesktopApp 是 Wails v3 Service,所有前端调用都通过它转发到对应 binding。
type DesktopApp struct {
	app           *application.App
	container     *app.Container
	bindings      *bindings.Registry
	notifications *notifications.NotificationService
	mu            sync.Mutex
	streams       map[string]context.CancelFunc
}

// NewDesktopApp 创建桌面应用 Service。
// app 引用在 ServiceStartup 中通过 application.Get() 注入。
// ns 为原生通知服务,可为 nil(无通知能力时降级)。
func NewDesktopApp(container *app.Container, ns *notifications.NotificationService) *DesktopApp {
	return &DesktopApp{
		container:     container,
		bindings:      bindings.NewRegistry(container),
		notifications: ns,
		streams:       make(map[string]context.CancelFunc),
	}
}

// ServiceStartup 是 Wails v3 Service 启动接口,在应用启动时调用。
func (a *DesktopApp) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	a.app = application.Get()
	a.bindings.SetContext(ctx)
	a.registerNotificationCallback()
	log.Println("[Desktop] Wails v3 应用启动完成")
	return nil
}

// ServiceShutdown 是 Wails v3 Service 关闭接口,在应用关闭时调用。
func (a *DesktopApp) ServiceShutdown() error {
	for sessionId, cancel := range a.streams {
		cancel()
		delete(a.streams, sessionId)
	}
	sqlhand.ShutdownHistoryWriter()
	audit.GetAuditService().Shutdown()
	log.Println("[Desktop] Wails v3 应用已关闭")
	return nil
}

// Invoke 是前端统一调用入口。前端通过 window.go.main.DesktopApp.Invoke(req) 调用。
// 根据 req.Module 和 req.Method 路由到对应 binding。
func (a *DesktopApp) Invoke(req rpc.Request) rpc.Response {
	if req.Module == "" || req.Method == "" {
		return rpc.Err(400, "module 和 method 不能为空")
	}
	return a.bindings.Dispatch(req)
}

// InvokeBlob 处理文件下载类请求。Go 端生成临时文件,返回路径供前端下载。
func (a *DesktopApp) InvokeBlob(req rpc.Request) (bindings.BlobResult, error) {
	return a.bindings.DispatchBlob(req)
}

// StartStream 启动流式响应(SSE 替代方案)。
// Go 端通过 app.Event.Emit 推送数据到前端,前端 wails.Events.On 监听事件。
func (a *DesktopApp) StartStream(req bindings.StreamRequest) error {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.streams[req.SessionID] = cancel
	a.mu.Unlock()

	emit := func(eventName string, data interface{}) {
		a.app.Event.Emit(eventName, data)
	}

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.streams, req.SessionID)
			a.mu.Unlock()
			cancel()
		}()
		a.bindings.DispatchStream(ctx, req, emit)
	}()
	return nil
}

// CancelStream 取消指定会话的流式响应。
func (a *DesktopApp) CancelStream(sessionId string) error {
	a.mu.Lock()
	cancel, ok := a.streams[sessionId]
	if ok {
		cancel()
		delete(a.streams, sessionId)
	}
	a.mu.Unlock()
	return nil
}

// OpenFileDialog 打开文件选择对话框,返回所选文件路径。
func (a *DesktopApp) OpenFileDialog(filters string) (string, error) {
	opts := &application.OpenFileDialogOptions{
		Title:          "选择文件",
		CanChooseFiles: true,
	}
	if filters != "" {
		var dialogFilters []application.FileFilter
		if err := json.Unmarshal([]byte(filters), &dialogFilters); err != nil {
			return "", fmt.Errorf("解析过滤器失败: %w", err)
		}
		opts.Filters = dialogFilters
	}
	selection, err := a.app.Dialog.OpenFileWithOptions(opts).PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return selection, nil
}

// SaveFileDialog 打开保存文件对话框,返回用户选择的保存路径。
func (a *DesktopApp) SaveFileDialog(filename string) (string, error) {
	if filename == "" {
		filename = fmt.Sprintf("websql-export-%s.xlsx", time.Now().Format("20060102-150405"))
	}
	opts := &application.SaveFileDialogOptions{
		Title:                "保存文件",
		Filename:             filename,
		CanCreateDirectories: true,
	}
	selection, err := a.app.Dialog.SaveFileWithOptions(opts).PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return selection, nil
}

// ReadFile 读取指定路径的文件内容,返回字节数组(用于 blob 下载场景)。
func (a *DesktopApp) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile 写入文件到指定路径。
func (a *DesktopApp) WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// initDataDir 初始化用户数据目录(%APPDATA%/websql)。
// 首次启动时从 embed 拷贝默认 config.json 并初始化 sqlite db。
func initDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	dataDir := filepath.Join(base, "websql")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("[Desktop] 创建数据目录失败: %v", err)
	}
	ensureFirstRun(dataDir)
	return dataDir
}

// ensureFirstRun 首次启动时初始化 config.json 和 sqlite db。
func ensureFirstRun(dataDir string) {
	configPath := filepath.Join(dataDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		var cfg config.Config
		if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {
			log.Fatalf("[Desktop] 解析默认配置失败: %v", err)
		}
		cfg.IsRemote = false
		cfg.DB.DriverName = "sqlite"
		cfg.DB.DataSourceName = filepath.Join(dataDir, "nway.sqlite3.db")
		cfg.DB.MaxOpenConns = 10
		cfg.DB.MaxIdleConns = 3
		out, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, out, 0644); err != nil {
			log.Fatalf("[Desktop] 写入默认配置失败: %v", err)
		}
		log.Printf("[Desktop] 已生成默认配置: %s", configPath)
	}

	dbPath := filepath.Join(dataDir, "nway.sqlite3.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := initSQLiteDB(dbPath, string(sqliteInitSQL)); err != nil {
			log.Fatalf("[Desktop] 初始化 sqlite 失败: %v", err)
		}
		log.Printf("[Desktop] 已初始化 sqlite: %s", dbPath)
	}
}

// initSQLiteDB 用 init SQL 初始化 sqlite 数据库(移植自 build_release.py 的逻辑)。
func initSQLiteDB(dbPath, initSQL string) error {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL", dbPath)
	db, err := openSQLiteForInit(dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := executeInitSQL(db, initSQL); err != nil {
		return err
	}
	return nil
}

// initDesktopContainer 初始化桌面版的应用容器,复用 HTTP 模式的初始化编排。
func initDesktopContainer() *app.Container {
	dataDir := initDataDir()
	config.ConfigPathOverride = dataDir

	config.Cfg = config.ReadConfig()
	if config.Cfg.DB.DataSourceName == "" || config.Cfg.DB.DataSourceName == "./nway.sqlite3.db" {
		config.Cfg.DB.DataSourceName = filepath.Join(dataDir, "nway.sqlite3.db")
	}

	database.InitMngtDbConn()
	database.LoadConfigFromDB()

	audit.GetAuditService()
	audit.StartAuditLogCleaner()

	monitor.StartMetricCleaner()
	monitor.StartMetricCollector()

	container := app.NewContainer()
	return container
}
