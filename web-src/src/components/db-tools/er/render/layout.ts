/**
 * ER 图布局：Dagre 预计算坐标 + 网格兜底。
 *
 * 方向语义（确认报告 5.6）：
 * - TB：dagre 输入边反向，让父表在上、子表在下（图边方向仍保持子→父）；
 * - LR：保持子→父从左到右。
 * 间距随最大节点高度动态计算，减少高表重叠。
 */
import { DagreLayout } from '@antv/layout'
import { NODE_WIDTH, getNodeHeightByState, type ERNodeData } from './nodeHtml'

export interface LayoutEdge {
  source: string
  target: string
}

export interface ComputeLayoutOptions {
  nodes: ERNodeData[]
  edges: LayoutEdge[]
  rankdir: 'TB' | 'LR'
  collapsed: Set<string>
}

export type LayoutPositions = Map<string, { x: number; y: number }>

function gridFallback(nodes: ERNodeData[], collapsed: Set<string>): LayoutPositions {
  const positions: LayoutPositions = new Map()
  const cols = Math.max(1, Math.ceil(Math.sqrt(nodes.length)))
  nodes.forEach((node, index) => {
    const row = Math.floor(index / cols)
    const col = index % cols
    const height = getNodeHeightByState(node, collapsed.has(node.id))
    positions.set(node.id, {
      x: col * (NODE_WIDTH + 60) + 40,
      y: row * (height + 60) + 40,
    })
  })
  return positions
}

/** 计算所有节点左上角坐标（与 graph 实例解耦，可在 addNode 之前调用） */
export async function computeLayout(options: ComputeLayoutOptions): Promise<LayoutPositions> {
  const { nodes, edges, rankdir, collapsed } = options
  const positions: LayoutPositions = new Map()
  if (nodes.length === 0) return positions

  const nodeMap = new Map(nodes.map(node => [node.id, node]))
  const heights = nodes.map(node => getNodeHeightByState(node, collapsed.has(node.id)))
  const maxHeight = heights.length > 0 ? Math.max(...heights) : 60
  const ranksep = Math.max(80, Math.min(240, Math.round(maxHeight * 0.6)))

  try {
    const dagreLayout = new DagreLayout({
      type: 'dagre',
      rankdir,
      nodesep: 60,
      ranksep,
      edgesep: 20,
      nodeSize: ((raw: any) => {
        const node = nodeMap.get(String(raw.id))
        return [NODE_WIDTH, node ? getNodeHeightByState(node, collapsed.has(String(raw.id))) : 60]
      }) as any,
    })
    // TB 下反向输入边，让父表排在上排；图边本身方向不变。
    const layoutEdges = rankdir === 'TB'
      ? edges.map(edge => ({ source: edge.target, target: edge.source }))
      : edges.map(edge => ({ source: edge.source, target: edge.target }))

    await dagreLayout.execute({
      nodes: nodes.map(node => ({ id: node.id })),
      edges: layoutEdges,
    })
    dagreLayout.forEachNode((layoutNode: any) => {
      if (layoutNode?.x == null || layoutNode?.y == null) return
      const id = String(layoutNode.id)
      const node = nodeMap.get(id)
      const height = node ? getNodeHeightByState(node, collapsed.has(id)) : 60
      positions.set(id, {
        x: layoutNode.x - NODE_WIDTH / 2,
        y: layoutNode.y - height / 2,
      })
    })
    dagreLayout.destroy()
    return positions
  } catch {
    return gridFallback(nodes, collapsed)
  }
}
