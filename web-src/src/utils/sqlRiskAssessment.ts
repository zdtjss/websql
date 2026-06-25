/**
 * SQL 风险评估工具（精简版）
 *
 * 设计原则：
 *   - 前端只做轻量的关键字/结构检测，复杂的 SQL 解析应交给后端 lezer/mysql 语法树
 *   - 表名提取只覆盖最常见的单表场景，多表语法（JOIN/多表 UPDATE）降级为取首个表
 *   - 伪 WHERE 检测仅识别最常见的恒真/恒假模式
 *
 * 仅被 ChatView 调用，用于在 AI 回复中标识 SQL 风险等级。
 */

/** 风险项严重程度 */
export type SQLRiskSeverity = 'danger' | 'warning'

/** 风险等级 */
export type SQLRiskLevel = 'low' | 'medium' | 'high'

/** SQL 语句类型 */
export type SQLType = 'SELECT' | 'INSERT' | 'UPDATE' | 'DELETE' | 'DDL' | 'MERGE' | 'DCL' | 'CALL' | 'UNKNOWN'

/** 单个风险项 */
export interface SQLRisk {
  type: string
  message: string
  severity: SQLRiskSeverity
}

/** analyzeSQL 的分析结果（保持兼容字段，外部调用方无需改动） */
export interface SQLAnalysisResult {
  type: SQLType
  riskLevel: SQLRiskLevel
  level: SQLRiskLevel
  tableName: string | undefined
  affectedTables: string[]
  hasWhereClause: boolean
  description: string
  warnings: string[]
  risks: SQLRisk[]
  summary: string
}

/**
 * 危险关键字列表（按严重程度分类）
 * 注：仅做关键字存在性检测，不做完整 SQL 解析
 */
const DANGEROUS_KEYWORDS: { pattern: RegExp; message: string; severity: SQLRiskSeverity }[] = [
  { pattern: /\bDROP\s+DATABASE\b/i, message: 'DROP DATABASE 将删除整个数据库，操作不可逆', severity: 'danger' },
  { pattern: /\bDROP\s+SCHEMA\b/i, message: 'DROP SCHEMA 将删除整个 schema，操作不可逆', severity: 'danger' },
  { pattern: /\bTRUNCATE\b/i, message: 'TRUNCATE 将清空表数据，操作不可逆且不可回滚', severity: 'danger' },
  { pattern: /\bDROP\s+TABLE\b/i, message: 'DROP TABLE 将删除表结构及全部数据，操作不可逆', severity: 'danger' },
  { pattern: /\bSHUTDOWN\b/i, message: 'SHUTDOWN 将关闭数据库服务，影响所有连接', severity: 'danger' },
  { pattern: /\bMERGE\s+INTO\b/i, message: 'MERGE 语句可能对多行数据进行插入/更新/删除', severity: 'warning' },
  { pattern: /\bGRANT\b/i, message: 'GRANT 修改数据库权限分配', severity: 'warning' },
  { pattern: /\bREVOKE\b/i, message: 'REVOKE 撤销已有权限分配', severity: 'warning' },
  { pattern: /\bLOAD\s+DATA\b/i, message: 'LOAD DATA 批量导入数据，可能影响大量行', severity: 'warning' },
  { pattern: /\bCALL\b/i, message: 'CALL 调用存储过程，可能产生不可预期的副作用', severity: 'warning' },
  { pattern: /\bALTER\s+/i, message: 'ALTER 修改数据库结构，可能造成数据丢失', severity: 'warning' },
]

/**
 * 从 SQL 中提取首个表名（简化版）
 * 覆盖最常见的单表场景：INSERT INTO / UPDATE / DELETE FROM / ALTER TABLE / FROM
 * 多表语法（JOIN/多表 UPDATE）降级为取首个表
 */
function extractFirstTableName(sql: string): string | undefined {
  const patterns: RegExp[] = [
    /(?:INSERT(?:\s+IGNORE)?|REPLACE)\s+INTO\s+(?:TABLE\s+)?((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
    /UPDATE\s+((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
    /DELETE\s+FROM\s+((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
    /ALTER\s+TABLE\s+((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
    /TRUNCATE\s+(?:TABLE\s+)?((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
    /DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
    /FROM\s+((?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*)(?:\s*\.\s*(?:`[^`]+`|"[^"]+"|\[[^\]]+\]|[a-zA-Z_]\w*))?)/i,
  ]
  for (const re of patterns) {
    const m = sql.match(re)
    if (m && m[1]) {
      return cleanIdentifier(m[1])
    }
  }
  return undefined
}

/** 清理表标识符：去除反引号、双引号、方括号 */
function cleanIdentifier(id: string): string {
  const parts = id.split(/\s*\.\s*/)
  return parts
    .map(p => {
      p = p.trim()
      if (/^`.*`$/.test(p)) return p.slice(1, -1)
      if (/^".*"$/.test(p)) return p.slice(1, -1)
      if (/^\[.*\]$/.test(p)) return p.slice(1, -1)
      return p
    })
    .filter(p => p.length > 0)
    .join('.')
}

/**
 * 检测伪 WHERE 条件（简化版）
 * 仅识别最常见的恒真/恒假模式：WHERE 1=1 / WHERE 1=2 / WHERE TRUE / WHERE FALSE
 * 含 AND/OR 的复合条件不判定（可能含真实列约束）
 */
function detectPseudoWhere(sql: string): { hasWhere: boolean; isPseudo: boolean; reason: string | null } {
  const m = sql.match(/\bWHERE\b\s+(.*?)(?=\s+(?:ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING)\b|;|$)/is)
  if (!m) {
    return { hasWhere: false, isPseudo: false, reason: null }
  }
  const content = m[1].trim().replace(/;\s*$/, '').trim()

  // 含 AND/OR 的复合条件不判定
  if (/\b(AND|OR)\b/i.test(content)) {
    return { hasWhere: true, isPseudo: false, reason: null }
  }

  // 恒真：1=1 / '1'='1' / TRUE / 非零数字
  // 恒假：1=2 / '1'='2' / FALSE / 0
  const alwaysTruePatterns = [
    /^['"]?(\d+)['"]?\s*=\s*['"]?\1['"]?\s*$/i,          // 1=1, '1'='1'
    /^TRUE$/i,
    /^[1-9]\d*$/,                                          // 非零数字
  ]
  const alwaysFalsePatterns = [
    /^['"]?(\d+)['"]?\s*(?:<>|!=)\s*['"]?\1['"]?\s*$/i,   // 1<>1
    /^['"]?(\d+)['"]?\s*=\s*['"]?(\d+)['"]?\s*$/i,         // 1=2 (两个不同的数字)
    /^FALSE$/i,
    /^0$/,
  ]

  for (const re of alwaysTruePatterns) {
    if (re.test(content)) {
      // 排除 1=2 这种"数字比较但值不同"的情况
      const numMatch = content.match(/^['"]?(\d+)['"]?\s*=\s*['"]?(\d+)['"]?\s*$/i)
      if (numMatch && numMatch[1] !== numMatch[2]) {
        // 这是恒假，跳过
        break
      }
      return {
        hasWhere: true,
        isPseudo: true,
        reason: `恒真条件 WHERE ${content} 不限制任何行，等同于全表操作`,
      }
    }
  }

  for (const re of alwaysFalsePatterns) {
    if (re.test(content)) {
      // 排除 1=1 这种"数字比较但值相同"的情况（已被上面 alwaysTrue 处理）
      const numMatch = content.match(/^['"]?(\d+)['"]?\s*=\s*['"]?(\d+)['"]?\s*$/i)
      if (numMatch && numMatch[1] === numMatch[2]) {
        continue
      }
      return {
        hasWhere: true,
        isPseudo: true,
        reason: `恒假条件 WHERE ${content} 不会匹配任何数据（可疑模式）`,
      }
    }
  }

  return { hasWhere: true, isPseudo: false, reason: null }
}

/** 获取操作描述 */
function getOperationDescription(type: SQLType): string {
  const descriptions: Record<SQLType, string> = {
    SELECT: '查询数据（只读操作，安全）',
    INSERT: '插入新数据（会修改数据库）',
    UPDATE: '更新现有数据（会修改数据库）',
    DELETE: '删除数据（可能不可恢复）',
    DDL: '修改数据库结构（高危操作）',
    MERGE: 'MERGE 合并操作（可能影响多行）',
    DCL: '权限管理操作（修改访问控制）',
    CALL: '调用存储过程（可能有副作用）',
    UNKNOWN: '未知类型的 SQL 语句',
  }
  return descriptions[type] || descriptions['UNKNOWN']
}

/**
 * 分析 SQL 语句（对外 API 保持兼容）
 * @param sql SQL 语句
 * @returns 分析结果
 */
export function analyzeSQL(sql: string): SQLAnalysisResult {
  if (!sql) {
    return {
      type: 'UNKNOWN',
      riskLevel: 'low',
      level: 'low',
      tableName: undefined,
      affectedTables: [],
      hasWhereClause: false,
      description: 'SQL 语句为空',
      warnings: [],
      risks: [],
      summary: 'SQL 语句为空',
    }
  }

  const fullSQL = sql.trim()
  // 跳过前导注释，找首个非空非注释行用于类型判断
  let firstLine = fullSQL
  for (const line of fullSQL.split('\n')) {
    const trimmed = line.trim()
    if (trimmed && !trimmed.startsWith('--') && !trimmed.startsWith('/*') && !trimmed.startsWith('#')) {
      firstLine = trimmed
      break
    }
  }

  const upperSQL = firstLine.toUpperCase()
  const warnings: string[] = []
  const risks: SQLRisk[] = []

  // 判断 SQL 类型
  let type: SQLType = 'UNKNOWN'
  const typeMatch = upperSQL.match(/^\s*(SELECT|INSERT|UPDATE|DELETE|CREATE|ALTER|DROP|TRUNCATE|SHOW|DESCRIBE|EXPLAIN|MERGE|GRANT|REVOKE|CALL)\b/i)
  if (typeMatch) {
    const kw = typeMatch[1]
    if (kw === 'SELECT' || kw === 'SHOW' || kw === 'DESCRIBE' || kw === 'EXPLAIN') {
      type = 'SELECT'
    } else if (kw === 'INSERT') {
      type = 'INSERT'
    } else if (kw === 'UPDATE') {
      type = 'UPDATE'
    } else if (kw === 'DELETE') {
      type = 'DELETE'
    } else if (['CREATE', 'ALTER', 'DROP', 'TRUNCATE'].includes(kw)) {
      type = 'DDL'
    } else if (kw === 'MERGE') {
      type = 'MERGE'
    } else if (kw === 'GRANT' || kw === 'REVOKE') {
      type = 'DCL'
    } else if (kw === 'CALL') {
      type = 'CALL'
    }
  }

  // 提取表名（简化版：仅取首个表）
  const tableName = extractFirstTableName(fullSQL)
  const affectedTables = tableName ? [tableName] : []

  // 伪 WHERE 检测
  const pseudoWhere = detectPseudoWhere(fullSQL)
  const hasWhereClause = pseudoWhere.hasWhere

  // 评估风险等级
  let riskLevel: SQLRiskLevel = 'low'
  const description = getOperationDescription(type)

  switch (type) {
    case 'SELECT':
      riskLevel = 'low'
      break
    case 'INSERT':
      riskLevel = 'medium'
      break
    case 'UPDATE':
      riskLevel = hasWhereClause ? 'medium' : 'high'
      if (!hasWhereClause) {
        warnings.push('⚠️ UPDATE 语句没有 WHERE 条件，将更新所有行！')
        risks.push({ type: 'no_where', message: 'UPDATE 语句缺少 WHERE 条件，将更新表中所有行', severity: 'danger' })
      }
      break
    case 'DELETE':
      riskLevel = 'high'
      if (!hasWhereClause) {
        warnings.push('⚠️ DELETE 语句没有 WHERE 条件，将删除所有行！')
        risks.push({ type: 'no_where', message: 'DELETE 语句缺少 WHERE 条件，将删除表中所有行', severity: 'danger' })
      }
      break
    case 'DDL':
      riskLevel = 'high'
      if (/\bDROP\b/i.test(fullSQL) || /\bTRUNCATE\b/i.test(fullSQL)) {
        warnings.push('⚠️ 此操作不可逆，数据将无法恢复！')
      }
      break
    case 'MERGE':
      riskLevel = 'high'
      warnings.push('⚠️ MERGE 语句可能影响多行数据')
      risks.push({ type: 'dangerous_keyword', message: 'MERGE 语句可能对多行数据进行插入/更新/删除', severity: 'warning' })
      break
    case 'DCL':
      riskLevel = 'medium'
      warnings.push('⚠️ 权限修改语句，可能影响数据库访问控制')
      risks.push({ type: 'dangerous_keyword', message: 'DCL 语句修改数据库权限分配', severity: 'warning' })
      break
    case 'CALL':
      riskLevel = 'medium'
      warnings.push('⚠️ 调用存储过程，可能产生副作用')
      risks.push({ type: 'dangerous_keyword', message: 'CALL 调用存储过程，可能产生不可预期的副作用', severity: 'warning' })
      break
    default:
      if (/\b(DROP|TRUNCATE|DELETE\s+FROM)\b/i.test(fullSQL)) {
        riskLevel = 'high'
      } else if (/\b(UPDATE|INSERT|ALTER)\b/i.test(fullSQL)) {
        riskLevel = 'medium'
      }
  }

  // 伪 WHERE 检测：提升风险等级
  if (pseudoWhere.isPseudo) {
    riskLevel = 'high'
    warnings.push(`⚠️ ${pseudoWhere.reason}`)
    risks.push({
      type: 'pseudo_where',
      message: pseudoWhere.reason ?? '检测到伪 WHERE 条件',
      severity: 'danger',
    })
  }

  // 危险关键字检测
  DANGEROUS_KEYWORDS.forEach(({ pattern, message, severity }) => {
    if (pattern.test(fullSQL)) {
      const exists = risks.some(r => r.message === message)
      if (!exists) {
        risks.push({ type: 'dangerous_keyword', message, severity })
        const warnMsg = `⚠️ ${message}`
        if (!warnings.includes(warnMsg)) {
          warnings.push(warnMsg)
        }
        if (severity === 'danger' && riskLevel !== 'high') {
          riskLevel = 'high'
        } else if (severity === 'warning' && riskLevel === 'low') {
          riskLevel = 'medium'
        }
      }
    }
  })

  // 构建 summary
  const parts: string[] = [description]
  if (tableName) parts.push(`涉及表：${tableName}`)
  if (pseudoWhere.isPseudo) {
    parts.push('检测到伪 WHERE 条件')
  } else if (!hasWhereClause && (type === 'UPDATE' || type === 'DELETE')) {
    parts.push('缺少 WHERE 条件，将影响全表')
  }
  const dangerCount = risks.filter(r => r.severity === 'danger').length
  const warningCount = risks.filter(r => r.severity === 'warning').length
  if (dangerCount > 0 || warningCount > 0) {
    parts.push(`共 ${risks.length} 项风险（${dangerCount} 项危险、${warningCount} 项警告）`)
  }
  const levelText = ({ low: '低风险', medium: '中风险', high: '高风险' } as Record<SQLRiskLevel, string>)[riskLevel]
  parts.push(`整体评级：${levelText}`)
  const summary = parts.join('；')

  return {
    type,
    riskLevel,
    level: riskLevel,
    tableName,
    affectedTables,
    hasWhereClause,
    description,
    warnings,
    risks,
    summary,
  }
}
