import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Ref } from 'vue'

/** 聊天消息角色类型 */
export type ChatMessageRole = 'user' | 'assistant' | 'thinking' | 'tool_call'

/** 聊天消息结构 */
export interface ChatMessage {
  role: ChatMessageRole
  content: string
  hasSql?: boolean
  collapsed?: boolean
  /** 渲染缓存：上次渲染结果 */
  _renderedHtml?: string | null
  /** 渲染缓存：上次渲染时的内容（用于检测变化） */
  _lastContent?: string | null
  /** 渲染缓存：是否使用了 markdown-it 渲染 */
  _renderedWithMd?: boolean
  /** UI 状态：是否已复制 */
  _copied?: boolean
  /** 扩展字段（兼容历史会话加载等） */
  [key: string]: unknown
}

/** 历史会话条目 */
export interface ChatSessionItem {
  id: string
  title?: string
  createdAt?: string
  [key: string]: unknown
}

/** 表信息（与父组件 tableList 项结构对齐） */
export interface TableInfo {
  name: string
  comment?: string
  schema?: string
  label?: string
  [key: string]: unknown
}

/** useChatHistory 依赖的外部上下文 */
export interface UseChatHistoryDeps {
  /** 当前会话 ID（双向同步） */
  sessionId: Ref<string>
  /** 当前选中的 schema 列表 */
  selectedSchemas: Ref<string[]>
  /** 当前选中的表列表 */
  selectedTables: Ref<string[]>
  /** 可选的表列表（用于过滤历史会话恢复的 tables） */
  tableList: Ref<TableInfo[]>
  /** 根据 selectedSchemas 加载表列表 */
  loadTableListForSchemas: () => Promise<void>
  /** 渲染 mermaid 图表 */
  doRenderMermaidBlocks: (scrollAfter?: boolean) => Promise<void>
  /** 滚动到底部 */
  scrollToBottom: () => void
  /** 处理会话过期 */
  handleSessionExpired: () => void
  /** 重置当前会话所有状态（由 ChatView 协调各 composable 一起 reset） */
  resetCurrentSession: (showMsg?: boolean) => void
}

/** 历史会话可见消息的窗口（用于限制渲染数量） */
const VISIBLE_MSG_LIMIT = 30

/**
 * 聊天历史与会话管理 composable。
 *
 * 负责：
 *   - chatHistory 状态（消息列表，所有 composable 共享）
 *   - 可见消息窗口（visibleChatHistory / hiddenMsgCount）
 *   - 历史会话列表的加载、搜索、分页、删除、切换
 */
export function useChatHistory(deps: UseChatHistoryDeps) {
  const { sessionId, selectedSchemas, selectedTables, tableList, loadTableListForSchemas,
    doRenderMermaidBlocks, scrollToBottom, handleSessionExpired, resetCurrentSession } = deps

  /** 聊天历史消息列表（核心共享状态） */
  const chatHistory = ref<ChatMessage[]>([])

  /** 是否展开全部历史消息（默认仅显示最近 N 条） */
  const showAllHistory = ref(false)

  /** 可见消息窗口：返回当前应渲染的消息列表 + 起始偏移量 */
  const visibleChatHistory = computed(() => {
    if (showAllHistory.value || chatHistory.value.length <= VISIBLE_MSG_LIMIT) {
      return { msgs: chatHistory.value, offset: 0 }
    }
    const offset = chatHistory.value.length - VISIBLE_MSG_LIMIT
    return { msgs: chatHistory.value.slice(offset), offset }
  })

  /** 被隐藏的更早消息数（用于"加载更多"提示） */
  const hiddenMsgCount = computed(() => visibleChatHistory.value.offset)

  // ── 历史会话列表（popover） ──
  const sessionHistoryVisible = ref(false)
  const sessionList = ref<ChatSessionItem[]>([])
  const sessionListTotal = ref(0)
  const loadingSessions = ref(false)
  const sessionSearchKeyword = ref('')
  const sessionCurrentPage = ref(1)
  const sessionPageSize = ref(10)

  let sessionSearchDebounceTimer: ReturnType<typeof setTimeout> | null = null

  /** 格式化 ISO 时间为 yyyy-MM-dd HH:mm:ss */
  function formatDate(isoString?: string): string {
    if (!isoString) return '未知时间'
    const date = new Date(isoString)
    if (isNaN(date.getTime())) return '未知时间'
    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    const seconds = String(date.getSeconds()).padStart(2, '0')
    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
  }

  /** 打开 popover 时初始化列表 */
  async function loadSessionList(): Promise<void> {
    loadingSessions.value = true
    sessionList.value = []
    sessionCurrentPage.value = 1
    sessionSearchKeyword.value = ''
    await fetchSessionList()
  }

  /** 拉取历史会话列表 */
  async function fetchSessionList(): Promise<void> {
    loadingSessions.value = true
    const apiBase = import.meta.env.VITE_API_URL || ''
    const params = new URLSearchParams()
    params.set('page', String(sessionCurrentPage.value))
    params.set('pageSize', String(sessionPageSize.value))
    const keyword = sessionSearchKeyword.value.trim()
    if (keyword) params.set('keyword', keyword)
    const url = apiBase + '/ai/agent/sessions?' + params.toString()
    const auth = sessionStorage.getItem('authentication') || ''

    try {
      const resp = await fetch(url, { method: 'GET', headers: { 'Authorization': auth } })

      if (resp.status === 401) {
        const errorData = await resp.json().catch(() => ({}))
        if (errorData.code === 401) {
          ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
          handleSessionExpired()
          return
        }
      }
      if (!resp.ok) throw new Error(`请求失败：${resp.status}`)

      const data = await resp.json()
      sessionList.value = data.sessions || []
      sessionListTotal.value = data.total || 0
    } catch (e) {
      console.error('[ChatHistory] 加载历史会话失败:', e)
      ElMessage({ message: '加载历史会话失败', type: 'error' })
      sessionList.value = []
      sessionListTotal.value = 0
    } finally {
      loadingSessions.value = false
    }
  }

  /** 搜索框输入（带 300ms 防抖） */
  function handleSessionSearchInput(): void {
    sessionCurrentPage.value = 1
    if (sessionSearchDebounceTimer) clearTimeout(sessionSearchDebounceTimer)
    sessionSearchDebounceTimer = setTimeout(() => {
      sessionSearchDebounceTimer = null
      fetchSessionList()
    }, 300)
  }

  function handleSessionPageChange(page: number): void {
    sessionCurrentPage.value = page
    fetchSessionList()
  }

  function handleSessionSizeChange(size: number): void {
    sessionPageSize.value = size
    sessionCurrentPage.value = 1
    fetchSessionList()
  }

  /** 删除一条历史会话 */
  async function deleteSession(id: string): Promise<void> {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/session/delete?sessionId=' + encodeURIComponent(id)
    const auth = sessionStorage.getItem('authentication') || ''

    try {
      const resp = await fetch(url, { method: 'POST', headers: { 'Authorization': auth } })
      if (resp.status === 401) {
        const errorData = await resp.json().catch(() => ({}))
        if (errorData.code === 401) {
          ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
          handleSessionExpired()
          return
        }
      }
      if (!resp.ok) throw new Error(`请求失败：${resp.status}`)

      ElMessage({ message: '会话已删除', type: 'success' })
      if (sessionList.value.length <= 1 && sessionCurrentPage.value > 1) {
        sessionCurrentPage.value -= 1
      }
      await fetchSessionList()
    } catch (e) {
      console.error('[ChatHistory] 删除会话失败:', e)
      ElMessage({ message: '删除会话失败', type: 'error' })
    }
  }

  /** 点击某条历史会话：先关闭 popover，再延迟加载 */
  function handleClickSession(id: string): void {
    sessionHistoryVisible.value = false
    setTimeout(() => { void loadSession(id) }, 100)
  }

  /** 加载指定会话的完整消息列表 */
  async function loadSession(id: string): Promise<void> {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const url = apiBase + '/ai/agent/session?sessionId=' + encodeURIComponent(id)
    const auth = sessionStorage.getItem('authentication') || ''

    try {
      const resp = await fetch(url, { method: 'GET', headers: { 'Authorization': auth } })

      if (resp.status === 401) {
        const errorData = await resp.json().catch(() => ({}))
        if (errorData.code === 401) {
          ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' })
          handleSessionExpired()
          return
        }
      }
      if (!resp.ok) throw new Error(`请求失败：${resp.status}`)

      const data = await resp.json()
      if (data.session) {
        // 清空当前会话所有状态
        resetCurrentSession()

        sessionId.value = data.session.id
        for (const msg of data.session.messages) {
          const contentStr = typeof msg.content === 'string' ? msg.content : String(msg.content ?? '')
          const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(contentStr.trim())
          chatHistory.value.push({
            role: msg.role,
            content: contentStr,
            hasSql: isSql,
            collapsed: true,
          })
        }

        // 恢复会话上下文（当时选择的 schemas 和 tables）
        const ctx = data.session.context
        if (ctx && ctx.schemas && ctx.schemas.length > 0) {
          const schemaValues = ctx.schemas
            .filter((s: { connId?: string; schema?: string }) => s.connId && s.schema)
            .map((s: { connId: string; schema: string }) => s.connId + '::' + s.schema)
          if (schemaValues.length > 0) {
            selectedSchemas.value = schemaValues
            await loadTableListForSchemas()
            if (ctx.tables && ctx.tables.length > 0) {
              selectedTables.value = ctx.tables.filter((t: string) =>
                tableList.value.some((tl) => tl.label === t || tl.name === t),
              )
            }
          }
        }

        ElMessage({ message: '已加载历史会话', type: 'success' })
        scrollToBottom()
        await doRenderMermaidBlocks()
      }
    } catch (e) {
      console.error('[ChatHistory] 加载会话失败:', e)
      ElMessage({ message: '加载会话失败', type: 'error' })
    }
  }

  /** 仅清空消息历史与展开状态（供 ChatView.clearSession 调用） */
  function clearMessages(): void {
    chatHistory.value = []
    showAllHistory.value = false
  }

  return {
    chatHistory,
    showAllHistory,
    visibleChatHistory,
    hiddenMsgCount,
    VISIBLE_MSG_LIMIT,
    // 历史会话列表
    sessionHistoryVisible,
    sessionList,
    sessionListTotal,
    loadingSessions,
    sessionSearchKeyword,
    sessionCurrentPage,
    sessionPageSize,
    // 方法
    formatDate,
    loadSessionList,
    fetchSessionList,
    handleSessionSearchInput,
    handleSessionPageChange,
    handleSessionSizeChange,
    deleteSession,
    handleClickSession,
    loadSession,
    clearMessages,
  }
}
