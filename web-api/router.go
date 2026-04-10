package webapi

import (
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"go-web/web-api/ai"
	aiagentv2 "go-web/web-api/ai/agent/v2"
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

	routerGroup := router.Group("api")

	routerGroup.GET("/listTable", admin.ListTableFat)
	routerGroup.GET("/exportXlsx", ExportXlsx)
	routerGroup.POST("/exportXlsxBySql", ExportXlsxBySql)
	routerGroup.POST("/importXlsx", ImportXlsx)
	routerGroup.POST("/execSQL", ExecSQL)

	routerGroup.POST("/saveConn", admin.SaveConn)
	routerGroup.POST("/testDbConn", admin.TestDbConn)
	routerGroup.GET("/delConn", admin.DelConn)
	routerGroup.GET("/connBaseTree", admin.ConnBaseTree)
	routerGroup.GET("/listConn2", admin.ListConn2)
	routerGroup.GET("/listUserConn", admin.ListUserConn)
	routerGroup.GET("/userPermissions", admin.UserPermissions)
	routerGroup.GET("/listTableNames", admin.ListTableNames)
	routerGroup.GET("/showTree", admin.ShowTree)

	routerGroup.POST("/listTableColumns", admin.ListTableColumns)
	routerGroup.POST("/tableOptions", admin.TableOptions)
	routerGroup.POST("/tableStatistics", admin.TableStatistics)
	routerGroup.POST("/listIndexes", admin.ListIndexes)

	routerGroup.POST("/saveTree", admin.SaveTree)
	routerGroup.GET("/listDirTree", admin.ListDirTree)
	routerGroup.GET("/delTreeNode", admin.DelTreeNode)

	routerGroup.POST("/login", admin.Login)
	routerGroup.POST("/logout", admin.Logout)

	routerGroup.POST("/saveRole", admin.SaveRole)
	routerGroup.GET("/delRole", admin.DelRole)
	routerGroup.GET("/roleList", admin.RoleList)
	routerGroup.GET("/roleBaseList", admin.RoleBaseList)
	routerGroup.GET("/findUserByRole", admin.FindUserByRole)
	routerGroup.GET("/permissionTree", admin.GetPermissionTree)

	routerGroup.GET("/findUser", admin.FindUser)
	routerGroup.POST("/saveUser", admin.SaveUser)
	routerGroup.GET("/delUser", admin.DelUser)

	routerGroup.POST("/saveUserBio", admin.SaveUserBio)

	routerGroup.GET("/listBackupData", admin.ListBackupData)
	routerGroup.GET("/showBackupData", admin.ShowBackupData)

	// 系统配置接口
	routerGroup.GET("/system/config/list", admin.GetSystemConfig)
	routerGroup.POST("/system/config/save", admin.SaveSystemConfigHandler)
	routerGroup.GET("/system/config/all/get", admin.GetAllSystemConfigHandler)
	routerGroup.POST("/system/config/all/save", admin.SaveAllSystemConfigHandler)
	routerGroup.GET("/system/config/ai/get", admin.GetAIConfigHandler)
	routerGroup.POST("/system/config/ai/save", admin.SaveAIConfigHandler)
	routerGroup.GET("/system/config/outterUser/get", admin.GetOutterUserHandler)
	routerGroup.POST("/system/config/outterUser/save", admin.SaveOutterUserHandler)
	routerGroup.POST("/system/config/outterUser/test", admin.TestOutterUserHandler)
	routerGroup.GET("/system/config/allowedIP/get", admin.GetAllowedIPHandler)
	routerGroup.POST("/system/config/allowedIP/save", admin.SaveAllowedIPHandler)

	routerGroup.POST("/ai/config/save", ai.HandleSaveConfig)
	routerGroup.GET("/ai/config/get", ai.HandleGetConfig)
	routerGroup.POST("/ai/config/test", ai.HandleTestConfig)

	// Eino 智能体路由（v2）
	agentHandler, err := aiagentv2.NewHandler()
	if err != nil {
		log.Fatalf("创建 AI Agent Handler 失败：%v", err)
	}
	routerGroup.POST("/ai/agent/chatStream", agentHandler.ChatStream)
	routerGroup.POST("/ai/agent/uploadExcel", aiagentv2.HandleUploadExcel)
	routerGroup.GET("/ai/agent/sessions", agentHandler.HandleGetSessions)
	routerGroup.GET("/ai/agent/session", agentHandler.HandleGetSession)
	routerGroup.GET("/ai/agent/session/delete", agentHandler.HandleDeleteSession)
	routerGroup.GET("/ai/agent/audit/logs", agentHandler.HandleGetSQLAuditLogs)

	routerGroup.GET("/sysMode", func(c *gin.Context) {
		utils.WriteJson(c.Writer, map[string]bool{"isRemote": config.Cfg.IsRemote})
	})

	routerGroup.GET("/healthCheck", func(c *gin.Context) {
		utils.WriteJson(c.Writer, "")
	})

	routerGroup.Any("/ext/", proxy)

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
	router.GET("/exports/:filename", handleExportDownload) // AI 导出文件下载（下载后自动删除）

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
			log.Println("非法 IP:" + c.Request.RemoteAddr)
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
