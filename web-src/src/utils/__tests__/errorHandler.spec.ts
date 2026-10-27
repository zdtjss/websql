import { describe, expect, it } from 'vitest'
import { handleError, sanitizeError } from '../errorHandler'

// 构造一个最小的 axios 风格错误对象
function axiosError(data: unknown, status = 200) {
  return { response: { data, status } }
}

describe('handleError', () => {
  it('优先展示后端 msg，并拼接分类建议 hint', () => {
    const msg = handleError(
      axiosError({ code: 500, msg: '当前账号权限不足', errCode: 'WEBSQL-DB-1006', hint: '请联系 DBA 授权' }),
      '执行 SQL',
      { silent: true }
    )
    expect(msg).toBe('执行 SQL失败: 当前账号权限不足（请联系 DBA 授权）')
  })

  it('没有 hint 时只展示 msg', () => {
    const msg = handleError(axiosError({ code: 500, msg: '执行失败' }), '', { silent: true })
    expect(msg).toBe('执行失败')
  })

  it('msg 已包含 hint 时不重复拼接', () => {
    const msg = handleError(
      axiosError({ code: 500, msg: '超时（请稍后重试）', hint: '请稍后重试' }),
      '',
      { silent: true }
    )
    expect(msg).toBe('超时（请稍后重试）')
  })

  it('无响应时给出网络类提示', () => {
    const msg = handleError({ message: 'Network Error' }, '加载数据', { silent: true })
    expect(msg).toContain('网络连接失败')
  })

  it('后端未返回 msg 时按 HTTP 状态兜底', () => {
    const msg = handleError(axiosError('', 502), '', { silent: true })
    expect(msg).toContain('502')
  })
})

describe('sanitizeError', () => {
  it('过滤堆栈行与文件路径', () => {
    const raw = "Error 1146: Table 'demo.t' doesn't exist\n\tgoroutine 1 [running]:\nC:\\Users\\me\\app.go"
    const cleaned = sanitizeError(raw)
    expect(cleaned).toContain("doesn't exist")
    expect(cleaned).not.toContain('goroutine')
    expect(cleaned).not.toContain('C:\\Users')
  })
})
