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
	"websql/internal/ai/agent/export"
	admin "websql/internal/app/admin"
	"websql/internal/app/backup"
	"websql/internal/app/configexport"
	"websql/internal/app/conn"
	"websql/internal/app/datadict"
	"websql/internal/app/dbadmin"
	"websql/internal/app/dbops"
	"websql/internal/app/ddl"
	"websql/internal/app/modeler"
	"websql/internal/app/monitor"
	"websql/internal/app/permission"
	"websql/internal/app/search"
	"websql/internal/app/snippet"
	"websql/internal/app/sql"
	"websql/internal/app/sqlopt"
	"websql/internal/app/storage"
	syncdb "websql/internal/app/sync"
	"websql/internal/app/system"
	tree "websql/internal/app/treehandler"
	"websql/internal/audit"
	"websql/internal/config"
	"websql/internal/database"
	"websql/internal/middleware"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"
	"websql/internal/pkg/safego"
	"websql/internal/version"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
)

// embeddedAssetsFS 非空时，/assets/* 和 SPA index.html 从此嵌入式文件系统服务，
// 否则回退到磁盘 ./static/ 目录。由桌面版入口通过 SetEmbeddedAssets 设置。
var (
	embeddedAssetsFS  http.FileSystem
	embeddedIndexHTML []byte
)

// SetEmbeddedAssets 配置嵌入式前端资源，供桌面版打包使用。
// assetsFS 应指向 static/assets 子目录，indexHTML 为 index.html 内容。
func SetEmbeddedAssets(assetsFS http.FileSystem, indexHTML []byte) {
	embeddedAssetsFS = assetsFS
	embeddedIndexHTML = indexHTML
}

func MainRegister(router *gin.Engine) {

	localMode := config.IsLocalMode()

	if !localMode {
		router.Use(gzip.Gzip(gzip.DefaultCompression,
			gzip.WithExcludedPaths([]string{"/assets/"}),
			gzip.WithExcludedExtensions([]string{".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip"}),
		))
	}

	router.Use(middleware.CustomRecovery())
	if !localMode {
		router.Use(middleware.LoginRateLimitMiddleware())
		router.Use(middleware.APIRateLimitMiddleware())
	}
	router.Use(middleware.AuthMiddleware())
	router.Use(ContextMiddleware())

	router.Use(middleware.HostCheck())
	router.Use(middleware.CORSMiddleware())

	routerGroup := router.Group("api")

	routerGroup.GET("/listTable", dbops.ListTableFat)
	routerGroup.GET("/exportXlsx", sql.ExportXlsx)
	routerGroup.POST("/exportXlsxBySql", sql.ExportXlsxBySql)
	// 文本格式（CSV/JSON/SQL）服务端流式导出：边读边写，不在内存里攒全量结果
	routerGroup.POST("/exportText", sql.ExportText)
	routerGroup.POST("/importXlsx", sql.ImportXlsx)
	routerGroup.POST("/execSQL", sql.ExecSQL)
	// 按 requestId 终止正在执行的 SQL（服务端取消，释放数据库连接）
	routerGroup.POST("/sql/cancel", sql.CancelHandler)
	// 结果集分页查询（列式返回，供前端「继续加载」）
	routerGroup.POST("/query/page", sql.PageQuery)

	routerGroup.POST("/saveConn", conn.SaveConn)
	routerGroup.POST("/testDbConn", conn.TestDbConn)
	routerGroup.POST("/delConn", conn.DelConn)
	routerGroup.GET("/connBaseTree", permission.ConnBaseTree)
	routerGroup.GET("/listConn2", conn.ListConn2)
	routerGroup.GET("/listUserConn", conn.ListUserConn)
	routerGroup.GET("/listUserConnSchemasStream", tree.ListUserConnSchemasStream)
	// 只读连接：查询状态 / 临时解锁写入
	routerGroup.GET("/conn/readonly/state", conn.ReadOnlyStateHandler)
	routerGroup.POST("/conn/readonly/unlock", conn.UnlockWriteHandler)
	routerGroup.GET("/userPermissions", admin.UserPermissions)
	routerGroup.GET("/listTableNames", tree.ListTableNames)
	routerGroup.GET("/showTree", tree.ShowTree)

	routerGroup.POST("/listTableColumns", tree.ListTableColumns)
	routerGroup.POST("/tableOptions", dbops.TableOptions)
	routerGroup.POST("/tableStatistics", dbops.TableStatistics)
	routerGroup.POST("/listIndexes", dbops.ListIndexes)

	// 数据库对象管理 - 列出对象 / 获取对象 DDL（视图、存储过程、函数、触发器、事件、表）
	routerGroup.GET("/db/objects", dbops.ListObjects)
	routerGroup.GET("/db/object/ddl", dbops.GetObjectDDL)

	// 表结构变更：先生成计划预览（/ddl/plan），确认后按计划执行（/ddl/apply）。
	// 旧 /execSQL 保持不变，前端可灰度切回。
	routerGroup.POST("/ddl/plan", ddl.PlanHandler)
	routerGroup.POST("/ddl/apply", ddl.ApplyHandler)

	// 数据库用户与库管理（仅 MySQL/MariaDB/Oracle；本地/桌面模式无限制，非本地仅管理员）
	routerGroup.GET("/db/admin/users", dbadmin.ListDbUsers)
	routerGroup.POST("/db/admin/user/save", dbadmin.SaveDbUser)
	routerGroup.POST("/db/admin/user/drop", dbadmin.DropDbUser)
	routerGroup.GET("/db/admin/schemas", dbadmin.ListAdminSchemas)
	routerGroup.POST("/db/admin/schema/create", dbadmin.CreateSchema)
	routerGroup.POST("/db/admin/schema/drop", dbadmin.DropSchema)
	routerGroup.GET("/db/admin/user/privileges", dbadmin.ListUserPrivileges)
	routerGroup.POST("/db/admin/user/privilege/grant", dbadmin.GrantPrivilege)
	routerGroup.POST("/db/admin/user/privilege/revoke", dbadmin.RevokePrivilege)

	routerGroup.POST("/saveTree", permission.SaveTree)
	routerGroup.GET("/listDirTree", permission.ListDirTree)
	routerGroup.POST("/delTreeNode", permission.DelTreeNode)

	routerGroup.POST("/login", admin.Login)
	routerGroup.POST("/logout", admin.Logout)

	routerGroup.POST("/saveRole", admin.SaveRole)
	routerGroup.POST("/delRole", admin.DelRole)
	routerGroup.GET("/roleList", admin.RoleList)
	routerGroup.GET("/roleBaseList", admin.RoleBaseList)
	routerGroup.GET("/roleCapability", admin.GetRoleCapability)
	routerGroup.GET("/findUserByRole", admin.FindUserByRole)
	routerGroup.GET("/permissionTree", admin.GetPermissionTree)
	routerGroup.GET("/canUseClassicView", admin.CanUseClassicView)
	routerGroup.GET("/canModifyData", admin.CanModifyData)

	// 数据脱敏规则（管理员对已授权列配置的数据保护策略，不改变权限层级）
	routerGroup.GET("/dataMaskRuleList", admin.ListMaskRulesHandler)
	routerGroup.POST("/saveDataMaskRule", admin.SaveMaskRuleHandler)
	routerGroup.POST("/delDataMaskRule", admin.DeleteMaskRuleHandler)

	routerGroup.GET("/promptList", admin.PromptList)
	routerGroup.GET("/promptListByRole", admin.PromptListByRole)
	routerGroup.GET("/promptDetail", admin.PromptDetail)
	routerGroup.POST("/savePrompt", admin.SavePrompt)
	routerGroup.POST("/delPrompt", admin.DelPrompt)

	routerGroup.GET("/findUser", admin.FindUser)
	routerGroup.GET("/findUserBase", admin.FindUserBase)
	routerGroup.POST("/saveUser", admin.SaveUser)
	routerGroup.POST("/delUser", admin.DelUser)

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
	routerGroup.POST("/system/config/ai/model/save", system.SaveAIModelHandler)
	routerGroup.POST("/system/config/ai/model/delete", system.DeleteAIModelHandler)
	routerGroup.POST("/system/config/ai/model/select", system.SelectAIModelHandler)

	// 用户配置导出：导出系统设置、提示词、连接、审计配置、收藏为 SQL 文件（管理员）
	routerGroup.GET("/system/config/export", configexport.ExportUserConfig)

	routerGroup.POST("/ai/config/save", ai.HandleSaveConfig)
	routerGroup.GET("/ai/config/get", ai.HandleGetConfig)
	routerGroup.POST("/ai/config/test", ai.HandleTestConfig)

	// Eino 智能体路由（v2）
	agentHandler, err := agent.NewHandler()
	if err != nil {
		log.Printf("创建 AI Agent Handler 失败: %v，AI 功能将不可用", err)
	} else {
		routerGroup.POST("/ai/agent/chatStream", agentHandler.ChatStream)
		// 技能执行排队：用户选择「改用 Go 原生导出」
		routerGroup.POST("/ai/agent/queue/degrade", agentHandler.HandleSkillDegrade)
		// 文件上传（新链路：仅校验 + 存储，不解析内容）
		routerGroup.POST("/ai/agent/uploadFile", agent.HandleUploadFile)
		routerGroup.POST("/ai/agent/uploadFile/delete", agent.HandleDeleteUploadFile)
		routerGroup.GET("/ai/agent/uploadFile/content", agent.HandleUploadFileContent)
		routerGroup.GET("/ai/agent/fileSupport", agent.HandleFileSupport)
		// legacy 上传接口（deprecated，保留旧契约供未升级前端使用）
		routerGroup.POST("/ai/agent/uploadExcel", agent.HandleUploadExcel)
		routerGroup.POST("/ai/agent/preMatchColumns", agent.HandlePreMatchColumns)
		routerGroup.GET("/ai/agent/sessions", agentHandler.HandleGetSessions)
		routerGroup.GET("/ai/agent/session", agentHandler.HandleGetSession)
		routerGroup.POST("/ai/agent/session/delete", agentHandler.HandleDeleteSession)
		// 安全指标快照：仅聚合计数，不含用户/连接/表/列/会话标识
		routerGroup.GET("/ai/securityMetrics", agent.GetAISecurityMetrics)
	}
	routerGroup.GET("/exports/:filename", handleExportDownload)
	// 同时注册不带 /api 前缀的路由，兼容前端未拼接 apiBase 的导出链接
	router.GET("/exports/:filename", handleExportDownload)

	// 审计日志 API
	routerGroup.GET("/audit/logs", audit.HandleGetAuditLogs)
	routerGroup.GET("/audit/stats", audit.HandleGetAuditStats)
	routerGroup.GET("/audit/config/get", audit.HandleGetAuditConfig)
	routerGroup.POST("/audit/config/save", audit.HandleSaveAuditConfig)

	// 数据同步与结构同步
	routerGroup.POST("/sync/compareSchema", syncdb.CompareSchema)
	routerGroup.POST("/sync/compareData", syncdb.CompareData)
	routerGroup.POST("/sync/compareDataChunked", syncdb.CompareDataChunked)
	routerGroup.POST("/sync/applySchemaDiff", syncdb.ApplySchemaDiff)
	routerGroup.POST("/sync/applyDataSync", syncdb.ApplyDataSync)
	routerGroup.POST("/sync/generateSyncSQL", syncdb.GenerateSyncSQL)
	routerGroup.GET("/sync/targets", syncdb.GetSyncTargets)
	// 数据同步增强：Dry-Run 试运行、回滚、报告导出
	routerGroup.POST("/sync/dryRun", syncdb.DryRunSync)
	routerGroup.GET("/sync/rollbackLog", syncdb.GetRollbackLog)
	routerGroup.POST("/sync/rollback", syncdb.RollbackSync)
	routerGroup.POST("/sync/exportReport", syncdb.ExportSyncReport)

	// 数据建模与ER图
	routerGroup.POST("/modeler/reverse", modeler.ReverseEngineer)
	routerGroup.POST("/modeler/forward", modeler.ForwardEngineer)
	routerGroup.POST("/modeler/export", modeler.ExportModel)
	// ER 关系 AI 推断：根据表结构（命名/注释/字段）推断表关系，不持久化
	routerGroup.POST("/er/analyzeRelations", modeler.AnalyzeRelationsHandler)

	// 备份恢复体系
	routerGroup.POST("/backup/create", backup.CreateBackup)
	routerGroup.GET("/backup/progress", backup.GetBackupProgress)
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
	routerGroup.GET("/monitor/history", monitor.GetMetricHistory)
	routerGroup.GET("/monitor/resources", monitor.GetResources)
	routerGroup.GET("/monitor/processes", monitor.GetProcesses)
	routerGroup.GET("/monitor/variables", monitor.GetServerVariables)
	routerGroup.GET("/monitor/variables/all", monitor.GetAllServerVariables)
	routerGroup.GET("/monitor/status/all", monitor.GetAllServerStatus)
	routerGroup.GET("/monitor/innodb-status", monitor.GetInnodbStatus)
	routerGroup.GET("/monitor/locks", monitor.GetLocks)
	routerGroup.GET("/monitor/slow-queries", monitor.GetSlowQueries)
	routerGroup.GET("/monitor/top-tables", monitor.GetTopTables)
	// 监控变量/状态 AI 分析（SSE 流式）
	routerGroup.POST("/monitor/aiAnalyze", monitor.AIAnalyze)

	// 全局数据库搜索
	routerGroup.GET("/search/objects", search.SearchObjects)
	routerGroup.GET("/search/objects/batch", search.SearchObjectsBatch)
	routerGroup.GET("/search/data", search.SearchData)
	routerGroup.GET("/search/all", search.SearchAll)
	routerGroup.GET("/search/tables", search.GetSearchTables)

	// SQL 收藏夹（后端同步、分类、标签、导入导出）
	routerGroup.GET("/snippet/list", snippet.List)
	routerGroup.POST("/snippet/save", snippet.Save)
	routerGroup.POST("/snippet/delete", snippet.Delete)
	routerGroup.GET("/snippet/export", snippet.Export)
	routerGroup.POST("/snippet/import", snippet.Import)
	routerGroup.GET("/snippet/categories", snippet.Categories)
	routerGroup.GET("/snippet/tags", snippet.Tags)
	routerGroup.GET("/snippet/schemas", snippet.Schemas)

	// 用户级 KV 存储：持久化前端 localStorage 数据，解决桌面模式重启后丢失问题
	routerGroup.GET("/storage/list", storage.List)
	routerGroup.GET("/storage/get", storage.Get)
	routerGroup.POST("/storage/save", storage.Save)
	routerGroup.POST("/storage/delete", storage.Delete)

	routerGroup.GET("/sysMode", func(c *gin.Context) {
		cfg := config.Get()
		localMode := !cfg.IsRemote || cfg.IsDesktop
		resp := map[string]any{
			"isRemote":  cfg.IsRemote,
			"isDesktop": cfg.IsDesktop,
		}
		if localMode {
			resp["localToken"] = LocalAutoToken
		}
		jsonutil.WriteJson(c.Writer, resp)
	})

	routerGroup.GET("/healthCheck", func(c *gin.Context) {
		status := "ok"
		dbStatus := "ok"
		// 优先使用容器持有的 DB；容器未构建时（如启动早期）回退到全局 database.Mngtdb
		var db *sqlx.DB
		if ctr := GetContainer(); ctr != nil {
			db = ctr.Mngtdb
		} else {
			db = database.Mngtdb // Deprecated: 回退兼容，应通过容器获取
		}
		if db != nil {
			if err := db.Ping(); err != nil {
				dbStatus = "error"
				status = "degraded"
			}
		}
		jsonutil.WriteJson(c.Writer, gin.H{
			"status":  status,
			"db":      dbStatus,
			"version": version.Version,
		})
	})

	if embeddedAssetsFS != nil {
		router.StaticFS("/assets", embeddedAssetsFS)
	} else {
		router.Static("/assets", "./static/assets")
	}

	// 3. 所有未匹配路由都返回 index.html（SPA 支持）
	router.NoRoute(func(c *gin.Context) {
		if embeddedIndexHTML != nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", embeddedIndexHTML)
			return
		}
		c.File("./static/index.html")
	})

	log.Println("路由注册完成")
}

func handleExportDownload(c *gin.Context) {
	fileName := c.Param("filename")
	if fileName == "" {
		response.WriteErr(c, http.StatusBadRequest, 400, "文件名不能为空")
		return
	}

	if strings.Contains(fileName, "..") || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		response.WriteErr(c, http.StatusBadRequest, 400, "非法文件名")
		return
	}

	cleanPath := filepath.Clean("exports/" + fileName)
	if !strings.HasPrefix(cleanPath, "exports") {
		response.WriteErr(c, http.StatusBadRequest, 400, "非法文件路径")
		return
	}

	// 归属校验：导出文件登记了归属用户时仅允许生成者下载；
	// 无归属记录的历史文件按兼容策略放行，随 7 天清理淘汰。
	// 不符时返回与「文件不存在」一致的 404，避免文件名探测。
	currentUserID, _ := c.Get("userId")
	uid, _ := currentUserID.(string)
	if allowed, known := export.CheckExportAccess(fileName, uid); known && !allowed {
		response.WriteErr(c, http.StatusNotFound, 404, "文件不存在")
		return
	}

	contentTypes := map[string]string{
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".html": "text/html; charset=utf-8",
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
		response.WriteErr(c, http.StatusBadRequest, 400, "不支持的文件类型")
		return
	}

	filePath := "exports/" + fileName

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		response.WriteErr(c, http.StatusNotFound, 404, "文件不存在")
		return
	}

	c.Header("Content-Type", ct)
	// HTML 文件使用 inline 方便浏览器直接打开预览
	if ext == ".html" {
		c.Header("Content-Disposition", "inline; filename=\""+fileName+"\"")
	} else {
		c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	}

	file, err := os.Open(filePath)
	if err != nil {
		response.WriteErr(c, http.StatusInternalServerError, 500, "读取文件失败")
		return
	}
	defer file.Close()

	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, file)
}

func StartCleanupScheduler() {
	safego.GoWithName("export-cleanup", func() {
		for {
			time.Sleep(1 * time.Hour)
			cleanExpiredExports()
		}
	})
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
	// 同步清理孤儿归属记录（对应文件已删除/过期淘汰）
	export.CleanupOrphanOwners()
	// 清理旧版全局 offload 目录（新版按用户/会话隔离）并清理过期副本。
	agent.CleanupLegacyOffload()
	agent.CleanupReductionOffload(7 * 24 * time.Hour)
}
