package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	app "websql/internal/app"
	"websql/internal/app/monitor"
	sqlhand "websql/internal/app/sql"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/pkg/safego"
	"websql/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/wailsapp/wails/v3/pkg/application"
)

var version = "dev"

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[MainRecovery] main goroutine panic recovered - panic=%v\n%s", r, debug.Stack())
		}
	}()

	gin.SetMode(gin.ReleaseMode)

	// 桌面模式加载配置：优先读取 config.json，找不到时回退到内置默认本地配置，
	// 避免 dev 模式（二进制与 config.json 不在同一目录）启动即 panic。
	if cfg, err := config.TryReadConfig(); err == nil {
		config.Cfg = cfg
	} else {
		log.Printf("[Desktop] 未找到 config.json，使用默认本地配置: %v", err)
		config.Cfg = &config.Config{}
		config.Cfg.DB.DriverName = "sqlite"
		config.Cfg.DB.DataSourceName = "./nway.sqlite3.db"
		config.Cfg.DB.MaxOpenConns = 10
		config.Cfg.DB.MaxIdleConns = 3
	}
	// 桌面模式强制本地：免登录、不走远程权限体系
	config.Cfg.IsRemote = false
	config.Cfg.IsDesktop = true

	// 加载图标（找不到时忽略，不阻塞启动）
	var iconData []byte
	if iconPath, err := config.TryFindFile("favicon.ico"); err == nil {
		iconData, _ = os.ReadFile(iconPath)
	}

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

	database.InitMngtDbConn()
	database.LoadConfigFromDB()

	// 二次断言桌面标志：LoadConfigFromDB 不会改 IsRemote/IsDesktop，但此处防御性重置，
	// 确保后续中间件/路由读到的 config.Cfg 一定是桌面本地模式。
	config.Cfg.IsRemote = false
	config.Cfg.IsDesktop = true

	audit.GetAuditService()
	audit.StartAuditLogCleaner()
	monitor.StartMetricCleaner()
	monitor.StartMetricCollector()

	if strings.TrimSpace(config.Cfg.Redis.Addr) != "" {
		store.InitRedis()
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
		ReadTimeout:    30 * time.Second,
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

// 注册关闭信号（备用，Wails 通常处理退出）
func init() {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		os.Exit(0)
	}()
}
