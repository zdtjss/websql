<template>
  <div>
    <div class="ai-sql-panel-container">
      <div class="container">
        <!-- 会话历史消息 -->
        <ChatMessageList
          ref="msgListRef"
          :visible-messages="chatHistory.visibleChatHistory.value"
          :hidden-msg-count="chatHistory.hiddenMsgCount.value"
          :loading="loading"
          :thinking-text="chatStream.thinkingText.value"
          :thinking-html="chatStream.thinkingHtml.value"
          :streaming-content="chatStream.streamingContent.value"
          :streaming-html="chatStream.streamingHtml.value"
          :streaming-exec-content="streamingExecContent"
          :streaming-exec-html="chatStream.streamingExecHtml.value"
          :retrying-msg="chatStream.retryingMsg.value"
          :can-retry="chatStream.canRetryMessage"
          :get-cached-html="mdRenderer.getCachedHtml"
          :highlight-sql="mdRenderer.highlightSql"
          @show-all="chatHistory.showAllHistory.value = true"
          @toggle-thinking="chatStream.toggleThinking"
          @copy="chatStream.copyMessage"
          @retry="chatStream.retryAssistantMessage"
        />

        <!-- 内联 SQL 确认区域 -->
        <SqlConfirmDialog
          v-model:visible="sqlConfirm.confirmVisible.value"
          :sql="sqlConfirm.confirmSQL.value"
          :operation-type="sqlConfirm.confirmOperationType.value"
          :risk-level="sqlConfirm.confirmRiskLevel.value"
          :description="sqlConfirm.confirmDescription.value"
          :table-name="sqlConfirm.confirmTableName.value"
          @confirm="sqlConfirm.handleConfirmExec"
          @cancel="sqlConfirm.handleConfirmCancel"
        />

        <!-- 多条 SQL 批量确认区域 -->
        <BatchSqlConfirmDialog
          :items="sqlConfirm.pendingSQLList.value"
          :select-all="sqlConfirm.selectAllChecked.value"
          :selected-count="sqlConfirm.selectedSQLCount.value"
          :highlight-sql="mdRenderer.highlightSql"
          @select-all="sqlConfirm.handleSelectAllChange"
          @confirm-selected="sqlConfirm.handleConfirmSelectedSQL"
          @cancel-all="sqlConfirm.handleCancelAllSQL"
        />

        <!-- 重试确认区域 -->
        <RetryConfirmDialog
          :visible="sqlConfirm.showRetryConfirm.value"
          :message="sqlConfirm.retryMessage.value"
          @continue="handleRetryContinue"
          @abort="sqlConfirm.showRetryConfirm.value = false"
        />

        <!-- 输入区域 -->
        <ChatInputArea
          v-model:question="question"
          v-model:selected-schemas="selectedSchemas"
          v-model:selected-tables="selectedTables"
          v-model:selected-model="selectedModel"
          :loading="loading"
          :has-uploaded-file="!!uploadedExcel"
          :processed-conn-list="processedConnList"
          :should-show-schema-selector="shouldShowSchemaSelector"
          :schemas-loading="schemasLoading"
          :table-list="tableList"
          :tables-loading="tablesLoading"
          :ai-model-list="aiModelList"
          :get-table-comment="getTableComment"
          @schema-change="handleSchemaChange"
          @send="chatStream.sendMessage"
          @stop="chatStream.stopGeneration"
        >
          <template #toolbar>
            <div style="display: flex; gap: 0px;">
              <!-- 已上传的文件信息 -->
              <div v-if="uploadedExcel" class="uploaded-file-info" style="margin-left: 12px;">
                <span>{{ uploadedExcel.name }}
                  <template v-if="uploadedExcel.fileType === 'markdown'">(Markdown, {{ uploadedExcel.charCount }} 字符)</template>
                  <template v-else>({{ uploadedExcel.rows }} 行, {{ uploadedExcel.columns.length }} 列)</template>
                </span>
                <el-button size="small" text type="danger" @click="clearUploadedExcel">✕</el-button>
              </div>
              <el-upload ref="excelUploadRef" :auto-upload="false" :show-file-list="false" style="margin-left: 12px;" accept=".xlsx,.xls,.csv,.md,.markdown"
                :on-change="handleExcelUpload" :disabled="excelUploading">
                <el-button class="toolbar-btn" size="small" title="上传数据文件（Excel/CSV/Markdown，可分析/结合数据库/导入）" :loading="excelUploading">
                  <el-icon v-if="!excelUploading"><Upload /></el-icon>
                </el-button>
              </el-upload>
              <!-- 提示词 Popover -->
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="promptPopoverVisible"
                @show="loadPrompts()">
                <div class="prompt-popover-body">
                  <el-tabs v-model="activeTab" class="prompt-tabs">
                    <el-tab-pane name="mine">
                      <template #label>
                        <span style="display: inline-flex; align-items: center; gap: 6px; width: 100%;">
                          我的
                          <el-icon v-if="myPromptLoading" class="is-loading" size="14"><Loading /></el-icon>
                          <el-icon :size="10" style="top: -10px;" @click="handlePromptAdd"><Plus /></el-icon>
                        </span>
                      </template>
                      <div class="prompt-search-box">
                        <el-input v-model="promptSearchKeyword" size="small" placeholder="按标题搜索" clearable
                          v-if="myPromptsTotal > promptPageSize" @input="handlePromptSearchInput">
                          <template #prefix><el-icon><Search /></el-icon></template>
                        </el-input>
                      </div>
                      <div class="prompt-list">
                        <div v-if="myPromptLoading && myPrompts.length === 0" style="text-align: center; padding: 10px;">
                          <el-icon class="is-loading"><Loading /></el-icon>
                        </div>
                        <div v-else-if="myPrompts.length === 0" class="prompt-empty">
                          {{ promptSearchKeyword ? '没有匹配的提示词' : '暂无提示词' }}
                        </div>
                        <div v-for="prompt in myPrompts" :key="prompt.id" class="prompt-item"
                          @click.stop="handlePromptSendToAI(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })">
                          <div class="prompt-item-info">
                            <div class="prompt-item-title">{{ prompt.title }}</div>
                            <div v-if="prompt.connSchemas && prompt.connSchemas.length > 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>{{ prompt.connSchemas.length }} 个 Schema
                            </div>
                            <div v-else-if="prompt.connSchemas && prompt.connSchemas.length === 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>{{ prompt.connSchemas[0].schema }}
                            </div>
                            <div v-if="prompt.isShared" class="prompt-item-sub">
                              <el-icon size="12"><Share /></el-icon>{{ prompt.sharedByName || '他人分享' }}
                            </div>
                          </div>
                          <div class="prompt-item-actions">
                            <el-button text type="primary" @click.stop="handlePromptFillInput(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })" title="填入输入框">
                              <el-icon><BottomLeft /></el-icon>
                            </el-button>
                            <el-button text type="primary">
                              <el-icon v-if="!prompt.isShared" @click.stop="handlePromptEdit(prompt.id)" title="编辑"><Edit /></el-icon>
                            </el-button>
                            <el-popconfirm v-if="!prompt.isShared" title="确定要删除？" @confirm="handleDeletePrompt(prompt)">
                              <template #reference>
                                <el-button style="margin-left: -10px;" text type="danger" title="删除" @click.stop>
                                  <el-icon><Delete /></el-icon>
                                </el-button>
                              </template>
                            </el-popconfirm>
                          </div>
                        </div>
                      </div>
                      <div v-if="myPromptsTotal > promptPageSize" class="prompt-pagination">
                        <el-pagination v-model:current-page="myPromptCurrentPage" :page-size="promptPageSize" :total="myPromptsTotal" layout="prev, pager, next" small @current-change="handleMyPromptPageChange" />
                      </div>
                    </el-tab-pane>
                    <el-tab-pane label="系统" name="system">
                      <div class="prompt-search-box">
                        <el-input v-model="promptSearchKeyword" size="small" placeholder="按标题搜索" clearable
                          v-if="systemPromptsTotal > promptPageSize" @input="handlePromptSearchInput">
                          <template #prefix><el-icon><Search /></el-icon></template>
                        </el-input>
                      </div>
                      <div class="prompt-list">
                        <div v-if="systemPromptLoading && systemPrompts.length === 0" style="text-align: center; padding: 10px;">
                          <el-icon class="is-loading"><Loading /></el-icon>
                        </div>
                        <div v-else-if="systemPrompts.length === 0" class="prompt-empty">
                          {{ promptSearchKeyword ? '没有匹配的系统提示词' : '暂无系统提示词' }}
                        </div>
                        <div v-for="prompt in systemPrompts" title="点击发送给大模型" :key="prompt.id" class="prompt-item"
                          @click.stop="handlePromptSendToAI(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })">
                          <div class="prompt-item-info">
                            <div class="prompt-item-title">{{ prompt.title }}</div>
                            <div v-if="prompt.connSchemas && prompt.connSchemas.length > 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>{{ prompt.connSchemas.length }} 个 Schema
                            </div>
                            <div v-else-if="prompt.connSchemas && prompt.connSchemas.length === 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>{{ prompt.connSchemas[0].schema }}
                            </div>
                          </div>
                          <div class="prompt-item-actions">
                            <el-button size="small" text type="primary" @click.stop="handlePromptFillInput(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })" title="填入输入框">
                              <el-icon><BottomLeft /></el-icon>
                            </el-button>
                            <el-button size="small" text type="info" @click.stop="handleViewPromptDetail(prompt)" title="查看">
                              <el-icon><View /></el-icon>
                            </el-button>
                          </div>
                        </div>
                      </div>
                      <div v-if="systemPromptsTotal > promptPageSize" class="prompt-pagination">
                        <el-pagination v-model:current-page="systemPromptCurrentPage" :page-size="promptPageSize" :total="systemPromptsTotal" layout="prev, pager, next" small @current-change="handleSystemPromptPageChange" />
                      </div>
                    </el-tab-pane>
                  </el-tabs>
                </div>
                <PromptEditDialog v-model="promptEditDialogVisible" :prompt-id="editingPromptId" :role-id="editingRoleId" @saved="handlePromptSaved" @send-to-AI="handleSendToAIFromDialog" />
                <el-dialog v-model="promptDetailVisible" :title="promptDetail?.title || '提示词详情'" width="800px" append-to-body>
                  <div v-if="promptDetail">
                    <div v-if="promptDetail.connSchemas && promptDetail.connSchemas.length" class="prompt-detail-meta">
                      <div class="prompt-detail-meta-label">关联 Schema</div>
                      <div class="prompt-detail-meta-tags">
                        <el-tag v-for="cs in promptDetail.connSchemas" :key="cs.connId + cs.schema" size="small" type="info">
                          <el-icon style="margin-right: 4px;"><Coin /></el-icon>{{ cs.schema }}
                        </el-tag>
                      </div>
                    </div>
                    <div v-if="promptDetail.tables && promptDetail.tables.length" class="prompt-detail-meta">
                      <div class="prompt-detail-meta-label">关联表</div>
                      <div class="prompt-detail-meta-tags">
                        <el-tooltip v-for="t in promptDetail.tables" :key="typeof t === 'string' ? t : t.name" :content="getTableComment(typeof t === 'string' ? t : t.name) || (typeof t === 'object' ? t.comment : '') || ''" :disabled="!(getTableComment(typeof t === 'string' ? t : t.name) || (typeof t === 'object' && t.comment))" placement="top">
                          <el-tag size="small">{{ typeof t === 'string' ? t : t.name }}</el-tag>
                        </el-tooltip>
                      </div>
                    </div>
                    <el-divider v-if="promptDetail.connSchemas?.length || promptDetail.tables?.length" style="margin: 12px 0;" />
                    <div class="prompt-detail-content markdown-body" v-html="mdRenderer.renderMarkdown(promptDetail.content)"></div>
                  </div>
                  <template #footer>
                    <el-button @click="promptDetailVisible = false">关闭</el-button>
                    <el-button type="primary" @click="handleFillFromDetail"><el-icon><BottomLeft /></el-icon>填入输入框</el-button>
                    <el-button type="primary" @click="handleSendFromDetail"><el-icon><Promotion /></el-icon>发送给大模型</el-button>
                  </template>
                </el-dialog>
                <template #reference>
                  <el-button class="toolbar-btn" circle size="small" title="常用提示词" style="margin-left: 12px;">
                    <el-icon><ChatLineRound /></el-icon>
                  </el-button>
                </template>
              </el-popover>
              <!-- 历史会话 Popover -->
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="chatHistory.sessionHistoryVisible.value"
                @show="chatHistory.loadSessionList()">
                <ChatHistorySidebar
                  v-model:search-keyword="chatHistory.sessionSearchKeyword.value"
                  v-model:current-page="chatHistory.sessionCurrentPage.value"
                  v-model:page-size="chatHistory.sessionPageSize.value"
                  :session-list="chatHistory.sessionList.value"
                  :total="chatHistory.sessionListTotal.value"
                  :loading="chatHistory.loadingSessions.value"
                  :format-date="chatHistory.formatDate"
                  @search-input="chatHistory.handleSessionSearchInput"
                  @page-change="chatHistory.handleSessionPageChange"
                  @size-change="chatHistory.handleSessionSizeChange"
                  @click-session="chatHistory.handleClickSession"
                  @delete-session="chatHistory.deleteSession"
                />
                <template #reference>
                  <el-button class="toolbar-btn" size="small" title="历史会话" style="margin-left: 12px;">
                    <el-icon><Document /></el-icon>
                  </el-button>
                </template>
              </el-popover>
              <!-- 语音录入 -->
              <el-button class="toolbar-btn" :type="isRecording ? 'danger' : 'primary'" size="small"
                @click="toggleRecording" :title="isRecording ? '停止录音' : '开始录音'">
                <el-icon style="vertical-align: middle;">
                  <component :is="isRecording ? VideoPause : Microphone" />
                </el-icon>
              </el-button>
              <!-- 清空会话 -->
              <el-button class="toolbar-btn" size="small" @click="clearSession(true)" title="清空并新建会话">
                <el-icon><Delete /></el-icon>
              </el-button>
              <!-- 经典视图 -->
              <el-button v-if="canUseClassicView" class="toolbar-btn" size="small" @click="$router.push('/classical')" title="经典视图">
                <el-icon><Switch /></el-icon>
              </el-button>
            </div>
          </template>
        </ChatInputArea>
      </div>
    </div>
    <div class="login-button-container">
      <div style="display: flex; flex-direction: column; gap: 8px; align-items: center;">
        <el-button circle size="small" @click="toggleTheme" :title="currentTheme === 'light' ? '切换到夜色模式' : '切换到日间模式'">
          <el-icon><component :is="currentTheme === 'light' ? Moon : Sunny" /></el-icon>
        </el-button>
        <el-button v-if="currentUser.isAdmin || !isRemote" circle size="small" @click="openSystemManagement" title="系统管理">
          <el-icon><Setting /></el-icon>
        </el-button>
        <el-button v-if="!loginSucc && isRemote" circle size="small" @click="showLoginDialog" title="登录">
          <el-icon><User /></el-icon>
        </el-button>
        <el-button v-else-if="loginSucc && isRemote" circle size="small" @click="logout" title="退出登录">
          <el-icon><SwitchButton /></el-icon>
        </el-button>
      </div>
    </div>
    <LoginDialog ref="loginDialogRef" v-model="loginDialogVisible" @login-success="handleLoginSuccess" />
  </div>
</template>

<script setup lang="ts">
import ChatMessageList from './components/ChatMessageList.vue'
import ChatInputArea from './components/ChatInputArea.vue'
import SqlConfirmDialog from './components/SqlConfirmDialog.vue'
import BatchSqlConfirmDialog from './components/BatchSqlConfirmDialog.vue'
import RetryConfirmDialog from './components/RetryConfirmDialog.vue'
import ChatHistorySidebar from './components/ChatHistorySidebar.vue'
import PromptEditDialog from '@/components/ai/PromptEditDialog.vue'
import LoginDialog from '@/components/auth/LoginDialog.vue'
import { preloadVditor } from '@/utils/vditorLoader'
import { usePromptEditDialog } from '@/components/ai/usePromptEditDialog'
import { getPromptList, delPrompt, getAIModels } from '@/api/ai'
import { listTableNames, listTableNamesBySchemas } from '@/api/conn'
import { logout as logoutApi, canUseClassicView as canUseClassicViewApi, getSysMode } from '@/api/auth'
import { getSystemConfig } from '@/api/system'
import { sanitizeError } from '@/utils/errorHandler'
import { useTheme } from '@/utils/useTheme'
import { useStorage } from '@/composables/useStorage'
import { useMarkdownRenderer } from './composables/useMarkdownRenderer'
import { useChatHistory } from './composables/useChatHistory'
import { useSqlConfirm } from './composables/useSqlConfirm'
import { useChatStream, type UploadedExcel } from './composables/useChatStream'
import { BottomLeft, ChatLineRound, Clock, Coin, Delete, Document, Edit, Loading, Microphone, Moon, Plus, Promotion, Search, Setting, Share, Sunny, Switch, SwitchButton, Upload, User, VideoPause, View } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, useTemplateRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import './styles/chat-global.css'

// ── 主题与存储 ──
const { currentTheme, toggleTheme } = useTheme()
const storage = useStorage()
const router = useRouter()

// ── ChatView 专属状态 ──
const question = ref('')
const selectedTables = ref<string[]>([])
const selectedSchemas = ref<string[]>([])
const selectedModel = ref('')
const sessionId = ref('')
const uploadedExcel = ref<UploadedExcel | null>(null)
const tableList = ref<{ name: string; comment?: string; schema?: string; label?: string }[]>([])
const msgListComponent = useTemplateRef('msgListRef')
// ChatMessageList 通过 defineExpose 暴露 containerRef（DOM 元素）
const msgContainer = computed<HTMLElement | null>(() => (msgListComponent.value as any)?.containerRef ?? null)
const isRecording = ref(false)
let speechRecognition: unknown = null

// ── 共享状态（由 ChatView 创建，传递给 useChatStream 和 useSqlConfirm） ──
const loading = ref(false)
const abortController = ref<AbortController | null>(null)
const streamingExecContent = ref('')

// ── 登录/认证状态 ──
const isRemote = ref(sessionStorage.getItem('isRemote') === 'true')
const canUseClassicView = ref(false)
const showLoginBtn = ref(true)
const loginDialogVisible = ref(false)
const loginForm = ref({ name: '', password: '' })
const loginName = ref()
function parseCurrentUser() {
  try {
    const stored = sessionStorage.getItem('currentUser')
    return stored ? JSON.parse(stored) : { id: '', name: '', isAdmin: false }
  } catch {
    return { id: '', name: '', isAdmin: false }
  }
}
const currentUser = ref(parseCurrentUser())
const loginSucc = ref(!!sessionStorage.getItem('authentication'))
const logining = ref(false)
const loginRules = reactive({
  name: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
})

// ── 连接/Schema/表 状态 ──
const connList = ref<unknown[]>([])
const connSchemaList = ref<unknown[]>([])
const schemasLoading = ref(false)
const tablesLoading = ref(false)
const aiModelList = ref<{ id: string; model: string }[]>([])
const modelLoading = ref(false)

// ── 提示词状态 ──
const { visible: promptEditDialogVisible, promptId: editingPromptId, roleId: editingRoleId, openDialog: openPromptEditDialog, closeDialog: closePromptEditDialog, triggerSaved: triggerPromptSaved, setSendToAIHandler, handleSendToAI: handleSendToAIFromDialog } = usePromptEditDialog()
const promptPopoverVisible = ref(false)
const myPrompts = ref<any[]>([])
const systemPrompts = ref<any[]>([])
const myPromptsTotal = ref(0)
const systemPromptsTotal = ref(0)
const myPromptLoading = ref(false)
const systemPromptLoading = ref(false)
const activeTab = ref('mine')
const promptDetailVisible = ref(false)
const promptDetail = ref<any>(null)
const promptSearchKeyword = ref('')
const myPromptCurrentPage = ref(1)
const systemPromptCurrentPage = ref(1)
const promptPageSize = ref(10)
let promptSearchDebounceTimer: ReturnType<typeof setTimeout> | null = null

// ── Excel 上传 ──
const excelUploadRef = useTemplateRef('excelUploadRef')
const excelUploading = ref(false)

// ── 后期绑定的函数包装器（解决 composable 间循环依赖） ──
let doRenderMermaidBlocksFn = async (_scroll?: boolean): Promise<void> => { }
let resetCurrentSessionFn = (_showMsg?: boolean): void => { }

// ── 辅助函数 ──
/** 判断用户是否已滚动到底部附近（80px 容差） */
function isNearBottom(): boolean {
  const el = msgContainer.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 80
}

function scrollToBottom(force?: boolean): void {
  nextTick(() => {
    if (msgContainer.value && (force || isNearBottom())) {
      msgContainer.value.scrollTop = msgContainer.value.scrollHeight
    }
  })
}

function handleSessionExpired(): void {
  loginSucc.value = false
  canUseClassicView.value = false
  sessionStorage.removeItem('authentication')
  sessionStorage.removeItem('currentUser')
  sessionStorage.removeItem('isRemote')
  nextTick(() => { loginDialogVisible.value = true })
}

function parseSchemaValue(value: string): { connId: string; schema: string } | null {
  const idx = value.indexOf('::')
  if (idx === -1) return null
  return { connId: value.substring(0, idx), schema: value.substring(idx + 2) }
}

function getPrimaryConnId(): string {
  if (selectedSchemas.value.length > 0) {
    const parsed = parseSchemaValue(selectedSchemas.value[0])
    if (parsed) return parsed.connId
  }
  return ''
}

function buildRequestSchemas(): { connId: string; schema: string }[] {
  return selectedSchemas.value
    .map((v) => parseSchemaValue(v))
    .filter((p): p is { connId: string; schema: string } => !!p && p.schema !== '')
    .map((p) => ({ connId: p.connId, schema: p.schema }))
}

function getTableComment(value: string): string {
  const found = tableList.value.find((t) => (t.label || t.name) === value)
  return found?.comment || ''
}

// ── 计算属性 ──
const processedConnList = computed(() => buildSchemaTree(connSchemaList.value))

const shouldShowSchemaSelector = computed(() => {
  if (schemasLoading.value) return true
  if (connSchemaList.value.length > 1) return true
  if (connSchemaList.value.length === 1) {
    const conn = connSchemaList.value[0] as { schemas?: unknown[] }
    const schemaCount = (conn.schemas || []).length
    if (schemaCount > 1) return true
  }
  return false
})

// ── 1. 创建 useChatHistory（需要后期绑定的 doRenderMermaidBlocks 和 resetCurrentSession） ──
const chatHistory = useChatHistory({
  sessionId,
  selectedSchemas,
  selectedTables,
  tableList,
  loadTableListForSchemas,
  doRenderMermaidBlocks: (...args: [boolean?]) => doRenderMermaidBlocksFn(...args),
  scrollToBottom,
  handleSessionExpired,
  resetCurrentSession: (...args: [boolean?]) => resetCurrentSessionFn(...args),
})

// ── 2. 创建 useMarkdownRenderer（需要 chatHistory） ──
const mdRenderer = useMarkdownRenderer({
  chatHistory: chatHistory.chatHistory,
  msgContainer,
  currentTheme,
})

// 更新后期绑定
doRenderMermaidBlocksFn = mdRenderer.doRenderMermaidBlocks

// ── 3. 创建 useSqlConfirm（需要 chatHistory、共享状态） ──
const sqlConfirm = useSqlConfirm({
  chatHistory: chatHistory.chatHistory,
  loading,
  abortController,
  sessionId,
  streamingExecContent,
  buildRequestSchemas,
  getPrimaryConnId,
  scrollToBottom,
  handleSessionExpired,
})

// ── 4. 创建 useChatStream（需要以上所有） ──
const chatStream = useChatStream({
  chatHistory: chatHistory.chatHistory,
  showAllHistory: chatHistory.showAllHistory,
  sessionId,
  selectedSchemas,
  selectedTables,
  selectedModel,
  question,
  uploadedExcel,
  loading,
  abortController,
  streamingExecContent,
  buildRequestSchemas,
  getPrimaryConnId,
  scrollToBottom,
  handleSessionExpired,
  renderMarkdown: mdRenderer.renderMarkdown,
  doRenderMermaidBlocks: mdRenderer.doRenderMermaidBlocks,
  countClosedMermaidBlocks: mdRenderer.countClosedMermaidBlocks,
  showConfirmDialog: sqlConfirm.showConfirmDialog,
  setBatchPendingSQL: sqlConfirm.setBatchPendingSQL,
  resetDetectFlag: sqlConfirm.resetDetectFlag,
  pendingSQLList: sqlConfirm.pendingSQLList,
  showRetryConfirm: sqlConfirm.showRetryConfirm,
  retryMessage: sqlConfirm.retryMessage,
  lastQuestion: sqlConfirm.lastQuestion,
})

// ── 5. 定义 clearSession（协调所有 composable 的重置） ──
function clearSession(showMsg?: boolean): void {
  chatStream.stopGeneration()
  chatHistory.clearMessages()
  sessionId.value = ''
  chatStream.resetStreamState()
  sqlConfirm.resetConfirmState()
  uploadedExcel.value = null
  if (showMsg) {
    ElMessage({ message: '已新建会话', type: 'success' })
  }
}

// 更新后期绑定
resetCurrentSessionFn = clearSession

// ── 重试继续 ──
function handleRetryContinue(): void {
  sqlConfirm.showRetryConfirm.value = false
  if (sqlConfirm.lastQuestion.value) {
    question.value = sqlConfirm.lastQuestion.value
    nextTick(() => { void chatStream.sendMessage() })
  }
}

// ── Schema 树构建 ──
function buildSchemaTree(rawList: any[]): any[] {
  const dirMap = new Map<string, any[]>()
  const noDir: any[] = []
  for (const item of rawList) {
    const schemas = item.schemas || []
    const dbType = item.dbType || ''
    if (item.available === false) {
      const node = { label: item.name, value: item.connId + '::', connId: item.connId, schemaName: '', disabled: true, isSchemaLeaf: false, dbType }
      const dir = item.dirName
      if (dir) { if (!dirMap.has(dir)) dirMap.set(dir, []); dirMap.get(dir)!.push(node) } else { noDir.push(node) }
      continue
    }
    if (schemas.length <= 1) {
      const singleSchema = schemas.length === 1 ? schemas[0].name : (item.dbSchema || '')
      const node = { label: item.name, value: item.connId + '::' + singleSchema, connId: item.connId, schemaName: singleSchema, disabled: false, isSchemaLeaf: true, dbType }
      const dir = item.dirName
      if (dir) { if (!dirMap.has(dir)) dirMap.set(dir, []); dirMap.get(dir)!.push(node) } else { noDir.push(node) }
    } else {
      const schemaChildren = schemas.map((s: any) => ({ label: s.name, value: item.connId + '::' + s.name, connId: item.connId, schemaName: s.name, disabled: false, isSchemaLeaf: true, dbType }))
      const connNode = { label: item.name, value: '__conn__' + item.connId, disabled: true, children: schemaChildren, dbType }
      const dir = item.dirName
      if (dir) { if (!dirMap.has(dir)) dirMap.set(dir, []); dirMap.get(dir)!.push(connNode) } else { noDir.push(connNode) }
    }
  }
  const tree: any[] = []
  for (const [dirName, children] of dirMap) {
    tree.push({ label: dirName, value: '__dir__' + dirName, disabled: true, children })
  }
  tree.push(...noDir)
  return tree
}

// ── 连接列表加载 ──
async function loadConnList(): Promise<void> {
  schemasLoading.value = true
  const auth = sessionStorage.getItem('authentication') || ''
  const apiBase = import.meta.env.VITE_API_URL || ''
  try {
    const resp = await fetch(apiBase + '/listUserConnSchemasStream', { headers: { Authorization: auth } })
    if (!resp.ok) {
      if (resp.status === 401) { handleSessionExpired(); return }
      throw new Error('HTTP ' + resp.status)
    }
    const reader = resp.body!.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    const rawList: any[] = []
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
          const parsed = JSON.parse(data)
          if (parsed.connId) rawList.push(parsed)
        } catch { /* ignore */ }
      }
    }
    connSchemaList.value = rawList
    if (rawList.length > 0) {
      const firstConn = rawList[0]
      const schemas = firstConn.schemas || []
      if (schemas.length > 0) {
        selectedSchemas.value = [firstConn.connId + '::' + schemas[0].name]
      } else if (firstConn.dbSchema) {
        selectedSchemas.value = [firstConn.connId + '::' + firstConn.dbSchema]
      }
    }
  } catch (e) {
    console.error('[ChatView] 加载连接列表失败:', e)
  } finally {
    schemasLoading.value = false
  }
}

// ── 表列表加载 ──
async function loadTableListForSchemas(): Promise<void> {
  tablesLoading.value = true
  if (selectedSchemas.value.length === 0) {
    tableList.value = []
    tablesLoading.value = false
    return
  }
  try {
    const schemaRefs = selectedSchemas.value.map((v) => parseSchemaValue(v)).filter(Boolean) as { connId: string; schema: string }[]
    if (schemaRefs.length === 0) {
      tableList.value = []
      tablesLoading.value = false
      return
    }
    const resp = await listTableNamesBySchemas(schemaRefs)
    const tables = resp.data.data || []
    const allTables = tables.map((t: any) => {
      const hasSchema = t.schema && selectedSchemas.value.length > 1
      return { name: t.name, comment: t.comment || '', schema: t.schema || '', label: hasSchema ? t.schema + '.' + t.name : t.name }
    })
    tableList.value = allTables
    if (selectedTables.value.length > 0) {
      const newValues = allTables.map((t) => t.label || t.name)
      selectedTables.value = selectedTables.value.filter((name) => newValues.includes(name))
    }
  } catch (e) {
    tableList.value = []
    if ((e as any).response && (e as any).response.status === 401) handleSessionExpired()
  } finally {
    tablesLoading.value = false
  }
}

function handleSchemaChange(): void {
  void loadTableListForSchemas()
}

// ── AI 模型列表 ──
function loadModelList(): void {
  modelLoading.value = true
  getAIModels().then((resp) => {
    if (resp.data && resp.data.data) {
      const data = resp.data.data
      if (data.aiModelList && Array.isArray(data.aiModelList) && data.aiModelList.length > 0) {
        aiModelList.value = data.aiModelList
        selectedModel.value = data.selectedModelId || data.aiModelList[0].id
      } else {
        aiModelList.value = []
        selectedModel.value = ''
      }
    }
  }).catch(() => {
    aiModelList.value = []
    selectedModel.value = ''
  }).finally(() => {
    modelLoading.value = false
  })
}

// ── 提示词管理 ──
async function fetchPrompts(tab: string): Promise<void> {
  const params: any = { tab, page: tab === 'mine' ? myPromptCurrentPage.value : systemPromptCurrentPage.value, pageSize: promptPageSize.value }
  const keyword = promptSearchKeyword.value.trim()
  if (keyword) params.keyword = keyword
  const targetLoading = tab === 'mine' ? myPromptLoading : systemPromptLoading
  targetLoading.value = true
  try {
    const resp = await getPromptList(params)
    const data = resp.data.data || {}
    const items = (data.items || []).map((p: any) => ({ ...p, isShared: p.createdBy !== p.currentUserId && !p.isRolePrompt }))
    if (tab === 'mine') { myPrompts.value = items; myPromptsTotal.value = data.total || 0 }
    else { systemPrompts.value = items; systemPromptsTotal.value = data.total || 0 }
  } catch (e) {
    console.error(`加载${tab === 'mine' ? '我的' : '系统'}提示词失败:`, e)
    if (tab === 'mine') { myPrompts.value = []; myPromptsTotal.value = 0 }
    else { systemPrompts.value = []; systemPromptsTotal.value = 0 }
  } finally {
    targetLoading.value = false
  }
}

function loadPrompts(): Promise<void[]> {
  promptSearchKeyword.value = ''
  myPromptCurrentPage.value = 1
  systemPromptCurrentPage.value = 1
  return Promise.all([fetchPrompts('mine'), fetchPrompts('system')])
}

function handlePromptSearchInput(): void {
  myPromptCurrentPage.value = 1
  systemPromptCurrentPage.value = 1
  if (promptSearchDebounceTimer) clearTimeout(promptSearchDebounceTimer)
  promptSearchDebounceTimer = setTimeout(() => {
    promptSearchDebounceTimer = null
    void Promise.all([fetchPrompts('mine'), fetchPrompts('system')])
  }, 300)
}

function handleMyPromptPageChange(page: number): void { myPromptCurrentPage.value = page; void fetchPrompts('mine') }
function handleSystemPromptPageChange(page: number): void { systemPromptCurrentPage.value = page; void fetchPrompts('system') }

function applyPromptToInput(content: string, connInfo: any, send: boolean): void {
  question.value = content
  if (connInfo && connInfo.connSchemas && connInfo.connSchemas.length > 0) {
    selectedSchemas.value = connInfo.connSchemas.map((cs: any) => cs.connId + '::' + cs.schema)
  }
  if (connInfo && connInfo.tables && connInfo.tables.length > 0) {
    const tableNames = connInfo.tables.map((t: any) => typeof t === 'string' ? t : t.name)
    nextTick(() => {
      loadTableListForSchemas().then(() => {
        const availableNames = tableList.value.map((t) => t.label || t.name)
        selectedTables.value = tableNames.filter((t: string) => availableNames.includes(t))
        if (send) nextTick(() => { void chatStream.sendMessage() })
      })
    })
  } else {
    selectedTables.value = []
    if (send) nextTick(() => { void chatStream.sendMessage() })
  }
}

function handlePromptSendToAI(content: string, connInfo: any): void {
  promptPopoverVisible.value = false
  promptEditDialogVisible.value = false
  applyPromptToInput(content, connInfo, true)
}

function handlePromptFillInput(content: string, connInfo: any): void {
  promptPopoverVisible.value = false
  applyPromptToInput(content, connInfo, false)
}

function handlePromptAdd(): void { openPromptEditDialog() }
function handlePromptEdit(promptId: string): void { openPromptEditDialog({ promptId }) }
function handlePromptSaved(): void { void loadPrompts(); triggerPromptSaved() }

async function handleDeletePrompt(prompt: any): Promise<void> {
  try {
    await delPrompt(prompt.id)
    ElMessage.success('删除成功')
    void loadPrompts()
  } catch { ElMessage.error('删除失败') }
}

function handleViewPromptDetail(prompt: any): void { promptDetail.value = prompt; promptDetailVisible.value = true }
function handleSendFromDetail(): void {
  if (promptDetail.value) {
    handlePromptSendToAI(promptDetail.value.content, { connSchemas: promptDetail.value.connSchemas, tables: promptDetail.value.tables })
    promptDetailVisible.value = false
  }
}
function handleFillFromDetail(): void {
  if (promptDetail.value) {
    handlePromptFillInput(promptDetail.value.content, { connSchemas: promptDetail.value.connSchemas, tables: promptDetail.value.tables })
    promptDetailVisible.value = false
  }
}

// ── Excel 上传 ──
async function handleExcelUpload(file: any): Promise<void> {
  const rawFile = file.raw || file
  const formData = new FormData()
  formData.append('file', rawFile)
  const lowerName = rawFile.name.toLowerCase()
  const isMarkdown = lowerName.endsWith('.md') || lowerName.endsWith('.markdown')
  if (!isMarkdown && rawFile.size > 20 * 1024 * 1024) { ElMessage.error('文件大小不能超过 20MB'); return }
  excelUploading.value = true
  try {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch(apiBase + '/ai/agent/uploadExcel', { method: 'POST', headers: { Authorization: auth }, body: formData })
    if (resp.status === 401) {
      const errorData = await resp.json().catch(() => ({}))
      if (errorData.code === 401) { ElMessage({ message: errorData.msg || '登录已过期，请重新登录', type: 'warning' }); handleSessionExpired(); return }
    }
    if (!resp.ok) {
      const errData = await resp.json().catch(() => ({}))
      throw new Error(sanitizeError(errData.error) || `上传失败：${resp.status}`)
    }
    const data = await resp.json()
    const result = data.data || {}
    uploadedExcel.value = { fileId: result.fileId, name: result.fileName, fileType: result.fileType, columns: result.columns || [], rows: result.totalRows || 0, charCount: result.charCount || 0 }
    let previewText = `📎 已上传文件：**${result.fileName}**\n`
    if (result.fileType === 'markdown') {
      previewText += `Markdown 文档，共 ${result.charCount} 字符\n\n内容预览：\n\n${result.textPreview || ''}\n`
    } else {
      const typeLabel = result.fileType === 'csv' ? 'CSV' : 'Excel'
      previewText += `${typeLabel} 文件，共 ${result.totalRows} 行数据，${result.columns.length} 列\n\n列名：\`${result.columns.join('`, `')}\`\n\n`
      const previewRows = result.preview || []
      if (previewRows.length > 0) {
        previewText += `前 ${previewRows.length} 行原始数据预览：\n`
        previewText += '| ' + result.columns.join(' | ') + ' |\n'
        previewText += '| ' + result.columns.map(() => '---').join(' | ') + ' |\n'
        for (const row of previewRows) {
          const cells = result.columns.map((_: any, i: number) => {
            const val = row[i] !== undefined && row[i] !== null ? String(row[i]) : ''
            return val.length > 50 ? val.substring(0, 50) + '…' : (val || ' ')
          })
          previewText += '| ' + cells.join(' | ') + ' |\n'
        }
        if (result.totalRows > previewRows.length) previewText += `\n*共 ${result.totalRows} 行，以上仅展示前 ${previewRows.length} 行*\n`
      }
    }
    chatHistory.chatHistory.value.push({ role: 'assistant', content: previewText, hasSql: false })
    scrollToBottom()
    void mdRenderer.doRenderMermaidBlocks()
    if (result.fileType === 'markdown') {
      ElMessage.success(`已上传 ${result.fileName}，可让我分析/解读这份文档，或结合数据库表分析`)
    } else {
      ElMessage.success(`已上传 ${result.fileName}，可让我：分析这份数据 / 结合数据库表分析 / 导入到数据库`)
    }
  } catch (e) {
    console.error('[App] 上传 Excel 文件失败:', e)
    ElMessage.error('上传 Excel 文件失败，请检查文件格式')
  } finally {
    excelUploading.value = false
  }
}

function clearUploadedExcel(): void { uploadedExcel.value = null }

// ── 语音识别 ──
function initSpeechRecognition(): any {
  const SR = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition
  if (!SR) { ElMessage({ message: '浏览器不支持语音识别', type: 'warning' }); return null }
  const recognition = new SR()
  recognition.lang = 'zh-CN'
  recognition.continuous = true
  recognition.interimResults = true
  recognition.onstart = () => { isRecording.value = true }
  recognition.onresult = (event: any) => {
    let finalTranscript = ''
    for (let i = event.resultIndex; i < event.results.length; i++) {
      if (event.results[i].isFinal) finalTranscript += event.results[i][0].transcript
    }
    if (finalTranscript) question.value += (question.value ? ' ' : '') + finalTranscript
  }
  recognition.onerror = (event: any) => {
    if (event.error === 'not-allowed') ElMessage({ message: '请允许使用麦克风', type: 'error' })
    isRecording.value = false
  }
  recognition.onend = () => { isRecording.value = false }
  return recognition
}

function toggleRecording(): void {
  if (isRecording.value) {
    (speechRecognition as any)?.stop()
    isRecording.value = false
  } else {
    if (!speechRecognition) speechRecognition = initSpeechRecognition()
    if (!speechRecognition) return
    try { (speechRecognition as any).start(); ElMessage({ message: '开始语音录入...', type: 'info' }) }
    catch { ElMessage({ message: '无法启动语音识别', type: 'error' }) }
  }
}

// ── 登录/认证 ──
function logout(): void {
  logoutApi().then((resp) => {
    currentUser.value = {}
    loginSucc.value = false
    canUseClassicView.value = false
    ElMessage(resp.data.data)
    sessionStorage.removeItem('authentication')
    sessionStorage.removeItem('currentUser')
    sessionStorage.removeItem('isRemote')
  })
}

const loginDialogRef = useTemplateRef('loginDialogRef')

function showLoginDialog(): void { loginDialogVisible.value = true }

function handleSessionExpiredEvent(event: Event): void {
  if (window.location.pathname === '/classical') return
  const message = (event as CustomEvent).detail?.message || ''
  if (message) ElMessage({ message, type: 'warning' })
  handleSessionExpired()
}

function handleLoginSuccess(userData: any): void {
  currentUser.value = userData
  loginSucc.value = true
  void loadConnList().then(() => {
    if (selectedSchemas.value.length > 0) void loadTableListForSchemas()
  })
  loadModelList()
  checkClassicViewPermission()
  void loadPrompts()
}

function checkClassicViewPermission(): void {
  canUseClassicViewApi().then((resp) => {
    canUseClassicView.value = !!(resp.data.data && resp.data.data.allowed)
  }).catch(() => { canUseClassicView.value = false })
  getSystemConfig().then((resp) => {
    if (resp.data && resp.data.data && resp.data.data.defaultHomepage) {
      storage.setItem('defaultHomepage', resp.data.data.defaultHomepage)
    }
  }).catch(() => { /* ignore */ })
}

function openSystemManagement(): void {
  sessionStorage.setItem('systemManagement_user', JSON.stringify(currentUser.value))
  router.push('/system-management')
}

function getSysModel(): void {
  getSysMode().then((resp) => {
    const data = resp.data as any
    isRemote.value = data?.isRemote ?? data?.data?.isRemote ?? false
    sessionStorage.setItem('isRemote', isRemote.value.toString())
    if (!loginSucc.value && isRemote.value) loginDialogVisible.value = true
  })
}

// ── Mermaid 事件绑定到 msgContainer ──
let _prevMsgEl: HTMLElement | null = null
function attachMsgContainerEvents(el: HTMLElement | null): void {
  if (!el || el === _prevMsgEl) return
  detachMsgContainerEvents(_prevMsgEl)
  _prevMsgEl = el
  el.addEventListener('wheel', mdRenderer.handleMermaidWheel, { passive: false })
  el.addEventListener('mousedown', mdRenderer.handleMermaidMouseDown)
  el.addEventListener('mousedown', mdRenderer.handleMermaidResizeDown)
}
function detachMsgContainerEvents(el: HTMLElement | null): void {
  if (!el) return
  el.removeEventListener('wheel', mdRenderer.handleMermaidWheel)
  el.removeEventListener('mousedown', mdRenderer.handleMermaidMouseDown)
  el.removeEventListener('mousedown', mdRenderer.handleMermaidResizeDown)
}

watch(msgContainer, (newEl, oldEl) => {
  if (oldEl && oldEl !== newEl) detachMsgContainerEvents(oldEl)
  if (newEl) attachMsgContainerEvents(newEl)
}, { flush: 'post' })

// ── selectedTables 持久化 ──
watch(selectedTables, (val) => {
  try { sessionStorage.setItem('lastSelectedTables', JSON.stringify(val)) } catch { /* ignore */ }
}, { deep: true })

// ── 生命周期 ──
onMounted(() => {
  const { initTheme } = useTheme()
  initTheme()
  void mdRenderer.initHeavyDeps()
  mdRenderer.setupMermaidObserver()
  setSendToAIHandler(handlePromptSendToAI)
  getSysModel()
  loadModelList()
  void loadConnList().then(() => {
    if (loginSucc.value || !isRemote.value) {
      void loadTableListForSchemas().then(() => {
        const savedTables = sessionStorage.getItem('lastSelectedTables')
        if (savedTables) {
          try {
            const parsedTables = JSON.parse(savedTables)
            if (Array.isArray(parsedTables) && parsedTables.length > 0) {
              selectedTables.value = parsedTables.filter((t: string) => tableList.value.some((tl) => tl.label === t || tl.name === t))
            }
          } catch { /* ignore */ }
        }
      })
    }
  })
  if (loginSucc.value || !isRemote.value) checkClassicViewPermission()
  document.addEventListener('keydown', mdRenderer.handleEscKey)
  document.addEventListener('keydown', mdRenderer.handleMermaidKeyDown)
  document.addEventListener('keyup', mdRenderer.handleMermaidKeyUp)
  document.addEventListener('mousemove', mdRenderer.handleMermaidMouseMove)
  document.addEventListener('mouseup', mdRenderer.handleMermaidMouseUp)
  document.addEventListener('mousemove', mdRenderer.handleMermaidResizeMove)
  document.addEventListener('mouseup', mdRenderer.handleMermaidResizeUp)
  document.addEventListener('click', mdRenderer.handleMermaidToolbarClick)
  window.addEventListener('session-expired', handleSessionExpiredEvent)
  document.addEventListener('click', mdRenderer.handleExportLinkClick)
  document.addEventListener('click', mdRenderer.handleCodeCopyClick)
  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization
  nextTick(() => { if (msgContainer.value) attachMsgContainerEvents(msgContainer.value) })
  if (window.requestIdleCallback) {
    window.requestIdleCallback(() => preloadVditor(), { timeout: 5000 })
  } else {
    setTimeout(() => preloadVditor(), 3000)
  }
})

onUnmounted(() => {
  document.removeEventListener('keydown', mdRenderer.handleEscKey)
  document.removeEventListener('keydown', mdRenderer.handleMermaidKeyDown)
  document.removeEventListener('keyup', mdRenderer.handleMermaidKeyUp)
  document.removeEventListener('mousemove', mdRenderer.handleMermaidMouseMove)
  document.removeEventListener('mouseup', mdRenderer.handleMermaidMouseUp)
  document.removeEventListener('mousemove', mdRenderer.handleMermaidResizeMove)
  document.removeEventListener('mouseup', mdRenderer.handleMermaidResizeUp)
  document.removeEventListener('click', mdRenderer.handleMermaidToolbarClick)
  window.removeEventListener('session-expired', handleSessionExpiredEvent)
  document.removeEventListener('click', mdRenderer.handleExportLinkClick)
  document.removeEventListener('click', mdRenderer.handleCodeCopyClick)
  detachMsgContainerEvents(_prevMsgEl)
  _prevMsgEl = null
  mdRenderer.teardownMermaidObserver()
  document.body.classList.remove('mermaid-ctrl-held', 'mermaid-dragging')
})
</script>


<style scoped>
/* ========== 外层容器 - 填满整个视口 ========== */
.ai-sql-panel-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 内容容器 - 添加左右留白 */
.container {
  width: 70%;
  margin: 0 auto;
  height: 100%;
  display: flex;
  flex-direction: column;
}

/* 登录按钮容器 - 固定在左下角 */
.login-button-container {
  position: fixed;
  left: 20px;
  bottom: 20px;
  z-index: 1000;
  opacity: 0.6;
  transition: opacity 0.3s ease;
}

.login-button-container:hover {
  opacity: 1;
}

/* 覆盖 Element Plus 的默认按钮间距，确保垂直布局不受影响 */
.login-button-container .el-button + .el-button {
  margin-left: 0 !important;
}

/* ========== 主容器 - 专业蓝灰渐变 ========== */
.container {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 0;
  padding: 0;
  border-radius: 0;
  background: linear-gradient(135deg, #f8f9fa 0%, #e9ecef 100%);
}

/* ========== 输入区域美化 ========== */
/* ChatInputArea 子组件样式已迁移至 ChatInputArea.vue */

/* 按钮组美化 */
.el-button-group {
  display: flex;
  gap: 6px;
}

/* 工具栏按钮美化 */
.toolbar-btn {
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  border-radius: 6px;
  font-weight: 500;
}

.toolbar-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 122, 204, 0.2);
}

/* 输入框美化 */
:deep(.el-textarea__inner) {
  border-radius: 10px;
  border: 1px solid var(--border-primary);
  transition: all 0.3s ease;
  font-size: 14px;
}

:deep(.el-textarea__inner:hover) {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.08);
}

:deep(.el-textarea__inner:focus) {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.12);
}

/* 选择器美化 */
:deep(.el-select) {
  width: 100%;
}

:deep(.el-select .el-input__inner) {
  border-radius: 10px;
}

:deep(.el-select-dropdown__item.selected) {
  background: linear-gradient(90deg, rgba(0, 122, 204, 0.1) 0%, transparent 100%);
  color: var(--accent-color);
  font-weight: 600;
}

/* 空状态美化 */
:deep(.el-empty) {
  padding: 20px 0;
}

:deep(.el-empty__description) {
  color: var(--text-tertiary);
  font-size: 13px;
}

/* 骨架屏美化 */
:deep(.el-skeleton) {
  border-radius: 8px;
}

:deep(.el-skeleton__item) {
  background: linear-gradient(90deg, var(--bg-hover) 25%, var(--bg-active) 50%, var(--bg-hover) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s infinite;
  border-radius: 6px;
}

@keyframes shimmer {
  0% {
    background-position: 200% 0;
  }

  100% {
    background-position: -200% 0;
  }
}

/* 滚动条全局美化 */
:deep(.el-drawer__body) {
  padding: 3px;
  scrollbar-width: thin;
  scrollbar-color: var(--text-tertiary) rgba(0, 0, 0, 0.05);
}

:deep(.el-drawer__header) {
  margin-bottom: 3px;
}

/* Popover 美化 */
:deep(.el-popover) {
  border-radius: 12px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.15);
  border: 1px solid var(--border-primary);
}



/* 多条 SQL 批量确认 */
.multi-sql-confirm {
  border: 2px solid var(--warning-color);
  border-radius: 8px;
  padding: 16px;
  background: var(--bg-row-changed);
  margin: 8px 16px;
  flex-shrink: 0;
}
.sql-confirm-item {
  margin: 8px 0;
  padding: 8px;
  background: var(--bg-primary);
  border-radius: 6px;
  border: 1px solid var(--border-primary);
}
.sql-confirm-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.sql-preview-code {
  margin: 0;
  padding: 8px;
  background: var(--bg-secondary);
  border-radius: 4px;
  font-family: 'Consolas', monospace;
  font-size: 12px;
  max-height: 100px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 重试确认 */
.retry-confirm-block {
  border: 1px solid var(--warning-color);
  border-radius: 8px;
  padding: 16px;
  background: var(--bg-row-changed);
  margin: 8px 16px;
  flex-shrink: 0;
}

/* Excel 上传 */
.uploaded-file-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background: var(--bg-active);
  border-radius: 6px;
  font-size: 13px;
  color: var(--accent-color);
}

/* 提示词弹窗 */
.prompt-popover-body {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.prompt-popover-body .prompt-tabs {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.prompt-popover-body .el-tabs__content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.prompt-popover-body .el-tab-pane {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.prompt-popover-body .prompt-toolbar {
  flex-shrink: 0;
}

.prompt-popover-body .prompt-search-box {
  flex-shrink: 0;
  padding: 4px 8px 8px;
}

.prompt-popover-body .prompt-pagination {
  flex-shrink: 0;
  display: flex;
  justify-content: center;
  padding-top: 4px;
  border-top: 1px solid var(--border-primary);
}

.prompt-popover-body .prompt-pagination :deep(.el-pager li),
.prompt-popover-body .prompt-pagination :deep(.btn-prev),
.prompt-popover-body .prompt-pagination :deep(.btn-next) {
  background: transparent !important;
}

.prompt-popover-body .prompt-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
}

.prompt-popover-body .prompt-empty {
  text-align: center;
  color: var(--text-tertiary);
  padding: 40px 0;
  font-size: 14px;
}

.prompt-popover-body .prompt-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 5px;
  border-radius: 5px;
  cursor: pointer;
  transition: all 0.2s;
  margin-bottom: 2px;
}

.prompt-popover-body .prompt-item:hover {
  background: var(--bg-hover);
}

.prompt-popover-body .prompt-item-info {
  flex: 1;
  min-width: 0;
}

.prompt-popover-body .prompt-item-title {
  font-size: 14px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}

.prompt-popover-body .prompt-item-sub {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text-tertiary);
  margin-top: 2px;
}

.prompt-popover-body .prompt-item-actions {
  display: flex;
  gap: 0;
  opacity: 0;
  transition: opacity 0.2s;
  flex-shrink: 0;
}

.prompt-popover-body .prompt-item-actions .el-button {
  padding: 4px;
  margin-left: 0 !important;
}

.prompt-popover-body .prompt-item:hover .prompt-item-actions {
  opacity: 1;
}

.prompt-detail-meta {
  margin-bottom: 8px;
}

.prompt-detail-meta-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.prompt-detail-meta-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 120px;
  overflow-y: auto;
}

.prompt-detail-content {
  max-height: 50vh;
  overflow-y: auto;
}
</style>
