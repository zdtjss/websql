import { describe, expect, it } from 'vitest'
import {
  PENDING_EXEC_LABEL,
  buildExecCardContent,
  resolveExecOutcome,
  settleExecCards,
} from '../execOutcome'

/**
 * F-27 执行卡片终态收敛：
 * 「⏳ 正在执行」是过渡态，流结束后必须就地改写为终态，
 * 不允许在会话里再追加一张卡片造成「正在执行」与终态并存。
 */
describe('settleExecCards', () => {
  it('就地覆盖过渡卡，不增加消息数量', () => {
    const messages = [
      { content: '无关消息' },
      { content: buildExecCardContent(PENDING_EXEC_LABEL, 'UPDATE t SET a = 1') },
    ]
    const before = messages.length
    const settled = settleExecCards(messages, '✅ 已执行')
    expect(settled).toBe(1)
    expect(messages.length).toBe(before)
    expect(messages[1].content).toBe(
      '✅ 已执行：\n```sql\nUPDATE t SET a = 1\n```',
    )
    expect(messages[1].content).not.toContain(PENDING_EXEC_LABEL)
  })

  it('批量场景覆盖全部过渡卡并保留各自 SQL', () => {
    const messages = [
      { content: buildExecCardContent(PENDING_EXEC_LABEL, 'DELETE FROM a') },
      { content: buildExecCardContent(PENDING_EXEC_LABEL, 'TRUNCATE TABLE b') },
    ]
    expect(settleExecCards(messages, '❌ 权限拒绝，未执行')).toBe(2)
    expect(messages[0].content).toContain('DELETE FROM a')
    expect(messages[1].content).toContain('TRUNCATE TABLE b')
    expect(messages.every((m) => !m.content.startsWith(PENDING_EXEC_LABEL))).toBe(true)
  })

  it('失败终态把错误详情附在卡片尾部', () => {
    const messages = [
      { content: buildExecCardContent(PENDING_EXEC_LABEL, 'SELECT 1') },
    ]
    settleExecCards(messages, '❌ 执行失败', 'no such table: t')
    expect(messages[0].content).toContain('❌ 执行失败')
    expect(messages[0].content).toContain('no such table: t')
  })

  it('没有过渡卡时返回 0，由调用方补卡', () => {
    const messages = [{ content: '普通回答' }]
    expect(settleExecCards(messages, '✅ 已执行')).toBe(0)
    expect(messages.length).toBe(1)
  })

  it('取不回 SQL 的异常卡片保持原样，不改写成语义不明的终态', () => {
    const messages = [{ content: `${PENDING_EXEC_LABEL}：（格式异常）` }]
    expect(settleExecCards(messages, '✅ 已执行')).toBe(0)
    expect(messages[0].content).toBe(`${PENDING_EXEC_LABEL}：（格式异常）`)
  })

  it('状态未知时不得声称已执行', () => {
    const outcome = resolveExecOutcome({ cancelled: false, hasError: false, toolStatus: '' })
    expect(outcome.confident).toBe(false)
    const messages = [{ content: buildExecCardContent(PENDING_EXEC_LABEL, 'UPDATE t SET a = 1') }]
    settleExecCards(messages, outcome.label)
    expect(messages[0].content).toContain('状态未知')
    expect(messages[0].content).not.toContain('已执行')
  })

  it('用户消息即使同前缀也不得被改写', () => {
    const messages = [
      { role: 'user', content: buildExecCardContent(PENDING_EXEC_LABEL, 'DELETE FROM a') },
      { role: 'assistant', content: buildExecCardContent(PENDING_EXEC_LABEL, 'DELETE FROM b') },
    ]
    expect(settleExecCards(messages, '✅ 已执行')).toBe(1)
    expect(messages[0].content.startsWith(PENDING_EXEC_LABEL)).toBe(true)
    expect(messages[1].content).toContain('✅ 已执行')
  })

  it('limit 只收敛本次推入的卡片，不动历史残留卡', () => {
    const messages = [
      { role: 'assistant', content: buildExecCardContent(PENDING_EXEC_LABEL, '上一次异常残留') },
      { role: 'assistant', content: buildExecCardContent(PENDING_EXEC_LABEL, '本次一') },
      { role: 'assistant', content: buildExecCardContent(PENDING_EXEC_LABEL, '本次二') },
    ]
    expect(settleExecCards(messages, '✅ 已执行', '', 2)).toBe(2)
    expect(messages[0].content.startsWith(PENDING_EXEC_LABEL)).toBe(true)
    expect(messages[1].content).toContain('✅ 已执行')
    expect(messages[2].content).toContain('✅ 已执行')
  })
})
