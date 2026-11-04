import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'

/** AI 推断的关系（与后端 modeler.AnalyzeRelation 对应） */
export interface ERRelation {
  source: string
  sourceCol: string
  target: string
  targetCol: string
  relationType: '1:1' | '1:N' | 'N:M'
  confidence: 'high' | 'medium' | 'low'
  reason: string
}

export interface ERAnalyzeTableColumn {
  name: string
  type: string
  comment: string
  primaryKey: boolean
  unique: boolean
}

export interface ERAnalyzeTable {
  name: string
  comment: string
  columns: ERAnalyzeTableColumn[]
}

export interface ERAnalyzeRequest {
  connId: string
  schema: string
  dbType: string
  tables: ERAnalyzeTable[]
  existingRelations?: ERRelation[]
}

export interface ERAnalyzeResponse {
  relations: ERRelation[]
}

/** ER 图统一元数据：字段（唯一性以 uniqueKeys 为事实源） */
export interface ERSchemaColumn {
  name: string
  type: string
  comment: string
  primaryKey: boolean
}

/** ER 图统一元数据：表/视图 */
export interface ERSchemaTable {
  name: string
  type: 'TABLE' | 'VIEW'
  comment: string
  columns: ERSchemaColumn[]
  uniqueKeys: string[][]
}

/** ER 图统一元数据：外键关系（SQLite 无约束名时用 constraintId 成组） */
export interface ERSchemaRelation {
  source: string
  sourceCol: string
  target: string
  targetCol: string
  constraintName: string
  constraintId?: string
}

export interface ERSchemaData {
  dbType: string
  schema: string
  tables: ERSchemaTable[]
  relations: ERSchemaRelation[]
  totalTables: number
  loadedTableCount: number
  nextCursor: string
}

export interface ERSchemaRequest {
  connId: string
  schema: string
  /** 上一页最后一张表名；空表示从第一页开始 */
  cursor?: string
  /** 0 或不传表示返回全部可见表 */
  limit?: number
}

/**
 * 拉取 ER 图元数据（服务端完成方言适配、表级权限过滤与批量取数）。
 * 只读接口，不写库、不落审计。
 */
export function getERSchema(params: ERSchemaRequest): Promise<AxiosResponse<ApiResponse<ERSchemaData>>> {
  return http.post('/er/schema', params, { skipGlobalError: true })
}

/**
 * 调用 AI 推断表关系。
 * 不持久化到数据库，仅返回推断结果供当前会话可视化使用。
 * skipGlobalError: AI 未配置等业务错误由调用方 handleError 统一展示，避免双弹窗
 */
export function analyzeERRelations(params: ERAnalyzeRequest): Promise<AxiosResponse<ApiResponse<ERAnalyzeResponse>>> {
  return http.post('/er/analyzeRelations', params, { skipGlobalError: true })
}
