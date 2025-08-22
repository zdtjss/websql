package main

import (
	"context"
	"crypto/tls"
	"flag"
	"go-web/config"
	"go-web/https"
	"go-web/utils/store"
	webapi "go-web/web-api"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	port    *string
	isHttps *bool
	router  = gin.Default()
)

func main() {

	port = flag.String("port", "80", "")
	isHttps = flag.Bool("https", false, "")
	initSqlFile := flag.String("sql", "", "")
	flag.Parse()

	router.MaxMultipartMemory = 30 * 1024 * 1024
	webapi.MainRegister(router)

	config.InitMngtDbConn()

	if *initSqlFile != "" {
		config.InitDB(*initSqlFile)
	}

	if config.Cfg.IsRemote && strings.TrimSpace(config.Cfg.Redis.Addr) != "" {
		store.InitRedis()
	}

	// https 默认端口 443
	if *isHttps && *port == "80" {
		*port = "443"
	}
	server := &http.Server{Addr: ":" + *port, Handler: router}

	// 启动服务
	go startServer(server, isHttps, port)

	// 检测是否启动成功
	go listenStartStatus()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, os.Interrupt)
	s := <-c
	log.Printf("服务正在关闭 %s......", s)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Println("服务关闭异常，err：" + err.Error())
	}
	log.Println("服务已关闭")
}

func startServer(server *http.Server, isHttps *bool, port *string) {
	var err error
	if *isHttps {
		// 初始化 TLS 证书
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
		time.Sleep(time.Duration(1 * time.Millisecond))
		// 注意，使用 InsecureSkipVerify: true 来跳过证书验证，否则总是请求失败
		client := &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}}
		protocol := "http"
		if *isHttps {
			protocol = "https"
		}
		r, _ := client.Get(strings.Join([]string{protocol, "://localhost:", *port, "/healthCheck"}, ""))
		if r != nil {
			r.Body.Close()
			log.Println(strings.Join([]string{"==================== 系统已启动完成，端口：", *port, " 、 https：", strconv.FormatBool(*isHttps), " ===================="}, ""))
			runtime.Goexit()
		}
	}
}
