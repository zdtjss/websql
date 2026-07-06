import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'

/** 执行 SQL 参数 */
export interface ExecSQLParams {
  connId: string
  schema: string
  sql: string
  maxLine: number | string
  tableName?: string
  batch?: string
  isFile?: string
}

/** 列信息 */
export interface ColumnInfo {
  name: string
  [key: string]: unknown
}

/** 单条 SQL 执行结果 */
export interface SQLResult {
  columns?: ColumnInfo[]
  results?: unknown[]
  [key: string]: unknown
}

/** 批量执行结果 */
export interface BatchSQLResult {
  results: SQLResult[]
  [key: string]: unknown
}

/** SQL 历史记录条目 */
export interface SQLHistoryItem {
  id?: string
  exec_sql?: string
  [key: string]: unknown
}

/** SQL 历史分页结果 */
export interface SQLHistoryPage {
  data: SQLHistoryItem[]
  total: number
}

/** SQL 历史查询参数 */
export interface ListBackupDataParams {
  connId: string
  schema: string
  current: number
  pageSize: number
}

/** 导出 XLSX 参数 */
export interface ExportXlsxParams {
  connId: string
  schema: string
  table: string
}

/**
 * 执行 SQL，对应 POST /execSQL
 * 使用 URLSearchParams 传参，支持 AbortSignal 取消
 */
export function execSQL(params: ExecSQLParams, signal?: AbortSignal): Promise<AxiosResponse<ApiResponse<SQLResult | BatchSQLResult>>> {
  const body = new URLSearchParams()
  body.append('connId', params.connId)
  body.append('schema', params.schema)
  body.append('sql', params.sql)
  body.append('maxLine', String(params.maxLine))
  if (params.tableName !== undefined) {
    body.append('tableName', params.tableName)
  }
  if (params.batch !== undefined) {
    body.append('batch', params.batch)
  }
  if (params.isFile !== undefined) {
    body.append('isFile', params.isFile)
  }
  const config = signal ? { signal } : undefined
  return http.post('/execSQL', body, config)
}

/** 查询 SQL 执行历史，对应 GET /listBackupData */
export function listBackupData(params: ListBackupDataParams): Promise<AxiosResponse<ApiResponse<SQLHistoryPage>>> {
  return http.get('/listBackupData', { params })
}

/** 查看备份数据详情，对应 GET /showBackupData */
export function showBackupData(backupId: string): Promise<AxiosResponse<ApiResponse<string>>> {
  return http.get('/showBackupData', { params: { backupId } })
}

/** 导出表数据为 XLSX，对应 GET /exportXlsx，返回 blob */
export function exportXlsx(params: ExportXlsxParams): Promise<AxiosResponse<Blob>> {
  return http.get('/exportXlsx', {
    params,
    responseType: 'blob'
  })
}

/** SQL 执行计划分析，对应 POST /sqlopt/explain */
export function explainSqlOpt(formData: FormData): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/sqlopt/explain', formData)
}

/** 搜索数据库对象，对应 GET /search/objects */
export interface SearchObjectsParams {
  connId: string
  schema?: string
  keyword: string
  searchType: string
}

export function searchObjects(params: SearchObjectsParams): Promise<AxiosResponse> {
  return http.get('/search/objects', { params })
}

/** 数据字典 - 列出表，对应 GET /datadict/tables */
export function getDatadictTables(connId: string, schema: string): Promise<AxiosResponse> {
  return http.get('/datadict/tables', { params: { connId, schema } })
}

/** 数据字典 - 生成字典，对应 POST /datadict/generate */
export function generateDatadict(formData: FormData): Promise<AxiosResponse> {
  return http.post('/datadict/generate', formData)
}

/** 数据字典 - 导出 HTML，对应 POST /datadict/export/html，返回 blob */
export function exportDatadictHtml(formData: FormData): Promise<AxiosResponse<Blob>> {
  return http.post('/datadict/export/html', formData, { responseType: 'blob' })
}

/** 数据字典 - 导出 PDF，对应 POST /datadict/export/pdf，返回 blob */
export function exportDatadictPdf(formData: FormData): Promise<AxiosResponse<Blob>> {
  return http.post('/datadict/export/pdf', formData, { responseType: 'blob' })
}

/** 备份 - 列出备份，对应 GET /backup/list */
export function listBackups(connId: string, schema: string): Promise<AxiosResponse> {
  return http.get('/backup/list', { params: { connId, schema } })
}

/** 备份 - 列出可备份表，对应 GET /backup/tables */
export function listBackupTables(connId: string, schema: string): Promise<AxiosResponse> {
  return http.get('/backup/tables', { params: { connId, schema } })
}

/** 备份 - 创建备份，对应 POST /backup/create
 * 异步执行，立即返回 taskId，需配合 getBackupProgress 轮询进度 */
export function createBackup(formData: FormData): Promise<AxiosResponse> {
  return http.post('/backup/create', formData)
}

/** 备份 - 查询备份进度，对应 GET /backup/progress
 * 返回 {status, totalTables, processedTables, currentTable, exportedBytes, result?, error?} */
export function getBackupProgress(taskId: string): Promise<AxiosResponse> {
  return http.get('/backup/progress', { params: { taskId } })
}

/** 备份 - 下载备份，对应 GET /backup/download，返回 blob */
export function downloadBackup(backupId: string): Promise<AxiosResponse<Blob>> {
  return http.get('/backup/download', { params: { backupId }, responseType: 'blob' })
}

/** 备份 - 恢复备份，对应 POST /backup/restore */
export function restoreBackup(formData: FormData): Promise<AxiosResponse> {
  return http.post('/backup/restore', formData)
}

/** 备份 - 删除备份，对应 POST /backup/delete */
export function deleteBackup(formData: FormData): Promise<AxiosResponse> {
  return http.post('/backup/delete', formData)
}

/** 数据库对象类型 */
export type DbObjectType = 'table' | 'view' | 'procedure' | 'function' | 'trigger' | 'event'

/** 列出指定类型的数据库对象，对应 GET /db/objects */
export function listDbObjects(params: {
  connId: string
  schema: string
  type: DbObjectType
}): Promise<AxiosResponse<ApiResponse<any[]>>> {
  return http.get('/db/objects', { params })
}

/** 获取对象的 DDL 定义文本，对应 GET /db/object/ddl */
export function getObjectDDL(params: {
  connId: string
  schema: string
  type: DbObjectType
  name: string
}): Promise<AxiosResponse<ApiResponse<string>>> {
  return http.get('/db/object/ddl', { params })
}
