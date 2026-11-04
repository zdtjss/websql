import { describe, expect, it } from 'vitest'
import {
  BOTTOM_PADDING,
  COL_ROW_HEIGHT,
  COMMENT_HEIGHT,
  HEADER_HEIGHT,
  NODE_BORDER,
  NODE_WIDTH,
  colNameByPortId,
  getNodeHeight,
  getNodeHeightByState,
  pickPortPair,
  pickPortSide,
  portIdForIndex,
  portY,
} from '../nodeHtml'

describe('节点几何', () => {
  it('高度包含上下边框，与端口公式一致', () => {
    const data = {
      comment: '订单明细',
      columns: [{ name: 'a', type: 'int', comment: '', primaryKey: false }],
    }
    const expected = NODE_BORDER * 2 + HEADER_HEIGHT + COMMENT_HEIGHT + COL_ROW_HEIGHT + BOTTOM_PADDING
    expect(getNodeHeight(data)).toBe(expected)
  })

  it('折叠后仅保留表头与边框', () => {
    expect(getNodeHeightByState({ comment: 'x', columns: [] }, true)).toBe(NODE_BORDER * 2 + HEADER_HEIGHT)
  })

  it('端口 Y 为字段行中心（含边框与注释偏移）', () => {
    expect(portY(0, false)).toBe(NODE_BORDER + HEADER_HEIGHT + COL_ROW_HEIGHT / 2)
    expect(portY(2, true)).toBe(NODE_BORDER + HEADER_HEIGHT + COMMENT_HEIGHT + 2 * COL_ROW_HEIGHT + COL_ROW_HEIGHT / 2)
  })
})

describe('端口映射', () => {
  const node = {
    id: 'orders',
    label: 'orders',
    columns: [
      { name: 'id', type: 'bigint', comment: '', primaryKey: true },
      { name: 'user_id', type: 'bigint', comment: '', primaryKey: false },
    ],
  }

  it('端口 ID 带方向前缀', () => {
    expect(portIdForIndex(1, 'left')).toBe('pl-1')
    expect(portIdForIndex(1, 'right')).toBe('pr-1')
  })

  it('端口 ID 反查字段名', () => {
    expect(colNameByPortId(node, 'pr-1')).toBe('user_id')
    expect(colNameByPortId(node, 'p-1')).toBe('')
    expect(colNameByPortId(node, undefined)).toBe('')
  })
})

describe('pickPortPair', () => {
  it('水平方向占优时取对面侧', () => {
    expect(pickPortSide({ x: 0, y: 0 }, { x: 400, y: 30 })).toEqual({ source: 'right', target: 'left' })
    expect(pickPortSide({ x: 400, y: 0 }, { x: 0, y: 30 })).toEqual({ source: 'left', target: 'right' })
  })

  it('垂直方向占优时取同侧，避免上下关系强行左右跨接', () => {
    expect(pickPortSide({ x: 0, y: 0 }, { x: 20, y: 300 })).toEqual({ source: 'right', target: 'right' })
    expect(pickPortSide({ x: 200, y: 0 }, { x: 0, y: 300 })).toEqual({ source: 'left', target: 'left' })
  })

  it('生成左右双端口配对', () => {
    const pair = pickPortPair({ x: 0, y: 0 }, { x: 400, y: 0 }, 1, 2)
    expect(pair).toEqual({ sourcePort: 'pr-1', targetPort: 'pl-2' })
  })

  it('字段不存在时返回空端口（回退节点中心）', () => {
    const pair = pickPortPair({ x: 0, y: 0 }, { x: 400, y: 0 }, -1, -1)
    expect(pair).toEqual({ sourcePort: '', targetPort: '' })
    expect(NODE_WIDTH).toBe(300)
  })
})
