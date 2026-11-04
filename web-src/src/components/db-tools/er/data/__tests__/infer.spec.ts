import { describe, expect, it } from 'vitest'
import type { ERRelation, ERTable } from '../types'
import {
  constraintGroupKey,
  displayRelationType,
  groupRelationsByConstraint,
  inferRelationTypes,
  isJunctionTable,
  isUniqueColumnSet,
  mergeAiRelations,
  relationKey,
  uniqueKeysOf,
} from '../infer'

function table(name: string, columns: { name: string; primaryKey?: boolean }[], uniqueKeys: string[][] = []): ERTable {
  return {
    name,
    type: 'TABLE',
    comment: '',
    columns: columns.map(col => ({ name: col.name, type: 'bigint', comment: '', primaryKey: !!col.primaryKey })),
    uniqueKeys,
  }
}

function relation(partial: Partial<ERRelation> & { source: string; target: string }): ERRelation {
  return {
    sourceCol: '',
    targetCol: '',
    constraintName: '',
    ...partial,
  }
}

const parent = table('parent', [{ name: 'id', primaryKey: true }, { name: 'code' }], [['id']])
const child = table('child', [{ name: 'id', primaryKey: true }, { name: 'parent_id' }], [['id']])

describe('uniqueKeysOf / isUniqueColumnSet', () => {
  it('从 uniqueKeys 与主键列派生唯一键组', () => {
    const keys = uniqueKeysOf(child)
    expect(keys).toContainEqual(['id'])
  })

  it('唯一键 K ⊆ 外键列组时判定为唯一（列顺序无关）', () => {
    const orderTable = table(
      'orders',
      [{ name: 'id', primaryKey: true }, { name: 'a' }, { name: 'b' }],
      [['b', 'a']]
    )
    expect(isUniqueColumnSet(orderTable, ['a', 'b'])).toBe(true)
    expect(isUniqueColumnSet(orderTable, ['a'])).toBe(false)
  })

  it('super set（唯一键包含外键列）不算覆盖', () => {
    const orderTable = table(
      'orders',
      [{ name: 'id', primaryKey: true }, { name: 'a' }, { name: 'b' }],
      [['a', 'b', 'id']]
    )
    expect(isUniqueColumnSet(orderTable, ['a', 'b'])).toBe(false)
  })
})

describe('inferRelationTypes', () => {
  it('普通外键为 1:N，不会因为目标列是主键而误判 1:1', () => {
    const target = table('parent', [{ name: 'id', primaryKey: true }, { name: 'code' }], [['id'], ['code']])
    const relations = [relation({ source: 'child', sourceCol: 'parent_id', target: 'parent', targetCol: 'id', constraintName: 'fk_child_parent' })]
    inferRelationTypes([target, child], relations)
    expect(relations[0].relationType).toBe('1:N')
  })

  it('源端外键列唯一时为 1:1', () => {
    const uniqueChild = table('child', [{ name: 'id', primaryKey: true }, { name: 'parent_id' }], [['parent_id']])
    const relations = [relation({ source: 'child', sourceCol: 'parent_id', target: 'parent', targetCol: 'id', constraintName: 'fk_child_parent' })]
    inferRelationTypes([parent, uniqueChild], relations)
    expect(relations[0].relationType).toBe('1:1')
  })

  it('复合外键按约束成组，整组唯一时为 1:1', () => {
    const compositeParent = table('parent', [{ name: 'a', primaryKey: true }, { name: 'b', primaryKey: true }], [['a', 'b']])
    const compositeChild = table('child', [{ name: 'a' }, { name: 'b' }], [['a', 'b']])
    const relations = [
      relation({ source: 'child', sourceCol: 'a', target: 'parent', targetCol: 'a', constraintName: 'fk_composite' }),
      relation({ source: 'child', sourceCol: 'b', target: 'parent', targetCol: 'b', constraintName: 'fk_composite' }),
    ]
    inferRelationTypes([compositeParent, compositeChild], relations)
    expect(relations.map(r => r.relationType)).toEqual(['1:1', '1:1'])
  })

  it('中间表标记 junction，边不再标 N:M', () => {
    const link = table('link', [
      { name: 'a', primaryKey: true },
      { name: 'b', primaryKey: true },
      { name: 'other_id' },
    ])
    const relations = [
      relation({ source: 'link', sourceCol: 'a', target: 'parent', targetCol: 'a', constraintName: 'fk_link_1' }),
      relation({ source: 'link', sourceCol: 'b', target: 'parent', targetCol: 'b', constraintName: 'fk_link_1' }),
      relation({ source: 'link', sourceCol: 'other_id', target: 'child', targetCol: 'id', constraintName: 'fk_link_2' }),
    ]
    inferRelationTypes([parent, child, link], relations)
    expect(link.junction).toBe(true)
    // 中间表只由节点徽标表达；边仍按源端唯一性给出 1:1 / 1:N，不再标 N:M
    expect(relations.some(r => r.relationType === 'N:M')).toBe(false)
    expect(relations[2].relationType).toBe('1:N')
  })

  it('AI/手动关系不参与自动推断', () => {
    const ai = relation({ source: 'child', sourceCol: 'parent_id', target: 'parent', targetCol: 'id', origin: 'ai', relationType: '1:1' })
    inferRelationTypes([parent, child], [ai])
    expect(ai.relationType).toBe('1:1')
  })
})

describe('groupRelationsByConstraint', () => {
  it('复合外键按约束名成组，SQLite 无约束名时按 constraintId 成组', () => {
    const fkA = relation({ source: 'child', sourceCol: 'a', target: 'parent', targetCol: 'a', constraintName: 'fk1' })
    const fkB = relation({ source: 'child', sourceCol: 'b', target: 'parent', targetCol: 'b', constraintName: 'fk1' })
    const sqliteA = relation({ source: 'child', sourceCol: 'a', target: 'parent', targetCol: 'a', constraintId: '2' })
    const sqliteB = relation({ source: 'child', sourceCol: 'b', target: 'parent', targetCol: 'b', constraintId: '2' })
    const groups = groupRelationsByConstraint([fkA, fkB, sqliteA, sqliteB])
    expect(groups.get(constraintGroupKey(fkA))).toHaveLength(2)
    expect(groups.get(constraintGroupKey(sqliteA))).toHaveLength(2)
  })

  it('无约束名的单列边各自成组', () => {
    const first = relation({ source: 'child', sourceCol: 'a', target: 'parent', targetCol: 'a' })
    const second = relation({ source: 'child', sourceCol: 'b', target: 'parent', targetCol: 'b' })
    const groups = groupRelationsByConstraint([first, second])
    expect(groups.size).toBe(2)
  })
})

describe('isJunctionTable', () => {
  it('主键不完全为外键列时不是中间表', () => {
    const link = table('link', [{ name: 'id', primaryKey: true }, { name: 'a' }, { name: 'b' }])
    const groups = groupRelationsByConstraint([
      relation({ source: 'link', sourceCol: 'a', target: 'p', targetCol: 'a', constraintName: 'fk1' }),
      relation({ source: 'link', sourceCol: 'b', target: 'q', targetCol: 'b', constraintName: 'fk2' }),
    ])
    expect(isJunctionTable(link, groups)).toBe(false)
  })
})

describe('mergeAiRelations', () => {
  it('过滤不存在的表/字段、自环与反向重复', () => {
    const sentTables = [parent, child]
    const existing = [relation({ source: 'child', sourceCol: 'parent_id', target: 'parent', targetCol: 'id', constraintName: 'fk1' })]
    const inferred = [
      relation({ source: 'child', sourceCol: 'parent_id', target: 'parent', targetCol: 'id' }), // 同向重复
      relation({ source: 'parent', sourceCol: 'id', target: 'child', targetCol: 'parent_id' }), // 反向重复
      relation({ source: 'ghost', sourceCol: 'id', target: 'parent', targetCol: 'id' }), // 表不存在
      relation({ source: 'child', sourceCol: 'missing', target: 'parent', targetCol: 'id' }), // 字段不存在
      relation({ source: 'child', sourceCol: 'id', target: 'child', targetCol: 'id' }), // 自环
      relation({ source: 'child', sourceCol: 'id', target: 'parent', targetCol: 'id', relationType: '1:1', confidence: 'high' }), // 合法
    ]
    const result = mergeAiRelations(inferred, sentTables, existing)
    expect(result.accepted).toHaveLength(1)
    expect(result.accepted[0].origin).toBe('ai')
    expect(result.accepted[0].relationType).toBe('1:1')
    expect(result.skipped).toBe(5)
  })
})

describe('displayRelationType', () => {
  it('边方向为子→父，1:N 展示为 N:1', () => {
    expect(displayRelationType('1:N')).toBe('N:1')
    expect(displayRelationType('1:1')).toBe('1:1')
    expect(displayRelationType('N:M')).toBe('N:M')
    expect(relationKey({ source: 'a', sourceCol: 'x', target: 'b', targetCol: 'y' })).toBe('a.x->b.y')
  })
})
