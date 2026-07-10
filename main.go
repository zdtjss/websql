package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	app "websql/internal/app"
	"websql/internal/app/monitor"
	sqlhand "websql/internal/app/sql"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/https"
	logutils "websql/internal/logger"
	"websql/internal/migration"
	"websql/internal/pkg/safego"
	"websql/internal/store"
	"websql/internal/version"

	"github.com/gin-gonic/gin"
)

var (
	port    *string
	isHttps *bool
	router  = gin.Default()
)

func main() {
	// 兜底：捕获 main goroutine 中的未预期 panic，防止进程崩溃
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[MainRecovery] main goroutine panic 已恢复 - panic=%v\n%s", r, debug.Stack())
		}
	}()

	gin.SetMode(gin.ReleaseMode)

	// 日志按天轮转，最早初始化，确保后续所有日志都落盘到轮转文件
	logutils.Init("./logs", "websql", 7)

	port = flag.String("port", "80", "")
	isHttps = flag.Bool("https", false, "")
	flag.Parse()

	router.MaxMultipartMemory = 30 * 1024 * 1024

	database.InitMngtDbConn()

	app.MainRegister(router)

	// 检测程序升级：对比数据库中记录的旧版本与当前二进制版本
	prevVer, _ := migration.GetPreviousAppVersion(database.Mngtdb)
	if prevVer != "" && prevVer != version.Version {
		log.Printf("[Main] 检测到程序升级: %s → %s", prevVer, version.Version)
	} else if prevVer == "" {
		log.Printf("[Main] 首次运行 (version=%s)", version.Version)
	}

	// SQLite 管理库自动迁移；MySQL/MariaDB 管理库跳过，由系统管理员手动升级
	// 全新库时优先使用全量脚本快速建库
	fullSQL, _ := os.ReadFile("./migrations/full/sqlite_full.sql")
	if err := migration.RunMigrations(database.Mngtdb, config.Get().DB.DriverName, os.DirFS("./migrations/sqlite"), string(fullSQL)); err != nil {
		log.Fatalf("[Main] 管理库迁移失败: %v", err)
	}

	// 校验 DB schema 版本是否满足要求
	if v, _ := migration.GetLatestAppliedVersion(database.Mngtdb); v != "" && v < version.RequiredMigrationVersion {
		driverName := config.Get().DB.DriverName
		if driverName == "mysql" || driverName == "mariadb" {
			log.Fatalf("[Main] DB schema 版本 %s 低于要求 %s，请手动执行增量迁移脚本", v, version.RequiredMigrationVersion)
		}
		log.Printf("[Main] 警告: DB schema 版本 %s 低于要求 %s", v, version.RequiredMigrationVersion)
	}

	// 持久化当前程序版本号，供下次启动对比
	if err := migration.RecordAppVersion(database.Mngtdb, version.Version); err != nil {
		log.Printf("[Main] 记录程序版本失败: %v", err)
	}

	// 从数据库加载系统配置（覆盖配置文件中的配置）
	database.LoadConfigFromDB()

	audit.GetAuditService()
	audit.StartAuditLogCleaner()

	// 启动监控指标自动清理任务（删除 30 天前的历史数据）
	monitor.StartMetricCleaner()
	// 启动后台指标采集任务（每 60 秒采集一次，供历史趋势查询）
	monitor.StartMetricCollector()

	if config.Get().IsRemote && strings.TrimSpace(config.Get().Redis.Addr) != "" {
		store.InitRedis(config.Get())
	}

	// 构建应用依赖容器（聚合已有全局变量，供后续 Handler/Service 使用）
	container := app.NewContainer()
	defer container.Close()

	// 本地模式自动登录 local 用户
	app.EnsureLocalUser()

	// https 默认端口 443
	if *isHttps && *port == "80" {
		*port = "443"
	}
	server := &http.Server{
		Addr:           ":" + *port,
		Handler:        router,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   0,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// 启动服务
	go startServer(server, isHttps, port)

	// 检测是否启动成功（使用 safego 防止 goroutine panic 导致进程退出）
	safego.GoWithName("startup-check", listenStartStatus)

	// 启动导出文件定时清理
	app.StartCleanupScheduler()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, os.Interrupt)
	s := <-c
	log.Printf("服务正在关闭 %s......", s)
	sqlhand.ShutdownHistoryWriter()
	audit.GetAuditService().Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Println("服务关闭异常，err：" + err.Error())
	}
	log.Println("服务已关闭")
}

func startServer(server *http.Server, isHttps *bool, port *string) {
	defer safego.Recover("startServer")
	var err error
	if *isHttps {
		// 初始化TLS 证书
		https.InitCertificateFile()
		err = server.ListenAndServeTLS(https.PemName, https.KeyName)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && !strings.Contains(err.Error(), "Server closed") {
		log.Println("********************* 启动失败，端口：" + *port + " *********************")
		log.Println(err)
		os.Exit(1)
	}
}

// 启动状态监听
func listenStartStatus() {
	for {
		time.Sleep(time.Millisecond)
		// 注意，使用InsecureSkipVerify: true 来跳过证书验证，否则总是请求失败
		client := &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}}
		protocol := "http"
		if *isHttps {
			protocol = "https"
		}
		r, _ := client.Get(protocol + "://localhost:" + *port + "/api/healthCheck")
		if r != nil {
			r.Body.Close()
			log.Println("==================== 系统已启动完成，端口：" + *port + " ，https：" + strconv.FormatBool(*isHttps) + " ====================")
			runtime.Goexit()
		}
	}
}
