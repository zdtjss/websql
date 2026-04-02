/**
 * SQL 风险评估工具
 * 
 * 功能：
 * 1. 分析 SQL 类型
 * 2. 评估风险等级
 * 3. 提取表名
 * 4. 估算影响范围
 * 5. 从文本中提取 SQL 语句
 */

/**
 * 从文本中提取所有 SQL 语句（包括代码块和内联）
 * @param {string} text - 包含 SQL 的文本
 * @returns {Array<string>} 提取到的 SQL 语句数组
 */
export function extractAllSQL(text) {
  const sqlStatements = []
  
  // 1. 首先提取完整的代码块中的 SQL
  const codeBlockRegex = /```(?:sql)?\s*([\s\S]*?)```/g
  let match
  while ((match = codeBlockRegex.exec(text)) !== null) {
    const sql = match[1].trim()
    if (sql) {
      // 分割多个语句（以分号分隔）
      const statements = splitSQLStatements(sql)
      sqlStatements.push(...statements)
    }
  }
  
  // 2. 如果没有代码块，尝试提取内联 SQL
  if (sqlStatements.length === 0) {
    const inlineSQL = extractInlineSQL(text)
    if (inlineSQL) {
      sqlStatements.push(inlineSQL)
    }
  }
  
  return sqlStatements.filter(sql => sql.length > 0)
}

/**
 * 分割多个 SQL 语句
 * @param {string} sql - 包含多个语句的 SQL
 * @returns {Array<string>} 分割后的单个 SQL 语句
 */
function splitSQLStatements(sql) {
  const statements = []
  let current = ''
  let inString = false
  let stringChar = ''
  
  for (let i = 0; i < sql.length; i++) {
    const char = sql[i]
    const nextChar = sql[i + 1]
    
    // 处理字符串转义
    if (inString && char === '\\' && (nextChar === "'" || nextChar === '"')) {
      current += char + nextChar
      i++
      continue
    }
    
    // 处理字符串开始/结束
    if ((char === "'" || char === '"') && (!inString || char === stringChar)) {
      inString = !inString
      stringChar = inString ? char : ''
      current += char
      continue
    }
    
    // 处理语句结束（分号不在字符串内）
    if (char === ';' && !inString) {
      const trimmed = current.trim()
      if (trimmed) {
        statements.push(trimmed)
      }
      current = ''
      continue
    }
    
    current += char
  }
  
  // 添加最后一个语句
  const trimmed = current.trim()
  if (trimmed) {
    statements.push(trimmed)
  }
  
  return statements
}

/**
 * 从文本中提取内联 SQL
 * @param {string} text - 文本
 * @returns {string|null} 提取到的 SQL
 */
function extractInlineSQL(text) {
  // 常见的 SQL 开头关键字
  const sqlKeywords = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'ALTER', 'CREATE', 'DROP', 'TRUNCATE', 'REPLACE', 'MERGE']
  
  // 查找以这些关键字开头的语句
  for (const keyword of sqlKeywords) {
    const regex = new RegExp(`\\b${keyword}\\b[\\s\\S]*?(?=(?:\\.\\s|\\n\\n|$|\\b${sqlKeywords.join('\\b|\\b')}\\b))`, 'gi')
    const matches = text.match(regex)
    if (matches) {
      // 返回最长的匹配（最可能是完整的 SQL）
      return matches.sort((a, b) => b.length - a.length)[0].trim()
    }
  }
  
  return null
}

/**
 * 分析 SQL 语句
 * @param {string} sql - SQL 语句
 * @returns {Object} 分析结果
 */
export function analyzeSQL(sql) {
  const upperSQL = sql.toUpperCase().trim()
  const warnings = []
  
  // 判断 SQL 类型
  let type = 'UNKNOWN'
  if (upperSQL.startsWith('SELECT') || 
      upperSQL.startsWith('SHOW') || 
      upperSQL.startsWith('DESCRIBE') || 
      upperSQL.startsWith('EXPLAIN')) {
    type = 'SELECT'
  } else if (upperSQL.startsWith('INSERT')) {
    type = 'INSERT'
  } else if (upperSQL.startsWith('UPDATE')) {
    type = 'UPDATE'
  } else if (upperSQL.startsWith('DELETE')) {
    type = 'DELETE'
  } else if (upperSQL.match(/^(CREATE|ALTER|DROP|TRUNCATE)/)) {
    type = 'DDL'
  }
  
  // 提取表名（简单实现）
  const tableName = extractTableName(sql)
  
  // 检查是否有 WHERE 条件
  const hasWhereClause = upperSQL.includes('WHERE')
  
  // 评估风险等级
  let riskLevel = 'low'
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
      }
      break
      
    case 'DELETE':
      riskLevel = 'high'
      if (!hasWhereClause) {
        warnings.push('⚠️ DELETE 语句没有 WHERE 条件，将删除所有行！')
      }
      break
      
    case 'DDL':
      riskLevel = 'high'
      if (upperSQL.startsWith('DROP') || upperSQL.startsWith('TRUNCATE')) {
        warnings.push('⚠️ 此操作不可逆，数据将无法恢复！')
      }
      break
      
    default:
      riskLevel = 'low'
  }
  
  return {
    type,
    riskLevel,
    tableName,
    hasWhereClause,
    description,
    warnings
  }
}

/**
 * 从 SQL 中提取表名（简单实现）
 * @param {string} sql - SQL 语句
 * @returns {string|undefined} 表名
 */
function extractTableName(sql) {
  const upperSQL = sql.toUpperCase()
  
  // FROM table
  const fromMatch = sql.match(/FROM\s+([a-zA-Z_][a-zA-Z0-9_]*)/i)
  if (fromMatch) {
    return fromMatch[1]
  }
  
  // INTO table
  const intoMatch = sql.match(/INTO\s+([a-zA-Z_][a-zA-Z0-9_]*)/i)
  if (intoMatch) {
    return intoMatch[1]
  }
  
  // UPDATE table
  const updateMatch = sql.match(/UPDATE\s+([a-zA-Z_][a-zA-Z0-9_]*)/i)
  if (updateMatch) {
    return updateMatch[1]
  }
  
  // DROP TABLE table
  const dropMatch = sql.match(/DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)/i)
  if (dropMatch) {
    return dropMatch[1]
  }
  
  return undefined
}

/**
 * 获取操作描述
 * @param {string} type - SQL 类型
 * @returns {string} 描述
 */
function getOperationDescription(type) {
  const descriptions = {
    SELECT: '查询数据（只读操作，安全）',
    INSERT: '插入新数据（会修改数据库）',
    UPDATE: '更新现有数据（会修改数据库）',
    DELETE: '删除数据（可能不可恢复）',
    DDL: '修改数据库结构（高危操作）',
    UNKNOWN: '未知类型的 SQL 语句'
  }
  return descriptions[type] || descriptions['UNKNOWN']
}

/**
 * 检查 SQL 是否包含危险操作
 * @param {string} sql - SQL 语句
 * @returns {boolean} 是否危险
 */
export function isDangerousSQL(sql) {
  const upperSQL = sql.toUpperCase().trim()
  
  const dangerousPatterns = [
    /^DROP\s+/i,
    /^TRUNCATE\s+/i,
    /^DELETE\s+FROM\s*$/i,  // DELETE without WHERE
    /^ALTER\s+/i,
    /^CREATE\s+/i,
  ]
  
  return dangerousPatterns.some(pattern => pattern.test(upperSQL))
}

/**
 * 检查 SQL 是否需要用户确认
 * @param {string} sql - SQL 语句
 * @returns {boolean} 是否需要确认
 */
export function needsConfirmation(sql) {
  const analysis = analyzeSQL(sql)
  return analysis.riskLevel === 'medium' || analysis.riskLevel === 'high'
}
