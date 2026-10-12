import { quoteId, fmtVal } from './sqlHelper.ts'

export function downloadBlob(data: BlobPart, filename: string, mimeType: string) {
  const blob = new Blob([data], { type: mimeType })
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  window.URL.revokeObjectURL(url)
}

export function exportToJson(data: any[], filename: string) {
  const json = JSON.stringify(data, null, 2)
  downloadBlob(json, filename + '.json', 'application/json')
}

export function exportToCsv(columns: string[], rows: any[], filename: string, comments?: string[]) {
  const header = columns.map(escapeCsvField).join(',')
  const commentRow = comments ? '\n' + comments.map(escapeCsvField).join(',') : ''
  const body = rows.map(row =>
    columns.map(col => escapeCsvField(row[col])).join(',')
  ).join('\n')

  const bom = '\uFEFF'
  downloadBlob(bom + header + commentRow + '\n' + body, filename + '.csv', 'text/csv;charset=utf-8')
}

function escapeCsvField(val: any): string {
  if (val === null || val === undefined) return '\\N'
  const str = String(val)
  if (str.includes(',') || str.includes('"') || str.includes('\n')) {
    return '"' + str.replace(/"/g, '""') + '"'
  }
  return str
}

/** 多结果集导出的单个结果集定义 */
export interface CsvResultSet {
  /** 结果集名称（作为分区标题行） */
  name: string
  /** 列名列表 */
  columns: string[]
  /** 数据行 */
  rows: any[]
  /** 列注释（可选，作为注释行输出） */
  comments?: string[]
}

/**
 * 将多个结果集导出到同一个 CSV 文件中，各结果集以空行分隔成一节。
 */
export function exportToCsvMulti(sets: CsvResultSet[], filename: string) {
  const sections = sets.map((set) => {
    const header = set.columns.map(escapeCsvField).join(',')
    const commentRow = set.comments ? '\n' + set.comments.map(escapeCsvField).join(',') : ''
    const body = set.rows
      .map(row => set.columns.map(col => escapeCsvField(row[col])).join(','))
      .join('\n')
    return escapeCsvField(set.name) + '\n' + header + commentRow + '\n' + body
  })

  const bom = '\uFEFF'
  downloadBlob(bom + sections.join('\n\n'), filename + '.csv', 'text/csv;charset=utf-8')
}

export function exportToSql(columns: string[], rows: any[], tableName: string, dbType?: string): string {
  const colList = columns.map(c => quoteId(c, dbType)).join(', ')
  const values = rows.map(row => {
    const vals = columns.map(col => fmtVal(row[col], dbType)).join(', ')
    return 'INSERT INTO ' + quoteId(tableName, dbType) + ' (' + colList + ') VALUES (' + vals + ');'
  }).join('\n')
  return values
}
