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

function upperType(colType?: string): string {
  return (colType || '').toUpperCase()
}

export function isDateColumnType(colType?: string): boolean {
  const t = upperType(colType)
  return (
    t === 'DATETIME' ||
    t === 'DATE' ||
    t === 'TIMESTAMP' ||
    t === 'TIMESTAMP(6)' ||
    t.includes('TIMESTAMP') ||
    t === 'TIMESTAMPTZ' ||
    t === 'TIMESTAMPLTZ'
  )
}

/** Oracle 的 TIMESTAMP WITH [LOCAL] TIME ZONE 需要保留显式时区偏移，不能当普通日期选择器处理。 */
export function isTimestampTZColumnType(colType?: string): boolean {
  const t = upperType(colType)
  if (!t.includes('TIMESTAMP') && t !== 'TIMESTAMPTZ' && t !== 'TIMESTAMPLTZ') return false
  return t.includes('TIME ZONE') || t.includes('TZ') || t === 'TIMESTAMPTZ' || t === 'TIMESTAMPLTZ'
}

/** MySQL/MariaDB 的 DATE 只到日；Oracle DATE 含时分秒，不在此列。 */
export function isDateOnlyColumnType(colType: string | undefined, dbType: string | undefined): boolean {
  const db = (dbType || '').toLowerCase()
  return (db === 'mysql' || db === 'mariadb') && upperType(colType) === 'DATE'
}

/** 二进制/LOB 列不适合在文本表格中编辑：读取与回写都会丢失字节信息。 */
export function isBinaryOrLobColumnType(colType?: string): boolean {
  const t = upperType(colType)
  if (!t) return false
  // Oracle BINARY_FLOAT/DOUBLE 是数值类型，不能按二进制列处理
  if (t.includes('BINARY_FLOAT') || t.includes('BINARY_DOUBLE')) return false
  return /(BLOB|CLOB|NCLOB|BINARY|VARBINARY|RAW|GEOMETRY|POINT|LINESTRING|POLYGON|MULTIPOINT|MULTILINESTRING|MULTIPOLYGON|GEOMETRYCOLLECTION)/.test(t)
}

export function isNumericColumnType(colType?: string): boolean {
  return /\b(TINYINT|SMALLINT|MEDIUMINT|INT|INTEGER|BIGINT|DECIMAL|NUMERIC|NUMBER|FLOAT|DOUBLE|REAL)\b/.test(upperType(colType))
}

/**
 * 后端为超过 JS 安全整数的数值加了 "s:" 前缀（防止 JSON 丢精度）。
 * 前端拿到数值列的值后应剥离该前缀，否则会生成 `id = 's:123...'` 这类永远不匹配的条件。
 */
export function stripBigNumberMarker(val: any, colType?: string): any {
  if (typeof val !== 'string' || !colType || !isNumericColumnType(colType)) return val
  return /^s:-?\d+(\.\d+)?$/.test(val) ? val.slice(2) : val
}

/** 日期列应使用的选择器类型；返回空串表示按文本编辑（如带时区的 TIMESTAMP）。 */
export function getDatePickerKind(colType?: string, dbType?: string): 'date' | 'datetime' | '' {
  if (!isDateColumnType(colType) || isTimestampTZColumnType(colType)) return ''
  return isDateOnlyColumnType(colType, dbType) ? 'date' : 'datetime'
}

function quoteStringLiteral(val: string, isMySQL: boolean): string {
  let escaped = val
  if (isMySQL) {
    escaped = escaped.replace(/\\/g, '\\\\')
  }
  escaped = escaped.replace(/'/g, "''")
  return "'" + escaped + "'"
}

function normalizeDateTimeText(val: string): string {
  return val.trim().replace('T', ' ')
}

function ensureTimePart(text: string): string {
  const t = normalizeDateTimeText(text)
  if (/^\d{4}-\d{2}-\d{2}$/.test(t)) return t + ' 00:00:00'
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(t)) return t + ':00'
  return t
}

function formatOracleDateLiteral(val: string): string {
  // Oracle DATE 无小数秒，截断到秒；显式 TO_DATE 避免依赖会话 NLS_DATE_FORMAT
  const base = ensureTimePart(val).slice(0, 19)
  return "TO_DATE(" + quoteStringLiteral(base, false) + ", 'YYYY-MM-DD HH24:MI:SS')"
}

function formatOracleTimestampLiteral(val: string, withTZ: boolean): string {
  const text = ensureTimePart(val)
  const m = text.match(/^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(?:\s*([+-]\d{2}:\d{2}))?$/)
  if (!m) return quoteStringLiteral(text, false)
  const base = m[1]
  const frac = m[2] || ''
  const offset = m[3] || ''
  if (withTZ && offset) {
    const value = base + (frac ? '.' + frac : '') + ' ' + offset
    const mask = 'YYYY-MM-DD HH24:MI:SS' + (frac ? '.FF' + frac.length : '') + ' TZH:TZM'
    return "TO_TIMESTAMP_TZ(" + quoteStringLiteral(value, false) + ", '" + mask + "')"
  }
  const value = base + (frac ? '.' + frac : '')
  const mask = 'YYYY-MM-DD HH24:MI:SS' + (frac ? '.FF' + frac.length : '')
  return "TO_TIMESTAMP(" + quoteStringLiteral(value, false) + ", '" + mask + "')"
}

export function fmtVal(val: any, dbType?: string, colType?: string): string {
  if (val === null || val === undefined) {
    return 'NULL'
  }
  const db = (dbType || '').toLowerCase()
  const isMySQL = db === 'mysql' || db === 'mariadb'
  // Oracle 的空字符串等价于 NULL；显式写 NULL，避免日期/数值列被包成非法字面量
  if (db === 'oracle' && val === '') {
    return 'NULL'
  }
  if (typeof val === 'string') {
    // MySQL BIT columns come back as b'...' literals; pass through only for MySQL/MariaDB
    if (isMySQL && val.length > 2 && val.startsWith("b'") && val.endsWith("'")) {
      return val
    }
    if (db === 'oracle' && isDateColumnType(colType)) {
      if (isTimestampTZColumnType(colType)) {
        return formatOracleTimestampLiteral(val, true)
      }
      return upperType(colType) === 'DATE'
        ? formatOracleDateLiteral(val)
        : formatOracleTimestampLiteral(val, false)
    }
    // MySQL/MariaDB DATE 列只保留日期部分，避免时间部分被隐式截断
    if (isDateOnlyColumnType(colType, db)) {
      const dateOnly = normalizeDateTimeText(val).slice(0, 10)
      return quoteStringLiteral(dateOnly, isMySQL)
    }
    // 高精度数值在剥离 "s:" 标记后是字符串；数值列直接用数字字面量，避免引号与 NLS 差异
    if (isNumericColumnType(colType) && /^-?\d+(\.\d+)?$/.test(val)) {
      return val
    }
    return quoteStringLiteral(val, isMySQL)
  }
  if (typeof val === 'number' || typeof val === 'bigint') {
    return String(val)
  }
  if (typeof val === 'boolean') {
    return val ? '1' : '0'
  }
  // Fallback: quote any unknown type as a string to avoid SQL syntax errors
  return quoteStringLiteral(String(val), isMySQL)
}

function isJsonColumnType(colType?: string): boolean {
  return !!colType && /json/i.test(colType)
}

export function buildWhereCondition(col: string, val: any, dbType?: string, colType?: string): string {
  if (val === null || val === undefined) {
    return quoteId(col, dbType) + ' IS NULL'
  }
  // Oracle treats '' as NULL; using "= ''" would never match
  const db = (dbType || '').toLowerCase()
  if (db === 'oracle' && val === '') {
    return quoteId(col, dbType) + ' IS NULL'
  }
  const literal = fmtVal(val, dbType, colType)
  // MySQL 的 JSON 列与字符串字面量比较时，字符串会被当作 JSON 字符串标量而不是解析成对象，
  // 直接 col = '{"k":1}' 永远不匹配（受影响行数为 0），必须显式 CAST 为 JSON。
  if (db === 'mysql' && isJsonColumnType(colType)) {
    return quoteId(col, dbType) + ' = CAST(' + literal + ' AS JSON)'
  }
  return quoteId(col, dbType) + ' = ' + literal
}

export function buildUpdateSQL(tableName: string, changedCols: Record<string, any>, pkCols: Record<string, any>, dbType?: string): string {
  const setClauses = Object.keys(changedCols)
    .map(key => quoteId(key, dbType) + ' = ' + fmtVal(changedCols[key], dbType))
    .join(', ')
  const whereClauses = Object.keys(pkCols)
    .map(key => buildWhereCondition(key, pkCols[key], dbType))
    .join(' AND ')
  return 'UPDATE ' + quoteId(tableName, dbType) + ' SET ' + setClauses + ' WHERE ' + whereClauses
}

export function buildInsertSQL(tableName: string, row: Record<string, any>, dbType?: string): string {
  const cols = Object.keys(row).filter(k => row[k] !== null && row[k] !== undefined)
  const colList = cols.map(k => quoteId(k, dbType)).join(', ')
  const valList = cols.map(k => fmtVal(row[k], dbType)).join(', ')
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
  // MariaDB 与 MySQL 方言完全兼容，统一使用 mysql 方言
  if (db === 'mysql' || db === 'mariadb') return 'mysql'
  return 'sql'
}
