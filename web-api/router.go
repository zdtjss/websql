package webapi

import (
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/exp/maps"
)

// 不需要以/结尾
var destAddr string = "http://localhost:8083"

func MainRegister(router *gin.Engine) {

	router.Use(CustomRecovery())

	router.GET("/listTable", admin.ListTableFat)
	router.GET("/exportXlsx", ExportXlsx)
	router.POST("/importXlsx", ImportXlsx)
	router.POST("/execSQL", ExecSQL)

	router.POST("/saveConn", admin.SaveConn)
	router.GET("/delConn", admin.DelConn)
	router.GET("/connBaseTree", admin.ConnBaseTree)
	router.GET("/listConn2", admin.ListConn2)
	router.GET("/showTree", admin.ShowTree)

	router.POST("/saveTree", admin.SaveTree)
	router.GET("/listDirTree", admin.ListDirTree)
	router.GET("/delTreeNode", admin.DelTreeNode)

	router.POST("/login", admin.Login)
	router.POST("/logout", admin.Logout)

	router.POST("/saveRole", admin.SaveRole)
	router.GET("/delRole", admin.DelRole)
	router.GET("/roleList", admin.RoleList)
	router.GET("/roleBaseList", admin.RoleBaseList)
	router.GET("/findUserByRole", admin.FindUserByRole)

	router.GET("/findUser", admin.FindUser)
	router.POST("/saveUser", admin.SaveUser)
	router.GET("/delUser", admin.DelUser)

	router.POST("/saveUserBio", admin.SaveUserBio)

	router.GET("/listBackupData", admin.ListBackupData)
	router.GET("/showBackupData", admin.ShowBackupData)

	router.GET("/sysMode", func(c *gin.Context) {
		utils.WriteJson(c.Writer, map[string]bool{"isRemote": config.Cfg.IsRemote})
	})

	router.GET("/healthCheck", func(c *gin.Context) {
		utils.WriteJson(c.Writer, "")
	})

	router.Any("/ext/", proxy)

	// router.NoRoute(notFound())

	router.Use(hostCheck())
	router.Use(CORSMiddleware())

	// 启用 gzip，排除静态文件
	/* router.Use(gzip.Gzip(gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{"/static/"}),
		gzip.WithExcludedExtensions([]string{".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip"}),
	)) */

	// 2. 注册静态文件（可选，用于明确的静态资源）
	router.Static("/assets", "./static/assets")

	// 3. 所有未匹配路由都返回 index.html（SPA 支持）
	router.NoRoute(func(c *gin.Context) {
		c.File("./static/index.html")
	})

	log.Println("路由注册完成")
}

// 对外代理的接口注册
func proxy(c *gin.Context) {

	req, _ := http.NewRequest(c.Request.Method, destAddr+c.Request.RequestURI[4:], c.Request.Body)
	defer c.Request.Body.Close()
	*&req.Header = c.Request.Header
	resp, err := http.DefaultClient.Do(req)
	logutils.PanicErr(err)

	maps.Copy(c.Request.Header, resp.Header)
	c.Status(resp.StatusCode)

	_, err2 := io.Copy(c.Writer, resp.Body)
	logutils.PanicErr(err2)
	defer resp.Body.Close()
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization")
	}
}

// 应该是第一个引入
func hostCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Cfg.IsRemote && !slices.ContainsFunc(config.Cfg.AllowedIP, func(allowedIp string) bool {
			return strings.HasPrefix(c.Request.RemoteAddr, allowedIp+":")
		}) {
			c.Writer.Write([]byte("<div style=\"text-align: center;font-size: xxx-large;\">非法 IP</div>"))
			c.Header("content-type", "text/html; charset=utf-8")
			log.Println("非法IP:" + c.Request.RemoteAddr)
			return
		}
	}
}

func CustomRecovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		if recovered != nil {

			// 1. 记录堆栈（必须在 Abort 前！）
			stack := string(debug.Stack())
			log.Println("PANIC:", recovered)
			log.Println(stack)

			// 2. 终止中间件链
			c.Abort()

			// 4. 使用 c.JSON —— 自动设置 Content-Type + 状态码 + 安全序列化
			c.JSON(http.StatusOK, gin.H{
				"code": 500,
				"msg":  recovered,
			})
		}
	})
}
