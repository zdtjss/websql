/**
 * ER 图视觉令牌（浅色/深色/导出打印主题）——颜色与线宽的单一来源。
 *
 * 对比度约定：默认/悬停/选中态在对应背景上 ≥ 3:1；
 * 低置信 AI 关系使用 0.7 透明度（不再使用低于阈值的 0.55）。
 */

export interface ERTheme {
  isDark: boolean
  nodeBg: string
  nodeBorder: string
  nodeShadow: string
  headerBg: string
  headerText: string
  headerSubText: string
  viewTagBg: string
  junctionTagBg: string
  commentColor: string
  colNameColor: string
  colTypeColor: string
  colCommentColor: string
  rowBorder: string
  pkColor: string
  rowActiveBg: string
  edgeColor: string
  edgeHover: string
  edgeSelected: string
  edgeFk: string
  edgeAi: string
  edgeManual: string
  edgeAiLowOpacity: number
  edgeWidth: number
  edgeHoverWidth: number
  edgeWrapWidth: number
  labelText: string
  rel11: string
  rel1N: string
  relNM: string
  gridColor: string
  portStroke: string
  portFill: string
}

const LIGHT: Omit<ERTheme, 'isDark'> = {
  nodeBg: '#ffffff',
  nodeBorder: '#d9d9d9',
  nodeShadow: '0 1px 4px rgba(0,0,0,0.1)',
  headerBg: '#409eff',
  headerText: '#ffffff',
  headerSubText: 'rgba(255,255,255,0.85)',
  viewTagBg: 'rgba(255,255,255,0.25)',
  junctionTagBg: 'rgba(255,255,255,0.3)',
  commentColor: '#5a6673',
  colNameColor: '#303133',
  colTypeColor: '#5a6673',
  colCommentColor: '#6b7785',
  rowBorder: '#ebeef5',
  pkColor: '#b8791f',
  rowActiveBg: '#eaf3ff',
  edgeColor: '#5c6b7a',
  edgeHover: '#1890ff',
  edgeSelected: '#1f6feb',
  edgeFk: '#5c6b7a',
  edgeAi: '#6f5fc0',
  edgeManual: '#b8791f',
  edgeAiLowOpacity: 0.7,
  edgeWidth: 2,
  edgeHoverWidth: 3,
  edgeWrapWidth: 20,
  labelText: '#ffffff',
  rel11: '#3f8f2c',
  rel1N: '#2f7fd1',
  relNM: '#b8791f',
  gridColor: '#e0e0e0',
  portStroke: '#5F95FF',
  portFill: '#ffffff',
}

const DARK: Omit<ERTheme, 'isDark'> = {
  nodeBg: '#1e1e2e',
  nodeBorder: '#45475a',
  nodeShadow: '0 1px 4px rgba(0,0,0,0.4)',
  headerBg: '#313147',
  headerText: '#ffffff',
  headerSubText: 'rgba(255,255,255,0.82)',
  viewTagBg: 'rgba(255,255,255,0.18)',
  junctionTagBg: 'rgba(255,255,255,0.22)',
  commentColor: '#a6adc8',
  colNameColor: '#cdd6f4',
  colTypeColor: '#a6adc8',
  colCommentColor: '#9399b2',
  rowBorder: '#313244',
  pkColor: '#f0b95c',
  rowActiveBg: '#2a3a52',
  edgeColor: '#7fa0c0',
  edgeHover: '#89b4fa',
  edgeSelected: '#b4befe',
  edgeFk: '#7fa0c0',
  edgeAi: '#b9a6ff',
  edgeManual: '#f0b95c',
  edgeAiLowOpacity: 0.7,
  edgeWidth: 2,
  edgeHoverWidth: 3,
  edgeWrapWidth: 20,
  labelText: '#ffffff',
  rel11: '#7fb069',
  rel1N: '#6ba7e0',
  relNM: '#d9a441',
  gridColor: '#2a2a3c',
  portStroke: '#89b4fa',
  portFill: '#1e1e2e',
}

export function getERTheme(isDark: boolean): ERTheme {
  return { isDark, ...(isDark ? DARK : LIGHT) }
}

/** 从 DOM 读取当前主题（节点 HTML 由 X6 渲染，需与全局主题保持一致） */
export function isDarkTheme(): boolean {
  return document.documentElement.getAttribute('data-theme') === 'dark'
}

/** 关系类型徽标底色 */
export function relationColor(theme: ERTheme, type: string | undefined): string {
  if (type === '1:1') return theme.rel11
  if (type === 'N:M') return theme.relNM
  return theme.rel1N
}

/** 按来源取边颜色 */
export function originColor(theme: ERTheme, origin: string | undefined): string {
  if (origin === 'ai') return theme.edgeAi
  if (origin === 'manual') return theme.edgeManual
  return theme.edgeFk
}
