/**
 * 数据库元数据加载与同步比较相关的公共工具函数
 *
 * 本文件抽取自 SchemaCompareDialog.vue / DataSyncDialog.vue / GlobalSearchDialog.vue
 * 三个组件中重复的元数据加载、响应解包、差异统计、FormData 构造等逻辑，
 * 以提升可维护性并保证错误处理一致。
 *
 * 说明：表结构（列）的实际加载与比较在后端 /sync/compareSchema 接口完成，
 * 前端只负责展示后端返回的 columnDiffs/indexDiffs，因此本文件不提供
 * loadTableColumns / compareTableColumns 等未被调用的函数，避免产生死代码。
 */
import type { AxiosResponse } from 'axios'
import { listConn, getSyncTargets } from '@/api/conn'
import type { Connection, ListConnParams } from '@/api/conn'
import { handleError } from './errorHandler'

/** Schema/数据比较场景下后端返回的差异条目类型 */
export type SchemaDiffType = 'ADD' | 'DROP' | 'MODIFY'

/** Schema 比较返回的单表差异结构（与后端 /sync/compareSchema 返回一致） */
export interface SchemaDiff {
  tableName: string
  diffType: SchemaDiffType
  columnDiffs?: Array<{ columnName: string; diffType: string; alterStatement?: string; sourceDef?: string }>
  indexDiffs?: Array<{ indexName: string; diffType: string; alterStatement?: string }>
  sourceDDL?: string
  targetDDL?: string
}

/** getSyncTargets 返回的同步目标信息（schemas + tables） */
export interface SyncTargets {
  schemas: string[]
  tables: string[]
  [key: string]: unknown
}

/**
 * 解包 axios 响应：兼容 `{ data: { data: ... } }` 与 `{ data: ... }` 两种结构。
 * 各组件原本散落 `res.data.data || res.data` 写法 18+ 处，统一抽取后便于维护。
 */
export function unwrapResponse<T = any>(res: AxiosResponse): T {
  const body = (res as any)?.data
  if (body && typeof body === 'object' && 'data' in body) {
    return (body.data as T) ?? (body as T)
  }
  return body as T
}

/**
 * 加载数据库连接列表。
 *
 * 抽取自 SchemaCompareDialog.onOpen / DataSyncDialog.loadConnections /
 * GlobalSearchDialog.init 三处近乎一致的实现。
 *
 * 注意：原三处实现是否过滤缺少 id 的项并不一致（SchemaCompareDialog 过滤，
 * 其余两处不过滤），为保持功能完全不变，本函数不做过滤，由调用方按需处理。
 *
 * @param params  透传给 listConn 的查询参数，默认拉取较大分页以保证一次性加载全部
 * @param context 错误提示上下文，默认"加载连接列表"
 */
export async function loadConnections(
  params: ListConnParams = { pageSize: 9999 },
  context = '加载连接列表',
): Promise<Connection[]> {
  try {
    const res = await listConn(params)
    // 不同调用点历史上有两种解包路径：SchemaCompare 直接取数组，其余取 result.data
    const body = (res as any)?.data
    let list: Connection[] = []
    if (Array.isArray(body)) {
      list = body
    } else if (body && Array.isArray(body.data)) {
      list = body.data
    } else if (body && body.data && Array.isArray(body.data.data)) {
      list = body.data.data
    }
    return list
  } catch (e) {
    handleError(e, context)
    return []
  }
}

/**
 * 加载指定连接下的同步目标信息（schemas + tables）。
 *
 * 抽取自 SchemaCompareDialog.onSourceConnChange/onTargetConnChange 与
 * DataSyncDialog.onSourceConnChange/onTargetConnChange 四处近乎一致的实现。
 *
 * @param connId  连接 ID
 * @param context 错误提示上下文
 */
export async function loadSyncTargets(connId: string, context = '加载库信息'): Promise<SyncTargets> {
  if (!connId) return { schemas: [], tables: [] }
  try {
    const res = await getSyncTargets(connId)
    const result = unwrapResponse<Partial<SyncTargets>>(res) || {}
    return {
      schemas: result.schemas || [],
      tables: result.tables || [],
    }
  } catch (e) {
    handleError(e, context)
    return { schemas: [], tables: [] }
  }
}

/**
 * 统计指定差异类型的表数量。
 *
 * 抽取自 SchemaCompareDialog 与 DataSyncDialog 中重复出现的
 * `schemaDiffs.filter(d => d.diffType === 'ADD').length` 等三连计算。
 */
export function countDiffsByType(diffs: SchemaDiff[], diffType: SchemaDiffType): number {
  return (diffs || []).filter((d) => d.diffType === diffType).length
}

/**
 * 同步接口 FormData 的可选项字段。
 * sourceConnId/targetConnId/sourceSchema/targetSchema 为必填，其余为可选。
 */
export interface SyncFormDataOptions {
  sourceConnId: string
  targetConnId: string
  sourceSchema: string
  targetSchema: string
  /** 单表数据比较/同步时的表名 */
  table?: string
  /** 分块大小 */
  chunkSize?: number | string
  /** 分块索引 */
  chunkIndex?: number | string
  /** 同步方向 */
  direction?: string
  /** 是否生成 SQL */
  generateSQL?: string
  /** 冲突处理策略 */
  conflictStrategy?: string
  /** 结构比较时过滤的表名列表（逗号拼接） */
  tables?: string
  /** 任意额外字段 */
  [key: string]: unknown
}

/**
 * 构造同步相关接口使用的 FormData。
 *
 * SchemaCompareDialog.startCompare / DataSyncDialog.startStructureCompare /
 * startDataCompareChunked / loadSyncSQL / runDryRun 等多处都重复地
 * `new FormData()` 后 append 同样几个字段，统一抽取避免遗漏。
 *
 * 仅追加非空字段，保证不改变后端接收行为。
 */
export function buildSyncFormData(options: SyncFormDataOptions): FormData {
  const formData = new FormData()
  const keys: (keyof SyncFormDataOptions)[] = [
    'sourceConnId',
    'targetConnId',
    'sourceSchema',
    'targetSchema',
    'table',
    'chunkSize',
    'chunkIndex',
    'direction',
    'generateSQL',
    'conflictStrategy',
    'tables',
  ]
  for (const key of keys) {
    const v = options[key]
    if (v !== undefined && v !== null && v !== '') {
      formData.append(key as string, String(v))
    }
  }
  // 透传其他未在白名单中的额外字段
  for (const k of Object.keys(options)) {
    if (keys.indexOf(k as keyof SyncFormDataOptions) !== -1) continue
    const v = (options as Record<string, unknown>)[k]
    if (v !== undefined && v !== null && v !== '') {
      formData.append(k, String(v))
    }
  }
  return formData
}

/**
 * 根据 Schema 差异列表生成结构同步 SQL 脚本。
 *
 * DataSyncDialog.generatedSQL 计算属性与 executeStructureSync 内部各有一份
 * 几乎相同的 reduce 逻辑（遍历 columnDiffs/indexDiffs 拼接 alterStatement），
 * 统一抽取避免双份维护。
 */
export function buildSchemaDiffSQL(diffs: SchemaDiff[]): string {
  let sql = ''
  for (const d of diffs || []) {
    if (d.columnDiffs) {
      for (const cd of d.columnDiffs) {
        if (cd.alterStatement) sql += cd.alterStatement + '\n'
      }
    }
    if (d.indexDiffs) {
      for (const id of d.indexDiffs) {
        if (id.alterStatement) sql += id.alterStatement + '\n'
      }
    }
  }
  return sql
}
