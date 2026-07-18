import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Ref } from 'vue'
import { sanitizeError } from '@/utils/errorHandler'
import { analyzeSQL, type SQLAnalysisResult } from '@/utils/sqlRiskAssessment'
import type { ChatMessage } from './useChatHistory'

/** 单条 SQL 风险评估结果（用于批量确认列表） */
export type SqlRiskItem = {
  sql: string
  selected: boolean
} & SQLAnalysisResult

/** 带 interruptIds/checkPointId 元信息的批量 SQL 列表（附加在 ref 上） */
type PendingSQLListWithMeta = Ref<SqlRiskItem[]> & {
  interruptIds?: string[]
  checkPointId?: string
}

/** useSqlConfirm 依赖的外部上下文 */
export interface UseSqlConfirmDeps {
  /** 聊天历史（追加执行结果消息） */
  chatHistory: Ref<ChatMessage[]>
  /** 全局 loading（执行期间置 true） */
  loading: Ref<boolean>
  /** 全局 AbortController（用于中止执行流） */
  abortController: Ref<AbortController | null>
  /** 当前会话 ID */
  sessionId: Ref<string>
  /** 流式执行内容（确认后 Agent 继续输出时使用） */
  streamingExecContent: Ref<string>
  /** 构建 schemas 数组（用于请求体） */
  buildRequestSchemas: () => { connId: string; schema: string }[]
  /** 获取主连接 connId */
  getPrimaryConnId: () => string
  /** 滚动到底部（force=true 时强制滚动，不判断用户位置） */
  scrollToBottom: (force?: boolean) => void
  /** 处理会话过期 */
  handleSessionExpired: () => void
  /** 流式 exec 内容变化时回调（用于触发外部 HTML 重渲染） */
  onStreamingExecContentChange?: () => void
}

/**
 * SQL 危险操作确认 composable（状态机）。
 *
 * 负责：
 *   - 单条危险 SQL 的内联确认（confirmVisible / confirmSQL / ...）
 *   - 多条 SQL 的批量确认（pendingSQLList / selectAll / 批量执行）
 *   - 重试确认（showRetryConfirm / retryMessage / lastQuestion）
 *   - 确认后恢复执行流（resume），并处理恢复后再次出现的 danger_confirm
 */
export function useSqlConfirm(deps: UseSqlConfirmDeps) {
  const {
    chatHistory, loading, abortController, sessionId, streamingExecContent,
    buildRequestSchemas, getPrimaryConnId, scrollToBottom, handleSessionExpired,
  } = deps

  // ── 单条 SQL 确认 ──
  const confirmVisible = ref(false)
  const confirmSQL = ref('')
  const confirmInterruptIds = ref<string[]>([])
  const confirmCheckPointId = ref('')
  const confirmOperationType = ref<string>('SELECT')
  const confirmRiskLevel = ref<string>('low')
  const confirmDescription = ref('')
  const confirmTableName = ref('')
  let hasShownConfirm = false

  // ── 多条 SQL 批量确认 ──
  const pendingSQLList = ref<SqlRiskItem[]>([]) as PendingSQLListWithMeta
  const selectAllChecked = ref(false)
  const selectedSQLCount = computed(() => pendingSQLList.value.filter((i) => i.selected).length)

  // ── 重试确认 ──
  const showRetryConfirm = ref(false)
  const retryMessage = ref('')
  const lastQuestion = ref('')

  /** 重置"已显示确认"标记（每次新对话/重试时调用） */
  function resetDetectFlag(): void {
    hasShownConfirm = false
  }

  /** 全选/取消全选 */
  function handleSelectAllChange(val: boolean): void {
    pendingSQLList.value.forEach((item) => { item.selected = val })
  }

  /** 显示单条 SQL 确认弹框 */
  function showConfirmDialog(sql: string, interruptIds: string | string[], checkPointId: string): void {
    const analysis = analyzeSQL(sql)
    confirmSQL.value = sql
    confirmInterruptIds.value = Array.isArray(interruptIds) ? interruptIds : [interruptIds]
    confirmCheckPointId.value = checkPointId || ''
    confirmOperationType.value = analysis.type
    confirmRiskLevel.value = analysis.riskLevel
    confirmDescription.value = analysis.description
    confirmTableName.value = analysis.tableName || ''
    confirmVisible.value = true
  }

  /** 设置批量 SQL 列表（统一构造，避免重复代码） */
  function setBatchPendingSQL(items: { sql: string; interruptId: string; checkPointId: string }[]): void {
    const cpId = items[0].checkPointId
    const ids = items.map((d) => d.interruptId)
    if (items.length === 1) {
      showConfirmDialog(items[0].sql, ids, cpId)
    } else {
      pendingSQLList.value = items.map((item) => {
        const analysis = analyzeSQL(item.sql)
        return { sql: item.sql, ...analysis, selected: true }
      })
      selectAllChecked.value = true
      pendingSQLList.interruptIds = ids
      pendingSQLList.checkPointId = cpId
    }
  }

  /**
   * 处理确认执行：发送 confirmed=true 的 Resume 请求，接收 Agent 后续输出。
   * 如果 Agent 继续续执行后又遇到新的危险 SQL，会再次弹出确认框。
   */
  async function handleConfirmExec(_confirmedSql: string): Promise<void> {
    loading.value = true
    confirmVisible.value = false

    const sqlForDisplay = confirmSQL.value
    chatHistory.value.push({
      role: 'assistant',
      content: `⏳ 正在执行：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\``,
    })
    scrollToBottom()

    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/chatStream'
    const auth = sessionStorage.getItem('authentication') || ''

    try {
      const controller = new AbortController()
      abortController.value = controller

      const schemas = buildRequestSchemas()
      const primaryConnId = getPrimaryConnId()
      const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
      const resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': auth },
        body: JSON.stringify({
          sessionId: sessionId.value,
          connId: primaryConnId,
          schema: primarySchema,
          schemas,
          question: '执行已确认的 SQL',
          confirmed: true,
          interruptIds: confirmInterruptIds.value,
          checkPointId: confirmCheckPointId.value,
        }),
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

      const reader = resp.body!.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      streamingExecContent.value = ''
      const collectedDangerSQLs: { sql: string; interruptId: string; checkPointId: string }[] = []
      let hasError = false
      let errorMsg = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() || ''
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const trimmed = line.slice(6).trim()
          if (!trimmed) continue
          try {
            const chunk = JSON.parse(trimmed)
            if (chunk.type === 'content') {
              streamingExecContent.value += chunk.content
              scrollToBottom()
            }
            if (chunk.type === 'danger_confirm') {
              collectedDangerSQLs.push({
                sql: chunk.sql || chunk.content,
                interruptId: chunk.interruptId || '',
                checkPointId: chunk.checkPointId || '',
              })
            }
            if (chunk.type === 'error') {
              hasError = true
              errorMsg = chunk.content || '执行失败'
            }
          } catch (_) { /* ignore */ }
        }
      }

      const execContent = streamingExecContent.value
      streamingExecContent.value = ''

      if (hasError) {
        chatHistory.value.push({
          role: 'assistant',
          content: `❌ 执行失败：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`\n${sanitizeError(errorMsg)}`,
        })
      } else {
        chatHistory.value.push({
          role: 'assistant',
          content: `✅ 已执行：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\``,
        })
      }

      if (execContent) {
        chatHistory.value.push({ role: 'assistant', content: execContent })
      }

      scrollToBottom()

      if (collectedDangerSQLs.length > 0) {
        loading.value = false
        abortController.value = null
        setBatchPendingSQL(collectedDangerSQLs)
        return
      }
    } catch (e) {
      if ((e as Error).name === 'AbortError') {
        chatHistory.value.push({
          role: 'assistant',
          content: `⏹ 已终止：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\``,
        })
      } else {
        ElMessage({ message: sanitizeError(e) || '执行失败', type: 'error' })
      }
      streamingExecContent.value = ''
    } finally {
      loading.value = false
      abortController.value = null
      scrollToBottom()
    }
  }

  /** 处理取消确认：向后端发送 confirmed=false，清理 checkpoint 状态 */
  async function handleConfirmCancel(): Promise<void> {
    confirmVisible.value = false
    chatHistory.value.push({
      role: 'assistant',
      content: `已取消执行：\n\`\`\`sql\n${confirmSQL.value}\n\`\`\``,
    })
    scrollToBottom()

    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/chatStream'
    const auth = sessionStorage.getItem('authentication') || ''
    try {
      await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': auth },
        body: JSON.stringify({
          sessionId: sessionId.value,
          connId: getPrimaryConnId(),
          schema: buildRequestSchemas().length > 0 ? buildRequestSchemas()[0].schema : '',
          schemas: buildRequestSchemas(),
          question: '取消执行',
          confirmed: false,
          interruptIds: confirmInterruptIds.value,
          checkPointId: confirmCheckPointId.value,
        }),
      })
    } catch (_) { /* 取消请求失败不影响用户体验 */ }
  }

  /** 批量确认：一次性 Resume 所有选中的 interruptId */
  async function handleConfirmSelectedSQL(): Promise<void> {
    const selectedItems = pendingSQLList.value.filter((item) => item.selected)
    if (selectedItems.length === 0) {
      ElMessage.warning('请至少选择一条SQL')
      return
    }

    const allItems = [...pendingSQLList.value]
    const allInterruptIds = pendingSQLList.interruptIds || []
    const checkPointId = pendingSQLList.checkPointId || ''

    const selectedInterruptIds: string[] = []
    for (let i = 0; i < allItems.length; i++) {
      if (allItems[i].selected && allInterruptIds[i]) {
        selectedInterruptIds.push(allInterruptIds[i])
      }
    }

    pendingSQLList.value = []
    selectAllChecked.value = false
    loading.value = true

    for (const item of selectedItems) {
      chatHistory.value.push({
        role: 'assistant',
        content: `⏳ 正在执行：\n\`\`\`sql\n${item.sql}\n\`\`\``,
      })
    }
    scrollToBottom()

    await executeBatchResume(selectedItems, selectedInterruptIds, checkPointId)

    loading.value = false
    scrollToBottom()
  }

  /** 批量恢复执行：一次 Resume 传入所有 interruptId */
  async function executeBatchResume(
    sqlItems: SqlRiskItem[],
    interruptIds: string[],
    checkPointId: string,
  ): Promise<void> {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/chatStream'
    const auth = sessionStorage.getItem('authentication') || ''

    try {
      const schemas = buildRequestSchemas()
      const primaryConnId = getPrimaryConnId()
      const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
      const resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': auth },
        body: JSON.stringify({
          sessionId: sessionId.value,
          connId: primaryConnId,
          schema: primarySchema,
          schemas,
          question: 'resume confirmed SQL',
          confirmed: true,
          interruptIds: interruptIds,
          checkPointId: checkPointId,
        }),
      })

      if (resp.status === 401) {
        const errorData = await resp.json().catch(() => ({}))
        if (errorData.code === 401) {
          ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
          handleSessionExpired()
          return
        }
      }

      const reader = resp.body!.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      streamingExecContent.value = ''
      const collectedDangerSQLs: { sql: string; interruptId: string; checkPointId: string }[] = []
      let hasError = false
      let errorMsg = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() || ''
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const trimmed = line.slice(6).trim()
          if (!trimmed) continue
          try {
            const chunk = JSON.parse(trimmed)
            if (chunk.type === 'content') streamingExecContent.value += chunk.content
            if (chunk.type === 'danger_confirm') {
              collectedDangerSQLs.push({
                sql: chunk.sql || chunk.content,
                interruptId: chunk.interruptId || '',
                checkPointId: chunk.checkPointId || '',
              })
            }
            if (chunk.type === 'error') { hasError = true; errorMsg = chunk.content || 'exec failed' }
          } catch (_) { /* ignore */ }
        }
      }

      const execContent = streamingExecContent.value
      streamingExecContent.value = ''

      for (const item of sqlItems) {
        chatHistory.value.push({
          role: 'assistant',
          content: hasError
            ? `❌ 执行失败：\n\`\`\`sql\n${item.sql}\n\`\`\``
            : `✅ 已执行：\n\`\`\`sql\n${item.sql}\n\`\`\``,
        })
      }

      if (execContent) {
        chatHistory.value.push({ role: 'assistant', content: execContent })
      }

      if (collectedDangerSQLs.length > 0) {
        setBatchPendingSQL(collectedDangerSQLs)
      }
    } catch (e) {
      streamingExecContent.value = ''
      ElMessage({ message: sanitizeError(e) || 'exec failed', type: 'error' })
    }
  }

  /** 批量取消：清理 pendingSQLList，并向后端发送取消请求 */
  async function handleCancelAllSQL(): Promise<void> {
    const items = pendingSQLList.value
    const allInterruptIds = pendingSQLList.interruptIds || []
    const checkPointId = pendingSQLList.checkPointId || ''
    pendingSQLList.value = []
    selectAllChecked.value = false
    for (const item of items) {
      chatHistory.value.push({
        role: 'assistant',
        content: `已取消执行：\n\`\`\`sql\n${item.sql}\n\`\`\``,
      })
    }
    scrollToBottom()

    if (allInterruptIds.length > 0 && checkPointId) {
      const apiBase = import.meta.env.VITE_API_URL || ''
      const url = apiBase + '/ai/agent/chatStream'
      const auth = sessionStorage.getItem('authentication') || ''
      try {
        await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': auth },
          body: JSON.stringify({
            sessionId: sessionId.value,
            connId: getPrimaryConnId(),
            schema: buildRequestSchemas().length > 0 ? buildRequestSchemas()[0].schema : '',
            schemas: buildRequestSchemas(),
            question: '取消执行',
            confirmed: false,
            interruptIds: allInterruptIds,
            checkPointId: checkPointId,
          }),
        })
      } catch (_) { /* 取消请求失败不影响用户体验 */ }
    }
  }

  /** 重试继续：把上一次的问题回填到输入框并发送 */
  function handleRetryContinue(sendMessage: () => void): void {
    showRetryConfirm.value = false
    // 通过返回 lastQuestion 让 ChatView 处理回填与发送
    void sendMessage
  }

  /** 仅清空 SQL 确认相关状态（供 ChatView.clearSession 调用） */
  function resetConfirmState(): void {
    confirmVisible.value = false
    confirmSQL.value = ''
    confirmInterruptIds.value = []
    confirmCheckPointId.value = ''
    pendingSQLList.value = []
    selectAllChecked.value = false
    showRetryConfirm.value = false
    retryMessage.value = ''
    hasShownConfirm = false
  }

  return {
    // 单条 SQL 确认状态
    confirmVisible,
    confirmSQL,
    confirmOperationType,
    confirmRiskLevel,
    confirmDescription,
    confirmTableName,
    // 批量 SQL 确认状态
    pendingSQLList,
    selectAllChecked,
    selectedSQLCount,
    // 重试确认状态
    showRetryConfirm,
    retryMessage,
    lastQuestion,
    // 方法
    resetDetectFlag,
    handleSelectAllChange,
    showConfirmDialog,
    setBatchPendingSQL,
    handleConfirmExec,
    handleConfirmCancel,
    handleConfirmSelectedSQL,
    handleCancelAllSQL,
    handleRetryContinue,
    resetConfirmState,
  }
}
