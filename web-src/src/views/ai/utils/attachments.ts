/**
 * 会话附件（发送与历史还原）纯逻辑。
 *
 * 数据契约与后端对齐：
 *   - 发送请求：ChatRequest.Attachments（仅 fileId 必填，其余由服务端按暂存元数据回填）；
 *   - 历史回看：SessionDetailMessage.Attachments（含 fileName/fileKind/fileExt/fileSize/expireAt）。
 */
import type { ChatMessage } from '../composables/useChatHistory'
import { isExpired, type FileAttachmentItem } from './uploadState'

/** 发送请求中的附件引用（与后端 agent.AttachmentRef 对齐） */
export interface RequestAttachment {
  fileId: string
  fileName?: string
  fileKind?: string
}

/** 服务端历史消息中的附件引用（SessionDetailMessage.attachments） */
export interface SessionAttachmentRef {
  fileId: string
  fileName?: string
  fileKind?: string
  fileExt?: string
  fileSize?: number
  expireAt?: number
}

/** 收集可随消息发送的附件：上传完成（ready）且未过期 */
export function collectSendAttachments(
  queue: FileAttachmentItem[],
  nowMs: number = Date.now(),
): FileAttachmentItem[] {
  return queue.filter((item) => item.status === 'ready' && !!item.fileId && !isExpired(item, nowMs))
}

/** 队列项 → 发送请求附件引用 */
export function toRequestAttachments(items: FileAttachmentItem[]): RequestAttachment[] {
  return items.map((item) => ({ fileId: item.fileId, fileName: item.name, fileKind: item.kind }))
}

/** 队列项 → 聊天历史文件卡片消息（类微信：发送时插入消息流，位于用户消息之前） */
export function toFileChatMessages(items: FileAttachmentItem[]): ChatMessage[] {
  return items.map((item) => ({
    role: 'file' as const,
    content: '',
    file: { ...item, status: 'ready' as const, sent: true },
  }))
}

/** 历史消息附件引用 → 文件卡片消息（过期态由 ChatFileCard 按 expireAt 实时判定） */
export function refsToFileChatMessages(refs?: SessionAttachmentRef[]): ChatMessage[] {
  if (!refs || refs.length === 0) return []
  return refs.map((ref) => ({
    role: 'file' as const,
    content: '',
    file: {
      localId: 'h-' + ref.fileId,
      fileId: ref.fileId,
      name: ref.fileName || '未命名文件',
      size: ref.fileSize || 0,
      ext: ref.fileExt || '',
      kind: ref.fileKind || '',
      status: 'ready' as const,
      progress: 100,
      expireAt: ref.expireAt || 0,
      sent: true,
    } as FileAttachmentItem,
  }))
}

/**
 * 重试一条 AI 消息时，收集与其同轮的文件附件引用。
 *
 * 规则：从用户消息往前扫描连续的文件卡片（本轮附件），未过期的才重发；
 * 已过期文件不重发（后端也会按暂存元数据二次校验丢弃）。
 */
export function collectRetryAttachments(
  history: ChatMessage[],
  userMsgIndex: number,
  nowMs: number = Date.now(),
): RequestAttachment[] {
  if (userMsgIndex <= 0 || userMsgIndex > history.length) return []
  const out: RequestAttachment[] = []
  for (let i = userMsgIndex - 1; i >= 0; i--) {
    const msg = history[i]
    if (msg.role !== 'file' || !msg.file) break
    const file = msg.file as FileAttachmentItem
    if (file.fileId && !isExpired(file, nowMs)) {
      out.unshift({ fileId: file.fileId, fileName: file.name, fileKind: file.kind })
    }
  }
  return out
}
