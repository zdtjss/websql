import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'

/** 数据库连接配置 */
export interface Connection {
  id?: string
  name?: string
  dbType?: string
  parentId?: string
  host?: string
  port?: number | string
  username?: string
  password?: string
  database?: string
  remark?: string
  [key: string]: unknown
}

/** 连接列表查询参数 */
export interface ListConnParams {
  name?: string
  parentId?: string
  page?: number
  pageSize?: number
}

/** 分页列表结果 */
export interface PageResult<T> {
  data: T[]
  total: number
}

/** 目录树节点 */
export interface DirTreeNode {
  id: string
  label: string
  value: string
  type?: string
  children?: DirTreeNode[]
  checked?: boolean
  [key: string]: unknown
}

/** showTree 查询参数 */
export interface ShowTreeParams {
  connId: string
  key: string
  type: string
  level: number | string
  schema?: string
}

/** 树节点（showTree 返回的数据库对象节点） */
export interface TreeNode {
  id?: string
  label?: string
  type?: string
  data?: unknown
  isLeaf?: boolean
  [key: string]: unknown
}

/** 表信息 */
export interface TableInfo {
  name: string
  comment?: string
  schema?: string
  [key: string]: unknown
}

/** listTableNames schema 引用 */
export interface SchemaRef {
  connId: string
  schema: string
}

/** 列出连接，对应 GET /listConn2 */
export function listConn(params: ListConnParams): Promise<AxiosResponse<ApiResponse<PageResult<Connection>> | ApiResponse>> {
  return http.get('/listConn2', { params })
}

/** 保存连接，对应 POST /saveConn */
export function saveConn(row: Connection): Promise<AxiosResponse<ApiResponse<Connection> | ApiResponse>> {
  return http.post('/saveConn', row)
}

/** 测试数据库连接，对应 POST /testDbConn */
export function testDbConn(row: Connection): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/testDbConn', row)
}

/** 删除连接，对应 POST /delConn */
export function delConn(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/delConn', null, { params: { id } })
}

/** 列出目录树，对应 GET /listDirTree */
export function listDirTree(): Promise<AxiosResponse<ApiResponse<DirTreeNode[]>>> {
  return http.get('/listDirTree')
}

/** 删除目录树节点，对应 POST /delTreeNode */
export function delTreeNode(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/delTreeNode', null, { params: { id } })
}

/** 保存目录树，对应 POST /saveTree */
export function saveTree(treeData: DirTreeNode[]): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/saveTree', treeData)
}

/** 浏览数据库对象树，对应 GET /showTree */
export function showTree(params: ShowTreeParams): Promise<AxiosResponse<ApiResponse<TreeNode[]>>> {
  return http.get('/showTree', { params })
}

/** 列出表，对应 GET /listTable */
export function listTable(connId: string, schema: string): Promise<AxiosResponse> {
  return http.get('/listTable', { params: { connId, schema } })
}

/** 列出表名（单连接单 schema），对应 GET /listTableNames */
export function listTableNames(connId: string, schema: string): Promise<AxiosResponse<ApiResponse<TableInfo[] | string[]>>> {
  return http.get('/listTableNames', { params: { connId, schema: schema || '' } })
}

/** 列出表名（多 schema），对应 GET /listTableNames，schemas 为 JSON 字符串 */
export function listTableNamesBySchemas(schemas: SchemaRef[]): Promise<AxiosResponse<ApiResponse<TableInfo[]>>> {
  return http.get('/listTableNames', { params: { schemas: JSON.stringify(schemas) } })
}

/** 数据同步 - 获取同步目标（schema/table 列表），对应 GET /sync/targets */
export function getSyncTargets(connId: string): Promise<AxiosResponse> {
  return http.get('/sync/targets', { params: { connId } })
}

/** 数据同步 - 比较表结构，对应 POST /sync/compareSchema */
export function compareSyncSchema(formData: FormData): Promise<AxiosResponse> {
  return http.post('/sync/compareSchema', formData)
}

/** 数据同步 - 比较表数据，对应 POST /sync/compareData */
export function compareSyncData(formData: FormData): Promise<AxiosResponse> {
  return http.post('/sync/compareData', formData)
}

/** 数据同步 - 生成同步 SQL，对应 POST /sync/generateSyncSQL */
export function generateSyncSQL(formData: FormData): Promise<AxiosResponse> {
  return http.post('/sync/generateSyncSQL', formData)
}

/** 数据同步 - 分块比较数据，对应 POST /sync/compareDataChunked，支持 AbortSignal */
export function compareDataChunked(formData: FormData, signal?: AbortSignal): Promise<AxiosResponse> {
  const config = signal ? { signal } : undefined
  return http.post('/sync/compareDataChunked', formData, config)
}

/** 数据同步 - 应用结构差异，对应 POST /sync/applySchemaDiff */
export function applySchemaDiff(formData: FormData): Promise<AxiosResponse> {
  return http.post('/sync/applySchemaDiff', formData)
}

/** 数据同步 - 应用数据同步，对应 POST /sync/applyDataSync，支持 AbortSignal */
export function applyDataSync(formData: FormData, signal?: AbortSignal): Promise<AxiosResponse> {
  const config = signal ? { signal } : undefined
  return http.post('/sync/applyDataSync', formData, config)
}

/** 数据同步 - Dry-Run 试运行，对应 POST /sync/dryRun
 *  不执行写操作，返回预估影响行数与示例 SQL。 */
export function dryRunSync(formData: FormData): Promise<AxiosResponse> {
  return http.post('/sync/dryRun', formData)
}

/** 数据同步 - 获取回滚日志，对应 GET /sync/rollbackLog
 *  返回指定会话的撤销 SQL 列表，供用户确认后回滚。 */
export function getRollbackLog(sessionId: string): Promise<AxiosResponse> {
  return http.get('/sync/rollbackLog', { params: { sessionId } })
}

/** 数据同步 - 执行回滚，对应 POST /sync/rollback
 *  按逆序执行撤销 SQL，需对目标连接有写权限。 */
export function rollbackSync(sessionId: string): Promise<AxiosResponse> {
  const formData = new FormData()
  formData.append('sessionId', sessionId)
  return http.post('/sync/rollback', formData)
}

/** 数据同步 - 导出报告，对应 POST /sync/exportReport
 *  payload 包含同步结果与元信息；format 为 'html' 或 'csv'。
 *  返回 { filename, url, format }，url 可经 /exports/<filename> 下载。 */
export function exportSyncReport(payload: Record<string, unknown>): Promise<AxiosResponse> {
  return http.post('/sync/exportReport', payload)
}

/** 监控 - 获取指标，对应 GET /monitor/metrics */
export function getMonitorMetrics(connId: string): Promise<AxiosResponse> {
  return http.get('/monitor/metrics', { params: { connId } })
}

/** 监控 - 获取资源，对应 GET /monitor/resources */
export function getMonitorResources(connId: string, schema: string): Promise<AxiosResponse> {
  return http.get('/monitor/resources', { params: { connId, schema } })
}

/** 监控 - 获取进程列表，对应 GET /monitor/processes */
export function getMonitorProcesses(connId: string): Promise<AxiosResponse> {
  return http.get('/monitor/processes', { params: { connId } })
}

/** 监控历史指标点 */
export interface MonitorHistoryPoint {
  timestamp: string
  value: number
}

/** 监控 - 获取历史趋势，对应 GET /monitor/history
 *  interval 取值：raw（原始）/ 5min（5分钟均值）/ 1hour（1小时均值）
 *  返回 { points: [{ timestamp, value }] } */
export function getMonitorHistory(
  connId: string,
  metric: string,
  from: string,
  to: string,
  interval: 'raw' | '5min' | '1hour'
): Promise<AxiosResponse<ApiResponse<{ points: MonitorHistoryPoint[] }>>> {
  return http.get('/monitor/history', { params: { connId, metric, from, to, interval } })
}

/** 监控 - 获取 InnoDB 引擎状态，对应 GET /monitor/innodb-status */
export function getInnodbStatus(connId: string): Promise<AxiosResponse> {
  return http.get('/monitor/innodb-status', { params: { connId } })
}

/** 监控 - 获取锁与事务等待，对应 GET /monitor/locks */
export function getLocks(connId: string): Promise<AxiosResponse> {
  return http.get('/monitor/locks', { params: { connId } })
}

/** 监控 - 获取慢查询分析，对应 GET /monitor/slow-queries */
export function getSlowQueries(connId: string, limit = 20): Promise<AxiosResponse> {
  return http.get('/monitor/slow-queries', { params: { connId, limit } })
}

/** 监控 - 获取表统计 TOP N，对应 GET /monitor/top-tables */
export function getTopTables(connId: string, schema: string, limit = 20): Promise<AxiosResponse> {
  return http.get('/monitor/top-tables', { params: { connId, schema, limit } })
}
