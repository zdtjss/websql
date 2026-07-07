import { api } from './adapter'
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

/**
 * 调用 AI 推断表关系。
 * 不持久化到数据库，仅返回推断结果供当前会话可视化使用。
 * skipGlobalError: AI 未配置等业务错误由调用方 handleError 统一展示，避免双弹窗
 */
export function analyzeERRelations(params: ERAnalyzeRequest): Promise<AxiosResponse<ApiResponse<ERAnalyzeResponse>>> {
  return api.request<ERAnalyzeResponse>({
    method: 'POST',
    url: '/er/analyzeRelations',
    body: params,
    skipGlobalError: true,
  })
}
