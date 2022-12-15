package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"go-web/https"
	"go-web/sqlite"
	"go-web/utils"
	webapi "go-web/web-api"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/julienschmidt/httprouter"
	"golang.org/x/exp/maps"
)

var (
	httpClient = &http.Client{}
	port       *string
	isHttps    *bool
	// 不需要以/结尾
	destAddr string = "http://localhost:8083"
	router          = httprouter.New()
)

func main() {

	port = flag.String("port", "80", "")
	isHttps = flag.Bool("https", false, "")
	flag.Parse()

	// 注册静态文件
	router.Handler("GET", "/", staticHandler("/"))
	router.HandlerFunc("GET", "/api/", mainHandler)
	router.HandlerFunc("GET", "/ext/", proxy)
	router.HandlerFunc("GET", "/api/sqlite", dbTest)

	webapi.MainRegister(router)

	// router.NotFound = &NotFound{}

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

// /api/
func mainHandler(w http.ResponseWriter, r *http.Request) {

	if strings.EqualFold(r.URL.Path, "/api/receiveNotify") {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(string(data))
	}
}

// 对外代理的接口注册
func proxy(w http.ResponseWriter, r *http.Request) {

	req, _ := http.NewRequest(r.Method, destAddr+r.RequestURI[4:], r.Body)
	defer r.Body.Close()
	*&req.Header = r.Header
	resp, err := httpClient.Do(req)
	utils.Panicln(err)

	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	_, err2 := io.Copy(w, resp.Body)
	utils.Panicln(err2)
	defer resp.Body.Close()
}

func dbTest(w http.ResponseWriter, r *http.Request) {
	userList := sqlite.ReadFromDB()
	jsonData, _ := json.Marshal(userList)
	w.Header().Add("content-type", "application/json;charset=UTF-8")
	w.Write(jsonData)
}

func staticHandler(baseUrl string) http.Handler {
	fs := http.FileServer(http.Dir("static/"))
	return http.StripPrefix(baseUrl, fs)
}

type NotFound struct {
}

func (n *NotFound) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	exec, err := os.Executable()
	if err != nil {
		println(err)
	}
	configFile := filepath.Join(filepath.Dir(exec), "../static/index.html")
	fileData, err := os.Open(configFile)
	if err != nil {
		configFile = filepath.Join(filepath.Dir(exec), "static/index.html")
		fileData, err = os.Open(configFile)
	}
	w.Header().Add("content-type", "text/html; charset=utf-8")
	io.Copy(w, fileData)
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
