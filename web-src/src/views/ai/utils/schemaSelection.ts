/**
 * 「数据库 / Schema」选择器的自动选中规则（纯函数，便于单测）。
 *
 * 背景：ChatView 在选择器只有一个可选值时会隐藏选择器（shouldShowSchemaSelector 为 false），
 * 但发送前又要求 selectedSchemas 非空，于是单连接 + 单 Schema 的远程部署下
 * AI 对话页完全无法发送提问。此处在确实只有一个选择时自动选中，保持「可见性 = 可选性」一致。
 */

export interface SchemaLike {
  connId?: string
  dbSchema?: string
  schemas?: { name?: string }[]
}

/**
 * 返回应自动选中的值列表（形如 `connId::schema`）；无法唯一确定时返回空数组。
 *
 * 规则：
 * - 只有 1 个连接，且该连接只有 1 个 Schema 时才自动选中；
 * - 多连接、多 Schema 一律不自动选中（必须让用户明确选）；
 * - 缺少 connId 或 Schema 名时不选中（宁缺毋滥，不猜默认连接）。
 */
export function resolveSoleSchemaSelection(
  connList: SchemaLike[],
  alreadySelected: string[],
): string[] {
  if (alreadySelected.length > 0) return []
  if (!Array.isArray(connList) || connList.length !== 1) return []

  const conn = connList[0]
  const connId = (conn?.connId || '').trim()
  if (!connId) return []

  const schemas = conn.schemas || []
  if (schemas.length > 1) return []

  const schema = ((schemas.length === 1 ? schemas[0]?.name : conn.dbSchema) || '').trim()
  if (!schema) return []

  return [`${connId}::${schema}`]
}
