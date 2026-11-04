/** ER 图前端数据类型（与后端 /er/schema 响应保持一致） */

export type ERRelationType = '1:1' | '1:N' | 'N:M'
export type ERRelationOrigin = 'fk' | 'ai' | 'manual'

export interface ERTableColumn {
  name: string
  type: string
  comment: string
  primaryKey: boolean
}

export interface ERTable {
  name: string
  type: 'TABLE' | 'VIEW'
  comment: string
  columns: ERTableColumn[]
  /** 唯一键组（含主键），用于基数推断；缺失时由 infer 从 primaryKey 派生 */
  uniqueKeys?: string[][]
  /** 纯连接表（复合主键全部为外键列），仅用于渲染徽标 */
  junction?: boolean
}

export interface ERRelation {
  source: string
  sourceCol: string
  target: string
  targetCol: string
  /** 外键约束名；SQLite 为空 */
  constraintName: string
  /** SQLite PRAGMA foreign_key_list 的 id，用于复合外键成组 */
  constraintId?: string
  relationType?: ERRelationType
  origin?: ERRelationOrigin
  /** AI 推断置信度 */
  confidence?: 'high' | 'medium' | 'low'
  reason?: string
  /** 前端合成主键，用于关系面板列表 key（不参与比较） */
  key?: string
}
