import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'
import type { AIModel } from './ai'

/** 角色信息 */
export interface Role {
  id: string
  name: string
  viewClassic?: number
  allowModify?: number
  powerList?: RolePower[]
  [key: string]: unknown
}

/** 角色权限项 */
export interface RolePower {
  connId?: string
  schemaName?: string
  tableName?: string
  columnName?: string
  [key: string]: unknown
}

/** 保存角色参数 */
export interface SaveRoleParams {
  id: string
  name: string
  addPowers: unknown[]
  delPowers: unknown[]
  viewClassic: number
  allowModify: number
  [key: string]: unknown
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

/** 保存角色，对应 POST /saveRole */
export function saveRole(params: SaveRoleParams): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/saveRole', params)
}

/** 删除角色，对应 GET /delRole */
export function delRole(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.get('/delRole', { params: { id } })
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

/** 删除用户，对应 GET /delUser */
export function delUser(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.get('/delUser', { params: { id } })
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
