import { describe, expect, it } from 'vitest'
import {
  UPLOAD_CONCURRENCY,
  createFileItem,
  hasUploading,
  isExpired,
  markRetry,
  markUploadFailure,
  markUploadProgress,
  markUploadStart,
  markUploadSuccess,
  nextUploadIndex,
  type FileAttachmentItem,
} from '../uploadState'

function makeItem(patch: Partial<FileAttachmentItem> = {}): FileAttachmentItem {
  return { ...createFileItem({ name: 'a.csv', size: 100 }), ...patch }
}

describe('createFileItem', () => {
  it('按文件名推导扩展名与类别', () => {
    const item = createFileItem({ name: '销售数据.XLSX', size: 1024, type: 'application/vnd.ms-excel' })
    expect(item.ext).toBe('.xlsx')
    expect(item.kind).toBe('table')
    expect(item.status).toBe('pending')
    expect(item.progress).toBe(0)
    expect(item.fileId).toBe('')
    expect(item.localId).toMatch(/^f/)
  })

  it('localId 不重复', () => {
    const a = createFileItem({ name: 'a.txt', size: 1 })
    const b = createFileItem({ name: 'a.txt', size: 1 })
    expect(a.localId).not.toBe(b.localId)
  })
})

describe('nextUploadIndex（并发调度）', () => {
  it('无 pending 项返回 -1', () => {
    const queue = [makeItem({ status: 'ready' }), makeItem({ status: 'error' })]
    expect(nextUploadIndex(queue)).toBe(-1)
  })

  it('达到并发上限时不再调度', () => {
    const queue = [
      makeItem({ status: 'uploading' }),
      makeItem({ status: 'uploading' }),
      makeItem({ status: 'pending' }),
    ]
    expect(nextUploadIndex(queue)).toBe(-1)
    expect(nextUploadIndex(queue, 3)).toBe(2)
  })

  it('按顺序挑选第一个 pending', () => {
    const queue = [makeItem({ status: 'uploading' }), makeItem({ status: 'pending' }), makeItem({ status: 'pending' })]
    expect(nextUploadIndex(queue)).toBe(1)
    expect(UPLOAD_CONCURRENCY).toBe(2)
  })
})

describe('上传状态迁移', () => {
  it('pending → uploading → ready（服务端字段回填）', () => {
    const item = makeItem()
    markUploadStart(item)
    expect(item.status).toBe('uploading')
    markUploadProgress(item, 45.6)
    expect(item.progress).toBe(46)
    markUploadSuccess(item, {
      fileId: 'abc', fileName: '销售数据.xlsx', fileExt: '.xlsx', fileKind: 'table', fileSize: 2048,
      mime: 'text/csv', expireAt: 1700000000,
    })
    expect(item.status).toBe('ready')
    expect(item.progress).toBe(100)
    expect(item.fileId).toBe('abc')
    expect(item.name).toBe('销售数据.xlsx')
    expect(item.kind).toBe('table')
    expect(item.size).toBe(2048)
    expect(item.expireAt).toBe(1700000000)
    expect(item.error).toBeUndefined()
  })

  it('进度仅在 uploading 状态生效且被夹取到 0-100', () => {
    const item = makeItem({ status: 'ready' })
    markUploadProgress(item, 50)
    expect(item.progress).toBe(0)
    const up = makeItem({ status: 'uploading' })
    markUploadProgress(up, 500)
    expect(up.progress).toBe(100)
    markUploadProgress(up, -3)
    expect(up.progress).toBe(0)
  })

  it('uploading → error → 重试回 pending', () => {
    const item = makeItem()
    markUploadStart(item)
    markUploadFailure(item, '文件大小不能超过 20MB')
    expect(item.status).toBe('error')
    expect(item.error).toContain('20MB')
    expect(markRetry(item)).toBe(true)
    expect(item.status).toBe('pending')
    expect(item.error).toBeUndefined()
  })

  it('非 error 状态不可重试', () => {
    const ready = makeItem({ status: 'ready' })
    expect(markRetry(ready)).toBe(false)
    expect(ready.status).toBe('ready')
  })
})

describe('过期与忙碌判定', () => {
  it('isExpired 按秒级时间戳判定，0 视为未知不过期', () => {
    const now = 1_700_000_000_000
    expect(isExpired({ expireAt: 0 }, now)).toBe(false)
    expect(isExpired({ expireAt: Math.floor(now / 1000) + 10 }, now)).toBe(false)
    expect(isExpired({ expireAt: Math.floor(now / 1000) - 1 }, now)).toBe(true)
  })

  it('hasUploading 覆盖 pending 与 uploading', () => {
    expect(hasUploading([])).toBe(false)
    expect(hasUploading([makeItem({ status: 'ready' }), makeItem({ status: 'error' })])).toBe(false)
    expect(hasUploading([makeItem({ status: 'pending' })])).toBe(true)
    expect(hasUploading([makeItem({ status: 'uploading' })])).toBe(true)
  })
})
