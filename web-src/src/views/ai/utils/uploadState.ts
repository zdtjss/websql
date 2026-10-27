/**
 * 上传队列的状态模型与纯状态迁移（不含副作用，便于单测）。
 *
 * 队列项（FileAttachmentItem）同时用于：
 *   - 输入区待发送文件卡片（含上传进度/重试）；
 *   - 聊天历史中的文件卡片（发送后快照，sent=true）。
 */
import { extOf, kindOfExt } from './fileTypes'

/** 上传状态机：pending（待上传）→ uploading（上传中）→ ready / error（可重试） */
export type UploadStatus = 'pending' | 'uploading' | 'ready' | 'error'

/** 文件附件项 */
export interface FileAttachmentItem {
  /** 本地唯一 ID（服务端 fileId 在历史还原时可能重复失效，前端用 localId 做 key） */
  localId: string
  /** 服务端暂存 ID（上传成功后填充） */
  fileId: string
  name: string
  size: number
  ext: string
  kind: string
  mime?: string
  status: UploadStatus
  /** 上传进度 0-100 */
  progress: number
  /** 上传失败原因（status=error 时展示） */
  error?: string
  /** 暂存过期时间（秒级时间戳；0 表示未知，不参与过期判定） */
  expireAt: number
  /** 本地预览地址（图片/视频，仅本地会话有效） */
  previewUrl?: string
  /** 是否已随消息发送（发送后队列移除，历史卡片置 true） */
  sent?: boolean
}

/** 并发上传上限 */
export const UPLOAD_CONCURRENCY = 2

/** 生成本地唯一 ID */
export function createLocalId(): string {
  return 'f' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

/** 由本地文件创建附件项（不触发上传） */
export function createFileItem(file: { name: string; size: number; type?: string }): FileAttachmentItem {
  const ext = extOf(file.name)
  return {
    localId: createLocalId(),
    fileId: '',
    name: file.name,
    size: file.size,
    ext,
    kind: kindOfExt(ext),
    mime: file.type || undefined,
    status: 'pending',
    progress: 0,
    expireAt: 0,
  }
}

/** 是否已过期（expireAt 为 0 表示未知，按未过期处理） */
export function isExpired(item: Pick<FileAttachmentItem, 'expireAt'>, nowMs: number = Date.now()): boolean {
  return item.expireAt > 0 && item.expireAt * 1000 <= nowMs
}

/**
 * 按并发上限挑选下一个可调度的队列下标（pending 项）；无可调度项返回 -1。
 * 并发计数只统计 uploading 状态。
 */
export function nextUploadIndex(queue: FileAttachmentItem[], concurrency: number = UPLOAD_CONCURRENCY): number {
  const uploading = queue.reduce((n, item) => n + (item.status === 'uploading' ? 1 : 0), 0)
  if (uploading >= concurrency) return -1
  return queue.findIndex((item) => item.status === 'pending')
}

/** 上传开始：pending → uploading */
export function markUploadStart(item: FileAttachmentItem): void {
  item.status = 'uploading'
  item.progress = 0
  item.error = undefined
}

/** 上传进度更新（仅 uploading 状态生效） */
export function markUploadProgress(item: FileAttachmentItem, percent: number): void {
  if (item.status !== 'uploading') return
  item.progress = Math.max(0, Math.min(100, Math.round(percent)))
}

/** 上传成功：uploading → ready（fileId/expireAt 以服务端返回为准） */
export function markUploadSuccess(
  item: FileAttachmentItem,
  resp: { fileId: string; fileName?: string; fileExt?: string; fileKind?: string; fileSize?: number; mime?: string; expireAt?: number },
): void {
  item.status = 'ready'
  item.progress = 100
  item.error = undefined
  item.fileId = resp.fileId
  if (resp.fileName) item.name = resp.fileName
  if (resp.fileExt) item.ext = resp.fileExt
  if (resp.fileKind) item.kind = resp.fileKind
  if (typeof resp.fileSize === 'number' && resp.fileSize > 0) item.size = resp.fileSize
  if (resp.mime) item.mime = resp.mime
  item.expireAt = resp.expireAt || 0
}

/** 上传失败：uploading → error（可重试） */
export function markUploadFailure(item: FileAttachmentItem, error: string): void {
  item.status = 'error'
  item.progress = 0
  item.error = error || '上传失败'
}

/** 重试上传：仅 error 状态可重试（error → pending） */
export function markRetry(item: FileAttachmentItem): boolean {
  if (item.status !== 'error') return false
  item.status = 'pending'
  item.progress = 0
  item.error = undefined
  return true
}

/** 队列中是否仍有上传任务（发送按钮据此禁用/提示） */
export function hasUploading(queue: FileAttachmentItem[]): boolean {
  return queue.some((item) => item.status === 'pending' || item.status === 'uploading')
}
