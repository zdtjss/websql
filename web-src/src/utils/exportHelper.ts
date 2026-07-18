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
  const escapeCsvField = (val: any): string => {
    if (val === null || val === undefined) return '\\N'
    const str = String(val)
    if (str.includes(',') || str.includes('"') || str.includes('\n')) {
      return '"' + str.replace(/"/g, '""') + '"'
    }
    return str
  }

  const header = columns.map(escapeCsvField).join(',')
  const commentRow = comments ? '\n' + comments.map(escapeCsvField).join(',') : ''
  const body = rows.map(row =>
    columns.map(col => escapeCsvField(row[col])).join(',')
  ).join('\n')

  const bom = '\uFEFF'
  downloadBlob(bom + header + commentRow + '\n' + body, filename + '.csv', 'text/csv;charset=utf-8')
}

export function exportToSql(columns: string[], rows: any[], tableName: string, dbType?: string): string {
  const colList = columns.map(c => quoteId(c, dbType)).join(', ')
  const values = rows.map(row => {
    const vals = columns.map(col => fmtVal(row[col], dbType)).join(', ')
    return 'INSERT INTO ' + quoteId(tableName, dbType) + ' (' + colList + ') VALUES (' + vals + ');'
  }).join('\n')
  return values
}
