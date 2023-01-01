package webapi

import (
	"go-web/utils"
	"io"
	"log"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"golang.org/x/exp/maps"
)

// 不需要以/结尾
var destAddr string = "http://localhost:8083"

func MainRegister(router *httprouter.Router) {

	router.HandlerFunc("GET", "/listTable", ListTable)
	router.HandlerFunc("GET", "/exportCsv", ExportCsv)
	router.HandlerFunc("POST", "/importCsv", ImportCsv)

	router.HandlerFunc("POST", "/saveConn", SaveConn)
	router.HandlerFunc("GET", "/delConn", DelConn)
	router.HandlerFunc("GET", "/listConn2", ListConn2)
	router.HandlerFunc("GET", "/showTree", ShowTree)
	router.HandlerFunc("GET", "/execSQL", ExecSQL)

	router.HandlerFunc("GET", "/ext/", proxy)
	router.HandlerFunc("POST", "/ext/", proxy)

	router.PanicHandler = func(w http.ResponseWriter, r *http.Request, err interface{}) {
		w.Header().Add("content-type", "application/json;charset=UTF-8")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(utils.ToJsonString(utils.Result{Code: 500, Msg: err}))
	}

	log.Println("路由注册完成")
}

// 对外代理的接口注册
func proxy(w http.ResponseWriter, r *http.Request) {

	req, _ := http.NewRequest(r.Method, destAddr+r.RequestURI[4:], r.Body)
	defer r.Body.Close()
	*&req.Header = r.Header
	resp, err := http.DefaultClient.Do(req)
	utils.Panicln(err)

	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	_, err2 := io.Copy(w, resp.Body)
	utils.Panicln(err2)
	defer resp.Body.Close()
}
