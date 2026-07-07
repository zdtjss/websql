import { api } from './adapter'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'
import type { SSEStreamHandle, SSEOptions } from './sse'

// 重新导出 SSE 流式工具
export { streamSSE, type SSEStreamHandle, type SSEOptions } from './sse'

/** 提示词信息 */
export interface Prompt {
  id: string
  title?: string
  content?: string
  createdBy?: string
  currentUserId?: string
  isRolePrompt?: boolean
  isShared?: boolean
  connSchemas?: unknown
  tables?: unknown
  [key: string]: unknown
}

/** 提示词列表查询参数 */
export interface PromptListParams {
  tab: string
  page: number
  pageSize: number
  keyword?: string
}

/** 提示词列表结果 */
export interface PromptListResult {
  items: Prompt[]
  total: number
}

/** 角色（提示词管理用） */
export interface RoleBase {
  id: string
  name: string
  [key: string]: unknown
}

/** AI 模型配置 */
export interface AIModel {
  id: string
  provider: string
  baseUrl: string
  model: string
  apiKey?: string
  temperature?: number
  maxContextTokens?: number
  enableThinking?: boolean
  [key: string]: unknown
}

/** AI 模型列表响应 */
export interface AIModelsResult {
  aiModelList: AIModel[]
  selectedModelId: string
}

/** AI 配置测试参数 */
export interface AIConfigTestParams {
  provider: string
  baseUrl: string
  model: string
  apiKey?: string
}

/** chatStream schema 引用 */
export interface ChatSchemaRef {
  connId: string
  schema: string
}

/** chatStream 请求参数 */
export interface ChatStreamParams {
  sessionId: string
  connId: string
  schema: string
  schemas: ChatSchemaRef[]
  question: string
  tableContext: unknown[]
  modelId: string
  excelData?: unknown
}

/** chatStream 回调 */
export interface ChatStreamCallbacks {
  onMessage: (data: string) => void
  onError?: (error: Error) => void
  onDone?: () => void
  signal?: AbortSignal
}

/** 历史会话条目 */
export interface ChatSession {
  id?: string
  sessionId?: string
  title?: string
  [key: string]: unknown
}

/** 历史会话列表结果 */
export interface ChatSessionListResult {
  sessions: ChatSession[]
  total: number
}

/** 历史会话查询参数 */
export interface SessionListParams {
  page: number
  pageSize: number
  keyword?: string
}

/** 查询提示词列表，对应 GET /promptList */
export function getPromptList(params: PromptListParams): Promise<AxiosResponse<ApiResponse<PromptListResult>>> {
  return api.request<PromptListResult>({ method: 'GET', url: '/promptList', params })
}

/** 删除提示词，对应 POST /delPrompt */
export function delPrompt(id: string): Promise<AxiosResponse<ApiResponse>> {
  return api.request({ method: 'POST', url: '/delPrompt', params: { id } })
}

/** 查询角色列表（提示词管理用），对应 GET /roleBaseList */
export function getRoleBaseList(): Promise<AxiosResponse<ApiResponse<RoleBase[]>>> {
  return api.request<RoleBase[]>({ method: 'GET', url: '/roleBaseList' })
}

/** 按角色查询提示词列表，对应 GET /promptListByRole */
export function getPromptListByRole(roleId: string): Promise<AxiosResponse<ApiResponse<Prompt[]>>> {
  return api.request<Prompt[]>({ method: 'GET', url: '/promptListByRole', params: { roleId } })
}

/** 获取 AI 模型列表，对应 GET /system/config/ai/models */
export function getAIModels(): Promise<AxiosResponse<ApiResponse<AIModelsResult>>> {
  return api.request<AIModelsResult>({ method: 'GET', url: '/system/config/ai/models' })
}

/** 测试 AI 配置连通性，对应 POST /ai/config/test */
export function testAIConfig(params: AIConfigTestParams): Promise<AxiosResponse<ApiResponse>> {
  return api.request({ method: 'POST', url: '/ai/config/test', body: params })
}

/**
 * AI 对话流式接口，对应 POST /ai/agent/chatStream
 * 基于 streamSSE 封装，返回流控制句柄
 */
export function chatStream(params: ChatStreamParams, callbacks: ChatStreamCallbacks): SSEStreamHandle {
  return api.streamSSE({
    url: '/ai/agent/chatStream',
    body: params,
    onMessage: callbacks.onMessage,
    onError: callbacks.onError,
    onDone: callbacks.onDone,
    signal: callbacks.signal,
  })
}

/** 查询历史会话列表，对应 GET /ai/agent/sessions */
export function getChatSessions(params: SessionListParams): Promise<AxiosResponse<ChatSessionListResult>> {
  return api.request<ChatSessionListResult>({ method: 'GET', url: '/ai/agent/sessions', params })
}

/** 删除历史会话，对应 POST /ai/agent/session/delete */
export function deleteChatSession(sessionId: string): Promise<AxiosResponse<ApiResponse>> {
  return api.request({ method: 'POST', url: '/ai/agent/session/delete', params: { sessionId } })
}

/** 加载历史会话详情，对应 GET /ai/agent/session */
export function getChatSession(sessionId: string): Promise<AxiosResponse<ApiResponse<unknown>>> {
  return api.request<unknown>({ method: 'GET', url: '/ai/agent/session', params: { sessionId } })
}

/** 上传 Excel 文件，对应 POST /ai/agent/uploadExcel */
export function uploadExcel(formData: FormData, signal?: AbortSignal): Promise<AxiosResponse<ApiResponse<unknown>>> {
  return api.request<unknown>({ method: 'POST', url: '/ai/agent/uploadExcel', body: formData, signal })
}

/** 预匹配 Excel 列，对应 POST /ai/agent/preMatchColumns */
export function preMatchColumns(formData: FormData, signal?: AbortSignal): Promise<AxiosResponse<ApiResponse<unknown>>> {
  return api.request<unknown>({ method: 'POST', url: '/ai/agent/preMatchColumns', body: formData, signal })
}

/** 获取提示词详情，对应 GET /promptDetail */
export function getPromptDetail(id: string): Promise<AxiosResponse<ApiResponse<unknown>>> {
  return api.request<unknown>({ method: 'GET', url: '/promptDetail', params: { id } })
}

/** 保存提示词，对应 POST /savePrompt */
export function savePrompt(form: unknown): Promise<AxiosResponse<ApiResponse>> {
  return api.request({ method: 'POST', url: '/savePrompt', body: form })
}
