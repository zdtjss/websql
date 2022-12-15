package webapi

import (
	"log"
	"net/http"
)

func MainRegister() {

	expose("exportCsv", ExportCsv)
	expose("listTable", ListTable)

	log.Println("路由注册完成")
}

func expose(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	http.HandleFunc("/api/"+pattern, handler)
}
