import { nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Ref } from 'vue'
import { sanitizeError } from '@/utils/errorHandler'
import type { ChatMessage } from './useChatHistory'
import { isExpired, type FileAttachmentItem } from '../utils/uploadState'
import {
  collectRetryAttachments,
  collectSendAttachments,
  toFileChatMessages,
  toRequestAttachments,
  type RequestAttachment,
} from '../utils/attachments'

/** streamChatResponse 的额外选项 */
export interface StreamChatOptions {
  /** schemas 为空时的回调（用于回滚刚加入的用户消息） */
  onSchemasEmpty?: () => void
}

/** useChatStream 依赖的外部上下文 */
export interface UseChatStreamDeps {
  /** 聊天历史（追加消息） */
  chatHistory: Ref<ChatMessage[]>
  /** 是否展开全部历史消息（流式开始时重置为 false） */
  showAllHistory: Ref<boolean>
  /** 当前会话 ID（双向同步） */
  sessionId: Ref<string>
  /** 当前选中的 schema 列表 */
  selectedSchemas: Ref<string[]>
  /** 当前选中的表列表 */
  selectedTables: Ref<string[]>
  /** 当前选中的 AI 模型 ID */
  selectedModel: Ref<string>
  /** 高级导出模式：勾选后本次对话强制走 export_by_code（仅 Python、只读临时代码）生成文件 */
  advancedExport: Ref<boolean>
  /** 输入框内容（发送后清空） */
  question: Ref<string>
  /** 待发送文件队列（发送成功后由 markAttachmentsSent 清理） */
  uploads: Ref<FileAttachmentItem[]>
  /** 标记附件已随消息发送（从待发送队列移除） */
  markAttachmentsSent: (items: FileAttachmentItem[]) => void
  /** 全局 loading（与 useSqlConfirm 共享，由 ChatView 创建） */
  loading: Ref<boolean>
  /** 全局 AbortController（与 useSqlConfirm 共享，由 ChatView 创建） */
  abortController: Ref<AbortController | null>
  /** 流式执行内容（与 useSqlConfirm 共享，由 ChatView 创建） */
  streamingExecContent: Ref<string>
  /** 构建 schemas 数组（用于请求体） */
  buildRequestSchemas: () => { connId: string; schema: string }[]
  /** 获取主连接 connId */
  getPrimaryConnId: () => string
  /** 滚动到底部（force=true 时强制滚动，不判断用户位置） */
  scrollToBottom: (force?: boolean) => void
  /** 处理会话过期 */
  handleSessionExpired: () => void
  /** 渲染 markdown 文本为 HTML */
  renderMarkdown: (content: string) => string
  /** 渲染 mermaid 图表 */
  doRenderMermaidBlocks: (scrollAfter?: boolean) => Promise<void>
  /** 统计已闭合的 mermaid 代码块数量 */
  countClosedMermaidBlocks: (content: string) => number
  /** 显示单条 SQL 确认弹框 */
  showConfirmDialog: (sql: string, interruptIds: string | string[], checkPointId: string) => void
  /** 设置批量 SQL 列表 */
  setBatchPendingSQL: (items: { sql: string; interruptId: string; checkPointId: string }[]) => void
  /** 重置"已显示确认"标记 */
  resetDetectFlag: () => void
  /** 批量 SQL 列表（发送/重试时清空） */
  pendingSQLList: Ref<unknown[]>
  /** 是否显示重试确认 */
  showRetryConfirm: Ref<boolean>
  /** 重试消息内容 */
  retryMessage: Ref<string>
  /** 上一次提问内容 */
  lastQuestion: Ref<string>
}

/**
 * AI 对话流式请求 composable。
 *
 * 负责：
 *   - 流式相关状态：streamingContent / streamingExecContent / thinkingText
 *     及其渲染缓存 streamingHtml / streamingExecHtml / thinkingHtml
 *   - 全局执行状态：loading / abortController（与 useSqlConfirm 共享）
 *   - 当前重试的消息引用 retryingMsg
 *   - 核心流式请求 streamChatResponse，以及 sendMessage / stopGeneration
 *   - 流式 mermaid 增量渲染 tryRenderStreamingMermaid
 *   - AI 消息操作：copyMessage / canRetryMessage / retryAssistantMessage
 *   - 思考块折叠 toggleThinking
 *
 * 注意：loading / abortController / streamingExecContent 由 ChatView 创建，
 *      同时传递给 useChatStream 和 useSqlConfirm 作为共享依赖。
 */
export function useChatStream(deps: UseChatStreamDeps) {
  const {
    chatHistory, showAllHistory, sessionId, selectedSchemas, selectedTables,
    selectedModel, question, uploads, markAttachmentsSent, advancedExport,
    loading, abortController, streamingExecContent,
    buildRequestSchemas, getPrimaryConnId, scrollToBottom, handleSessionExpired,
    renderMarkdown, doRenderMermaidBlocks, countClosedMermaidBlocks,
    showConfirmDialog, setBatchPendingSQL, resetDetectFlag,
    pendingSQLList, showRetryConfirm, retryMessage, lastQuestion,
  } = deps

  // ── 流式内容状态（本 composable 专有） ──
  const thinkingText = ref('')
  const streamingContent = ref('')

  // ── 流式内容渲染缓存 ──
  const streamingHtml = ref('')
  const streamingExecHtml = ref('')
  const thinkingHtml = ref('')

  // ── 当前正在重试的 AI 消息引用（用于按钮 loading 状态） ──
  const retryingMsg = ref<ChatMessage | null>(null)

  // ── 流式渲染节流参数与 mermaid 增量计数 ──
  /** 缓冲增长达到该字符数才重新渲染一次（避免长回答全量渲染累计开销接近 O(n²)） */
  const STREAM_RENDER_CHARS_STEP = 80
  /** 缓冲增长停滞该时长后渲染最后一次（首字 / 停顿 / 收尾时不丢帧） */
  const STREAM_RENDER_IDLE_MS = 100
  let lastStreamingMermaidCount = 0
  let lastStreamingExecMermaidCount = 0
  let lastRenderedMermaidCount = 0
  let mermaidRenderTimer: ReturnType<typeof setTimeout> | null = null

  // ── 流式渲染节流：按帧合并 + 增量阈值 ──
  /**
   * 创建流式渲染调度器（按帧合并 + 增量阈值 + 空闲兜底）。
   *
   * 原实现每 50ms 对整段文本执行一次 renderMarkdown + DOMPurify，
   * 长回答下渲染总量随文本长度线性增长，累计开销接近 O(n²)。
   * 现改为：仅当缓冲比上次渲染增长 ≥ STREAM_RENDER_CHARS_STEP 时，
   * 在下一帧渲染（requestAnimationFrame 合并同帧多次触发）；
   * 增长未达阈值时推迟到空闲（STREAM_RENDER_IDLE_MS）渲染最后一次；
   * 缓冲被清空（新会话 / 内容已落历史）时立即同步，保证结束时完整渲染。
   */
  function createStreamRenderScheduler(
    source: Ref<string>,
    target: Ref<string>,
    afterRender?: (content: string) => void,
  ): { schedule: () => void } {
    let raf: number | null = null
    let idleTimer: ReturnType<typeof setTimeout> | null = null
    let renderedLength = 0

    function flush(): void {
      if (raf !== null) {
        cancelAnimationFrame(raf)
        raf = null
      }
      if (idleTimer) {
        clearTimeout(idleTimer)
        idleTimer = null
      }
      const content = source.value
      renderedLength = content.length
      target.value = renderMarkdown(content)
      // 内容为空时无需再做 mermaid 后置处理
      if (!content) return
      nextTick(() => { afterRender?.(content) })
    }

    return {
      schedule(): void {
        const len = source.value.length
        // 缓冲被重置：立即同步，避免残留旧内容
        if (len === 0) {
          flush()
          return
        }
        if (len - renderedLength >= STREAM_RENDER_CHARS_STEP) {
          if (raf !== null) return
          raf = requestAnimationFrame(() => {
            raf = null
            flush()
          })
          return
        }
        if (idleTimer) clearTimeout(idleTimer)
        idleTimer = setTimeout(() => {
          idleTimer = null
          flush()
        }, STREAM_RENDER_IDLE_MS)
      },
    }
  }

  // ── 流式内容变化时按节流策略更新渲染缓存并触发 mermaid 渲染 ──
  const streamingRender = createStreamRenderScheduler(streamingContent, streamingHtml, (content) => {
    if (content.includes('```mermaid')) {
      const closedCount = countClosedMermaidBlocks(content)
      if (closedCount > lastStreamingMermaidCount) {
        lastStreamingMermaidCount = closedCount
        void doRenderMermaidBlocks(true)
      }
    }
    // 即使 countClosedMermaidBlocks 未检测到新块，也检查 DOM 中是否有未渲染的容器
    // 这处理了 autoWrapMermaidCode 自动包裹的情况
    const unprocessed = document.querySelectorAll('.mermaid-container[data-mermaid-processed="false"]')
    if (unprocessed.length > 0) {
      void doRenderMermaidBlocks(true)
    }
  })
  watch(streamingContent, () => { streamingRender.schedule() }, { immediate: true })

  const streamingExecRender = createStreamRenderScheduler(streamingExecContent, streamingExecHtml, (content) => {
    if (content.includes('```mermaid')) {
      const closedCount = countClosedMermaidBlocks(content)
      if (closedCount > lastStreamingExecMermaidCount) {
        lastStreamingExecMermaidCount = closedCount
        void doRenderMermaidBlocks(true)
      }
    }
  })
  watch(streamingExecContent, () => { streamingExecRender.schedule() }, { immediate: true })

  const thinkingRender = createStreamRenderScheduler(thinkingText, thinkingHtml)
  watch(thinkingText, () => { thinkingRender.schedule() }, { immediate: true })

  /** 流式输出中检测已完成的 mermaid 代码块并立即渲染 */
  function tryRenderStreamingMermaid(): void {
    const content = streamingContent.value
    if (!content.includes('```mermaid')) return

    const closedCount = countClosedMermaidBlocks(content)

    if (closedCount > lastRenderedMermaidCount) {
      lastRenderedMermaidCount = closedCount
      lastStreamingMermaidCount = closedCount
      if (mermaidRenderTimer) clearTimeout(mermaidRenderTimer)
      mermaidRenderTimer = setTimeout(() => {
        void doRenderMermaidBlocks(true)
        mermaidRenderTimer = null
      }, 100)
    }
  }

  /** 中止当前流式请求 */
  function stopGeneration(): void {
    if (abortController.value) {
      abortController.value.abort()
      abortController.value = null
    }
  }

  /** 判断一条 SQL 文本是否为 SQL 语句 */
  function isSqlContent(content: string): boolean {
    return /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(content.trim())
  }

  /**
   * 是否具备数据来源：本轮文件附件，或会话历史中已发送且未过期的文件卡片。
   * 大模型分析的数据来源可以是数据库，也可以是文件，二者至少有其一即可发送。
   */
  function hasFileDataSource(attachments: RequestAttachment[] | null): boolean {
    if (attachments && attachments.length > 0) return true
    return chatHistory.value.some(
      (m) => m.role === 'file' && !!m.file?.fileId && !isExpired(m.file),
    )
  }

  /**
   * 核心流式请求函数（供 sendMessage / 重试 / Mermaid 修复共用）。
   * messageContent: 发送给 AI 的提问文本（不含文件上下文，附件经 attachments 透传给后端）；
   * attachments: 本轮携带的文件引用（重试时按同轮文件卡片还原；无附件传 null）
   */
  async function streamChatResponse(
    messageContent: string,
    attachments: RequestAttachment[] | null,
    options: StreamChatOptions = {},
  ): Promise<void> {
    const { onSchemasEmpty = null } = options

    loading.value = true
    thinkingText.value = ''
    streamingContent.value = ''
    lastRenderedMermaidCount = 0
    lastStreamingMermaidCount = 0
    lastStreamingExecMermaidCount = 0
    showAllHistory.value = false
    scrollToBottom(true)

    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/chatStream'
    const auth = sessionStorage.getItem('authentication') || ''

    const controller = new AbortController()
    abortController.value = controller

    const schemas = buildRequestSchemas()
    if (schemas.length === 0 && !hasFileDataSource(attachments)) {
      ElMessage.warning('请选择至少一个数据库 Schema，或上传要分析的文件')
      loading.value = false
      retryingMsg.value = null
      if (onSchemasEmpty) onSchemasEmpty()
      return
    }

    try {
      const primaryConnId = getPrimaryConnId()
      const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
      const body: Record<string, unknown> = {
        sessionId: sessionId.value,
        connId: primaryConnId,
        schema: primarySchema,
        schemas,
        question: messageContent,
        tableContext: selectedTables.value,
        modelId: selectedModel.value,
      }
      if (attachments && attachments.length > 0) {
        body.attachments = attachments
      }
      if (advancedExport.value) {
        body.advancedExport = true
      }

      const resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': auth },
        body: JSON.stringify(body),
        signal: controller.signal,
      })

      if (resp.status === 401) {
        const errorData = await resp.json().catch(() => ({}))
        if (errorData.code === 401) {
          ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
          handleSessionExpired()
          return
        }
      }

      if (!resp.ok) {
        ElMessage({ message: `请求失败: ${resp.status}`, type: 'error' })
        return
      }

      const reader = resp.body!.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      const collectedDangerSQLs: { sql: string; interruptId: string; checkPointId: string }[] = []

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })

        const lines = buf.split('\n')
        buf = lines.pop() || ''

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const data = line.slice(6).trim()
          if (!data) continue
          try {
            const chunk = JSON.parse(data)
            switch (chunk.type) {
              case 'session':
                sessionId.value = chunk.content
                break
              case 'retract': {
                // 模型流式重试：后端撤回本次尝试已推送的文本（与前端已接收内容一致，按后缀回退），
                // 避免重试后的内容与失败尝试的内容重复展示
                const retractContent = chunk.content || ''
                const retractThinking = chunk.thinking || ''
                if (retractContent && streamingContent.value.endsWith(retractContent)) {
                  streamingContent.value = streamingContent.value.slice(0, -retractContent.length)
                }
                if (retractThinking && thinkingText.value.endsWith(retractThinking)) {
                  thinkingText.value = thinkingText.value.slice(0, -retractThinking.length)
                }
                break
              }
              case 'thinking':
                thinkingText.value += chunk.content
                scrollToBottom()
                break
              case 'content':
                streamingContent.value += chunk.content
                // 检测是否有新的完整 mermaid 代码块可以渲染
                tryRenderStreamingMermaid()
                scrollToBottom()
                break
              case 'danger_confirm':
                collectedDangerSQLs.push({
                  sql: chunk.sql || chunk.content,
                  interruptId: chunk.interruptId || '',
                  checkPointId: chunk.checkPointId || '',
                })
                break
              case 'retry_limit':
                retryMessage.value = chunk.content
                showRetryConfirm.value = true
                break
              case 'error':
                // 先将已有的思考过程和内容加入历史
                if (thinkingText.value) {
                  chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
                  thinkingText.value = ''
                }
                if (streamingContent.value) {
                  const content = streamingContent.value
                  chatHistory.value.push({ role: 'assistant', content, hasSql: isSqlContent(content) })
                  streamingContent.value = ''
                }
                // 然后再添加错误消息，确保显示在结果区域下方
                chatHistory.value.push({ role: 'assistant', content: '❌ ' + (sanitizeError(chunk.content) || 'AI 服务错误') })
                scrollToBottom()
                break
              case 'done':
                break
              case 'cancelled':
                // 后端主动取消（events.go 发送 cancelled，如超时/上游取消）：
                // 保留已生成内容并给出普通提示（不是 error 气泡），同时结束 loading 与 abort 状态
                if (thinkingText.value) {
                  chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
                  thinkingText.value = ''
                }
                if (streamingContent.value) {
                  const content = streamingContent.value
                  chatHistory.value.push({ role: 'assistant', content, hasSql: isSqlContent(content) })
                  streamingContent.value = ''
                }
                chatHistory.value.push({ role: 'assistant', content: `⏹ ${chunk.content || '已停止生成'}` })
                loading.value = false
                abortController.value = null
                scrollToBottom()
                break
            }
          } catch (_) { /* ignore */ }
        }
        scrollToBottom()
      }

      // 流结束，将思考过程和内容加入历史
      if (thinkingText.value) {
        chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
        thinkingText.value = ''
      }
      if (streamingContent.value) {
        const content = streamingContent.value
        chatHistory.value.push({ role: 'assistant', content, hasSql: isSqlContent(content) })
        streamingContent.value = ''
      }

      // 处理收集到的危险 SQL
      if (collectedDangerSQLs.length > 0) {
        setBatchPendingSQL(collectedDangerSQLs)
      }
    } catch (e) {
      const err = e as Error
      if (err.name === 'AbortError') {
        if (thinkingText.value) {
          chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
          thinkingText.value = ''
        }
        if (streamingContent.value) {
          chatHistory.value.push({ role: 'assistant', content: streamingContent.value })
          streamingContent.value = ''
        }
        chatHistory.value.push({ role: 'assistant', content: '⏹ 对话已被手动终止' })
        scrollToBottom()
      } else {
        // 网络中断 / 服务端异常：已生成内容用户已看到，必须先落为历史消息，
        // 否则刷新或切换会话后这段内容会凭空消失
        const reason = sanitizeError(e) || '网络异常'
        ElMessage({ message: reason, type: 'error' })
        if (thinkingText.value) {
          chatHistory.value.push({ role: 'thinking', content: thinkingText.value, collapsed: true })
          thinkingText.value = ''
        }
        if (streamingContent.value) {
          const content = streamingContent.value
          chatHistory.value.push({
            role: 'assistant',
            content: `${content}\n\n> ⚠️ 响应中断：${reason}`,
            hasSql: isSqlContent(content),
          })
          streamingContent.value = ''
          scrollToBottom()
        }
      }
    } finally {
      loading.value = false
      // 仅当 abortController 仍指向本次请求时才清理，避免覆盖其他并发请求（如确认执行）
      if (abortController.value === controller) abortController.value = null
      retryingMsg.value = null
      // 桌面模式：回复完成时闪烁任务栏图标（窗口未激活时才闪，获焦自动停止；web 模式 no-op）
      ;(window as unknown as { Flash?: () => void }).Flash?.()
      // 流结束后渲染 mermaid 图表
      scrollToBottom()
      void doRenderMermaidBlocks()
      // 延迟再次检查：处理 md 异步加载完成后 getCachedHtml 重新渲染的情况
      setTimeout(() => { void doRenderMermaidBlocks(false) }, 500)
      setTimeout(() => { void doRenderMermaidBlocks(false) }, 1500)
    }
  }

  /** 发送一条新消息（文本 + 待发送文件附件） */
  async function sendMessage(): Promise<void> {
    const text = question.value.trim()
    const ready = collectSendAttachments(uploads.value)
    if (!text && ready.length === 0) return
    if (loading.value) return
    // 上传未完成：提示等待，避免发出「引用不可用」的半成品消息
    if (uploads.value.some((i) => i.status === 'pending' || i.status === 'uploading')) {
      ElMessage.warning('文件正在上传中，请稍候再发送')
      return
    }
    // 上传文件后若未描述需求，无法判断意图（分析/结合库/导入），提示用户补充
    if (!text && ready.length > 0) {
      ElMessage.warning('请描述你对这个文件的需求（例如：分析这份数据 / 导入到 xx 表 / 和 xx 表对比）')
      return
    }
    // 数据库与文件均未提供时不进入发送流程（避免先落消息再回滚）
    if (buildRequestSchemas().length === 0 && ready.length === 0 && !hasFileDataSource(null)) {
      ElMessage.warning('请选择至少一个数据库 Schema，或上传要分析的文件')
      return
    }

    // 重置状态
    resetDetectFlag()
    pendingSQLList.value = []
    showRetryConfirm.value = false
    lastQuestion.value = text

    // 文件卡片先入历史（类微信独立气泡，位于用户消息之前），再入用户消息
    for (const card of toFileChatMessages(ready)) {
      chatHistory.value.push(card)
    }
    chatHistory.value.push({ role: 'user', content: text })
    question.value = ''
    markAttachmentsSent(ready)

    // 附件以 fileId 引用透传给后端（内容由后端按需解析，不进消息体）
    await streamChatResponse(text, ready.length > 0 ? toRequestAttachments(ready) : null)
  }

  /** 复制 AI 消息内容到剪贴板 */
  async function copyMessage(msg: ChatMessage): Promise<void> {
    try {
      await navigator.clipboard.writeText(msg.content || '')
      msg._copied = true
      setTimeout(() => { msg._copied = false }, 2000)
    } catch (e) {
      ElMessage.error('复制失败，请手动选择文本复制')
    }
  }

  /** 检查 AI 消息是否可重试（前方是否存在用户消息） */
  function canRetryMessage(msg: ChatMessage): boolean {
    const idx = chatHistory.value.indexOf(msg)
    if (idx < 0) return false
    for (let i = idx - 1; i >= 0; i--) {
      if (chatHistory.value[i].role === 'user') return true
    }
    return false
  }

  /** 重试 AI 消息：找到上一条用户消息，移除当前 AI 消息，重新发送请求 */
  async function retryAssistantMessage(msg: ChatMessage): Promise<void> {
    if (loading.value) return
    const idx = chatHistory.value.indexOf(msg)
    if (idx < 0) return
    // 向前查找最近的用户消息
    let userIdx = -1
    for (let i = idx - 1; i >= 0; i--) {
      if (chatHistory.value[i].role === 'user') {
        userIdx = i
        break
      }
    }
    if (userIdx < 0) return

    // 标记重试状态（按钮 loading）
    retryingMsg.value = msg

    // 移除当前 AI 消息
    chatHistory.value.splice(idx, 1)

    // 重置状态
    resetDetectFlag()
    pendingSQLList.value = []
    showRetryConfirm.value = false
    lastQuestion.value = chatHistory.value[userIdx].content

    // 重新发送请求：还原同轮文件附件（未过期的才重发），保持文件上下文
    const attachments = collectRetryAttachments(chatHistory.value, userIdx)
    await streamChatResponse(chatHistory.value[userIdx].content, attachments.length > 0 ? attachments : null)
  }

  /** 折叠/展开思考块 */
  function toggleThinking(msg: ChatMessage): void {
    msg.collapsed = !msg.collapsed
    if (!msg.collapsed) {
      nextTick(() => { void doRenderMermaidBlocks(false) })
    }
  }

  /** 仅重置流式相关状态（供 ChatView.clearSession 调用） */
  function resetStreamState(): void {
    stopGeneration()
    thinkingText.value = ''
    streamingContent.value = ''
    streamingExecContent.value = ''
    streamingHtml.value = ''
    streamingExecHtml.value = ''
    thinkingHtml.value = ''
    retryingMsg.value = null
    lastRenderedMermaidCount = 0
    lastStreamingMermaidCount = 0
    lastStreamingExecMermaidCount = 0
  }

  return {
    // 流式内容状态
    thinkingText,
    streamingContent,
    // 流式内容渲染缓存
    streamingHtml,
    streamingExecHtml,
    thinkingHtml,
    // 重试状态
    retryingMsg,
    // 方法
    stopGeneration,
    sendMessage,
    streamChatResponse,
    tryRenderStreamingMermaid,
    copyMessage,
    canRetryMessage,
    retryAssistantMessage,
    toggleThinking,
    resetStreamState,
  }
}
