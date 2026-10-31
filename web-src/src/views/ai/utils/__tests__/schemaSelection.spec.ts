import { describe, expect, it } from 'vitest'
import { resolveSoleSchemaSelection } from '../schemaSelection'

/**
 * D-01 回归：单连接 + 单 Schema 时选择器被隐藏，若不再自动选中，
 * 发送会被「请选择至少一个数据库 Schema」拦死，AI 对话页不可用。
 */
describe('resolveSoleSchemaSelection', () => {
  it('单连接单 Schema 自动选中', () => {
    const list = [{ connId: 'c1', dbSchema: 'main', schemas: [{ name: 'main' }] }]
    expect(resolveSoleSchemaSelection(list, [])).toEqual(['c1::main'])
  })

  it('schemas 为空时回退到 dbSchema', () => {
    const list = [{ connId: 'c1', dbSchema: 'public', schemas: [] }]
    expect(resolveSoleSchemaSelection(list, [])).toEqual(['c1::public'])
  })

  it('已有选中项时不覆盖', () => {
    const list = [{ connId: 'c1', dbSchema: 'main', schemas: [{ name: 'main' }] }]
    expect(resolveSoleSchemaSelection(list, ['c9::other'])).toEqual([])
  })

  it('多连接不自动选中', () => {
    const list = [
      { connId: 'c1', dbSchema: 'main', schemas: [{ name: 'main' }] },
      { connId: 'c2', dbSchema: 'main', schemas: [{ name: 'main' }] },
    ]
    expect(resolveSoleSchemaSelection(list, [])).toEqual([])
  })

  it('单连接多 Schema 不自动选中', () => {
    const list = [{ connId: 'c1', dbSchema: 'main', schemas: [{ name: 'a' }, { name: 'b' }] }]
    expect(resolveSoleSchemaSelection(list, [])).toEqual([])
  })

  it('缺少 connId 或 Schema 名时不猜默认值', () => {
    expect(resolveSoleSchemaSelection([{ dbSchema: 'main', schemas: [{ name: 'main' }] }], [])).toEqual([])
    expect(resolveSoleSchemaSelection([{ connId: 'c1', schemas: [] }], [])).toEqual([])
    expect(resolveSoleSchemaSelection([{ connId: 'c1', dbSchema: '   ', schemas: [] }], [])).toEqual([])
    expect(resolveSoleSchemaSelection([], [])).toEqual([])
  })

  it('选中值按 connId::schema 组装并去除首尾空白', () => {
    const list = [{ connId: ' c1 ', dbSchema: ' main ', schemas: [] }]
    expect(resolveSoleSchemaSelection(list, [])).toEqual(['c1::main'])
  })
})
