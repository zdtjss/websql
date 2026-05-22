package app

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"websql/internal/ai"
	"websql/internal/ai/agent"
	admin "websql/internal/app/admin"
	"websql/internal/app/backup"
	"websql/internal/app/conn"
	"websql/internal/app/datadict"
	"websql/internal/app/dbops"
	"websql/internal/app/modeler"
	"websql/internal/app/monitor"
	"websql/internal/app/permission"
	"websql/internal/app/search"
	"websql/internal/app/sql"
	"websql/internal/app/sqlopt"
	syncdb "websql/internal/app/sync"
	"websql/internal/app/system"
	tree "websql/internal/app/treehandler"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/middleware"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// 不需要以/结尾
var destAddr = "http://localhost:8083"

var proxyHttpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

func MainRegister(router *gin.Engine) {

	router.Use(middleware.CustomRecovery())
	router.Use(gzip.Gzip(gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{"/assets/"}),
		gzip.WithExcludedExtensions([]string{".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip"}),
	))
	router.Use(middleware.LoginRateLimitMiddleware())
	router.Use(middleware.APIRateLimitMiddleware())
	router.Use(middleware.CircuitBreakerMiddleware())
	router.Use(middleware.AuthMiddleware())

	router.Use(middleware.HostCheck())
	router.Use(middleware.CORSMiddleware())

	routerGroup := router.Group("api")

	routerGroup.GET("/listTable", dbops.ListTableFat)
	routerGroup.GET("/exportXlsx", sql.ExportXlsx)
	routerGroup.POST("/exportXlsxBySql", sql.ExportXlsxBySql)
	routerGroup.POST("/importXlsx", sql.ImportXlsx)
	routerGroup.POST("/execSQL", sql.ExecSQL)

	routerGroup.POST("/saveConn", conn.SaveConn)
	routerGroup.POST("/testDbConn", conn.TestDbConn)
	routerGroup.GET("/delConn", conn.DelConn)
	routerGroup.GET("/connBaseTree", permission.ConnBaseTree)
	routerGroup.GET("/listConn2", conn.ListConn2)
	routerGroup.GET("/listUserConn", conn.ListUserConn)
	routerGroup.GET("/listUserConnSchemasStream", tree.ListUserConnSchemasStream)
	routerGroup.GET("/userPermissions", admin.UserPermissions)
	routerGroup.GET("/listTableNames", tree.ListTableNames)
	routerGroup.GET("/showTree", tree.ShowTree)

	routerGroup.POST("/listTableColumns", tree.ListTableColumns)
	routerGroup.POST("/tableOptions", dbops.TableOptions)
	routerGroup.POST("/tableStatistics", dbops.TableStatistics)
	routerGroup.POST("/listIndexes", dbops.ListIndexes)

	routerGroup.POST("/saveTree", permission.SaveTree)
	routerGroup.GET("/listDirTree", permission.ListDirTree)
	routerGroup.GET("/delTreeNode", permission.DelTreeNode)

	routerGroup.POST("/login", admin.Login)
	routerGroup.POST("/logout", admin.Logout)

	routerGroup.POST("/saveRole", admin.SaveRole)
	routerGroup.GET("/delRole", admin.DelRole)
	routerGroup.GET("/roleList", admin.RoleList)
	routerGroup.GET("/roleBaseList", admin.RoleBaseList)
	routerGroup.GET("/findUserByRole", admin.FindUserByRole)
	routerGroup.GET("/permissionTree", admin.GetPermissionTree)
	routerGroup.GET("/canUseClassicView", admin.CanUseClassicView)

	routerGroup.GET("/promptList", admin.PromptList)
	routerGroup.GET("/promptListByRole", admin.PromptListByRole)
	routerGroup.GET("/promptDetail", admin.PromptDetail)
	routerGroup.POST("/savePrompt", admin.SavePrompt)
	routerGroup.GET("/delPrompt", admin.DelPrompt)

	routerGroup.GET("/findUser", admin.FindUser)
	routerGroup.GET("/findUserBase", admin.FindUserBase)
	routerGroup.POST("/saveUser", admin.SaveUser)
	routerGroup.GET("/delUser", admin.DelUser)

	routerGroup.POST("/saveUserBio", admin.SaveUserBio)
	routerGroup.POST("/changePassword", admin.ChangePassword)

	routerGroup.GET("/listBackupData", admin.ListBackupData)
	routerGroup.GET("/showBackupData", admin.ShowBackupData)

	// 系统配置接口
	routerGroup.GET("/system/config/list", system.GetSystemConfig)
	routerGroup.POST("/system/config/save", system.SaveSystemConfigHandler)
	routerGroup.GET("/system/config/all/get", system.GetAllSystemConfigHandler)
	routerGroup.POST("/system/config/all/save", system.SaveAllSystemConfigHandler)
	routerGroup.GET("/system/config/ai/get", system.GetAIConfigHandler)
	routerGroup.POST("/system/config/ai/save", system.SaveAIConfigHandler)
	routerGroup.GET("/system/config/outterUser/get", system.GetOutterUserHandler)
	routerGroup.POST("/system/config/outterUser/save", system.SaveOutterUserHandler)
	routerGroup.POST("/system/config/outterUser/test", system.TestOutterUserHandler)
	routerGroup.GET("/system/config/allowedIP/get", system.GetAllowedIPHandler)
	routerGroup.POST("/system/config/allowedIP/save", system.SaveAllowedIPHandler)
	routerGroup.GET("/system/config/ai/models", system.GetAIModelListHandler)

	routerGroup.POST("/ai/config/save", ai.HandleSaveConfig)
	routerGroup.GET("/ai/config/get", ai.HandleGetConfig)
	routerGroup.POST("/ai/config/test", ai.HandleTestConfig)

	// Eino 智能体路由（v2）
	agentHandler, err := agent.NewHandler()
	if err != nil {
		log.Fatalf("创建 AI Agent Handler 失败: %v", err)
	}
	routerGroup.POST("/ai/agent/chatStream", agentHandler.ChatStream)
	routerGroup.POST("/ai/agent/uploadExcel", agent.HandleUploadExcel)
	routerGroup.POST("/ai/agent/preMatchColumns", agent.HandlePreMatchColumns)
	routerGroup.GET("/ai/agent/sessions", agentHandler.HandleGetSessions)
	routerGroup.GET("/ai/agent/session", agentHandler.HandleGetSession)
	routerGroup.GET("/ai/agent/session/delete", agentHandler.HandleDeleteSession)
	routerGroup.GET("/ai/agent/audit/logs", agentHandler.HandleGetSQLAuditLogs)
	routerGroup.GET("/exports/:filename", handleExportDownload)

	// 数据同步与结构同步
	routerGroup.POST("/sync/compareSchema", syncdb.CompareSchema)
	routerGroup.POST("/sync/compareData", syncdb.CompareData)
	routerGroup.POST("/sync/compareDataChunked", syncdb.CompareDataChunked)
	routerGroup.POST("/sync/applySchemaDiff", syncdb.ApplySchemaDiff)
	routerGroup.POST("/sync/applyDataSync", syncdb.ApplyDataSync)
	routerGroup.POST("/sync/generateSyncSQL", syncdb.GenerateSyncSQL)
	routerGroup.GET("/sync/targets", syncdb.GetSyncTargets)

	// 数据建模与ER图
	routerGroup.POST("/modeler/reverse", modeler.ReverseEngineer)
	routerGroup.POST("/modeler/forward", modeler.ForwardEngineer)
	routerGroup.POST("/modeler/export", modeler.ExportModel)

	// 备份恢复体系
	routerGroup.POST("/backup/create", backup.CreateBackup)
	routerGroup.GET("/backup/list", backup.ListBackups)
	routerGroup.POST("/backup/restore", backup.RestoreBackup)
	routerGroup.POST("/backup/delete", backup.DeleteBackup)
	routerGroup.GET("/backup/tables", backup.GetBackupTables)
	routerGroup.GET("/backup/download", backup.DownloadBackup)

	// 数据字典
	routerGroup.POST("/datadict/generate", datadict.GenerateDict)
	routerGroup.POST("/datadict/export/html", datadict.ExportDictHTML)
	routerGroup.POST("/datadict/export/pdf", datadict.ExportDictPDF)
	routerGroup.GET("/datadict/tables", datadict.GetDictTables)

	// SQL编辑器增强 - AI优化建议
	routerGroup.POST("/sqlopt/explain", sqlopt.ExplainSQL)
	routerGroup.POST("/sqlopt/optimize", sqlopt.OptimizeSQLStream)

	// 监控面板增强
	routerGroup.GET("/monitor/metrics", monitor.GetMetrics)
	routerGroup.GET("/monitor/metrics/history", monitor.GetMetricsHistory)
	routerGroup.GET("/monitor/resources", monitor.GetResources)
	routerGroup.GET("/monitor/processes", monitor.GetProcesses)
	routerGroup.GET("/monitor/variables", monitor.GetServerVariables)

	// 全局数据库搜索
	routerGroup.GET("/search/objects", search.SearchObjects)
	routerGroup.GET("/search/data", search.SearchData)
	routerGroup.GET("/search/all", search.SearchAll)
	routerGroup.GET("/search/tables", search.GetSearchTables)

	routerGroup.GET("/sysMode", func(c *gin.Context) {
		jsonutil.WriteJson(c.Writer, map[string]bool{"isRemote": config.Cfg.IsRemote})
	})

	routerGroup.GET("/healthCheck", func(c *gin.Context) {
		status := "ok"
		dbStatus := "ok"
		if database.Mngtdb != nil {
			if err := database.Mngtdb.Ping(); err != nil {
				dbStatus = "error"
				status = "degraded"
			}
		}
		jsonutil.WriteJson(c.Writer, gin.H{
			"status": status,
			"db":     dbStatus,
		})
	})

	routerGroup.Any("/ext/", proxy)

	router.Static("/assets", "./static/assets")

	// 3. 所有未匹配路由都返回 index.html（SPA 支持）
	router.NoRoute(func(c *gin.Context) {
		c.File("./static/index.html")
	})

	log.Println("路由注册完成")
}

// proxy 对外代理的接口
func proxy(c *gin.Context) {
	req, err := http.NewRequest(c.Request.Method, destAddr+c.Request.RequestURI[4:], c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "创建代理请求失败"})
		return
	}
	req.Header = c.Request.Header.Clone()
	resp, err := proxyHttpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "代理请求失败"})
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			c.Header(k, vv)
		}
	}
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

func handleExportDownload(c *gin.Context) {
	fileName := c.Param("filename")
	if fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件名"})
		return
	}

	cleanPath := filepath.Clean("exports/" + fileName)
	if !strings.HasPrefix(cleanPath, "exports") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件路径"})
		return
	}

	contentTypes := map[string]string{
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".png":  "image/png",
		".jpg":  "image/jpeg",
	}

	ext := ""
	ct := ""
	for e, t := range contentTypes {
		if strings.HasSuffix(fileName, e) {
			ext = e
			ct = t
			break
		}
	}
	if ext == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的文件类型"})
		return
	}

	filePath := "exports/" + fileName

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在"})
		return
	}

	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")

	file, err := os.Open(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件失败"})
		return
	}
	defer file.Close()

	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, file)
}

func StartCleanupScheduler() {
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			cleanExpiredExports()
		}
	}()
	log.Println("[Cleanup] 导出文件清理任务已启动（每1小时清理超过7天的文件）")
}

func cleanExpiredExports() {
	dir := "exports"
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Cleanup] 读取导出目录失败: %v\n", err)
		}
		return
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	deleted := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			if err := os.Remove(path); err != nil {
				log.Printf("[Cleanup] 删除失败 %s: %v\n", path, err)
			} else {
				deleted++
			}
		}
	}

	if deleted > 0 {
		log.Printf("[Cleanup] 已清理 %d 个过期导出文件\n", deleted)
	}
}