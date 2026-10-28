import { describe, expect, it } from 'vitest'
import {
  buildDiff,
  displayCheckedKeys,
  isDescendantOf,
  keyLevel,
  normalizeForDiff,
  parseKey,
  permToKey,
  type RolePower,
} from '../permissionPolicy'

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

describe('permissionPolicy 基础解析', () => {
  it('permToKey 与 parseKey 互逆', () => {
    const p = power('c1', 's1', 'orders', 'id')
    const key = permToKey(p)
    expect(key).toBe('c1::s1::orders::id')
    expect(parseKey(key)).toMatchObject({
      connId: 'c1',
      schemaName: 's1',
      tableName: 'orders',
      columnName: 'id',
      level: 'column',
    })
  })

  it('keyLevel 识别四级与目录', () => {
    expect(keyLevel('dir::root')).toBe('dir')
    expect(keyLevel('c1')).toBe('conn')
    expect(keyLevel('c1::s1')).toBe('schema')
    expect(keyLevel('c1::s1::t1')).toBe('table')
    expect(keyLevel('c1::s1::t1::col')).toBe('column')
  })

  it('isDescendantOf 只认严格下级', () => {
    expect(isDescendantOf('c1::s1::t1::id', 'c1::s1::t1')).toBe(true)
    expect(isDescendantOf('c1::s1::t1', 'c1::s1::t1')).toBe(false)
    expect(isDescendantOf('c1::s1::t2', 'c1::s1::t1')).toBe(false)
  })
})

describe('normalizeForDiff', () => {
  it('有活跃子级时移除冗余 schema', () => {
    const powers = [power('c1', 's1', null, null), power('c1', 's1', 't1', null)]
    const active = [power('c1', 's1', 't1', null)]
    const result = normalizeForDiff(powers, active)
    expect(result.map(permToKey)).toEqual(['c1::s1::t1'])
  })

  it('子级全部取消后保留 schema', () => {
    const powers = [power('c1', 's1', null, null), power('c1', 's1', 't1', null)]
    const active = [power('c1', 's1', null, null)]
    const result = normalizeForDiff(powers, active)
    // 期望保留了 schema；如果子表最终仍活跃，则由 buildDiff 与 working 对比决定删除
    expect(result.map(permToKey)).toContain('c1::s1')
  })
})

describe('buildDiff 文档专项用例', () => {
  it('无编辑时不产生 add/del', () => {
    const baseline = [power('c1', null, null, null), power('c1', 's1', 't1', null)]
    const { addPowers, delPowers } = buildDiff(baseline, [...baseline])
    expect(addPowers).toHaveLength(0)
    expect(delPowers).toHaveLength(0)
  })

  it('Schema + 最后一个子表全部取消 → 删除 schema 与子表', () => {
    const baseline = [power('c1', 's1', null, null), power('c1', 's1', 't1', null)]
    const working: RolePower[] = []
    const { addPowers, delPowers } = buildDiff(baseline, working)
    expect(addPowers).toHaveLength(0)
    expect(delPowers.map(permToKey).sort()).toEqual(['c1::s1', 'c1::s1::t1'])
  })

  it('Schema + 子表切换 → 只切换子表，不动 schema', () => {
    const baseline = [power('c1', 's1', null, null), power('c1', 's1', 't1', null)]
    const working = [power('c1', 's1', null, null), power('c1', 's1', 't2', null)]
    const { addPowers, delPowers } = buildDiff(baseline, working)
    expect(addPowers.map(permToKey)).toEqual(['c1::s1::t2'])
    expect(delPowers.map(permToKey)).toEqual(['c1::s1::t1'])
  })

  it('仅取消子表保留 Schema → 删除子表，保留 schema', () => {
    const baseline = [power('c1', 's1', null, null), power('c1', 's1', 't1', null)]
    const working = [power('c1', 's1', null, null)]
    const { addPowers, delPowers } = buildDiff(baseline, working)
    expect(addPowers).toHaveLength(0)
    expect(delPowers.map(permToKey)).toEqual(['c1::s1::t1'])
  })

  it('重复保存幂等：第二次 diff 为空', () => {
    const baseline = [power('c1', 's1', 't1', null)]
    const working = [power('c1', 's1', 't1', null), power('c1', 's1', 't1', 'id')]
    const first = buildDiff(baseline, working)
    expect(first.addPowers.map(permToKey)).toEqual(['c1::s1::t1::id'])
    const second = buildDiff(working, working)
    expect(second.addPowers).toHaveLength(0)
    expect(second.delPowers).toHaveLength(0)
  })
})

describe('displayCheckedKeys', () => {
  it('包含 working 与隐式父级', () => {
    const treeKeys = new Set([
      'c1',
      'c1::s1',
      'c1::s1::t1',
      'c1::s1::t1::id',
      'c2',
    ])
    const keys = displayCheckedKeys(['c1::s1::t1::id'], treeKeys)
    expect(keys.sort()).toEqual(['c1', 'c1::s1', 'c1::s1::t1', 'c1::s1::t1::id'])
  })
})
