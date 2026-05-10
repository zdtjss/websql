export interface TableMeta {
  connId: string
  schema: string
  tableName: string
  dbType?: string
}

export function fmtVal(val: any): string {
  if (val === null || val === undefined) {
    return 'NULL'
  }
  if (typeof val === 'string') {
    if (val.length > 2 && val.startsWith("b'") && val.endsWith("'")) {
      return val
    }
    return "'" + val.replace(/\\/g, '\\\\').replace(/'/g, "''") + "'"
  }
  if (typeof val === 'number' || typeof val === 'bigint') {
    return String(val)
  }
  if (typeof val === 'boolean') {
    return val ? '1' : '0'
  }
  return String(val)
}

export function buildUpdateSQL(tableName: string, changedCols: Record<string, any>, pkCols: Record<string, any>): string {
  const setClauses = Object.keys(changedCols)
    .map(key => `\`${key}\` = ${fmtVal(changedCols[key])}`)
    .join(', ')
  const whereClauses = Object.keys(pkCols)
    .map(key => `\`${key}\` = ${fmtVal(pkCols[key])}`)
    .join(' AND ')
  return `UPDATE \`${tableName}\` SET ${setClauses} WHERE ${whereClauses}`
}

export function buildInsertSQL(tableName: string, row: Record<string, any>): string {
  const cols = Object.keys(row).filter(k => row[k] !== '' && row[k] !== null && row[k] !== undefined)
  const colList = cols.map(k => `\`${k}\``).join(', ')
  const valList = cols.map(k => fmtVal(row[k])).join(', ')
  return `INSERT INTO \`${tableName}\` (${colList}) VALUES (${valList})`
}

export function buildDeleteSQL(tableName: string, pkCols: Record<string, any>): string {
  const whereClauses = Object.keys(pkCols)
    .map(key => `\`${key}\` = ${fmtVal(pkCols[key])}`)
    .join(' AND ')
  return `DELETE FROM \`${tableName}\` WHERE ${whereClauses}`
}

export function getSqlDialect(dbType: string): 'mysql' | 'plsql' | 'sql' {
  const db = (dbType || '').toLowerCase()
  if (db === 'oracle') return 'plsql'
  if (db === 'mysql') return 'mysql'
  return 'sql'
}