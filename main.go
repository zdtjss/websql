package main

import (
	"encoding/json"
	"fmt"
	"go-web/https"
	"go-web/sqlite"
	"io"
	"net/http"
	"strings"
)

var httpClient = &http.Client{}

func main() {

	// 注册静态文件
	fsRegister("/static/")
	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/sqlite", dbTest)

	// 初始化 TLS 证书
	https.InitCertificateFile()
	err := http.ListenAndServeTLS(":443", https.PemName, https.KeyName, nil)

	if err != nil {
		fmt.Println(err)
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
