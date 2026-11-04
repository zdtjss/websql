/**
 * ER 表节点 HTML 与几何（唯一几何来源）
 *
 * 几何约定（确认报告 5.3 / P1-7）：
 * - 所有行 box-sizing:border-box，固定高度与常量一一对应；
 * - 节点高度 = 上下边框 + 表头 + 注释 + 字段行 + 底部留白；
 * - 端口 Y = 上边框 + 表头 + 注释 + 行序号*行高 + 行高/2，保证圆点与字段行中心对齐。
 */
import { getERTheme, isDarkTheme, type ERTheme } from './theme'

export const NODE_WIDTH = 300
export const NODE_BORDER = 2
export const HEADER_HEIGHT = 30
export const COMMENT_HEIGHT = 20
export const COL_ROW_HEIGHT = 22
export const BOTTOM_PADDING = 4

export interface ERNodeColumn {
  name: string
  type: string
  comment: string
  primaryKey: boolean
}

export interface ERNodeData {
  id: string
  label: string
  /** 原始表名（与 id 相同，用于与 /er/schema 的 ERTable 结构对齐） */
  name?: string
  type?: 'TABLE' | 'VIEW'
  comment?: string
  columns?: ERNodeColumn[]
  uniqueKeys?: string[][]
  collapsed?: boolean
  junction?: boolean
  /** 导出打印主题：强制浅色高对比，避免深色图在白色背景上不可读 */
  print?: boolean
  /** 主题版本号：变化时触发 HTML 重渲染（用于主题增量更新） */
  themeVersion?: number
}

export interface PointLike {
  x: number
  y: number
}

/** 展开状态下的节点高度 */
export function getNodeHeight(data: Pick<ERNodeData, 'comment' | 'columns'>): number {
  let height = NODE_BORDER * 2 + HEADER_HEIGHT + BOTTOM_PADDING
  if (data.comment) height += COMMENT_HEIGHT
  if (data.columns?.length) height += data.columns.length * COL_ROW_HEIGHT
  return height
}

export function getNodeHeightByState(data: Pick<ERNodeData, 'comment' | 'columns'>, collapsed: boolean): number {
  if (collapsed) return NODE_BORDER * 2 + HEADER_HEIGHT
  return getNodeHeight(data)
}

/** 第 index 个字段行的端口 Y 坐标（相对节点左上角） */
export function portY(index: number, hasComment: boolean): number {
  return NODE_BORDER + HEADER_HEIGHT + (hasComment ? COMMENT_HEIGHT : 0) + index * COL_ROW_HEIGHT + COL_ROW_HEIGHT / 2
}

export type PortSide = 'left' | 'right'

/** 端口 ID：pl-<colIdx> / pr-<colIdx> */
export function portIdForIndex(index: number, side: PortSide): string {
  return `${side === 'left' ? 'pl' : 'pr'}-${index}`
}

/** 字段名 → 端口 ID；字段不存在返回 null（回退到节点中心） */
export function portIdFor(node: ERNodeData | undefined, colName: string, side: PortSide): string | null {
  if (!node?.columns || !colName) return null
  const index = node.columns.findIndex(c => c.name === colName)
  return index >= 0 ? portIdForIndex(index, side) : null
}

/** 端口 ID → 字段名 */
export function colNameByPortId(node: ERNodeData | undefined, portId: string | undefined): string {
  if (!node?.columns || !portId) return ''
  const match = /^(pl|pr)-(\d+)$/.exec(String(portId))
  if (!match) return ''
  const index = parseInt(match[2], 10)
  return Number.isNaN(index) ? '' : node.columns[index]?.name || ''
}

/** 字段行下标（用于高亮两端字段行） */
export function columnIndexFor(node: ERNodeData | undefined, colName: string): number {
  if (!node?.columns || !colName) return -1
  return node.columns.findIndex(c => c.name === colName)
}

/**
 * 按相对位置选择端口：
 * - 水平距离占优：两端取对面侧（源右→目标左，或源左→目标右）；
 * - 垂直距离占优：两端取同侧（避免上下关系时强行左右跨接）。
 * 注意：X6 smooth 连接器不读取端口法线，此策略只能缩短端点距离、减少长距离回绕，
 * 不能消除所有 S 形曲线（确认报告 P1-9）。
 */
export function pickPortSide(source: PointLike, target: PointLike): { source: PortSide; target: PortSide } {
  const dx = target.x - source.x
  const dy = target.y - source.y
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0 ? { source: 'right', target: 'left' } : { source: 'left', target: 'right' }
  }
  const sameSide: PortSide = dx >= 0 ? 'right' : 'left'
  return { source: sameSide, target: sameSide }
}

export function pickPortPair(
  source: PointLike,
  target: PointLike,
  sourceColIndex: number,
  targetColIndex: number
): { sourcePort: string; targetPort: string } {
  const sides = pickPortSide(source, target)
  const sourcePort = sourceColIndex >= 0 ? portIdForIndex(sourceColIndex, sides.source) : ''
  const targetPort = targetColIndex >= 0 ? portIdForIndex(targetColIndex, sides.target) : ''
  return { sourcePort, targetPort }
}

/** 端口组配置（左右各一组） */
export function buildPortGroups(theme: ERTheme) {
  const circleAttrs = () => ({
    circle: {
      r: 4,
      magnet: true,
      stroke: theme.portStroke,
      fill: theme.portFill,
      strokeWidth: 1,
    },
  })
  return {
    left: { position: { name: 'absolute' }, attrs: circleAttrs() },
    right: { position: { name: 'absolute' }, attrs: circleAttrs() },
  }
}

/** 为节点生成左右双端口（每个字段一组） */
export function buildNodePorts(data: ERNodeData, theme: ERTheme) {
  const hasComment = !!data.comment
  return {
    groups: buildPortGroups(theme),
    items: (data.columns || []).flatMap((_, index) => [
      { id: portIdForIndex(index, 'left'), group: 'left', args: { x: 0, y: portY(index, hasComment) } },
      { id: portIdForIndex(index, 'right'), group: 'right', args: { x: NODE_WIDTH, y: portY(index, hasComment) } },
    ]),
  }
}

function createEl(tag: string, cssText: string, text?: string): HTMLElement {
  const el = document.createElement(tag)
  el.style.cssText = cssText
  if (text !== undefined) el.textContent = text
  return el
}

/** 生成 ER 表节点 HTML（折叠态只渲染表头） */
export function createErTableHtml(cell: { id: string; getData: () => ERNodeData | undefined }): HTMLElement {
  const data = cell.getData() || ({} as ERNodeData)
  const theme = data.print ? getERTheme(false) : getERTheme(isDarkTheme())
  const collapsed = !!data.collapsed
  const nodeId = data.id || cell.id

  const root = document.createElement('div')
  root.setAttribute('data-node-id', nodeId)
  root.style.cssText = [
    'box-sizing:border-box',
    'width:100%',
    'height:100%',
    'display:flex',
    'flex-direction:column',
    'overflow:hidden',
    `background:${theme.nodeBg}`,
    `border:${NODE_BORDER}px solid ${theme.nodeBorder}`,
    'border-radius:6px',
    `box-shadow:${theme.nodeShadow}`,
    'font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",sans-serif',
    'transition:opacity 0.15s ease,border-color 0.15s ease',
  ].join(';')

  // 表头：固定 30px，box-sizing:border-box
  const header = createEl(
    'div',
    [
      'box-sizing:border-box',
      'height:30px',
      'flex:0 0 30px',
      'display:flex',
      'align-items:center',
      'gap:6px',
      'padding:0 10px',
      `background:${theme.headerBg}`,
      'font-size:12px',
      'font-weight:600',
      `color:${theme.headerText}`,
      'cursor:pointer',
      'user-select:none',
      'white-space:nowrap',
      'overflow:hidden',
    ].join(';')
  )
  header.className = 'er-table-header'
  header.setAttribute('data-table-id', nodeId)

  const toggle = createEl(
    'span',
    'cursor:pointer;display:inline-block;width:14px;text-align:center;flex-shrink:0;font-size:11px;opacity:0.9;',
    collapsed ? '▶' : '▾'
  )
  toggle.className = 'er-collapse-toggle'
  toggle.setAttribute('role', 'button')
  toggle.setAttribute('aria-label', collapsed ? '展开表字段' : '折叠表字段')
  header.appendChild(toggle)

  const title = createEl('span', 'flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;', data.label || nodeId)
  title.className = 'er-table-title'
  header.appendChild(title)

  if (data.junction) {
    const badge = createEl(
      'span',
      `font-size:9px;background:${theme.junctionTagBg};padding:1px 5px;border-radius:3px;font-weight:400;flex-shrink:0;color:${theme.headerSubText};`,
      '关联表'
    )
    badge.className = 'er-junction-badge'
    header.appendChild(badge)
  }

  if (data.type === 'VIEW') {
    const tag = createEl(
      'span',
      `font-size:9px;background:${theme.viewTagBg};padding:1px 5px;border-radius:3px;font-weight:400;flex-shrink:0;color:${theme.headerSubText};`,
      'VIEW'
    )
    tag.className = 'er-view-badge'
    header.appendChild(tag)
  }

  root.appendChild(header)

  if (!collapsed) {
    if (data.comment) {
      const comment = createEl(
        'div',
        [
          'box-sizing:border-box',
          'height:20px',
          'flex:0 0 20px',
          'display:flex',
          'align-items:center',
          'padding:0 10px',
          'font-size:10px',
          `color:${theme.commentColor}`,
          `border-bottom:1px solid ${theme.rowBorder}`,
          'white-space:nowrap',
          'overflow:hidden',
          'text-overflow:ellipsis',
        ].join(';'),
        data.comment
      )
      comment.className = 'er-table-comment'
      comment.title = data.comment
      root.appendChild(comment)
    }

    ;(data.columns || []).forEach((col, index) => {
      const row = createEl(
        'div',
        [
          'box-sizing:border-box',
          'height:22px',
          'flex:0 0 22px',
          'display:flex',
          'align-items:center',
          'gap:6px',
          'padding:0 10px',
          'font-size:10px',
          `border-bottom:1px solid ${theme.rowBorder}`,
          'user-select:text',
          'cursor:text',
        ].join(';')
      )
      row.className = 'er-col-row'
      row.setAttribute('data-row-index', String(index))
      row.setAttribute('title', `字段: ${col.name}\n类型: ${col.type}${col.comment ? '\n注释: ' + col.comment : ''}`)

      const nameSpan = createEl(
        'span',
        [
          'flex:2',
          'min-width:0',
          'overflow:hidden',
          'text-overflow:ellipsis',
          'white-space:nowrap',
          `color:${col.primaryKey ? theme.pkColor : theme.colNameColor}`,
          `font-weight:${col.primaryKey ? 600 : 500}`,
          'user-select:text',
        ].join(';'),
        `${col.primaryKey ? '🔑 ' : ''}${col.name}`
      )
      nameSpan.title = col.name
      row.appendChild(nameSpan)

      const typeSpan = createEl(
        'span',
        [
          'flex:1',
          'min-width:0',
          'overflow:hidden',
          'text-overflow:ellipsis',
          'white-space:nowrap',
          `color:${theme.colTypeColor}`,
          'font-family:Consolas,Monaco,monospace',
          'font-size:9px',
          'user-select:text',
        ].join(';'),
        col.type
      )
      typeSpan.title = col.type
      row.appendChild(typeSpan)

      const commentSpan = createEl(
        'span',
        [
          'flex:1',
          'min-width:0',
          'overflow:hidden',
          'text-overflow:ellipsis',
          'white-space:nowrap',
          `color:${theme.colCommentColor}`,
          'font-size:9px',
          'user-select:text',
        ].join(';'),
        col.comment || ''
      )
      commentSpan.title = col.comment || ''
      row.appendChild(commentSpan)

      root.appendChild(row)
    })

    // 底部留白：计入固定高度，保证端口公式与 CSS 完全一致
    root.appendChild(createEl('div', `flex:0 0 ${BOTTOM_PADDING}px;height:${BOTTOM_PADDING}px;`))
  }

  return root
}
