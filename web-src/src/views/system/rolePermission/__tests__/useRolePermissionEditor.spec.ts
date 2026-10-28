import { describe, expect, it } from 'vitest'
import { useRolePermissionEditor, type EditorRole } from '../useRolePermissionEditor'
import { permToKey, type RolePower } from '../permissionPolicy'

function power(
  connId: string,
  schemaName: string | null,
  tableName: string | null,
  columnName: string | null,
): RolePower {
  let level: RolePower['level'] = 'conn'
  if (columnName) level = 'column'
  else if (tableName) level = 'table'
  else if (schemaName) level = 'schema'
  return { connId, schemaName, tableName, columnName, level }
}

function roleWith(powers: RolePower[]): EditorRole {
  return {
    id: 'r1',
    name: '角色一',
    viewClassic: 1,
    allowModify: 1,
    aiAllowModify: 1,
    permissionDigest: 'digest-1',
    powerList: powers,
  }
}

describe('useRolePermissionEditor', () => {
  it('无变更时不发请求（buildPayload 返回 null）', () => {
    const editor = useRolePermissionEditor()
    const role = roleWith([power('c1', 's1', 't1', null)])
    editor.load(role)
    const payload = editor.buildPayload(role, {
      viewClassic: 1,
      allowModify: 1,
      aiAllowModify: 1,
    })
    expect(payload).toBeNull()
  })

  it('能力开关变化也触发保存，并携带 expectedDigest', () => {
    const editor = useRolePermissionEditor()
    const role = roleWith([power('c1', 's1', 't1', null)])
    editor.load(role)
    const payload = editor.buildPayload(role, {
      viewClassic: 1,
      allowModify: 1,
      aiAllowModify: 0,
    })
    expect(payload).not.toBeNull()
    expect(payload?.expectedDigest).toBe('digest-1')
    expect(payload?.aiAllowModify).toBe(0)
    expect(payload?.addPowers).toHaveLength(0)
    expect(payload?.delPowers).toHaveLength(0)
  })

  it('取消子表后只删除子表，schema 保留', () => {
    const editor = useRolePermissionEditor()
    const schema = power('c1', 's1', null, null)
    const table = power('c1', 's1', 't1', null)
    editor.load(roleWith([schema, table]))

    editor.toggle(permToKey(table), false)
    const payload = editor.buildPayload(roleWith([schema, table]), {
      viewClassic: 1,
      allowModify: 1,
      aiAllowModify: 1,
    })
    expect(payload?.delPowers.map(permToKey)).toEqual([permToKey(table)])
    expect(payload?.addPowers).toHaveLength(0)
  })

  it('取消父节点会级联删除子级', () => {
    const editor = useRolePermissionEditor()
    const schema = power('c1', 's1', null, null)
    const table = power('c1', 's1', 't1', null)
    const column = power('c1', 's1', 't1', 'id')
    editor.load(roleWith([schema, table, column]))

    editor.toggle(permToKey(table), false)
    expect(editor.isWorking(permToKey(table))).toBe(false)
    expect(editor.isWorking(permToKey(column))).toBe(false)
    expect(editor.isWorking(permToKey(schema))).toBe(true)
  })

  it('保存成功用新 digest 刷新 baseline；冲突只标记不覆盖', () => {
    const editor = useRolePermissionEditor()
    const role = roleWith([power('c1', 's1', 't1', null)])
    editor.load(role)
    editor.toggle(permToKey(power('c1', 's1', 't1', 'id')), true)

    editor.markSaved('digest-2')
    expect(editor.state.baselineDigest).toBe('digest-2')
    expect(editor.state.baseline.has(permToKey(power('c1', 's1', 't1', 'id')))).toBe(true)

    editor.toggle(permToKey(power('c1', 's1', 't2', null)), true)
    editor.markConflict()
    expect(editor.state.conflict).toBe(true)
    // 冲突时不清空 working，交由界面提示重新加载
    expect(editor.state.working.has(permToKey(power('c1', 's1', 't2', null)))).toBe(true)
  })

  it('取消编辑回到 baseline', () => {
    const editor = useRolePermissionEditor()
    const role = roleWith([power('c1', 's1', 't1', null)])
    editor.load(role)
    editor.toggle(permToKey(power('c1', 's1', 't1', 'id')), true)
    editor.reset()
    expect(editor.isWorking(permToKey(power('c1', 's1', 't1', 'id')))).toBe(false)
    expect(editor.state.conflict).toBe(false)
  })

  it('隐式父级用于展示但不写入 working', () => {
    const editor = useRolePermissionEditor()
    const column = power('c1', 's1', 't1', 'id')
    editor.load(roleWith([column]))
    const treeKeys = new Set([
      permToKey(power('c1', null, null, null)),
      permToKey(power('c1', 's1', null, null)),
      permToKey(power('c1', 's1', 't1', null)),
      permToKey(column),
    ])
    const checked = editor.checkedKeysFor(treeKeys)
    expect(checked).toContain(permToKey(column))
    expect(checked).toContain(permToKey(power('c1', 's1', 't1', null)))
    expect(editor.state.working.has(permToKey(power('c1', 's1', 't1', null)))).toBe(false)
  })
})
