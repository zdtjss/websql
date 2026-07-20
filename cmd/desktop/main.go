package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	app "websql/internal/app"
	"websql/internal/app/monitor"
	sqlhand "websql/internal/app/sql"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	logutils "websql/internal/logger"
	"websql/internal/migration"
	"websql/internal/pkg/safego"
	"websql/internal/store"
	"websql/internal/version"

	"github.com/gin-gonic/gin"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[MainRecovery] main goroutine panic recovered - panic=%v\n%s", r, debug.Stack())
		}
	}()

	// 在最早阶段创建 Job Object（KILL_ON_JOB_CLOSE），确保主进程退出时
	// 所有子进程（msedgewebview2.exe 等）被操作系统强制终止，避免残留。
	// 必须先于 WebView2 创建子进程执行。
	setupJobObject()

	gin.SetMode(gin.ReleaseMode)

	// 桌面版日志目录：优先使用 exe 同目录的 logs/，便于用户查看；
	// exe 同目录无写权限时回退到 %APPDATA%/WebSQL/logs/。
	var logDir string
	if exePath, err := os.Executable(); err == nil {
		logDir = filepath.Join(filepath.Dir(exePath), "logs")
	} else {
		logDir = "./logs"
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logDir = filepath.Join(os.Getenv("APPDATA"), "WebSQL", "logs")
	}
	logutils.Init(logDir, "websql", 7)

	// 桌面模式：从内嵌的 config.desktop.json 加载配置（isRemote=false, https.enable=false）
	cfg, err := config.ParseFromBytes(configData)
	if err != nil {
		log.Fatalf("[Desktop] 解析内嵌配置失败: %v", err)
	}
	config.Cfg = cfg
	config.SetActive(cfg)
	// 桌面版 DSN 中的相对路径基于 exe 所在目录解析
	if exePath, err := os.Executable(); err == nil {
		config.SetConfigDir(filepath.Dir(exePath))
	}
	// 桌面模式强制本地：免登录、不走远程权限体系
	cfg.IsRemote = false
	cfg.IsDesktop = true

	// 从内嵌前端资源加载窗口图标（找不到时忽略，不阻塞启动）
	iconData, _ := staticFS.ReadFile("static/favicon.ico")

	// 配置嵌入式前端资源：/assets/* 和 index.html 从 embed.FS 服务，无需磁盘文件
	setupEmbeddedAssets()

	// 单实例检测：必须先于后端初始化，避免第二个实例重复初始化数据库与端口
	// 命中已运行实例时，本进程在此直接退出，并通知已运行实例显示窗口
	var mainWindow *application.WebviewWindow
	wailsApp := application.New(application.Options{
		Name: "WebSQL",
		Icon: iconData,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.websql.desktop",
			ExitCode: 0,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if mainWindow == nil {
					return
				}
				mainWindow.Show()
				mainWindow.Focus()
			},
		},
	})

	// 初始化后端服务
	router := gin.Default()
	router.MaxMultipartMemory = 30 * 1024 * 1024
	app.MainRegister(router)

	// 桌面专属：任务栏闪烁端点。前端在 AI 回复完成后调用 window.Flash() 经此触发，
	// 由 Wails 内置 WebviewWindow.Flash 使任务栏图标闪烁（窗口未激活时才闪，获焦后自动停止）。
	// mainWindow 在下方 NewWithOptions 后赋值，此闭包按引用捕获，调用时读取最新值。
	router.Group("api").POST("/desktop/flash", func(c *gin.Context) {
		if mainWindow != nil {
			mainWindow.Flash(true)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 桌面专属：页面缩放端点。前端通过 keydown 监听 Ctrl/Cmd+加号/减号/0 调用此端点，
	// 由 Wails 内置 WebviewWindow.ZoomIn/ZoomOut/ZoomReset 实现真正的 WebView 级缩放。
	// 不使用 Wails KeyBindings 是因为其在 Windows 上对 OEM_PLUS/OEM_MINUS 键名不匹配
	// （注册 "plus"→"+"，但触发时 VirtualKeyCodes 返回 "oem_plus"），且各平台键名不一致。
	router.Group("api").POST("/desktop/zoom", func(c *gin.Context) {
		if mainWindow == nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": "window not ready"})
			return
		}
		action := c.Query("action")
		switch action {
		case "in":
			mainWindow.ZoomIn()
		case "out":
			mainWindow.ZoomOut()
		case "reset":
			mainWindow.ZoomReset()
		default:
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid action"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	database.InitMngtDbConn()

	// 检测程序升级：对比数据库中记录的旧版本与当前二进制版本
	prevVer, _ := migration.GetPreviousAppVersion(database.Mngtdb)
	if prevVer != "" && prevVer != version.Version {
		log.Printf("[Desktop] 检测到程序升级: %s → %s", prevVer, version.Version)
	} else if prevVer == "" {
		log.Printf("[Desktop] 首次运行 (version=%s)", version.Version)
	}

	// 执行管理库迁移：全新库使用全量脚本快速建库，存量库自动标记基线并仅运行增量迁移
	migrationSub, err := fs.Sub(migrationFS, "migrations/sqlite")
	if err != nil {
		log.Fatalf("[Desktop] 提取嵌入迁移脚本失败: %v", err)
	}
	if err := migration.RunMigrations(database.Mngtdb, config.Get().DB.DriverName, migrationSub, string(fullInitSQL)); err != nil {
		log.Fatalf("[Desktop] 管理库迁移失败: %v", err)
	}

	// 校验 DB schema 版本是否满足要求
	if v, _ := migration.GetLatestAppliedVersion(database.Mngtdb); v != "" && v < version.RequiredMigrationVersion {
		log.Printf("[Desktop] 警告: DB schema 版本 %s 低于要求 %s", v, version.RequiredMigrationVersion)
	}

	// 持久化当前程序版本号，供下次启动对比
	if err := migration.RecordAppVersion(database.Mngtdb, version.Version); err != nil {
		log.Printf("[Desktop] 记录程序版本失败: %v", err)
	}

	database.LoadConfigFromDB()

	// 二次断言桌面标志：LoadConfigFromDB 不会改 IsRemote/IsDesktop，但此处防御性重置，
	// 确保后续中间件/路由读到的配置一定是桌面本地模式。
	cfg.IsRemote = false
	cfg.IsDesktop = true

	audit.GetAuditService()
	audit.StartAuditLogCleaner()
	monitor.StartMetricCleaner()
	monitor.StartMetricCollector()

	if strings.TrimSpace(config.Get().Redis.Addr) != "" {
		store.InitRedis(config.Get())
	}

	container := app.NewContainer()
	defer container.Close()

	// 确保 local 用户存在并自动登录
	app.EnsureLocalUser()

	// 启动内部 HTTP 服务（动态端口，避免冲突）
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("无法监听端口: %v", err)
	}
	internalPort := listener.Addr().(*net.TCPAddr).Port
	internalURL := fmt.Sprintf("http://127.0.0.1:%d", internalPort)
	log.Printf("[Desktop] 内部 HTTP 服务端口: %d", internalPort)

	server := &http.Server{
		Handler:        router,
		ReadTimeout:    5 * time.Minute,
		WriteTimeout:   0,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		defer safego.Recover("desktop-http-server")
		if err := server.Serve(listener); err != nil && !strings.Contains(err.Error(), "Server closed") {
			log.Printf("内部 HTTP 服务错误: %v", err)
		}
	}()

	// 等待 HTTP 服务就绪
	waitForServer(internalURL)

	app.StartCleanupScheduler()

	// 创建主窗口 — 直接加载内部 HTTP 服务地址
	mainWindow = wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "WebSQL",
		Name:             "main",
		Width:            1400,
		Height:           900,
		MinWidth:         1000,
		MinHeight:        600,
		URL:              internalURL,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(255, 255, 255),
		BackgroundType:   application.BackgroundTypeTranslucent,
		Windows: application.WindowsWindow{
			BackdropType: application.Mica,
			Theme:        application.SystemDefault,
		},
	})

	// 启动 Wails 应用（阻塞）
	if err := wailsApp.Run(); err != nil {
		log.Printf("Wails 应用退出: %v", err)
	}

	// 应用退出后清理
	sqlhand.ShutdownHistoryWriter()
	audit.GetAuditService().Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

func waitForServer(url string) {
	client := &http.Client{Timeout: 2 * time.Second}
	for range 50 {
		resp, err := client.Get(url + "/api/healthCheck")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Println("警告: 内部 HTTP 服务启动超时")
}

// setupEmbeddedAssets 从内嵌的 staticFS 中提取 assets 子目录和 index.html，
// 通过 app.SetEmbeddedAssets 注入路由，使前端资源完全从可执行文件内服务。
func setupEmbeddedAssets() {
	assetsSub, err := fs.Sub(staticFS, "static/assets")
	if err != nil {
		log.Printf("[Desktop] 内嵌 assets 目录不可用: %v，回退到磁盘模式", err)
		return
	}
	indexHTML, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		log.Printf("[Desktop] 内嵌 index.html 不可用: %v，回退到磁盘模式", err)
		return
	}
	app.SetEmbeddedAssets(http.FS(assetsSub), indexHTML)
}
