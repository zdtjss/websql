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
	"websql/internal/pkg/safego"
	"websql/internal/store"

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

	port = flag.String("port", "80", "")
	isHttps = flag.Bool("https", false, "")
	initSqlFile := flag.String("sql", "", "")
	flag.Parse()

	router.MaxMultipartMemory = 30 * 1024 * 1024
	app.MainRegister(router)

	database.InitMngtDbConn()

	if *initSqlFile != "" {
		database.InitDB(*initSqlFile)
	}

	// 从数据库加载系统配置（覆盖配置文件中的配置）
	database.LoadConfigFromDB()

	audit.GetAuditService()
	audit.StartAuditLogCleaner()

	// 启动监控指标自动清理任务（删除 30 天前的历史数据）
	monitor.StartMetricCleaner()
	// 启动后台指标采集任务（每 60 秒采集一次，供历史趋势查询）
	monitor.StartMetricCollector()

	if config.Cfg.IsRemote && strings.TrimSpace(config.Cfg.Redis.Addr) != "" {
		store.InitRedis()
	}

	// 构建应用依赖容器（聚合已有全局变量，供后续 Handler/Service 使用）
	container := app.NewContainer()
	defer container.Close()

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
