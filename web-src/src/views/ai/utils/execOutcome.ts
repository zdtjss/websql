/**
 * 危险 SQL 确认流的终态判定（纯函数，便于单测覆盖）。
 *
 * 背景：权限拒绝经 ToolErrorRecovery 转成工具结果而非 error 事件，
 * 若前端固定展示「已执行」会谎报结果；用户停止（cancelled）与模型重试（retract）
 * 同样必须区分。服务端通过 `tool_status` 事件给出结构化终态。
 */
export type ExecStreamSignals = {
  /** 收到 cancelled 事件（用户停止或服务端取消） */
  cancelled: boolean
  /** 收到 error 事件 */
  hasError: boolean
  /** 服务端 tool_status：executed | denied | cancelled，空串表示未收到 */
  toolStatus: string
}

export type ExecOutcome = {
  /** 展示用前缀文案（含状态图标） */
  label: string
  /** 是否为可信终态（false 表示状态未知，不得宣称成功） */
  confident: boolean
}

export function resolveExecOutcome(signals: ExecStreamSignals): ExecOutcome {
  if (signals.cancelled || signals.toolStatus === 'cancelled') {
    return { label: '⏹ 已取消', confident: true }
  }
  if (signals.toolStatus === 'denied') {
    return { label: '❌ 权限拒绝，未执行', confident: true }
  }
  if (signals.hasError) {
    return { label: '❌ 执行失败', confident: true }
  }
  if (signals.toolStatus === 'executed') {
    return { label: '✅ 已执行', confident: true }
  }
  return { label: 'ℹ️ 执行状态未知，请查看下方输出', confident: false }
}

/** 多条 SQL（批量确认）的终态聚合信号。 */
export type BatchExecSignals = {
  /** 本次提交的 SQL 条数（= 过渡卡张数） */
  total: number
  /** 服务端上报 executed 的条数 */
  executed: number
  /** 服务端上报 denied 的条数 */
  denied: number
  cancelled: boolean
  hasError: boolean
}

/**
 * 批量执行终态：只有全部 executed 才能宣称「已执行」。
 *
 * 单值 `toolStatus` 覆盖式赋值会把「先 denied 后 executed」误标为成功——
 * 一次 resume 会执行多条 exec_sql，服务端对每条都上报 tool_status。
 */
export function resolveBatchExecOutcome(signals: BatchExecSignals): ExecOutcome {
  const total = Math.max(signals.total, signals.executed + signals.denied)
  if (total > 0 && signals.executed === total) {
    return { label: '✅ 已执行', confident: true }
  }
  if (signals.executed > 0) {
    const progress = `（${signals.executed}/${total}）`
    return signals.cancelled
      ? { label: `⏹ 已终止，已执行 ${signals.executed}/${total}`, confident: true }
      : { label: `⚠️ 部分执行成功${progress}`, confident: true }
  }
  if (signals.denied > 0) {
    return { label: '❌ 权限拒绝，未执行', confident: true }
  }
  if (signals.cancelled) {
    return { label: '⏹ 已取消', confident: true }
  }
  if (signals.hasError) {
    return { label: '❌ 执行失败', confident: true }
  }
  return { label: 'ℹ️ 执行状态未知，请查看下方输出', confident: false }
}

/** 过渡态卡片前缀：确认流执行中展示，终态必须就地覆盖，不得残留在会话里。 */
export const PENDING_EXEC_LABEL = '⏳ 正在执行'

/** 拼接执行卡片正文（终态与过渡态共用，保证两侧格式一致）。 */
export function buildExecCardContent(label: string, sql: string, detail = ''): string {
  const head = `${label}：\n\`\`\`sql\n${sql}\n\`\`\``
  return detail ? `${head}\n${detail}` : head
}

/** 从卡片正文取回 SQL，用于把过渡卡改写为同一语句的终态卡。 */
function extractExecSQL(content: string): string | null {
  const matched = content.match(/```sql\n([\s\S]*?)\n```/)
  return matched ? matched[1] : null
}

/**
 * 把消息列表里的「⏳ 正在执行」过渡卡就地改写为终态卡。
 *
 * 返回被改写的张数；0 表示没找到（调用方需自行补一张，避免丢状态）。
 * 早期实现是在流结束后另推一张终态卡，导致会话里永远留着一张「正在执行」。
 *
 * 只从末尾向前收敛，且最多改写 `limit` 张：调用方传入本次请求推入的卡片数，
 * 避免把上一次异常流残留的卡片（或历史里同前缀的用户消息）一并改掉。
 */
export function settleExecCards(
  messages: { role?: string; content: string }[],
  label: string,
  detail = '',
  limit = Number.POSITIVE_INFINITY,
): number {
  let settled = 0
  for (let i = messages.length - 1; i >= 0 && settled < limit; i--) {
    const msg = messages[i]
    // 只改助手卡片：用户消息即使同前缀也不得被改写
    if (msg.role && msg.role !== 'assistant') continue
    const content = msg.content
    if (!content.startsWith(`${PENDING_EXEC_LABEL}：`)) continue
    const sql = extractExecSQL(content)
    if (sql === null) continue
    msg.content = buildExecCardContent(label, sql, detail)
    settled++
  }
  return settled
}
