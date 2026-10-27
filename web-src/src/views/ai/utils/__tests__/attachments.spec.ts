import { describe, expect, it } from 'vitest'
import {
  collectRetryAttachments,
  collectSendAttachments,
  refsToFileChatMessages,
  toFileChatMessages,
  toRequestAttachments,
} from '../attachments'
import { createFileItem, type FileAttachmentItem } from '../uploadState'
import type { ChatMessage } from '../../composables/useChatHistory'

const NOW = 1_700_000_000_000
const FUTURE = Math.floor(NOW / 1000) + 600
const PAST = Math.floor(NOW / 1000) - 10

function makeItem(patch: Partial<FileAttachmentItem> = {}): FileAttachmentItem {
  return { ...createFileItem({ name: 'a.csv', size: 100 }), ...patch }
}

describe('collectSendAttachments', () => {
  it('仅收集上传完成且未过期的文件', () => {
    const queue = [
      makeItem({ status: 'ready', fileId: 'ok', expireAt: FUTURE }),
      makeItem({ status: 'ready', fileId: 'expired', expireAt: PAST }),
      makeItem({ status: 'ready', fileId: '' }),
      makeItem({ status: 'uploading', fileId: 'up' }),
      makeItem({ status: 'error', fileId: 'err' }),
      makeItem({ status: 'pending' }),
    ]
    const got = collectSendAttachments(queue, NOW)
    expect(got).toHaveLength(1)
    expect(got[0].fileId).toBe('ok')
  })

  it('expireAt=0（未知）按未过期处理', () => {
    const got = collectSendAttachments([makeItem({ status: 'ready', fileId: 'ok', expireAt: 0 })], NOW)
    expect(got).toHaveLength(1)
  })
})

describe('toRequestAttachments', () => {
  it('仅保留 fileId/fileName/fileKind（展示字段由服务端回填）', () => {
    const items = [makeItem({ status: 'ready', fileId: 'f1', name: '销售.xlsx', kind: 'table' })]
    expect(toRequestAttachments(items)).toEqual([{ fileId: 'f1', fileName: '销售.xlsx', fileKind: 'table' }])
  })
})

describe('toFileChatMessages', () => {
  it('生成 file 角色消息并标记 sent', () => {
    const items = [makeItem({ status: 'ready', fileId: 'f1', name: 'a.csv' })]
    const msgs = toFileChatMessages(items)
    expect(msgs).toHaveLength(1)
    expect(msgs[0].role).toBe('file')
    expect(msgs[0].file?.fileId).toBe('f1')
    expect(msgs[0].file?.sent).toBe(true)
    expect(msgs[0].file?.status).toBe('ready')
  })
})

describe('refsToFileChatMessages（历史还原）', () => {
  it('空引用返回空数组', () => {
    expect(refsToFileChatMessages(undefined)).toEqual([])
    expect(refsToFileChatMessages([])).toEqual([])
  })

  it('按服务端引用还原卡片（含过期时间）', () => {
    const msgs = refsToFileChatMessages([
      { fileId: 'f1', fileName: '销售.xlsx', fileKind: 'table', fileExt: '.xlsx', fileSize: 2048, expireAt: PAST },
    ])
    expect(msgs).toHaveLength(1)
    const file = msgs[0].file!
    expect(msgs[0].role).toBe('file')
    expect(file.fileId).toBe('f1')
    expect(file.name).toBe('销售.xlsx')
    expect(file.kind).toBe('table')
    expect(file.size).toBe(2048)
    expect(file.expireAt).toBe(PAST)
    expect(file.sent).toBe(true)
    expect(file.localId).toBe('h-f1')
  })

  it('缺省字段有兜底', () => {
    const msgs = refsToFileChatMessages([{ fileId: 'f2' }])
    expect(msgs[0].file?.name).toBe('未命名文件')
    expect(msgs[0].file?.size).toBe(0)
    expect(msgs[0].file?.expireAt).toBe(0)
  })
})

describe('collectRetryAttachments（重试重发附件）', () => {
  function historyWithFiles(): { history: ChatMessage[]; userIdx: number } {
    const history: ChatMessage[] = [
      { role: 'user', content: '第一轮' },
      { role: 'assistant', content: '回答' },
      ...toFileChatMessages([makeItem({ status: 'ready', fileId: 'f-old', expireAt: PAST })]),
      { role: 'user', content: '分析这份文件' },
      { role: 'assistant', content: '好的' },
    ]
    return { history, userIdx: 3 }
  }

  it('收集同轮未过期附件，跳过已过期', () => {
    const { history, userIdx } = historyWithFiles()
    expect(collectRetryAttachments(history, userIdx, NOW)).toEqual([])
  })

  it('未过期附件会随重试重发', () => {
    const history: ChatMessage[] = [
      ...toFileChatMessages([
        makeItem({ status: 'ready', fileId: 'f1', name: 'a.csv', kind: 'table', expireAt: FUTURE }),
        makeItem({ status: 'ready', fileId: 'f2', name: 'b.png', kind: 'image', expireAt: PAST }),
      ]),
      { role: 'user', content: '分析' },
      { role: 'assistant', content: '好的' },
    ]
    expect(collectRetryAttachments(history, 2, NOW)).toEqual([
      { fileId: 'f1', fileName: 'a.csv', fileKind: 'table' },
    ])
  })

  it('用户消息前没有文件卡片时返回空', () => {
    const history: ChatMessage[] = [
      { role: 'user', content: '你好' },
      { role: 'assistant', content: '你好' },
    ]
    expect(collectRetryAttachments(history, 0, NOW)).toEqual([])
    expect(collectRetryAttachments(history, 1, NOW)).toEqual([])
  })
})
