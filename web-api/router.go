package webapi

import (
	"log"

	"github.com/julienschmidt/httprouter"
)

func MainRegister(router *httprouter.Router) {

	router.GET("/listTable", ListTable)

	log.Println("路由注册完成")
}
