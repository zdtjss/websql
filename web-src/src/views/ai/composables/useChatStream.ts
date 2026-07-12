import { nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Ref } from 'vue'
import { sanitizeError } from '@/utils/errorHandler'
import type { ChatMessage } from './useChatHistory'

/** 上传的 Excel 文件元信息 */
export interface UploadedExcel {
  fileId: string
  name: string
  fileType: string
  columns: string[]
  rows: number
  charCount: number
  [key: string]: unknown
}

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
  /** 输入框内容（发送后清空） */
  question: Ref<string>
  /** 已上传的 Excel 文件（发送成功后清空） */
  uploadedExcel: Ref<UploadedExcel | null>
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
  /** 滚动到底部 */
  scrollToBottom: () => void
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
    selectedModel, question, uploadedExcel,
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

  // ── 流式渲染节流计时器与 mermaid 增量计数 ──
  let streamingRenderTimer: ReturnType<typeof setTimeout> | null = null
  let streamingExecRenderTimer: ReturnType<typeof setTimeout> | null = null
  let thinkingRenderTimer: ReturnType<typeof setTimeout> | null = null
  let lastStreamingMermaidCount = 0
  let lastStreamingExecMermaidCount = 0
  let lastRenderedMermaidCount = 0
  let mermaidRenderTimer: ReturnType<typeof setTimeout> | null = null

  // ── 流式内容变化时更新渲染缓存并触发 mermaid 渲染 ──
  watch(streamingContent, () => {
    if (streamingRenderTimer) clearTimeout(streamingRenderTimer)
    streamingRenderTimer = setTimeout(() => {
      streamingHtml.value = renderMarkdown(streamingContent.value)
      nextTick(() => {
        const content = streamingContent.value
        if (content && content.includes('```mermaid')) {
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
      streamingRenderTimer = null
    }, 50)
  }, { immediate: true })

  watch(streamingExecContent, () => {
    if (streamingExecRenderTimer) clearTimeout(streamingExecRenderTimer)
    streamingExecRenderTimer = setTimeout(() => {
      streamingExecHtml.value = renderMarkdown(streamingExecContent.value)
      nextTick(() => {
        const content = streamingExecContent.value
        if (content && content.includes('```mermaid')) {
          const closedCount = countClosedMermaidBlocks(content)
          if (closedCount > lastStreamingExecMermaidCount) {
            lastStreamingExecMermaidCount = closedCount
            void doRenderMermaidBlocks(true)
          }
        }
      })
      streamingExecRenderTimer = null
    }, 50)
  }, { immediate: true })

  watch(thinkingText, () => {
    if (thinkingRenderTimer) clearTimeout(thinkingRenderTimer)
    thinkingRenderTimer = setTimeout(() => {
      thinkingHtml.value = renderMarkdown(thinkingText.value)
      thinkingRenderTimer = null
    }, 50)
  }, { immediate: true })

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
   * 核心流式请求函数（供 sendMessage 和重试功能共用）。
   * messageContent: 发送给 AI 的完整内容；excelContext: 文件上下文（重试时为 null）
   */
  async function streamChatResponse(
    messageContent: string,
    excelContext: { fileId: string; columns: string[]; totalRows: number; fileType: string; charCount: number } | null,
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
    scrollToBottom()

    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/chatStream'
    const auth = sessionStorage.getItem('authentication') || ''

    const controller = new AbortController()
    abortController.value = controller

    const schemas = buildRequestSchemas()
    if (schemas.length === 0) {
      ElMessage.warning('请先选择至少一个数据库 schema')
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
      if (excelContext) {
        body.excelData = excelContext
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

      // 清除已上传的 Excel
      if (uploadedExcel.value) {
        uploadedExcel.value = null
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
        ElMessage({ message: sanitizeError(e) || '请求失败', type: 'error' })
      }
    } finally {
      loading.value = false
      abortController.value = null
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

  /** 发送一条新消息 */
  async function sendMessage(): Promise<void> {
    const text = question.value.trim()
    if (!text && !uploadedExcel.value) return
    if (loading.value) return
    // 上传文件后若未描述需求，无法判断意图（分析/结合库/导入），提示用户补充
    if (!text && uploadedExcel.value) {
      ElMessage.warning('请描述你对这个文件的需求（例如：分析这份数据 / 导入到 xx 表 / 和 xx 表对比）')
      return
    }

    // 重置状态
    resetDetectFlag()
    pendingSQLList.value = []
    showRetryConfirm.value = false
    lastQuestion.value = text

    // 构建消息内容
    let messageContent = text
    let excelContext: { fileId: string; columns: string[]; totalRows: number; fileType: string; charCount: number } | null = null

    if (uploadedExcel.value) {
      const excel = uploadedExcel.value
      excelContext = {
        fileId: excel.fileId,
        columns: excel.columns,
        totalRows: excel.rows,
        fileType: excel.fileType,
        charCount: excel.charCount,
      }
      // 仅附带文件元信息上下文，不预设"导入"意图；意图由用户提问决定
      if (excel.fileType === 'markdown') {
        messageContent += `\n\n[已上传文件] Markdown 文档，文件ID：${excel.fileId}，共 ${excel.charCount} 字符。`
      } else {
        messageContent += `\n\n[已上传文件] 文件ID：${excel.fileId}，列名：${excel.columns.join(', ')}，共 ${excel.rows} 行数据。`
      }
    }

    chatHistory.value.push({ role: 'user', content: text })
    question.value = ''

    // 调用核心流式请求（schemas 为空时回退刚加入的用户消息）
    await streamChatResponse(messageContent, excelContext, {
      onSchemasEmpty: () => chatHistory.value.pop(),
    })
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
    let userMsg: ChatMessage | null = null
    for (let i = idx - 1; i >= 0; i--) {
      if (chatHistory.value[i].role === 'user') {
        userMsg = chatHistory.value[i]
        break
      }
    }
    if (!userMsg) return

    // 标记重试状态（按钮 loading）
    retryingMsg.value = msg

    // 移除当前 AI 消息
    chatHistory.value.splice(idx, 1)

    // 重置状态
    resetDetectFlag()
    pendingSQLList.value = []
    showRetryConfirm.value = false
    lastQuestion.value = userMsg.content

    // 重新发送请求（不附带 excel 上下文，因为文件已处理完毕）
    await streamChatResponse(userMsg.content, null)
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
