import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'
import { downloadBlob } from '@/utils/exportHelper'

/** SQL 收藏条目 */
export interface SqlSnippet {
  id?: string
  userId?: string
  title: string
  description?: string
  sqlContent: string
  category?: string
  /** 标签，逗号分隔字符串 */
  tags?: string
  dbType?: string
  connId?: string
  schemaName?: string
  createdAt?: string
  updatedAt?: string
}

/** 新增/更新收藏参数 */
export interface SaveSnippetParams {
  id?: string
  title: string
  description?: string
  sqlContent: string
  category?: string
  tags?: string
  dbType?: string
  connId?: string
  schemaName?: string
}

/** 列表查询参数 */
export interface ListSnippetParams {
  keyword?: string
  category?: string
  tag?: string
}

/** 列表响应 */
export interface ListSnippetResult {
  items: SqlSnippet[]
  total: number
}

/** 分类统计项 */
export interface SnippetCategoryStat {
  name: string
  count: number
}

/** 导出数据单条 */
export interface SnippetExportItem {
  title: string
  description?: string
  sqlContent: string
  category?: string
  tags?: string
  dbType?: string
  connId?: string
  schemaName?: string
  createdAt?: string
  updatedAt?: string
}

/** 导出数据根结构 */
export interface SnippetExportData {
  exportedAt: string
  count: number
  items: SnippetExportItem[]
}

/** 导入请求体 */
export interface ImportSnippetReq {
  items: SnippetExportItem[]
}

/** 查询收藏列表，对应 GET /snippet/list */
export function listSnippets(params: ListSnippetParams): Promise<AxiosResponse<ApiResponse<ListSnippetResult>>> {
  return http.get('/snippet/list', { params })
}

/** 查询分类统计，对应 GET /snippet/categories */
export function listSnippetCategories(): Promise<AxiosResponse<ApiResponse<SnippetCategoryStat[]>>> {
  return http.get('/snippet/categories')
}

/** 查询全部标签，对应 GET /snippet/tags */
export function listSnippetTags(): Promise<AxiosResponse<ApiResponse<string[]>>> {
  return http.get('/snippet/tags')
}

/** 新增/更新收藏，对应 POST /snippet/save */
export function saveSnippet(params: SaveSnippetParams): Promise<AxiosResponse<ApiResponse<SqlSnippet>>> {
  return http.post('/snippet/save', params)
}

/** 删除收藏，对应 POST /snippet/delete */
export function deleteSnippet(id: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/snippet/delete', null, { params: { id } })
}

/** 导入收藏，对应 POST /snippet/import */
export function importSnippets(items: SnippetExportItem[]): Promise<AxiosResponse<ApiResponse<{ count: number }>>> {
  return http.post('/snippet/import', { items } as ImportSnippetReq)
}

/**
 * 导出当前用户全部收藏为 JSON 文件下载。
 * 后端返回标准 {code,data} 信封，前端取出 data 后触发浏览器下载。
 */
export async function exportSnippetsToFile(): Promise<void> {
  const resp = await http.get<SnippetExportData | ApiResponse<SnippetExportData>>('/snippet/export')
  // 兼容拦截器返回的响应体：标准信封在 resp.data.data，直接 blob 则在 resp.data
  const payload = (resp as AxiosResponse<ApiResponse<SnippetExportData>>).data
  const exportData = payload?.data ?? (payload as unknown as SnippetExportData)
  const json = JSON.stringify(exportData, null, 2)
  const ts = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
  downloadBlob(json, `sql-snippets-${ts}.json`, 'application/json')
}
