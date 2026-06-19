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

/** 删除连接，对应 GET /delConn */
export function delConn(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.get('/delConn', { params: { id } })
}

/** 列出目录树，对应 GET /listDirTree */
export function listDirTree(): Promise<AxiosResponse<ApiResponse<DirTreeNode[]>>> {
  return http.get('/listDirTree')
}

/** 删除目录树节点，对应 GET /delTreeNode */
export function delTreeNode(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.get('/delTreeNode', { params: { id } })
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
