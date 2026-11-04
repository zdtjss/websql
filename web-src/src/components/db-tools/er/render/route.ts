/**
 * ER 图连线路由（X6 自定义 router）
 *
 * 不用内置 orth 的原因：边连接端口时 X6 把「端口圆点」的 bbox 当作终端 bbox，
 * orth 的免费折点 (目标端口.x, 源端口.y) 只与圆点自身做包含判断，两端跨行时必然落在圆点外，
 * 于是折点退化为「先水平到目标端口 x，再沿目标节点边框垂直」：
 * 垂直线压在目标节点的整列端口上，视觉上像连到了中间的某个字段。
 *
 * 这里按表节点 bbox 计算 Z 形折线：
 * - 端口面对面（源右→目标左 / 源左→目标右）：垂直段落在两节点之间的空隙；
 * - 端口同侧：垂直段绕到节点外侧；
 * - 自环：绕节点上方/下方形成闭环，避免横穿卡片。
 */
import type { EdgeView, PointLike } from '@antv/x6'

export interface RouteBox {
  x: number
  y: number
  width: number
  height: number
}

export type RouteSide = 'left' | 'right'

export interface OrthRouteInput {
  source: PointLike
  target: PointLike
  sourceSide: RouteSide
  targetSide: RouteSide
  sourceBox: RouteBox
  targetBox: RouteBox
  /** 源/目标为同一张表（自环） */
  selfLoop?: boolean
}

/** 折线距节点边框的保留距离 */
export const ROUTE_GAP = 24
/** 自环折线的外扩距离 */
const LOOP_GAP = 32

function rightEdge(box: RouteBox): number {
  return box.x + box.width
}

function centerOf(box: RouteBox): PointLike {
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 }
}

/** 自环：绕节点上方或下方（端口在节点上下半区就近选择）走一圈 */
function selfLoopRoute(input: OrthRouteInput): PointLike[] {
  const { source, target, sourceSide, targetSide, sourceBox } = input
  const box = sourceBox
  const midY = (source.y + target.y) / 2
  const bendY = midY <= box.y + box.height / 2 ? box.y - LOOP_GAP : box.y + box.height + LOOP_GAP
  const outX = sourceSide === 'right' ? rightEdge(box) + ROUTE_GAP : box.x - ROUTE_GAP
  const inX = targetSide === 'right' ? rightEdge(box) + ROUTE_GAP : box.x - ROUTE_GAP
  return [
    { x: outX, y: source.y },
    { x: outX, y: bendY },
    { x: inX, y: bendY },
    { x: inX, y: target.y },
  ]
}

/** 计算正交折线（返回折点，不含两端锚点） */
export function computeErRoute(input: OrthRouteInput): PointLike[] {
  const { source, target, sourceSide, targetSide, sourceBox, targetBox, selfLoop } = input
  if (selfLoop) return selfLoopRoute(input)

  const sourceRight = rightEdge(sourceBox)
  const targetRight = rightEdge(targetBox)
  const facing = sourceSide !== targetSide

  let bendX: number
  if (facing) {
    // 两个端口朝向对方：垂直段落在两节点之间的空隙中线；
    // 节点横向重叠（gap <= 0）时退到源节点外侧，避免折线进入目标卡片。
    const gapStart = sourceSide === 'right' ? sourceRight : sourceBox.x
    const gapEnd = sourceSide === 'right' ? targetBox.x : targetRight
    bendX = gapEnd > gapStart
      ? (gapStart + gapEnd) / 2
      : gapStart + (sourceSide === 'right' ? ROUTE_GAP : -ROUTE_GAP)
  } else {
    // 同侧端口：垂直段绕到两节点外侧，再垂直对齐后回到端口
    bendX = sourceSide === 'right'
      ? Math.max(sourceRight, targetRight) + ROUTE_GAP
      : Math.min(sourceBox.x, targetBox.x) - ROUTE_GAP
  }

  // 两端同高：面对面时为一条直线，同侧时只保留一个折点
  if (Math.abs(source.y - target.y) < 0.5) {
    return facing ? [] : [{ x: bendX, y: source.y }]
  }

  return [
    { x: bendX, y: source.y },
    { x: bendX, y: target.y },
  ]
}

/** 端口 ID（pl-<idx>/pr-<idx>）→ 端口所在侧 */
function sideOfPort(portId: string | null | undefined): RouteSide | null {
  if (!portId) return null
  if (portId.startsWith('pr-')) return 'right'
  if (portId.startsWith('pl-')) return 'left'
  return null
}

/** 无端口（折叠表/兜底锚点）时按另一节点的相对位置取向 */
function inferSide(box: RouteBox, otherBox: RouteBox): RouteSide {
  const dx = otherBox.x + otherBox.width / 2 - (box.x + box.width / 2)
  return dx >= 0 ? 'right' : 'left'
}

/**
 * X6 router 适配：从 edgeView 读取节点 bbox、端口与锚点，交给 computeErRoute。
 * 端点指向坐标（拖拽中）或用户自带了折点时保持原样。
 */
export function erOrthRouter(
  this: EdgeView,
  vertices: PointLike[],
  _options: unknown,
  edgeView: EdgeView
): PointLike[] {
  if (vertices.length > 0) return vertices

  const edge = edgeView.cell
  const sourceCell = edge.getSourceCell()
  const targetCell = edge.getTargetCell()
  if (!sourceCell || !targetCell) return vertices

  const sourceBox = sourceCell.getBBox()
  const targetBox = targetCell.getBBox()
  const source = edgeView.sourceAnchor || centerOf(sourceBox)
  const target = edgeView.targetAnchor || centerOf(targetBox)
  const sourceSide = sideOfPort(edge.getSourcePortId()) || inferSide(sourceBox, targetBox)
  const targetSide = sideOfPort(edge.getTargetPortId()) || inferSide(targetBox, sourceBox)

  return computeErRoute({
    source,
    target,
    sourceSide,
    targetSide,
    sourceBox,
    targetBox,
    selfLoop: sourceCell === targetCell,
  })
}
