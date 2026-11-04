/**
 * ER 图数据加载器：统一走 POST /er/schema。
 *
 * 前端不再拼接 information_schema / PRAGMA SQL，也不再依赖 ASCII 标识符白名单；
 * 方言差异、权限过滤、批量取数由服务端完成，中文 schema/表名可正常加载。
 */
import { getERSchema } from '@/api/er'
import type { ERRelation, ERTable, ERTableColumn } from './types'

export interface LoadedERSchema {
  dbType: string
  schema: string
  tables: ERTable[]
  relations: ERRelation[]
  totalTables: number
}

/** 单次请求的页大小；0 表示不分页（默认一次取全，满足“一次取数”验收） */
const PAGE_LIMIT = 0
/** 防御性上限：即使服务端返回游标，也不无限拉取 */
const MAX_PAGES = 20

function normalizeColumn(raw: Partial<ERTableColumn> | undefined): ERTableColumn {
  return {
    name: raw?.name || '',
    type: raw?.type || '',
    comment: raw?.comment || '',
    primaryKey: !!raw?.primaryKey,
  }
}

function normalizeTable(raw: Partial<ERTable>): ERTable {
  return {
    name: raw.name || '',
    type: raw.type === 'VIEW' ? 'VIEW' : 'TABLE',
    comment: raw.comment || '',
    columns: (raw.columns || []).map(normalizeColumn),
    uniqueKeys: (raw.uniqueKeys || []).filter(key => Array.isArray(key) && key.length > 0),
  }
}

function normalizeRelation(raw: Partial<ERRelation>): ERRelation {
  return {
    source: raw.source || '',
    sourceCol: raw.sourceCol || '',
    target: raw.target || '',
    targetCol: raw.targetCol || '',
    constraintName: raw.constraintName || '',
    constraintId: raw.constraintId || '',
    relationType: raw.relationType,
    origin: raw.origin,
  }
}

/**
 * 加载整库 ER 元数据。
 * 需要分页的 schema 会按 nextCursor 继续拉取，但 HTTP 请求数为 1（未分页时）。
 */
export async function loadERSchema(params: { connId: string; schema: string }): Promise<LoadedERSchema> {
  const tables: ERTable[] = []
  const relations: ERRelation[] = []
  let cursor = ''
  let dbType = ''
  let totalTables = 0
  let pages = 0

  while (true) {
    const resp = await getERSchema({
      connId: params.connId,
      schema: params.schema,
      cursor,
      limit: PAGE_LIMIT,
    })
    const data = resp?.data?.data
    if (!data) throw new Error('ER 元数据响应为空')

    dbType = data.dbType || dbType
    totalTables = data.totalTables ?? totalTables
    tables.push(...(data.tables || []).map(normalizeTable).filter(t => t.name))
    relations.push(...(data.relations || []).map(normalizeRelation).filter(r => r.source && r.target))
    cursor = data.nextCursor || ''
    pages++
    if (!cursor || pages >= MAX_PAGES) break
  }

  return {
    dbType,
    schema: params.schema,
    tables,
    relations,
    totalTables,
  }
}
