package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"go-web/https"
	"go-web/sqlite"
	"io"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	httpClient = &http.Client{}
	port       *string
	isHttps    *bool
	// 不需要以/结尾
	destAddr string = ""
)

func main() {

	port = flag.String("port", "80", "")
	isHttps = flag.Bool("https", false, "")
	flag.Parse()

	// 注册静态文件
	fsRegister("/")
	http.HandleFunc("/api/", mainHandler)
	http.HandleFunc("/ext/", proxy)
	http.HandleFunc("/api/sqlite", dbTest)

	// 检测是否启动成功
	go sLsn()

	var err error
	if *isHttps {
		// https 默认端口 443
		if *port == "80" {
			*port = "443"
		}
		// 初始化 TLS 证书
		https.InitCertificateFile()
		err = http.ListenAndServeTLS(":"+*port, https.PemName, https.KeyName, nil)
	} else {
		err = http.ListenAndServe(":"+*port, nil)
	}

	if err != nil {
		log.Println("********************* 系统启动失败，端口：" + *port + " *********************")
		log.Println(err)
	}

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

	req, _ := http.NewRequest(r.Method, destAddr+r.RequestURI, r.Body)
	defer r.Body.Close()
	for k, vv := range r.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Println(err)
	}
	w.WriteHeader(resp.StatusCode)
	_, err2 := io.Copy(w, resp.Body)
	if err2 != nil {
		fmt.Println(err2)
	}
	defer resp.Body.Close()
}

func dbTest(w http.ResponseWriter, r *http.Request) {
	userList := sqlite.ReadFromDB()
	jsonData, _ := json.Marshal(userList)
	w.Header().Add("content-type", "application/json;charset=UTF-8")
	w.Write(jsonData)
}

func fsRegister(baseUrl string) {
	fs := http.FileServer(http.Dir("static/"))
	http.Handle(baseUrl, http.StripPrefix(baseUrl, fs))
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
