import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'
import type { AIModel } from './ai'
import { downloadBlob } from '@/utils/exportHelper'

/** 角色信息 */
export interface Role {
  id: string
  name: string
  viewClassic?: number
  /** 全局数据修改能力（同时影响经典 SQL 与 AI） */
  allowModify?: number
  /** 仅约束 AI 数据写工具，需与 allowModify 同时开启 */
  aiAllowModify?: number
  /** 权限摘要（并发编辑乐观锁） */
  permissionDigest?: string
  powerList?: RolePower[]
}

/** 角色权限项（与后端 PowerDetail 对齐） */
export interface RolePower {
  id?: string
  roleId?: string
  connId: string
  connName?: string | null
  schemaName: string | null
  tableName: string | null
  columnName: string | null
  level: 'conn' | 'schema' | 'table' | 'column'
}

/** 保存角色参数（含并发控制与 AI 独立开关） */
export interface SaveRoleParams {
  id: string
  name: string
  addPowers: RolePower[]
  delPowers: RolePower[]
  viewClassic: number
  allowModify: number
  aiAllowModify?: number
  /** 加载角色时的权限摘要；缺省表示不做乐观锁校验（兼容旧客户端） */
  expectedDigest?: string
}

/** 角色能力配置（决定是否展示 AI 开关） */
export interface RoleCapability {
  capabilityVersion: number
  aiPolicyEnforce: boolean
  maskVersion?: string
}

/** 权限树查询参数 */
export interface PermissionTreeParams {
  level: string
  roleId?: string
  connId?: string
  schema?: string
  table?: string
}

/** 权限树节点 */
export interface PermissionTreeNode {
  id?: string
  label?: string
  type?: string
  children?: PermissionTreeNode[]
  [key: string]: unknown
}

/** 用户查询参数 */
export interface FindUserParams {
  name?: string
  loginName?: string
  roleId?: string
}

/** 用户信息 */
export interface User {
  id?: string
  loginName?: string
  name?: string
  pwd?: string
  roleId?: string[]
  roleName?: string[]
  [key: string]: unknown
}

/** 用户基础信息（搜索用） */
export interface UserBase {
  id?: string
  name?: string
  loginName?: string
  [key: string]: unknown
}

/** 系统配置 */
export interface SystemConfig {
  outterUser?: string
  selectedModelId?: string
  redisAddr?: string
  redisPassword?: string
  redisDB?: number
  defaultHomepage?: string
  allowedIP?: string[] | string
  aiModelList?: AIModel[]
  [key: string]: unknown
}

/** 保存 AI 模型参数 */
export interface SaveAIModelParams {
  id: string
  provider: string
  baseUrl: string
  model: string
  apiKey?: string
  temperature?: number
  maxContextTokens?: number
  enableThinking?: boolean
}

/** 审计日志查询参数 */
export interface AuditLogParams {
  page: number
  pageSize: number
  userId?: string
  source?: string
  sqlType?: string
  riskLevel?: string
  keyword?: string
  startTime?: string
  endTime?: string
}

/** 审计日志条目 */
export interface AuditLog {
  id?: string
  sqlText?: string
  [key: string]: unknown
}

/** 审计日志列表结果 */
export interface AuditLogResult {
  data: AuditLog[]
  total: number
}

/** 审计配置 */
export interface AuditConfig {
  [key: string]: unknown
}

/** 查询角色列表，对应 GET /roleList */
export function getRoleList(): Promise<AxiosResponse<ApiResponse<Role[]>>> {
  return http.get('/roleList')
}

/** 查询角色能力配置（AI 开关是否开放），对应 GET /roleCapability */
export function getRoleCapability(): Promise<AxiosResponse<ApiResponse<RoleCapability>>> {
  return http.get('/roleCapability')
}

/** 保存角色，对应 POST /saveRole */
export function saveRole(params: SaveRoleParams): Promise<AxiosResponse<ApiResponse>> {
  // skipGlobalError：409 与参数错误由页面统一处理，避免重复提示
  return http.post('/saveRole', params, { skipGlobalError: true })
}

/** 删除角色，对应 POST /delRole */
export function delRole(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/delRole', null, { params: { id } })
}

/** 查询权限树，对应 GET /permissionTree */
export function getPermissionTree(params: PermissionTreeParams): Promise<AxiosResponse<ApiResponse<PermissionTreeNode[]>>> {
  return http.get('/permissionTree', { params })
}

/** 查询用户列表，对应 GET /findUser */
export function findUser(params: FindUserParams): Promise<AxiosResponse<ApiResponse<User[]>>> {
  return http.get('/findUser', { params })
}

/** 保存用户，对应 POST /saveUser */
export function saveUser(row: User): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/saveUser', row)
}

/** 删除用户，对应 POST /delUser */
export function delUser(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/delUser', null, { params: { id } })
}

/** 搜索用户基础信息，对应 GET /findUserBase */
export function findUserBase(key: string): Promise<AxiosResponse<ApiResponse<UserBase[]>>> {
  return http.get('/findUserBase', { params: { key } })
}

/** 按登录名搜索用户基础信息，对应 GET /findUserBase（使用 loginName 参数） */
export function findUserByLoginName(loginName: string): Promise<AxiosResponse<ApiResponse<UserBase[]>>> {
  return http.get('/findUserBase', { params: { loginName } })
}

/** 获取系统配置，对应 GET /system/config/all/get */
export function getSystemConfig(): Promise<AxiosResponse<ApiResponse<SystemConfig>>> {
  return http.get('/system/config/all/get')
}

/** 保存系统配置，对应 POST /system/config/all/save */
export function saveSystemConfig(config: SystemConfig): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/system/config/all/save', config)
}

/** 保存 AI 模型，对应 POST /system/config/ai/model/save */
export function saveAIModel(params: SaveAIModelParams): Promise<AxiosResponse<ApiResponse<AIModel>>> {
  return http.post('/system/config/ai/model/save', params)
}

/** 删除 AI 模型，对应 POST /system/config/ai/model/delete */
export function deleteAIModel(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/system/config/ai/model/delete', { id })
}

/** 选择 AI 模型，对应 POST /system/config/ai/model/select */
export function selectAIModel(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/system/config/ai/model/select', { id })
}

/** 测试外部用户配置，对应 POST /system/config/outterUser/test */
export function testOutterUser(url: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/system/config/outterUser/test', { url })
}

/** 查询审计日志，对应 GET /audit/logs */
export function getAuditLogs(params: AuditLogParams): Promise<AxiosResponse<AuditLogResult>> {
  return http.get('/audit/logs', { params })
}

/** 获取审计配置，对应 GET /audit/config/get */
export function getAuditConfig(): Promise<AxiosResponse<ApiResponse<AuditConfig>>> {
  return http.get('/audit/config/get')
}

/** 保存审计配置，对应 POST /audit/config/save */
export function saveAuditConfig(config: AuditConfig): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/audit/config/save', config)
}

/** 用户配置导出结果 */
export interface UserConfigExportData {
  filename: string
  content: string
}

/**
 * 导出用户配置（系统设置、提示词、连接、审计配置、收藏）为 SQL 文件并下载。
 * 后端返回标准 {code,data} 信封，前端取出 data 后触发浏览器下载。
 */
export async function exportUserConfigSql(): Promise<void> {
  const resp = await http.get<UserConfigExportData | ApiResponse<UserConfigExportData>>('/system/config/export')
  const payload = (resp as AxiosResponse<ApiResponse<UserConfigExportData>>).data
  const data = payload?.data ?? (payload as unknown as UserConfigExportData)
  downloadBlob(data.content, data.filename, 'application/sql;charset=utf-8')
}
