/**
 * 角色权限的纯函数策略层。
 *
 * 约束：
 * - 本文件只做「键解析 / 级联 / 对称规范化 / diff」，不访问接口、不持有状态；
 * - 与后端 golden cases 对齐：真正的授权仍由服务端裁决，前端展示态不是授权执行点。
 */

export type PowerLevel = 'conn' | 'schema' | 'table' | 'column'

/** 后端 PowerDetail 的前端投影（与 /saveRole 协议一致）。 */
export interface RolePower {
  id?: string
  roleId?: string
  connId: string
  schemaName: string | null
  tableName: string | null
  columnName: string | null
  level: PowerLevel
  connName?: string | null
}

/** 权限项 → 树节点 key：conn::schema::table::column。 */
export function permToKey(p: RolePower): string {
  const parts = [p.connId]
  if (p.schemaName) parts.push(p.schemaName)
  if (p.tableName) parts.push(p.tableName)
  if (p.columnName) parts.push(p.columnName)
  return parts.join('::')
}

/** 树节点 key → 权限项（不含 id/roleId）。 */
export function parseKey(key: string): RolePower {
  const parts = key.split('::')
  const connId = parts[0] || ''
  const schema = parts[1] || ''
  const table = parts[2] || ''
  const column = parts[3] || ''
  let level: PowerLevel = 'conn'
  if (column) level = 'column'
  else if (table) level = 'table'
  else if (schema) level = 'schema'
  return {
    connId,
    schemaName: schema || null,
    tableName: table || null,
    columnName: column || null,
    level,
  }
}

/** key 的层级；dir:: 前缀为目录节点。 */
export function keyLevel(key: string): PowerLevel | 'dir' {
  if (key.startsWith('dir::')) return 'dir'
  const parts = key.split('::')
  if (parts.length >= 4 && parts[3]) return 'column'
  if (parts.length >= 3 && parts[2]) return 'table'
  if (parts.length >= 2 && parts[1]) return 'schema'
  return 'conn'
}

/** child 是否为 parent 的严格下级。 */
export function isDescendantOf(childKey: string, parentKey: string): boolean {
  if (!childKey.startsWith(parentKey + '::')) return false
  return childKey.split('::').length > parentKey.split('::').length
}

/** 返回 key 的所有祖先（由近到远）。 */
export function ancestorKeys(key: string): string[] {
  if (key.startsWith('dir::')) return []
  const parts = key.split('::')
  const ancestors: string[] = []
  for (let i = parts.length - 1; i >= 1; i--) {
    ancestors.push(parts.slice(0, i).join('::'))
  }
  return ancestors
}

/**
 * 对称规范化：baseline 与 working 都按「working 的子级活跃状态」处理 schema 冗余项。
 *
 * 规则：
 * 1) 无编辑时 Schema + table 不产生删除；
 * 2) 删除最后一个子级时保留并删除应删祖先；
 * 3) 仅有子表切换时不误保留/误删 Schema。
 */
export function normalizeForDiff(powers: RolePower[], activePowers: RolePower[]): RolePower[] {
  const schemasWithChildren = new Set(
    powers.filter((p) => p.tableName).map((p) => `${p.connId}::${p.schemaName}`),
  )
  return powers.filter((p) => {
    if (p.level !== 'schema') return true
    const schemaKey = permToKey(p)
    if (!schemasWithChildren.has(schemaKey)) return true
    return !activePowers.some((child) => isDescendantOf(permToKey(child), schemaKey))
  })
}

/** 计算 baseline → working 的增量。 */
export function buildDiff(
  baseline: RolePower[],
  working: RolePower[],
): { addPowers: RolePower[]; delPowers: RolePower[] } {
  const before = normalizeForDiff(baseline, working)
  const after = normalizeForDiff(working, working)
  const beforeMap = new Map(before.map((p) => [permToKey(p), p]))
  const afterMap = new Map(after.map((p) => [permToKey(p), p]))

  const addPowers = after.filter((p) => !beforeMap.has(permToKey(p)))
  const delPowers = before.filter((p) => !afterMap.has(permToKey(p)))
  return { addPowers, delPowers }
}

/** 计算需要以「隐式父级」展示的 key（有下级但自身未授权）。 */
export function computeImplicitKeys(workingKeys: Iterable<string>, treeKeys: Set<string>): Set<string> {
  const workingSet = new Set(workingKeys)
  const implicit = new Set<string>()
  for (const key of workingSet) {
    for (const ancestor of ancestorKeys(key)) {
      if (!workingSet.has(ancestor) && treeKeys.has(ancestor)) {
        implicit.add(ancestor)
      }
    }
  }
  return implicit
}

/** 树展示需要勾选的 key：working + 隐式父级（仅限当前树中存在的节点）。 */
export function displayCheckedKeys(workingKeys: Iterable<string>, treeKeys: Set<string>): string[] {
  const workingSet = new Set(workingKeys)
  const implicit = computeImplicitKeys(workingSet, treeKeys)
  const out: string[] = []
  for (const key of treeKeys) {
    if (key.startsWith('dir::')) continue
    if (workingSet.has(key) || implicit.has(key)) out.push(key)
  }
  return out
}
