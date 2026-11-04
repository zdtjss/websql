/** ER 面板共享类型（供 SFC 与容器复用） */

export interface RelationRow {
  key: string
  /** 展示文案，如 N:1（子:父） */
  relationType: string
  /** 样式用类型键：11 / 1n / nm */
  typeKey: string
  origin: 'fk' | 'ai' | 'manual'
  source: string
  target: string
  sourceCol: string
  targetCol: string
  confidence?: string
  constraintName?: string
}

export interface LegendItem {
  key: string
  label: string
  color: string
  count: number
  active: boolean
}
