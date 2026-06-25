/**
 * SQL 标识符校验工具
 * 用于防止通过表名/schema 名/列名进行的 SQL 注入
 * 允许：字母、数字、下划线、$，首字符必须为字母或下划线，长度 1-64
 */

const IDENTIFIER_PATTERN = /^[a-zA-Z_][a-zA-Z0-9_$]{0,63}$/

/** 校验是否为合法的 SQL 标识符 */
export function isValidIdentifier(name: string | undefined | null): boolean {
  if (!name || typeof name !== 'string') return false
  return IDENTIFIER_PATTERN.test(name)
}

/** 校验标识符，非法时抛出错误 */
export function validateIdentifier(name: string | undefined | null, label: string): void {
  if (!isValidIdentifier(name)) {
    throw new Error(`非法的${label}: ${name ?? '(空)'}`)
  }
}

/** 安全地引号包裹标识符；非法时返回空字符串 */
export function quoteIdentifier(name: string, dbType: string): string {
  if (!isValidIdentifier(name)) return ''
  return dbType?.toLowerCase() === 'oracle' ? `"${name}"` : `\`${name}\``
}
