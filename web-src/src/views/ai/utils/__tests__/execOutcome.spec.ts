import { describe, expect, it } from 'vitest'
import { resolveBatchExecOutcome, resolveExecOutcome } from '../execOutcome'

describe('resolveExecOutcome', () => {
  it('用户停止优先展示已取消', () => {
    const outcome = resolveExecOutcome({ cancelled: true, hasError: false, toolStatus: 'executed' })
    expect(outcome.label).toContain('已取消')
    expect(outcome.confident).toBe(true)
  })

  it('tool_status=cancelled 同样判为已取消', () => {
    const outcome = resolveExecOutcome({ cancelled: false, hasError: false, toolStatus: 'cancelled' })
    expect(outcome.label).toContain('已取消')
  })

  it('权限拒绝不得谎报成功', () => {
    const outcome = resolveExecOutcome({ cancelled: false, hasError: false, toolStatus: 'denied' })
    expect(outcome.label).toContain('权限拒绝')
    expect(outcome.label).not.toContain('已执行')
    expect(outcome.confident).toBe(true)
  })

  it('error 事件优先于成功状态展示失败', () => {
    const outcome = resolveExecOutcome({ cancelled: false, hasError: true, toolStatus: 'executed' })
    expect(outcome.label).toContain('执行失败')
  })

  it('executed 才显示已执行', () => {
    const outcome = resolveExecOutcome({ cancelled: false, hasError: false, toolStatus: 'executed' })
    expect(outcome.label).toContain('已执行')
    expect(outcome.confident).toBe(true)
  })

  it('无终态信号时标记为状态未知', () => {
    const outcome = resolveExecOutcome({ cancelled: false, hasError: false, toolStatus: '' })
    expect(outcome.label).toContain('状态未知')
    expect(outcome.confident).toBe(false)
  })
})

/**
 * 批量确认一次 resume 会执行多条 exec_sql，服务端逐条上报 tool_status。
 * 单值覆盖式赋值会把「先 denied 后 executed」误标为成功（审核 H1）。
 */
describe('resolveBatchExecOutcome', () => {
  it('全部 executed 才显示已执行', () => {
    const outcome = resolveBatchExecOutcome({
      total: 2, executed: 2, denied: 0, cancelled: false, hasError: false,
    })
    expect(outcome.label).toContain('已执行')
    expect(outcome.confident).toBe(true)
  })

  it('先 denied 后 executed 不得谎报成功', () => {
    const outcome = resolveBatchExecOutcome({
      total: 2, executed: 1, denied: 1, cancelled: false, hasError: false,
    })
    expect(outcome.label).not.toContain('✅')
    expect(outcome.label).toContain('部分执行成功')
    expect(outcome.label).toContain('1/2')
  })

  it('全部 denied 显示权限拒绝', () => {
    const outcome = resolveBatchExecOutcome({
      total: 2, executed: 0, denied: 2, cancelled: false, hasError: false,
    })
    expect(outcome.label).toContain('权限拒绝')
    expect(outcome.label).not.toContain('已执行')
  })

  it('部分执行后取消标记已终止并给出进度', () => {
    const outcome = resolveBatchExecOutcome({
      total: 3, executed: 1, denied: 0, cancelled: true, hasError: false,
    })
    expect(outcome.label).toContain('已终止')
    expect(outcome.label).toContain('1/3')
  })

  it('无任何终态信号时状态未知且不宣称成功', () => {
    const outcome = resolveBatchExecOutcome({
      total: 2, executed: 0, denied: 0, cancelled: false, hasError: false,
    })
    expect(outcome.confident).toBe(false)
    expect(outcome.label).not.toContain('已执行')
  })
})
