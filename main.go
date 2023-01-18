package main

import (
	"context"
	"crypto/tls"
	"flag"
	"go-web/https"
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

	"github.com/julienschmidt/httprouter"
)

var (
	port    *string
	isHttps *bool
	router  = httprouter.New()
)

func main() {

	port = flag.String("port", "80", "")
	isHttps = flag.Bool("https", false, "")
	flag.Parse()

	// config.Cfg = config.ReadConfig()

	webapi.MainRegister(router)

	webapi.InitTable()

	router.NotFound = &webapi.NotFound{}

	// 检测是否启动成功
	go sLsn()

	// https 默认端口 443
	if *isHttps && *port == "80" {
		*port = "443"
	}
	server := http.Server{Addr: ":" + *port, Handler: router}

	// 启动服务
	go func() {
		var err error
		if *isHttps {
			// 初始化 TLS 证书
			https.InitCertificateFile()
			err = server.ListenAndServeTLS(https.PemName, https.KeyName)
		} else {
			err = server.ListenAndServe()
		}

		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			log.Println("********************* 系统启动失败，端口：" + *port + " *********************")
			log.Println(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	s := <-c
	log.Printf("服务正在关闭 %s......", s)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Println("服务关闭异常，err：" + err.Error())
	}
	log.Println("服务已关闭")
}

// 启动状态监听
func sLsn() {
	for {
		// 2两秒后测试是否启动成功
		time.Sleep(time.Duration(2 * time.Millisecond))
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
		r, _ := client.Get(protocol + "://localhost:" + *port)
		if r != nil {
			r.Body.Close()
			log.Println("==================== 系统已启动完成，端口：" + *port + " 、 https：" + strconv.FormatBool(*isHttps) + " ====================")
			runtime.Goexit()
		}
	}
}
