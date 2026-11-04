/**
 * ER 关系推断（纯函数，便于单测）
 *
 * 基数口径（确认报告 5.2 / F-16）：
 * - 1:1 的判断看「源端」：存在唯一键 K，且 K 是外键列组的子集（K ⊆ fkCols，忽略列顺序）。
 * - 不再看目标列：目标列通常就是父表主键/唯一键，用它判断会把绝大多数 1:N 误标为 1:1。
 * - 复合外键必须按约束成组，逐列判断会拆散列组。
 * - 纯连接表（≥2 个外键约束，且主键列全部是外键列）标记 junction，由节点徽标展示，
 *   边本身仍按源端唯一性给出 1:N / 1:1，不再给每条边标 N:M。
 */
import type { ERRelation, ERRelationType, ERTable, ERTableColumn } from './types'

/** 仅需字段与唯一键组即可计算唯一性（兼容节点数据 / AI 请求数据） */
type UniqueKeySource = {
  columns?: ERTableColumn[]
  uniqueKeys?: string[][]
}

/** 关系的正向键（表名 + 字段） */
export function relationKey(r: Pick<ERRelation, 'source' | 'sourceCol' | 'target' | 'targetCol'>): string {
  return `${r.source}.${r.sourceCol || ''}->${r.target}.${r.targetCol || ''}`
}

/** 关系的反向键，用于避免 AI 结果与已有关系反向重复 */
export function reverseRelationKey(r: Pick<ERRelation, 'source' | 'sourceCol' | 'target' | 'targetCol'>): string {
  return `${r.target}.${r.targetCol || ''}->${r.source}.${r.sourceCol || ''}`
}

/** 约束分组键：优先约束名，其次 SQLite constraintId，最后退化为字段映射 */
export function constraintGroupKey(r: ERRelation): string {
  const constraint = r.constraintName || r.constraintId || relationKey(r)
  return `${r.source}::${constraint}`
}

/**
 * 表的唯一键组（含主键）。
 * 后端 uniqueKeys 是事实源；同时从 columns.primaryKey 补一条主键组，
 * 兼容旧数据 / AI 请求里的表结构（只带 primaryKey 标记）。
 */
export function uniqueKeysOf(table: UniqueKeySource | undefined): string[][] {
  if (!table) return []
  const keys: string[][] = []
  const seen = new Set<string>()
  const add = (cols: string[]) => {
    const normalized = cols.map(c => (c || '').trim()).filter(Boolean)
    if (normalized.length === 0) return
    const sig = normalized.join('\u0000')
    if (seen.has(sig)) return
    seen.add(sig)
    keys.push(normalized)
  }
  for (const key of table.uniqueKeys || []) add(key)
  add((table.columns || []).filter(c => c.primaryKey).map(c => c.name))
  return keys
}

/**
 * 源表外键列组是否被某个唯一键覆盖。
 * 语义为「存在唯一键 K，K ⊆ fkCols」；列顺序无关。
 */
export function isUniqueColumnSet(table: ERTable | undefined, fkCols: string[]): boolean {
  if (!table || fkCols.length === 0) return false
  const fkSet = new Set(fkCols.map(c => c.trim()).filter(Boolean))
  if (fkSet.size === 0) return false
  return uniqueKeysOf(table).some(key => key.every(col => fkSet.has(col)))
}

/** 按外键约束分组，保持输入顺序 */
export function groupRelationsByConstraint(relations: ERRelation[]): Map<string, ERRelation[]> {
  const groups = new Map<string, ERRelation[]>()
  for (const relation of relations) {
    const key = constraintGroupKey(relation)
    const group = groups.get(key)
    if (group) group.push(relation)
    else groups.set(key, [relation])
  }
  return groups
}

/** 纯连接表判定：≥2 个外键约束，且主键列全部出现在外键源列中 */
export function isJunctionTable(table: ERTable | undefined, groups: Map<string, ERRelation[]>): boolean {
  if (!table) return false
  const sourceGroups = [...groups.values()].filter(group => group.length > 0 && group[0].source === table.name)
  if (sourceGroups.length < 2) return false
  const pkCols = (table.columns || []).filter(c => c.primaryKey).map(c => c.name)
  if (pkCols.length === 0) return false
  const fkCols = new Set(sourceGroups.flatMap(group => group.map(r => r.sourceCol).filter(Boolean)))
  return pkCols.every(col => fkCols.has(col))
}

/**
 * 为物理外键推断关系类型，并标记 junction 表。
 * AI / 手动关系的类型由来源给定，不参与推断。
 */
export function inferRelationTypes(tables: ERTable[], relations: ERRelation[]): ERRelation[] {
  const tableMap = new Map(tables.map(table => [table.name, table]))
  const fkRelations = relations.filter(r => !r.origin || r.origin === 'fk')
  const groups = groupRelationsByConstraint(fkRelations)

  for (const table of tables) {
    table.junction = isJunctionTable(tableMap.get(table.name), groups)
  }

  for (const group of groups.values()) {
    if (group.length === 0) continue
    const source = tableMap.get(group[0].source)
    const fkCols = group.map(r => r.sourceCol).filter(Boolean)
    const type: ERRelationType = isUniqueColumnSet(source, fkCols) ? '1:1' : '1:N'
    for (const relation of group) {
      relation.relationType = type
    }
  }
  return relations
}

/** 关系展示文案：箭头方向为子表 → 父表，因此 1:N 展示为 N:1（子:父） */
export function displayRelationType(type: ERRelationType | undefined): string {
  if (type === '1:1') return '1:1'
  if (type === 'N:M') return 'N:M'
  return 'N:1'
}

export interface MergeAiResult {
  accepted: ERRelation[]
  skipped: number
}

function normalizeType(type: string | undefined): ERRelationType {
  const value = (type || '').toUpperCase()
  if (value === '1:1') return '1:1'
  if (value === 'N:M' || value === 'M:N') return 'N:M'
  return '1:N'
}

function hasColumn(table: ERTable, name: string): boolean {
  const target = name.trim()
  if (!target) return false
  return (table.columns || []).some(c => c.name === target || c.name.toLowerCase() === target.toLowerCase())
}

/**
 * 合并 AI 推断结果：
 * - 端点表必须属于本次实际发送给 AI 的表集合（与后端 maxTables 截断口径一致）；
 * - 字段若填写必须存在；
 * - 过滤自环、与已有关系同向/反向重复的边。
 *
 * 调用方需在合并前清除旧的 origin='ai' 边，避免二次分析叠加。
 */
export function mergeAiRelations(
  inferred: ERRelation[],
  sentTables: ERTable[],
  existingRelations: ERRelation[]
): MergeAiResult {
  const tableMap = new Map(sentTables.map(table => [table.name, table]))
  const tableByLower = new Map(sentTables.map(table => [table.name.toLowerCase(), table]))
  const keys = new Set<string>()
  for (const relation of existingRelations) {
    keys.add(relationKey(relation))
    keys.add(reverseRelationKey(relation))
  }

  const accepted: ERRelation[] = []
  let skipped = 0
  for (const raw of inferred) {
    const source = (raw.source || '').trim()
    const target = (raw.target || '').trim()
    const sourceCol = (raw.sourceCol || '').trim()
    const targetCol = (raw.targetCol || '').trim()
    const sourceTable = tableMap.get(source) || tableByLower.get(source.toLowerCase())
    const targetTable = tableMap.get(target) || tableByLower.get(target.toLowerCase())
    if (!sourceTable || !targetTable || sourceTable.name === targetTable.name) {
      skipped++
      continue
    }
    if (sourceCol && sourceTable.columns.length > 0 && !hasColumn(sourceTable, sourceCol)) {
      skipped++
      continue
    }
    if (targetCol && targetTable.columns.length > 0 && !hasColumn(targetTable, targetCol)) {
      skipped++
      continue
    }
    const relation: ERRelation = {
      source: sourceTable.name,
      sourceCol,
      target: targetTable.name,
      targetCol,
      constraintName: '',
      relationType: normalizeType(raw.relationType),
      origin: 'ai',
      confidence: raw.confidence || 'medium',
      reason: raw.reason || '',
    }
    const key = relationKey(relation)
    const reverse = reverseRelationKey(relation)
    if (keys.has(key) || keys.has(reverse)) {
      skipped++
      continue
    }
    keys.add(key)
    keys.add(reverse)
    accepted.push(relation)
  }
  return { accepted, skipped }
}

/** 移除旧 AI 关系，保留物理外键与手动关系 */
export function removeAiRelations(relations: ERRelation[]): ERRelation[] {
  return relations.filter(r => r.origin !== 'ai')
}
