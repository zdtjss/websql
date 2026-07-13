/**
 * 数据库监控面板共用的格式化工具函数。
 *
 * 这些函数原本散落在 DatabaseMonitorPanel.vue 内部，拆分组件后
 * 被概览 / 表统计等多个 Tab 复用，故抽取为独立模块。
 */

/** 将字节数格式化为 B/KB/MB/GB 字符串 */
export function formatBytes(val: unknown): string {
  if (!val || val === 0) return '0 B'
  const n = Number(val)
  if (isNaN(n) || n <= 0) return '0 B'
  if (n < 1024) return n + ' B'
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1073741824) return (n / 1048576).toFixed(2) + ' MB'
  return (n / 1073741824).toFixed(2) + ' GB'
}

/** 将数值格式化为带 K/M 后缀的紧凑字符串 */
export function formatNum(val: unknown): string {
  if (!val) return '0'
  const n = Number(val)
  if (isNaN(n)) return '0'
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}

/** 将秒数格式化为 "x天 x时 x分" 形式的运行时间字符串 */
export function formatUptime(seconds: unknown): string {
  const s = Number(seconds)
  if (!s || s <= 0 || isNaN(s)) return '-'
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const parts: string[] = []
  if (d > 0) parts.push(d + '天')
  if (h > 0) parts.push(h + '时')
  parts.push(m + '分')
  return parts.join(' ')
}
