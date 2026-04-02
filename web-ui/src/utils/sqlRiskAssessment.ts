/**
 * SQL 风险评估工具
 * 
 * 功能：
 * 1. 分析 SQL 类型
 * 2. 评估风险等级
 * 3. 提取表名
 * 4. 估算影响范围
 */

export interface SQLAnalysisResult {
  type: 'SELECT' | 'INSERT' | 'UPDATE' | 'DELETE' | 'DDL' | 'UNKNOWN';
  riskLevel: 'low' | 'medium' | 'high';
  tableName?: string;
  hasWhereClause: boolean;
  description: string;
  warnings: string[];
}

/**
 * 分析 SQL 语句
 */
export function analyzeSQL(sql: string): SQLAnalysisResult {
  const upperSQL = sql.toUpperCase().trim();
  const warnings: string[] = [];
  
  // 判断 SQL 类型
  let type: SQLAnalysisResult['type'] = 'UNKNOWN';
  if (upperSQL.startsWith('SELECT') || upperSQL.startsWith('SHOW') || 
      upperSQL.startsWith('DESCRIBE') || upperSQL.startsWith('EXPLAIN')) {
    type = 'SELECT';
  } else if (upperSQL.startsWith('INSERT')) {
    type = 'INSERT';
  } else if (upperSQL.startsWith('UPDATE')) {
    type = 'UPDATE';
  } else if (upperSQL.startsWith('DELETE')) {
    type = 'DELETE';
  } else if (upperSQL.match(/^(CREATE|ALTER|DROP|TRUNCATE)/)) {
    type = 'DDL';
  }
  
  // 提取表名（简单实现）
  const tableName = extractTableName(sql);
  
  // 检查是否有 WHERE 条件
  const hasWhereClause = upperSQL.includes('WHERE');
  
  // 评估风险等级
  let riskLevel: SQLAnalysisResult['riskLevel'] = 'low';
  const description = getOperationDescription(type);
  
  switch (type) {
    case 'SELECT':
      riskLevel = 'low';
      break;
      
    case 'INSERT':
      riskLevel = 'medium';
      break;
      
    case 'UPDATE':
      riskLevel = hasWhereClause ? 'medium' : 'high';
      if (!hasWhereClause) {
        warnings.push('⚠️ UPDATE 语句没有 WHERE 条件，将更新所有行！');
      }
      break;
      
    case 'DELETE':
      riskLevel = hasWhereClause ? 'high' : 'high';
      if (!hasWhereClause) {
        warnings.push('⚠️ DELETE 语句没有 WHERE 条件，将删除所有行！');
      }
      break;
      
    case 'DDL':
      riskLevel = 'high';
      if (upperSQL.startsWith('DROP') || upperSQL.startsWith('TRUNCATE')) {
        warnings.push('⚠️ 此操作不可逆，数据将无法恢复！');
      }
      break;
      
    default:
      riskLevel = 'low';
  }
  
  return {
    type,
    riskLevel,
    tableName,
    hasWhereClause,
    description,
    warnings,
  };
}

/**
 * 从 SQL 中提取表名（简单实现）
 */
function extractTableName(sql: string): string | undefined {
  const upperSQL = sql.toUpperCase();
  
  // FROM table
  const fromMatch = sql.match(/FROM\s+([a-zA-Z_][a-zA-Z0-9_]*)/i);
  if (fromMatch) {
    return fromMatch[1];
  }
  
  // INTO table
  const intoMatch = sql.match(/INTO\s+([a-zA-Z_][a-zA-Z0-9_]*)/i);
  if (intoMatch) {
    return intoMatch[1];
  }
  
  // UPDATE table
  const updateMatch = sql.match(/UPDATE\s+([a-zA-Z_][a-zA-Z0-9_]*)/i);
  if (updateMatch) {
    return updateMatch[1];
  }
  
  // DROP TABLE table
  const dropMatch = sql.match(/DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)/i);
  if (dropMatch) {
    return dropMatch[1];
  }
  
  return undefined;
}

/**
 * 获取操作描述
 */
function getOperationDescription(type: string): string {
  const descriptions: Record<string, string> = {
    SELECT: '查询数据（只读操作，安全）',
    INSERT: '插入新数据（会修改数据库）',
    UPDATE: '更新现有数据（会修改数据库）',
    DELETE: '删除数据（可能不可恢复）',
    DDL: '修改数据库结构（高危操作）',
    UNKNOWN: '未知类型的 SQL 语句',
  };
  return descriptions[type] || descriptions['UNKNOWN'];
}

/**
 * 预执行估算影响范围（使用 EXPLAIN）
 */
export async function estimateAffectedRows(
  sql: string,
  connId: string
): Promise<number | undefined> {
  try {
    // 对于 SELECT/UPDATE/DELETE，使用 EXPLAIN 估算
    const explainSQL = `EXPLAIN ${sql}`;
    
    // 调用后端接口（需要实现）
    // const result = await api.query({ sql: explainSQL, connId });
    // 解析 EXPLAIN 结果，返回估算的行数
    
    // 这里只是示例，实际需要调用后端接口
    return undefined;
  } catch (error) {
    console.error('估算影响范围失败:', error);
    return undefined;
  }
}

/**
 * 检查 SQL 是否包含危险操作
 */
export function isDangerousSQL(sql: string): boolean {
  const upperSQL = sql.toUpperCase().trim();
  
  const dangerousPatterns = [
    /^DROP\s+/i,
    /^TRUNCATE\s+/i,
    /^DELETE\s+FROM\s*$/i,  // DELETE without WHERE
    /^ALTER\s+/i,
    /^CREATE\s+/i,
  ];
  
  return dangerousPatterns.some(pattern => pattern.test(upperSQL));
}

/**
 * 检查 SQL 是否需要用户确认
 */
export function needsConfirmation(sql: string): boolean {
  const analysis = analyzeSQL(sql);
  return analysis.riskLevel === 'medium' || analysis.riskLevel === 'high';
}
