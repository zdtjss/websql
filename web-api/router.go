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

	router.GET("/listTable", admin.ListTableFat)
	router.GET("/exportXlsx", ExportXlsx)
	router.POST("/exportXlsxBySql", ExportXlsxBySql)
	router.POST("/importXlsx", ImportXlsx)
	router.POST("/execSQL", ExecSQL)

	router.POST("/saveConn", admin.SaveConn)
	router.POST("/testDbConn", admin.TestDbConn)
	router.GET("/delConn", admin.DelConn)
	router.GET("/connBaseTree", admin.ConnBaseTree)
	router.GET("/listConn2", admin.ListConn2)
	router.GET("/listUserConn", admin.ListUserConn)
	router.GET("/userPermissions", admin.UserPermissions)
	router.GET("/listTableNames", admin.ListTableNames)
	router.GET("/showTree", admin.ShowTree)

	router.POST("/listTableColumns", admin.ListTableColumns)
	router.POST("/tableOptions", admin.TableOptions)
	router.POST("/tableStatistics", admin.TableStatistics)
	router.POST("/listIndexes", admin.ListIndexes)

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
	router.GET("/permissionTree", admin.GetPermissionTree)

	router.GET("/findUser", admin.FindUser)
	router.POST("/saveUser", admin.SaveUser)
	router.GET("/delUser", admin.DelUser)

	router.POST("/saveUserBio", admin.SaveUserBio)

	router.GET("/listBackupData", admin.ListBackupData)
	router.GET("/showBackupData", admin.ShowBackupData)

	// 系统配置接口
	router.GET("/system/config/list", admin.GetSystemConfig)
	router.POST("/system/config/save", admin.SaveSystemConfigHandler)
	router.GET("/system/config/all/get", admin.GetAllSystemConfigHandler)
	router.POST("/system/config/all/save", admin.SaveAllSystemConfigHandler)
	router.GET("/system/config/ai/get", admin.GetAIConfigHandler)
	router.POST("/system/config/ai/save", admin.SaveAIConfigHandler)
	router.GET("/system/config/outterUser/get", admin.GetOutterUserHandler)
	router.POST("/system/config/outterUser/save", admin.SaveOutterUserHandler)
	router.POST("/system/config/outterUser/test", admin.TestOutterUserHandler)
	router.GET("/system/config/allowedIP/get", admin.GetAllowedIPHandler)
	router.POST("/system/config/allowedIP/save", admin.SaveAllowedIPHandler)

	router.POST("/ai/config/save", ai.HandleSaveConfig)
	router.GET("/ai/config/get", ai.HandleGetConfig)
	router.POST("/ai/config/test", ai.HandleTestConfig)

	// Eino 智能体路由（v2）
	agentHandler, err := aiagentv2.NewHandler()
	if err != nil {
		log.Fatalf("创建 AI Agent Handler 失败：%v", err)
	}
	router.POST("/ai/agent/chatStream", agentHandler.ChatStream)
	router.GET("/ai/agent/sessions", agentHandler.HandleGetSessions)
	router.GET("/ai/agent/session", agentHandler.HandleGetSession)
	router.GET("/ai/agent/session/delete", agentHandler.HandleDeleteSession)
	router.GET("/ai/agent/audit/logs", agentHandler.HandleGetSQLAuditLogs)

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
