export interface RouteEntry {
  module: string
  method: string
  stream?: boolean
  blob?: boolean
  upload?: boolean
}

const ROUTE_TABLE: Record<string, RouteEntry> = {
  '/listTable': { module: 'dbops', method: 'ListTableFat' },
  '/exportXlsx': { module: 'sql', method: 'ExportXlsx', blob: true },
  '/exportXlsxBySql': { module: 'sql', method: 'ExportXlsxBySql', blob: true },
  '/importXlsx': { module: 'sql', method: 'ImportXlsx', upload: true },
  '/execSQL': { module: 'sql', method: 'ExecSQL' },

  '/saveConn': { module: 'conn', method: 'SaveConn' },
  '/testDbConn': { module: 'conn', method: 'TestDbConn' },
  '/delConn': { module: 'conn', method: 'DelConn' },
  '/connBaseTree': { module: 'permission', method: 'ConnBaseTree' },
  '/listConn2': { module: 'conn', method: 'ListConn2' },
  '/listUserConn': { module: 'conn', method: 'ListUserConn' },
  '/listUserConnSchemasStream': { module: 'tree', method: 'ListUserConnSchemasStream', stream: true },

  '/userPermissions': { module: 'admin', method: 'UserPermissions' },
  '/listTableNames': { module: 'tree', method: 'ListTableNames' },
  '/showTree': { module: 'tree', method: 'ShowTree' },
  '/listTableColumns': { module: 'tree', method: 'ListTableColumns' },
  '/tableOptions': { module: 'dbops', method: 'TableOptions' },
  '/tableStatistics': { module: 'dbops', method: 'TableStatistics' },
  '/listIndexes': { module: 'dbops', method: 'ListIndexes' },

  '/db/objects': { module: 'dbops', method: 'ListObjects' },
  '/db/object/ddl': { module: 'dbops', method: 'GetObjectDDL' },

  '/saveTree': { module: 'permission', method: 'SaveTree' },
  '/listDirTree': { module: 'permission', method: 'ListDirTree' },
  '/delTreeNode': { module: 'permission', method: 'DelTreeNode' },

  '/login': { module: 'admin', method: 'Login' },
  '/logout': { module: 'admin', method: 'Logout' },
  '/saveRole': { module: 'admin', method: 'SaveRole' },
  '/delRole': { module: 'admin', method: 'DelRole' },
  '/roleList': { module: 'admin', method: 'RoleList' },
  '/roleBaseList': { module: 'admin', method: 'RoleBaseList' },
  '/findUserByRole': { module: 'admin', method: 'FindUserByRole' },
  '/permissionTree': { module: 'admin', method: 'GetPermissionTree' },
  '/canUseClassicView': { module: 'admin', method: 'CanUseClassicView' },
  '/canModifyData': { module: 'admin', method: 'CanModifyData' },

  '/promptList': { module: 'admin', method: 'PromptList' },
  '/promptListByRole': { module: 'admin', method: 'PromptListByRole' },
  '/promptDetail': { module: 'admin', method: 'PromptDetail' },
  '/savePrompt': { module: 'admin', method: 'SavePrompt' },
  '/delPrompt': { module: 'admin', method: 'DelPrompt' },

  '/findUser': { module: 'admin', method: 'FindUser' },
  '/findUserBase': { module: 'admin', method: 'FindUserBase' },
  '/saveUser': { module: 'admin', method: 'SaveUser' },
  '/delUser': { module: 'admin', method: 'DelUser' },
  '/saveUserBio': { module: 'admin', method: 'SaveUserBio' },
  '/changePassword': { module: 'admin', method: 'ChangePassword' },

  '/listBackupData': { module: 'admin', method: 'ListBackupData' },
  '/showBackupData': { module: 'admin', method: 'ShowBackupData' },

  '/system/config/list': { module: 'system', method: 'GetSystemConfig' },
  '/system/config/save': { module: 'system', method: 'SaveSystemConfigHandler' },
  '/system/config/all/get': { module: 'system', method: 'GetAllSystemConfigHandler' },
  '/system/config/all/save': { module: 'system', method: 'SaveAllSystemConfigHandler' },
  '/system/config/ai/get': { module: 'system', method: 'GetAIConfigHandler' },
  '/system/config/ai/save': { module: 'system', method: 'SaveAIConfigHandler' },
  '/system/config/outterUser/get': { module: 'system', method: 'GetOutterUserHandler' },
  '/system/config/outterUser/save': { module: 'system', method: 'SaveOutterUserHandler' },
  '/system/config/outterUser/test': { module: 'system', method: 'TestOutterUserHandler' },
  '/system/config/allowedIP/get': { module: 'system', method: 'GetAllowedIPHandler' },
  '/system/config/allowedIP/save': { module: 'system', method: 'SaveAllowedIPHandler' },
  '/system/config/ai/models': { module: 'system', method: 'GetAIModelListHandler' },
  '/system/config/ai/model/save': { module: 'system', method: 'SaveAIModelHandler' },
  '/system/config/ai/model/delete': { module: 'system', method: 'DeleteAIModelHandler' },
  '/system/config/ai/model/select': { module: 'system', method: 'SelectAIModelHandler' },

  '/ai/config/save': { module: 'ai', method: 'HandleSaveConfig' },
  '/ai/config/get': { module: 'ai', method: 'HandleGetConfig' },
  '/ai/config/test': { module: 'ai', method: 'HandleTestConfig' },

  '/ai/agent/chatStream': { module: 'agent', method: 'ChatStream', stream: true },
  '/ai/agent/uploadExcel': { module: 'agent', method: 'HandleUploadExcel', upload: true },
  '/ai/agent/preMatchColumns': { module: 'agent', method: 'HandlePreMatchColumns' },
  '/ai/agent/sessions': { module: 'agent', method: 'HandleGetSessions' },
  '/ai/agent/session': { module: 'agent', method: 'HandleGetSession' },
  '/ai/agent/session/delete': { module: 'agent', method: 'HandleDeleteSession' },

  '/audit/logs': { module: 'audit', method: 'HandleGetAuditLogs' },
  '/audit/stats': { module: 'audit', method: 'HandleGetAuditStats' },
  '/audit/config/get': { module: 'audit', method: 'HandleGetAuditConfig' },
  '/audit/config/save': { module: 'audit', method: 'HandleSaveAuditConfig' },

  '/sync/compareSchema': { module: 'syncdb', method: 'CompareSchema', upload: true },
  '/sync/compareData': { module: 'syncdb', method: 'CompareData', upload: true },
  '/sync/compareDataChunked': { module: 'syncdb', method: 'CompareDataChunked', upload: true },
  '/sync/applySchemaDiff': { module: 'syncdb', method: 'ApplySchemaDiff', upload: true },
  '/sync/applyDataSync': { module: 'syncdb', method: 'ApplyDataSync', upload: true },
  '/sync/generateSyncSQL': { module: 'syncdb', method: 'GenerateSyncSQL', upload: true },
  '/sync/targets': { module: 'syncdb', method: 'GetSyncTargets' },
  '/sync/dryRun': { module: 'syncdb', method: 'DryRunSync', upload: true },
  '/sync/rollbackLog': { module: 'syncdb', method: 'GetRollbackLog' },
  '/sync/rollback': { module: 'syncdb', method: 'RollbackSync' },
  '/sync/exportReport': { module: 'syncdb', method: 'ExportSyncReport' },

  '/modeler/reverse': { module: 'modeler', method: 'ReverseEngineer' },
  '/modeler/forward': { module: 'modeler', method: 'ForwardEngineer' },
  '/modeler/export': { module: 'modeler', method: 'ExportModel', blob: true },
  '/er/analyzeRelations': { module: 'modeler', method: 'AnalyzeRelationsHandler' },

  '/backup/create': { module: 'backup', method: 'CreateBackup' },
  '/backup/progress': { module: 'backup', method: 'GetBackupProgress' },
  '/backup/list': { module: 'backup', method: 'ListBackups' },
  '/backup/restore': { module: 'backup', method: 'RestoreBackup' },
  '/backup/delete': { module: 'backup', method: 'DeleteBackup' },
  '/backup/tables': { module: 'backup', method: 'GetBackupTables' },
  '/backup/download': { module: 'backup', method: 'DownloadBackup', blob: true },

  '/datadict/generate': { module: 'datadict', method: 'GenerateDict' },
  '/datadict/export/html': { module: 'datadict', method: 'ExportDictHTML', blob: true },
  '/datadict/export/pdf': { module: 'datadict', method: 'ExportDictPDF', blob: true },
  '/datadict/tables': { module: 'datadict', method: 'GetDictTables' },

  '/sqlopt/explain': { module: 'sqlopt', method: 'ExplainSQL' },
  '/sqlopt/optimize': { module: 'sqlopt', method: 'OptimizeSQLStream', stream: true },

  '/monitor/metrics': { module: 'monitor', method: 'GetMetrics' },
  '/monitor/history': { module: 'monitor', method: 'GetMetricHistory' },
  '/monitor/resources': { module: 'monitor', method: 'GetResources' },
  '/monitor/processes': { module: 'monitor', method: 'GetProcesses' },
  '/monitor/variables': { module: 'monitor', method: 'GetServerVariables' },
  '/monitor/variables/all': { module: 'monitor', method: 'GetAllServerVariables' },
  '/monitor/status/all': { module: 'monitor', method: 'GetAllServerStatus' },
  '/monitor/innodb-status': { module: 'monitor', method: 'GetInnodbStatus' },
  '/monitor/locks': { module: 'monitor', method: 'GetLocks' },
  '/monitor/slow-queries': { module: 'monitor', method: 'GetSlowQueries' },
  '/monitor/top-tables': { module: 'monitor', method: 'GetTopTables' },
  '/monitor/aiAnalyze': { module: 'monitor', method: 'AIAnalyze', stream: true },

  '/search/objects': { module: 'search', method: 'SearchObjects' },
  '/search/data': { module: 'search', method: 'SearchData' },
  '/search/all': { module: 'search', method: 'SearchAll' },
  '/search/tables': { module: 'search', method: 'GetSearchTables' },

  '/snippet/list': { module: 'snippet', method: 'List' },
  '/snippet/save': { module: 'snippet', method: 'Save' },
  '/snippet/delete': { module: 'snippet', method: 'Delete' },
  '/snippet/export': { module: 'snippet', method: 'Export' },
  '/snippet/import': { module: 'snippet', method: 'Import', upload: true },
  '/snippet/categories': { module: 'snippet', method: 'Categories' },
  '/snippet/tags': { module: 'snippet', method: 'Tags' },

  '/sysMode': { module: 'system', method: 'GetSysMode' },
  '/healthCheck': { module: 'system', method: 'HealthCheck' },
}

export function lookupRoute(url: string): RouteEntry | undefined {
  return ROUTE_TABLE[url]
}

export function listRoutes(): RouteEntry[] {
  return Object.values(ROUTE_TABLE)
}
