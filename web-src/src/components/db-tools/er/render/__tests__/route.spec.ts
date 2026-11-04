import type { EdgeView } from '@antv/x6'
import { describe, expect, it } from 'vitest'
import { ROUTE_GAP, computeErRoute, erOrthRouter } from '../route'

// 截图中的真实几何：左表右边缘端口（adjustment_department_id，第 6 行）→ 右表左边缘端口（id，第 1 行）
const sourceBox = { x: 22.5, y: 43, width: 300, height: 344 }
const targetBox = { x: 457.5, y: 78, width: 300, height: 256 }
const source = { x: 322.5, y: 216.5 }
const target = { x: 457.5, y: 140.5 }

describe('computeErRoute', () => {
  it('面对面端口：垂直段落在两节点之间的空隙，不压住目标端口列', () => {
    const route = computeErRoute({
      source,
      target,
      sourceSide: 'right',
      targetSide: 'left',
      sourceBox,
      targetBox,
    })
    expect(route).toEqual([
      { x: 390, y: 216.5 },
      { x: 390, y: 140.5 },
    ])
    // 回归保护：折点必须离两端节点边框足够远，不能退回“沿目标节点边缘”
    expect(route[0].x).toBeGreaterThan(sourceBox.x + sourceBox.width + 20)
    expect(route[0].x).toBeLessThan(targetBox.x - 20)
  })

  it('同侧端口：垂直段绕到两节点外侧', () => {
    const route = computeErRoute({
      source,
      target,
      sourceSide: 'right',
      targetSide: 'right',
      sourceBox,
      targetBox,
    })
    expect(route).toEqual([
      { x: 757.5 + ROUTE_GAP, y: 216.5 },
      { x: 757.5 + ROUTE_GAP, y: 140.5 },
    ])
  })

  it('两端同高且面对面：直线，不产生折点', () => {
    expect(
      computeErRoute({
        source: { x: 322.5, y: 140.5 },
        target: { x: 457.5, y: 140.5 },
        sourceSide: 'right',
        targetSide: 'left',
        sourceBox,
        targetBox,
      })
    ).toEqual([])
  })

  it('自环：绕节点上方或下方闭环，不横穿卡片', () => {
    const route = computeErRoute({
      source: { x: 322.5, y: 216.5 },
      target: { x: 22.5, y: 120 },
      sourceSide: 'right',
      targetSide: 'left',
      sourceBox,
      targetBox: sourceBox,
      selfLoop: true,
    })
    expect(route).toHaveLength(4)
    expect(route[0].x).toBe(322.5 + ROUTE_GAP)
    expect(route[2].x).toBe(22.5 - ROUTE_GAP)
    expect(route[1].y).toBeLessThan(sourceBox.y)
    expect(route[2].y).toBeLessThan(sourceBox.y)
  })
})

describe('erOrthRouter', () => {
  const makeView = (sourcePort: string | null, targetPort: string | null, targetCell: unknown) =>
    ({
      sourceAnchor: source,
      targetAnchor: target,
      cell: {
        getSourceCell: () => ({ getBBox: () => sourceBox }),
        getTargetCell: () => targetCell,
        getSourcePortId: () => sourcePort,
        getTargetPortId: () => targetPort,
      },
    }) as unknown as EdgeView

  it('端口 ID 映射到正确的左右侧', () => {
    const view = makeView('pr-5', 'pl-0', { getBBox: () => targetBox })
    expect(erOrthRouter.call(view, [], {}, view)).toEqual([
      { x: 390, y: 216.5 },
      { x: 390, y: 140.5 },
    ])
  })

  it('无端口（折叠表）时按相对位置取向', () => {
    const view = makeView(null, null, { getBBox: () => targetBox })
    expect(erOrthRouter.call(view, [], {}, view)[0].x).toBe(390)
  })

  it('自环按同一节点处理', () => {
    const cell = { getBBox: () => sourceBox }
    const view = {
      sourceAnchor: source,
      targetAnchor: { x: 22.5, y: 120 },
      cell: {
        getSourceCell: () => cell,
        getTargetCell: () => cell,
        getSourcePortId: () => 'pr-5',
        getTargetPortId: () => 'pl-2',
      },
    } as unknown as EdgeView
    expect(erOrthRouter.call(view, [], {}, view)).toHaveLength(4)
  })

  it('拖拽中（端点指向坐标）不做路由', () => {
    const view = {
      sourceAnchor: source,
      targetAnchor: target,
      cell: { getSourceCell: () => null, getTargetCell: () => null },
    } as unknown as EdgeView
    expect(erOrthRouter.call(view, [], {}, view)).toEqual([])
  })
})
