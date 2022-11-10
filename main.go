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
	"strings"
	"time"
)

var (
	httpClient = &http.Client{}
	port       *string
	isHttps    *string
)

func main() {

	port = flag.String("port", "443", "")
	isHttps = flag.String("https", "true", "")
	flag.Parse()

	// 注册静态文件
	fsRegister("/static/")
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/sqlite", dbTest)

	// 检测是否启动成功
	go sLsn()

	var err error
	if strings.EqualFold(*isHttps, "true") {
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

func mainHandler(w http.ResponseWriter, r *http.Request) {

	uri := r.URL.Path

	if strings.HasPrefix(uri, "/api/") {

		processSys(w, r)

		// 人脸识别接口
	} else if strings.HasPrefix(uri, "/faceid/") {
		proxy(w, r)
	}
}

// 业务服务接口
func processSys(w http.ResponseWriter, r *http.Request) {

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

	faceIdHost := "https://api.megvii.com"

	req, _ := http.NewRequest(r.Method, faceIdHost+r.RequestURI, r.Body)
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
		protocol := "https"
		if !strings.EqualFold(*isHttps, "true") {
			protocol = "http"
		}
		r, _ := client.Get(protocol + "://localhost:" + *port)
		if r != nil {
			r.Body.Close()
			log.Println("==================== 系统已启动完成，端口：" + *port + " ====================")
			runtime.Goexit()
		}
	}
}
