export interface TableMeta {
  connId: string
  schema: string
  tableName: string
  dbType?: string
}

export function quoteId(identifier: string, dbType?: string): string {
  const db = (dbType || '').toLowerCase()
  if (db === 'mysql' || db === 'mariadb') {
    return '`' + identifier + '`'
  }
  if (db === 'oracle') {
    return '"' + identifier.toUpperCase() + '"'
  }
  return '"' + identifier + '"'
}

export function buildPagedSQL(baseSQL: string, dbType: string, limit: number, offset: number): string {
  const db = (dbType || '').toLowerCase()
  if (db === 'oracle') {
    const limitHint = ' /*  LIMIT  */'
    if (limit <= 0) {
      if (baseSQL.includes(' WHERE ')) {
        return baseSQL + ' AND 1=0' + limitHint
      }
      return baseSQL + ' WHERE 1=0' + limitHint
    }
    if (offset === 0) {
      return 'SELECT * FROM (' + baseSQL + ') WHERE ROWNUM <= ' + limit + limitHint
    }
    return 'SELECT * FROM (SELECT t.*, ROWNUM AS rn FROM (' + baseSQL + ') t WHERE ROWNUM <= ' + (offset + limit) + ') WHERE rn > ' + offset + limitHint
  }
  return baseSQL + ' LIMIT ' + limit + ' OFFSET ' + offset
}

export function buildCountSQL(tableName: string, dbType: string, whereClause?: string): string {
  let sql = 'SELECT COUNT(*) as cnt FROM ' + quoteId(tableName, dbType)
  if (whereClause && whereClause.trim()) {
    sql += ' WHERE ' + whereClause.trim()
  }
  return sql
}

export function buildSelectSQL(tableName: string, dbType: string, options?: {
  where?: string
  orderBy?: string
  limit?: number
  offset?: number
}): string {
  let sql = 'SELECT * FROM ' + quoteId(tableName, dbType)
  if (options?.where && options.where.trim()) {
    sql += ' WHERE ' + options.where.trim()
  }
  if (options?.orderBy) {
    sql += ' ORDER BY ' + options.orderBy
  }
  if (options?.limit !== undefined && options?.offset !== undefined) {
    sql = buildPagedSQL(sql, dbType, options.limit, options.offset)
  }
  return sql
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

export function buildWhereCondition(col: string, val: any, dbType?: string): string {
  if (val === null || val === undefined) {
    return quoteId(col, dbType) + ' IS NULL'
  }
  return quoteId(col, dbType) + ' = ' + fmtVal(val)
}

export function buildUpdateSQL(tableName: string, changedCols: Record<string, any>, pkCols: Record<string, any>, dbType?: string): string {
  const setClauses = Object.keys(changedCols)
    .map(key => quoteId(key, dbType) + ' = ' + fmtVal(changedCols[key]))
    .join(', ')
  const whereClauses = Object.keys(pkCols)
    .map(key => buildWhereCondition(key, pkCols[key], dbType))
    .join(' AND ')
  return 'UPDATE ' + quoteId(tableName, dbType) + ' SET ' + setClauses + ' WHERE ' + whereClauses
}

export function buildInsertSQL(tableName: string, row: Record<string, any>, dbType?: string): string {
  const cols = Object.keys(row).filter(k => row[k] !== null && row[k] !== undefined)
  const colList = cols.map(k => quoteId(k, dbType)).join(', ')
  const valList = cols.map(k => fmtVal(row[k])).join(', ')
  return 'INSERT INTO ' + quoteId(tableName, dbType) + ' (' + colList + ') VALUES (' + valList + ')'
}

export function buildDeleteSQL(tableName: string, pkCols: Record<string, any>, dbType?: string): string {
  const whereClauses = Object.keys(pkCols)
    .map(key => buildWhereCondition(key, pkCols[key], dbType))
    .join(' AND ')
  return 'DELETE FROM ' + quoteId(tableName, dbType) + ' WHERE ' + whereClauses
}

export function getSqlDialect(dbType: string): 'mysql' | 'plsql' | 'sql' {
  const db = (dbType || '').toLowerCase()
  if (db === 'oracle') return 'plsql'
  if (db === 'mysql') return 'mysql'
  return 'sql'
}
