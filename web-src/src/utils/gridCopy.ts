/**
 * 结果网格复制格式。
 *
 * 背景：原实现只支持「复制选中区域为 TSV」，粘贴到 Excel 之外的地方（文档、代码、SQL 脚本）
 * 都要手工整理。这里集中实现多种格式，纯函数、可单测，避免在各组件里重复拼接。
 */

export type GridCopyFormat = 'tsv' | 'csv' | 'json' | 'insert' | 'markdown'

/** 复制单元格时用空串表示 NULL（TSV/CSV/Markdown 保持一致，避免与字符串 "NULL" 混淆） */
const NULL_TEXT = ''

export function formatCellText(value: unknown): string {
  if (value === null || value === undefined) return NULL_TEXT
  return String(value)
}

/** TSV：与 Excel / 文本编辑器互通，制表符分隔、换行分行 */
function toTsv(columns: string[], rows: Record<string, any>[]): string {
  return rows.map((row) => columns.map((c) => formatCellText(row[c])).join('\t')).join('\n')
}

/** CSV：按 RFC4180 需要时加引号 */
function toCsv(columns: string[], rows: Record<string, any>[], withHeader = true): string {
  const esc = (v: string) => (/[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)
  const lines: string[] = []
  if (withHeader) lines.push(columns.map(esc).join(','))
  for (const row of rows) {
    lines.push(columns.map((c) => esc(formatCellText(row[c]))).join(','))
  }
  return lines.join('\n')
}

/** JSON：对象数组，保持列顺序 */
function toJson(columns: string[], rows: Record<string, any>[]): string {
  const picked = rows.map((row) => {
    const obj: Record<string, any> = {}
    for (const c of columns) obj[c] = row[c] ?? null
    return obj
  })
  return JSON.stringify(picked, null, 2)
}

/** Markdown 表格：管道符与换行需要转义，否则会破坏表格结构 */
function toMarkdown(columns: string[], rows: Record<string, any>[]): string {
  const esc = (v: string) => v.replace(/\|/g, '\\|').replace(/\r?\n/g, '<br>')
  const header = `| ${columns.map(esc).join(' | ')} |`
  const divider = `| ${columns.map(() => '---').join(' | ')} |`
  const body = rows.map((row) => `| ${columns.map((c) => esc(formatCellText(row[c]))).join(' | ')} |`)
  return [header, divider, ...body].join('\n')
}

/** 标识符引用：Oracle 用双引号，其余用反引号 */
function quoteIdent(name: string, dbType: string): string {
  if (String(dbType).toLowerCase() === 'oracle') {
    return `"${name.replace(/"/g, '""')}"`
  }
  return '`' + name.replace(/`/g, '``') + '`'
}

/** SQL 字面量：NULL / 数字 / 布尔原样，字符串转义单引号（MySQL 额外转义反斜杠） */
export function toSqlLiteral(value: unknown, dbType: string): string {
  if (value === null || value === undefined) return 'NULL'
  if (typeof value === 'number') {
    return Number.isFinite(value) ? String(value) : 'NULL'
  }
  if (typeof value === 'boolean') {
    return value ? '1' : '0'
  }
  const raw = String(value)
  const mysqlLike = ['mysql', 'mariadb', ''].includes(String(dbType).toLowerCase())
  let escaped = raw.replace(/'/g, "''")
  if (mysqlLike) escaped = escaped.replace(/\\/g, '\\\\')
  return `'${escaped}'`
}

/** INSERT 语句：一行一条，便于直接贴进 SQL 脚本重放 */
function toInsert(columns: string[], rows: Record<string, any>[], table: string, dbType: string): string {
  const tableRef = table ? quoteIdent(table, dbType) : 'result_set'
  const cols = columns.map((c) => quoteIdent(c, dbType)).join(', ')
  return rows
    .map((row) => {
      const values = columns.map((c) => toSqlLiteral(row[c], dbType)).join(', ')
      return `INSERT INTO ${tableRef} (${cols}) VALUES (${values});`
    })
    .join('\n')
}

export interface GridCopyOptions {
  format: GridCopyFormat
  columns: string[]
  rows: Record<string, any>[]
  /** INSERT 用到的表名 */
  table?: string
  dbType?: string
  /** CSV 是否带表头，默认 true */
  withHeader?: boolean
}

/**
 * 按格式生成复制文本。
 * 无列或无行时返回空串，由调用方决定是否提示。
 */
export function buildGridText(options: GridCopyOptions): string {
  const { format, rows } = options
  const columns = options.columns.filter(Boolean)
  if (columns.length === 0 || rows.length === 0) return ''

  switch (format) {
    case 'tsv':
      return toTsv(columns, rows)
    case 'csv':
      return toCsv(columns, rows, options.withHeader !== false)
    case 'json':
      return toJson(columns, rows)
    case 'markdown':
      return toMarkdown(columns, rows)
    case 'insert':
      return toInsert(columns, rows, options.table || '', options.dbType || '')
    default:
      return toTsv(columns, rows)
  }
}

/** 复制格式的展示名，用于右键菜单 */
export const GRID_COPY_FORMAT_LABELS: Record<GridCopyFormat, string> = {
  tsv: '复制（TSV，可直接粘贴到 Excel）',
  csv: '复制为 CSV',
  json: '复制为 JSON',
  insert: '复制为 INSERT 语句',
  markdown: '复制为 Markdown 表格',
}
