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
          :can-use-classic-view="canUseClassicView"
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
function scrollToBottom(): void {
  nextTick(() => {
    if (msgContainer.value) {
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
    uploadedExcel.value = { fileId: data.fileId, name: data.fileName, fileType: data.fileType, columns: data.columns || [], rows: data.totalRows || 0, charCount: data.charCount || 0 }
    let previewText = `📎 已上传文件：**${data.fileName}**\n`
    if (data.fileType === 'markdown') {
      previewText += `Markdown 文档，共 ${data.charCount} 字符\n\n内容预览：\n\n${data.textPreview || ''}\n`
    } else {
      const typeLabel = data.fileType === 'csv' ? 'CSV' : 'Excel'
      previewText += `${typeLabel} 文件，共 ${data.totalRows} 行数据，${data.columns.length} 列\n\n列名：\`${data.columns.join('`, `')}\`\n\n`
      const previewRows = data.preview || []
      if (previewRows.length > 0) {
        previewText += `前 ${previewRows.length} 行原始数据预览：\n`
        previewText += '| ' + data.columns.join(' | ') + ' |\n'
        previewText += '| ' + data.columns.map(() => '---').join(' | ') + ' |\n'
        for (const row of previewRows) {
          const cells = data.columns.map((_: any, i: number) => {
            const val = row[i] !== undefined && row[i] !== null ? String(row[i]) : ''
            return val.length > 50 ? val.substring(0, 50) + '…' : (val || ' ')
          })
          previewText += '| ' + cells.join(' | ') + ' |\n'
        }
        if (data.totalRows > previewRows.length) previewText += `\n*共 ${data.totalRows} 行，以上仅展示前 ${previewRows.length} 行*\n`
      }
    }
    chatHistory.chatHistory.value.push({ role: 'assistant', content: previewText, hasSql: false })
    scrollToBottom()
    void mdRenderer.doRenderMermaidBlocks()
    if (data.fileType === 'markdown') {
      ElMessage.success(`已上传 ${data.fileName}，可让我分析/解读这份文档，或结合数据库表分析`)
    } else {
      ElMessage.success(`已上传 ${data.fileName}，可让我：分析这份数据 / 结合数据库表分析 / 导入到数据库`)
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

/* ========== 聊天消息容器 ========== */
.chat-messages {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 8px 5px;
  min-height: 0;
  background: rgba(255, 255, 255, 0.9);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.06);
  overflow-x: hidden;
}

/* 自定义滚动条 - 蓝灰色 */
.chat-messages::-webkit-scrollbar {
  width: 6px;
}

.chat-messages::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.03);
  border-radius: 3px;
}

.chat-messages::-webkit-scrollbar-thumb {
  background: linear-gradient(180deg, #546e7a 0%, #37474f 100%);
  border-radius: 3px;
  transition: background 0.3s ease;
}

.chat-messages::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(180deg, #607d8b 0%, #455a64 100%);
}

/* ========== 聊天气泡 ========== */
.chat-bubble {
  border-radius: 16px;
  padding: 12px 16px;
  font-size: 14px;
  line-height: 1.6;
  position: relative;
  animation: slideIn 0.3s ease-out;
  backdrop-filter: blur(10px);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  transition: all 0.3s ease;
}

.chat-bubble:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
}

@keyframes slideIn {
  from {
    opacity: 0;
    transform: translateY(10px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 用户消息气泡 - 浅蓝渐变 */
.chat-bubble.user {
  align-self: flex-end;
  background: linear-gradient(135deg, #64b5f6 0%, #f0f0f0 100%);
  color: #fff;
  border-bottom-right-radius: 4px;
  box-shadow: 0 4px 12px rgba(100, 181, 246, 0.25);
}

.chat-bubble.user .bubble-label {
  color: rgba(255, 255, 255, 0.95);
}

/* AI 消息气泡 - 冷白色 */
.chat-bubble.assistant {
  align-self: flex-start;
  background: linear-gradient(135deg, #ffffff 0%, #f5f5f5 100%);
  color: #212121;
  border-bottom-left-radius: 4px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.chat-bubble.assistant .bubble-label {
  color: #546e7a;
}

/* ========== 标签样式 ========== */
.bubble-label {
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 4px;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}

.bubble-content {
  word-break: break-word;
}

/* ========== AI 消息操作栏（复制/重试） ========== */
.msg-action-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-top: 6px;
  padding-top: 4px;
  border-top: 1px solid rgba(0, 0, 0, 0.05);
  opacity: 0;
  transition: opacity 0.2s ease;
  height: 0;
  overflow: hidden;
}
.chat-bubble.assistant:hover .msg-action-bar {
  opacity: 1;
  height: auto;
}
.msg-action-btn {
  font-size: 12px !important;
  color: #909399;
  padding: 2px 6px !important;
  height: 24px;
}
.msg-action-btn:hover {
  color: #409eff;
}
.msg-action-btn.is-disabled {
  opacity: 0.4;
}
.msg-action-text {
  margin-left: 2px;
}

/* ========== 思考过程块 - 冷色调 ========== */
.thinking-block {
  border: 1px solid rgba(84, 110, 122, 0.2);
  border-radius: 12px;
  background: linear-gradient(135deg, rgba(236, 239, 241, 0.6) 0%, rgba(224, 228, 230, 0.4) 100%);
  padding: 12px;
  margin: 8px 0;
  backdrop-filter: blur(10px);
  box-shadow: 0 2px 8px rgba(84, 110, 122, 0.1);
  transition: all 0.3s ease;
}

.thinking-block:hover {
  box-shadow: 0 4px 12px rgba(84, 110, 122, 0.15);
  transform: translateX(4px);
}

.thinking-label {
  font-size: 13px;
  color: #37474f;
  margin-bottom: 8px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  transition: color 0.3s ease;
}

.thinking-label:hover {
  color: #546e7a;
}

.thinking-content {
  font-size: 13px;
  color: #455a64;
  word-break: break-word;
  max-height: 400px;
  overflow-y: auto;
  margin: 0;
  padding: 8px 12px;
  background: rgba(255, 255, 255, 0.7);
  border-radius: 8px;
  line-height: 1.6;
}

.thinking-content :deep(p) {
  margin-top: 0;
  margin-bottom: 8px;
}

.thinking-content :deep(p:last-child) {
  margin-bottom: 0;
}

.thinking-content :deep(code) {
  padding: 2px 6px;
  background: rgba(0, 0, 0, 0.06);
  border-radius: 4px;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  color: #c62828;
}

.thinking-content :deep(pre) {
  margin: 8px 0;
  padding: 12px;
  background: rgba(0, 0, 0, 0.04);
  border-radius: 6px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.5;
}

.thinking-content :deep(pre code) {
  padding: 0;
  background: transparent;
  color: inherit;
}

.thinking-content::-webkit-scrollbar {
  width: 4px;
}

.thinking-content::-webkit-scrollbar-thumb {
  background: #78909c;
  border-radius: 2px;
}

/* ========== 工具调用块 - 青绿色 ========== */
.tool-call-block {
  font-size: 13px;
  color: #00796b;
  padding: 10px 14px;
  background: linear-gradient(135deg, rgba(224, 242, 241, 0.9) 0%, rgba(178, 223, 219, 0.7) 100%);
  border-radius: 10px;
  border: 1px solid rgba(0, 121, 107, 0.2);
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  box-shadow: 0 2px 8px rgba(0, 121, 107, 0.1);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {

  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.85;
  }
}

/* ========== SQL 代码块 - VSCode 风格 ========== */
.sql-pre {
  margin: 0;
  padding: 12px;
  overflow: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  color: #d4d4d4;
  border-radius: 8px;
  border: 1px solid #3c3c3c;
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.5);
}

.sql-pre::-webkit-scrollbar {
  height: 6px;
}

.sql-pre::-webkit-scrollbar-thumb {
  background: #505050;
  border-radius: 3px;
}

.cursor-blink {
  animation: blink 1s step-start infinite;
  font-size: 14px;
  color: #569cd6;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

/* ========== Markdown 样式（基础容器，子元素样式在 unscoped 块中） ========== */
.markdown-body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif;
  font-size: 14px;
  line-height: 1.7;
  color: #1f2937;
  word-wrap: break-word;
  overflow-wrap: break-word;
}

/* ========== 历史会话项样式 ========== */
.session-history-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 340px;
}

.session-search-box {
  flex-shrink: 0;
}

.session-list-scroll {
  max-height: 400px;
  overflow-y: auto;
  min-height: 60px;
}

.session-pagination {
  display: flex;
  justify-content: center;
  padding-top: 4px;
  border-top: 1px solid var(--border-primary);
}

.session-pagination :deep(.el-pager li),
.session-pagination :deep(.btn-prev),
.session-pagination :deep(.btn-next) {
  background: transparent !important;
}

.session-item {
  display: flex;
  align-items: center;
  padding: 10px 16px;
  border: 1px solid var(--border-primary);
  border-radius: 10px;
  background: var(--bg-primary);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
  position: relative;
  overflow: hidden;
}

.session-item::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  width: 3px;
  background: linear-gradient(180deg, var(--accent-color) 0%, var(--text-secondary) 100%);
  opacity: 0;
  transition: opacity 0.3s ease;
}

.session-item:hover {
  border-color: var(--border-secondary);
  background: var(--bg-hover);
}

.session-item:hover::before {
  opacity: 0.8;
}

.session-content {
  flex: 1;
  cursor: pointer;
  min-width: 0;
  padding-right: 8px;
}

.session-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 0.3s ease;
}

.session-item:hover .session-title {
  color: var(--accent-color);
}

.session-time {
  font-size: 12px;
  color: var(--text-tertiary);
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 500;
}

.session-time .el-icon {
  font-size: 12px;
}

.session-actions {
  margin-left: 8px;
  display: flex;
  align-items: center;
  gap: 4px;
  opacity: 1;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  transform: translateX(0);
}

/* ========== 输入区域美化 ========== */
.input-area {
  flex-shrink: 0;
  border-top: 1px solid var(--border-primary);
  padding: 12px 16px;
  background: var(--bg-secondary);
  backdrop-filter: blur(10px);
  box-shadow: 0 -2px 8px rgba(0, 0, 0, 0.05);
  box-sizing: border-box;
}

.input-label {
  margin-bottom: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.input-label span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.input-label span::before {
  content: '💡';
  font-size: 14px;
}

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

/* ========== 输入区域布局 ========== */
.input-action-row {
  display: flex;
  gap: 12px;
  margin-top: 12px;
  flex-shrink: 0;
}



.question-input {
  flex: 1;
}

.question-input :deep(.el-textarea__inner) {
  border-radius: 12px;
  border: 1.5px solid var(--border-primary);
  transition: all 0.3s ease;
  font-size: 14px;
  line-height: 1.6;
  background: var(--bg-primary);
  backdrop-filter: blur(10px);
}

.question-input :deep(.el-textarea__inner:hover) {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.08);
}

.question-input :deep(.el-textarea__inner:focus) {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.12);
}

.action-buttons {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 50px;
}

/* 发送按钮 - 使用默认 primary 颜色 */
.send-btn {
  padding: 8px 20px;
  min-width: 38px;
  min-height: 30px;
  border-radius: 8px;
}

.send-btn .el-icon {
  margin-right: 0;
}

/* 停止按钮 */
.stop-btn {
  padding: 8px 20px;
  min-width: 38px;
  min-height: 30px;
  border-radius: 8px;
  animation: stopPulse 1.5s ease-in-out infinite;
}

.stop-btn .el-icon {
  margin-right: 0;
}

@keyframes stopPulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}

/* 切换视图链接 - 无下划线超链接样式 */
.switch-view-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #409eff;
  text-decoration: none;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: 4px;
  transition: all 0.3s ease;
}

.switch-view-link:hover {
  color: #66b1ff;
  background-color: rgba(64, 158, 255, 0.05);
}

.switch-view-link .el-icon {
  font-size: 16px;
}

.switch-view-link span {
  font-weight: 500;
}

/* ========== 数据库 / 表选择器 - 现代化设计 ========== */
.table-selector-row {
  display: flex;
  gap: 16px;
  margin-bottom: 12px;
  flex-shrink: 0;
  align-items: flex-start;
}

.table-selector-row .table-selector-container:first-child {
  flex: 0 0 calc(20% - 8px);
}

.table-selector-row .table-selector-container:nth-child(2) {
  flex: 0 0 calc(60% - 8px);
}

.table-selector-row .model-selector-container {
  flex: 0 0 calc(20% - 8px);
}

.table-selector-row .table-selector-container.full-width {
  flex: 1;
}

.table-selector-container {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.selector-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.selector-header .table-selector-label {
  margin-bottom: 0;
}

.table-selector-label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  font-weight: 600;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}

.selector-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 10px;
  font-size: 11px;
  font-weight: 600;
  border-radius: 100px;
  background: linear-gradient(135deg, rgba(0, 122, 204, 0.15) 0%, rgba(0, 122, 204, 0.08) 100%);
  color: var(--accent-color);
  letter-spacing: 0.3px;
  transition: all 0.3s ease;
  white-space: nowrap;
}

.selector-badge.ready {
  background: linear-gradient(135deg, rgba(78, 201, 176, 0.15) 0%, rgba(78, 201, 176, 0.08) 100%);
  color: var(--success-color);
}

@keyframes badgePulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.selector-skeleton {
  width: 100%;
  height: 32px;
  border-radius: 10px;
  border: 1.5px solid var(--border-primary);
  overflow: hidden;
  position: relative;
  box-sizing: border-box;
}

.selector-skeleton::after {
  content: '加载中...';
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 14px;
  color: var(--text-placeholder);
  pointer-events: none;
  letter-spacing: 0.3px;
}

.skeleton-shimmer {
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, var(--bg-hover) 0%, var(--bg-active) 35%, var(--bg-hover) 65%);
  background-size: 200% 100%;
  animation: skeletonSlide 1.5s ease-in-out infinite;
  border-radius: 8px;
}

@keyframes skeletonSlide {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

.modern-tree-select {
  width: 100%;
  margin: 0;
}

.modern-select {
  width: 100%;
  margin: 0;
}

.table-option-content {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0;
  width: 100%;
}

.table-option-name {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 13px;
  flex-shrink: 0;
}

.table-option-comment {
  font-size: 12px;
  color: var(--text-tertiary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.table-option-comment::before {
  content: '— ';
  color: var(--text-placeholder);
}

.table-option-schema {
  font-size: 11px;
  color: var(--accent-color);
  background: rgba(0, 122, 204, 0.1);
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 500;
  flex-shrink: 0;
}

.model-option-content {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
}

.model-option-name {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 13px;
  flex: 1;
}

.model-option-provider {
  font-size: 11px;
  color: var(--text-tertiary);
  background: var(--bg-active);
  padding: 1px 6px;
  border-radius: 4px;
  flex-shrink: 0;
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

<style>
/* ========== Markdown v-html 内容样式（unscoped，确保 v-html 注入的 DOM 元素能被正确样式化） ========== */
.markdown-body p {
  margin-top: 0;
  margin-bottom: 12px;
}
.markdown-body p:last-child {
  margin-bottom: 0;
}
.markdown-body h1,
.markdown-body h2,
.markdown-body h3,
.markdown-body h4,
.markdown-body h5,
.markdown-body h6 {
  margin-top: 20px;
  margin-bottom: 10px;
  font-weight: 700;
  line-height: 1.3;
  color: #1a202c;
}
.markdown-body h1 { font-size: 24px; border-bottom: 2px solid #e2e8f0; padding-bottom: 6px; }
.markdown-body h2 { font-size: 20px; border-bottom: 1px solid #e2e8f0; padding-bottom: 4px; }
.markdown-body h3 { font-size: 18px; }
.markdown-body h4 { font-size: 16px; }
.markdown-body h5 { font-size: 14px; }
.markdown-body h6 { font-size: 13px; }
.markdown-body ul,
.markdown-body ol {
  padding-left: 2em;
  margin-top: 8px;
  margin-bottom: 12px;
}
.markdown-body ul { list-style-type: disc; }
.markdown-body ul ul { list-style-type: circle; }
.markdown-body ul ul ul { list-style-type: square; }
.markdown-body ol { list-style-type: decimal; }
.markdown-body li {
  margin-top: 6px;
  margin-bottom: 6px;
  line-height: 1.6;
}
.markdown-body li+li { margin-top: 6px; }
.markdown-body code {
  padding: 3px 8px;
  margin: 0;
  font-size: 13px;
  background: linear-gradient(135deg, #eceff1 0%, #cfd8dc 100%);
  border-radius: 6px;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  color: #c62828;
  border: 1px solid rgba(0, 0, 0, 0.08);
}
.markdown-body eq {
  display: inline;
}
.markdown-body eqn {
  display: block;
  text-align: center;
  margin: 1em 0;
}
.markdown-body eq code,
.markdown-body eqn code {
  padding: 0;
  margin: 0;
  font-size: inherit;
  background: none;
  border-radius: 0;
  font-family: inherit;
  color: inherit;
  border: none;
}
.markdown-body pre {
  overflow: auto;
  font-size: 13px;
  line-height: 1.6;
  border-radius: 10px;
  margin-top: 12px;
  margin-bottom: 12px;
  max-width: 100%;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  border: 1px solid rgba(0, 0, 0, 0.2);
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  padding: 14px 16px;
}
.markdown-body pre code {
  display: block;
  padding: 0;
  margin: 0;
  overflow: visible;
  line-height: inherit;
  word-wrap: normal;
  background: none;
  border-radius: 0;
  white-space: pre;
  border: none;
  font-size: inherit;
}
.markdown-body pre code:not(.hljs) {
  color: #d4d4d4;
}
.markdown-body pre code.hljs {
  background: transparent;
}
.markdown-body blockquote {
  padding: 12px 16px;
  color: #546e7a;
  border-left: 4px solid #546e7a;
  margin: 12px 0;
  background: rgba(84, 110, 122, 0.05);
  border-radius: 0 8px 8px 0;
  font-style: italic;
}
.markdown-body table {
  border-collapse: collapse;
  width: 100%;
  max-width: 100%;
  margin-top: 12px;
  margin-bottom: 12px;
  font-size: 13px;
  display: block;
  overflow-x: auto;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}
.markdown-body table th,
.markdown-body table td {
  padding: 10px 14px;
  border: 1px solid #e2e8f0;
  white-space: nowrap;
}
.markdown-body table th {
  font-weight: 700;
  background: linear-gradient(180deg, #eceff1 0%, #cfd8dc 100%);
  color: #263238;
  position: sticky;
  top: 0;
  text-transform: uppercase;
  font-size: 12px;
  letter-spacing: 0.5px;
}
.markdown-body table tr:nth-child(2n) { background-color: #f5f5f5; }
.markdown-body table tr:hover { background-color: #eceff1; }
.markdown-body .table-wrapper {
  overflow-x: auto;
  margin: 12px 0;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}
.markdown-body a {
  color: #1976d2;
  text-decoration: none;
  cursor: pointer;
  font-weight: 500;
  transition: all 0.2s ease;
  border-bottom: 1px solid transparent;
}
.markdown-body a:hover {
  color: #1565c0;
  text-decoration: underline;
  border-bottom-color: #1565c0;
}
.markdown-body a[target="_blank"]::after {
  content: " ↗";
  font-size: 11px;
  margin-left: 2px;
}
.markdown-body hr {
  height: 2px;
  padding: 0;
  margin: 20px 0;
  background: linear-gradient(90deg, #e2e8f0 0%, #cbd5e0 50%, #e2e8f0 100%);
  border: 0;
}
.markdown-body strong {
  font-weight: 700;
  color: #1a202c;
}
.markdown-body em {
  font-style: italic;
  color: #4a5568;
}
.markdown-body img {
  max-width: 100%;
  height: auto;
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  margin: 8px 0;
}

/* 思考区域内的 markdown 样式覆盖（更紧凑） */
.thinking-content.markdown-body { font-size: 13px; color: #455a64; }
.thinking-content.markdown-body h1 { font-size: 18px; }
.thinking-content.markdown-body h2 { font-size: 16px; }
.thinking-content.markdown-body h3 { font-size: 15px; }
.thinking-content.markdown-body p { margin-bottom: 8px; }
.thinking-content.markdown-body pre {
  margin: 8px 0;
  padding: 12px;
  font-size: 12px;
}
.thinking-content.markdown-body code { font-size: 12px; }
.thinking-content.markdown-body ul,
.thinking-content.markdown-body ol { padding-left: 1.5em; margin: 6px 0; }
.thinking-content.markdown-body li { margin: 4px 0; }
.thinking-content.markdown-body blockquote {
  padding: 8px 12px;
  margin: 8px 0;
  border-left-width: 3px;
}
.thinking-content.markdown-body table { font-size: 12px; margin: 8px 0; }
.thinking-content.markdown-body table th,
.thinking-content.markdown-body table td { padding: 6px 10px; }
.thinking-content.markdown-body strong { color: #37474f; }

/* 用户消息气泡中的链接 */
.chat-bubble.user a {
  color: #ffffff;
  text-decoration-color: rgba(255, 255, 255, 0.6);
}
.chat-bubble.user a:hover {
  color: #e2e8f0;
  text-decoration-color: #ffffff;
}

/* Mermaid 容器（unscoped） */
.mermaid-container {
  margin: 12px 0;
  padding: 16px;
  padding-top: 16px;
  border-radius: 10px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  overflow: hidden;
  text-align: center;
  position: relative;
  max-height: 600px;
  cursor: grab;
}
body.mermaid-ctrl-held .mermaid-container {
  cursor: zoom-in !important;
}
body.mermaid-dragging,
body.mermaid-dragging .mermaid-container {
  cursor: grabbing !important;
  user-select: none !important;
}
body.mermaid-dragging .mermaid-container {
  overflow: hidden;
}

/* Mermaid 内容包装器，负责滚动 */
.mermaid-content-wrapper {
  max-height: 500px;
  position: relative;
  z-index: 0;
  overflow: hidden;
}
.mermaid-source-preview {
  margin: 0;
  padding: 12px;
  background: linear-gradient(180deg, #263238 0%, #1c282c 100%);
  border-radius: 8px;
  color: #90a4ae;
  font-size: 12px;
  text-align: left;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
  font-family: 'Consolas', 'Monaco', monospace;
  user-select: text;
  cursor: text;
  max-height: 400px;
  overflow: auto;
  position: relative;
}
.mermaid-source-preview code {
  background: transparent;
  color: #90a4ae;
  padding: 0;
  border: none;
  font-size: 12px;
  user-select: text;
}
.mermaid-source-preview::selection {
  background: rgba(25, 118, 210, 0.3);
  color: #ffffff;
}

.mermaid-container .edgeLabel,
.mermaid-container .edgeLabel p {
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
  color: #333;
}
.mermaid-container .labelBkg {
  background: transparent !important;
}

.mermaid-fullscreen-container {
  background: var(--bg-secondary, #1e1e2e);
  box-shadow: 0 8px 40px rgba(255, 255, 255, 0.6);
}

[data-theme="dark"] .mermaid-container .edgeLabel,
[data-theme="dark"] .mermaid-container .edgeLabel p {
  color: #d4d4d4;
}

[data-theme="dark"] .mermaid-source-preview {
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  color: var(--text-secondary);
}

.mermaid-error {
  margin: 0;
  padding: 12px;
  background: var(--bg-row-changed);
  border: 1px solid var(--danger-color);
  border-radius: 6px;
  color: var(--danger-color);
  font-size: 12px;
  text-align: left;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 300px;
  overflow: auto;
}
.mermaid-error-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--warning-color);
  text-align: center;
}
.mermaid-toolbar {
  position: absolute;
  top: 8px;
  right: 8px;
  display: flex;
  align-items: center;
  gap: 2px;
  z-index: 100;
  backdrop-filter: blur(8px);
  padding: 4px;
  opacity: 0;
  transition: opacity 0.2s ease;
}
.mermaid-container:hover .mermaid-toolbar {
  opacity: 1;
}
.mermaid-tb-sep {
  width: 1px;
  height: 16px;
  background: #3c3c3c;
  margin: 0 2px;
}
.mermaid-tb-btn {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  font-size: 11px;
  color: #9cdcfe;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 4px;
  padding: 0;
  line-height: 1;
  transition: all 0.15s;
  user-select: none;
  font-family: inherit;
  white-space: nowrap;
}
.mermaid-tb-btn:hover {
  color: #0fa1ef;
  background: rgba(0, 122, 204, 0.15);
  border-color: rgba(0, 122, 204, 0.3);
}
.mermaid-tb-btn:active {
  background: rgba(0, 122, 204, 0.25);
}
.mermaid-tb-btn svg {
  flex-shrink: 0;
}
.mermaid-container .node rect,
.mermaid-container .node polygon,
.mermaid-container .node path,
.mermaid-container .cluster rect,
.mermaid-container .cluster path,
.mermaid-container .subgraph rect,
.mermaid-container .subgraph path {
  rx: 8;
  ry: 8;
}
.mermaid-svg-wrap {
  text-align: center;
  transform-origin: 0 0;
}
.mermaid-svg-wrap svg {
  max-width: 100%;
  height: auto;
}
/* Mermaid 高度拖拽手柄 */
.mermaid-resize-handle {
  height: 12px;
  cursor: ns-resize;
  display: flex;
  align-items: center;
  justify-content: center;
  user-select: none;
  border-top: 1px solid #3c3c3c;
  margin-top: 4px;
  opacity: 0;
  transition: opacity 0.2s ease;
}
.mermaid-container:hover .mermaid-resize-handle {
  opacity: 1;
}
.mermaid-resize-dots {
  font-size: 14px;
  color: #808080;
  letter-spacing: 2px;
  line-height: 1;
}
.mermaid-resize-handle:hover .mermaid-resize-dots {
  color: #569cd6;
}
body.mermaid-resizing,
body.mermaid-resizing * {
  cursor: ns-resize !important;
  user-select: none !important;
}

/* ========== Dark Mode Overrides ========== */

/* ── Markdown Dark Mode ── */
[data-theme="dark"] .markdown-body {
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body h1,
[data-theme="dark"] .markdown-body h2,
[data-theme="dark"] .markdown-body h3,
[data-theme="dark"] .markdown-body h4,
[data-theme="dark"] .markdown-body h5,
[data-theme="dark"] .markdown-body h6 {
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body h1 {
  border-bottom-color: #3c3c3c;
}

[data-theme="dark"] .markdown-body h2 {
  border-bottom-color: #3c3c3c;
}

[data-theme="dark"] .markdown-body code {
  background: linear-gradient(135deg, #2d2d2d 0%, #3c3c3c 100%);
  color: #d16969;
  border-color: rgba(255, 255, 255, 0.1);
}
[data-theme="dark"] .markdown-body eq code,
[data-theme="dark"] .markdown-body eqn code {
  padding: 0;
  margin: 0;
  font-size: inherit;
  background: none;
  border-radius: 0;
  font-family: inherit;
  color: inherit;
  border: none;
}

[data-theme="dark"] .markdown-body pre {
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  border-color: #3c3c3c;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
}

[data-theme="dark"] .markdown-body blockquote {
  color: #9cdcfe;
  border-left-color: #007acc;
  background: rgba(0, 122, 204, 0.1);
}

[data-theme="dark"] .markdown-body table th,
[data-theme="dark"] .markdown-body table td {
  border-color: #3c3c3c;
}

[data-theme="dark"] .markdown-body table th {
  background: linear-gradient(180deg, #2d2d2d 0%, #3c3c3c 100%);
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body table tr:nth-child(2n) {
  background-color: #252526;
}

[data-theme="dark"] .markdown-body table tr:hover {
  background-color: #2a2d2e;
}

[data-theme="dark"] .markdown-body a {
  color: #569cd6;
}

[data-theme="dark"] .markdown-body a:hover {
  color: #7ab3e8;
}

[data-theme="dark"] .markdown-body hr {
  background: linear-gradient(90deg, #3c3c3c 0%, #505050 50%, #3c3c3c 100%);
}

[data-theme="dark"] .markdown-body strong {
  color: #d4d4d4;
}

[data-theme="dark"] .markdown-body em {
  color: #9cdcfe;
}

[data-theme="dark"] .thinking-content.markdown-body {
  color: #9cdcfe;
}

[data-theme="dark"] .thinking-content.markdown-body strong {
  color: #d4d4d4;
}

[data-theme="dark"] .thinking-content.markdown-body code {
  background: rgba(0, 0, 0, 0.3);
  color: #f44747;
}
[data-theme="dark"] .thinking-content.markdown-body pre code.hljs {
  background: transparent;
  color: inherit;
}
[data-theme="dark"] .thinking-content.markdown-body pre code:not(.hljs) {
  color: #f44747;
}

[data-theme="dark"] .thinking-content.markdown-body pre {
  background: rgba(0, 0, 0, 0.25);
}

/* ── Layout & Container ── */
[data-theme="dark"] .ai-sql-panel-container {
  background: var(--bg-tertiary);
}

[data-theme="dark"] .container {
  background: var(--bg-tertiary);
}

[data-theme="dark"] .el-config-provider {
  background: var(--bg-tertiary);
}

[data-theme="dark"] .login-button-container .el-button {
  background-color: var(--bg-secondary);
  border-color: var(--border-primary);
  color: var(--text-secondary);
}

[data-theme="dark"] .login-button-container .el-button:hover {
  color: var(--accent-color);
  border-color: var(--accent-color);
}

/* ── Chat Messages ── */
[data-theme="dark"] .chat-messages {
  background: var(--bg-primary);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.4);
}

[data-theme="dark"] .chat-messages::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.15);
}

[data-theme="dark"] .chat-messages::-webkit-scrollbar-thumb {
  background: linear-gradient(180deg, #505050 0%, #3c3c3c 100%);
}

[data-theme="dark"] .chat-messages::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(180deg, #6a6a6a 0%, #505050 100%);
}

/* ── Chat Bubbles ── */
[data-theme="dark"] .chat-bubble.user {
  background: linear-gradient(135deg, #2a4a7f 0%, #2d2d2d 100%);
  color: #d4d4d4;
  box-shadow: 0 4px 12px rgba(42, 74, 127, 0.3);
}

[data-theme="dark"] .chat-bubble.user .bubble-label {
  color: rgba(212, 212, 212, 0.85);
}

[data-theme="dark"] .chat-bubble.assistant {
  background: linear-gradient(135deg, #252526 0%, #1e1e1e 100%);
  color: #d4d4d4;
  border-color: var(--border-primary);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

[data-theme="dark"] .chat-bubble.assistant:hover {
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}

[data-theme="dark"] .chat-bubble.assistant .bubble-label {
  color: var(--text-tertiary);
}

/* AI 消息操作栏暗色主题 */
[data-theme="dark"] .msg-action-bar {
  border-top-color: rgba(255, 255, 255, 0.08);
}
[data-theme="dark"] .msg-action-btn {
  color: var(--text-tertiary);
}
[data-theme="dark"] .msg-action-btn:hover {
  color: var(--el-color-primary, #409eff);
}

/* ── Thinking Block ── */
[data-theme="dark"] .thinking-block {
  border-color: rgba(128, 128, 128, 0.3);
  background: linear-gradient(135deg, rgba(60, 60, 60, 0.4) 0%, rgba(50, 50, 50, 0.3) 100%);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.25);
}

[data-theme="dark"] .thinking-block:hover {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

[data-theme="dark"] .thinking-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .thinking-label:hover {
  color: var(--text-primary);
}

[data-theme="dark"] .thinking-content {
  color: var(--text-secondary);
  background: rgba(30, 30, 30, 0.6);
}

[data-theme="dark"] .thinking-content code {
  background: rgba(0, 0, 0, 0.3);
  color: var(--danger-color);
}

[data-theme="dark"] .thinking-content pre {
  background: rgba(0, 0, 0, 0.2);
}

[data-theme="dark"] .thinking-content::-webkit-scrollbar-thumb {
  background: #505050;
}

/* ── Tool Call Block ── */
[data-theme="dark"] .tool-call-block {
  color: #a6e3a1;
  background: linear-gradient(135deg, rgba(166, 227, 161, 0.1) 0%, rgba(166, 227, 161, 0.05) 100%);
  border-color: rgba(166, 227, 161, 0.2);
  box-shadow: 0 2px 8px rgba(166, 227, 161, 0.08);
}

/* ── Input Area ── */
[data-theme="dark"] .input-area {
  background: var(--bg-secondary);
  border-top-color: var(--border-primary);
  box-shadow: 0 -2px 8px rgba(0, 0, 0, 0.2);
}

[data-theme="dark"] .input-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .question-input .el-textarea__inner {
  background: #202032;
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .question-input .el-textarea__inner:hover {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.06);
}

[data-theme="dark"] .question-input .el-textarea__inner:focus {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.12);
}

[data-theme="dark"] .question-input .el-textarea__inner::placeholder {
  color: var(--text-placeholder);
}

/* ── Textarea (general) ── */
[data-theme="dark"] .el-textarea__inner {
  background-color: #202032;
  color: var(--text-primary);
  border-color: var(--border-primary);
}

[data-theme="dark"] .el-textarea__inner:hover {
  border-color: var(--border-secondary);
}

[data-theme="dark"] .el-textarea__inner:focus {
  border-color: var(--accent-color);
}

/* ── Select / Tree-Select ── */
[data-theme="dark"] .modern-select .el-input__wrapper,
[data-theme="dark"] .modern-tree-select .el-input__wrapper {
  background-color: #202032;
  box-shadow: 0 0 0 1px var(--border-primary) inset;
}

[data-theme="dark"] .modern-select .el-input__wrapper:hover,
[data-theme="dark"] .modern-tree-select .el-input__wrapper:hover {
  box-shadow: 0 0 0 1px var(--border-secondary) inset;
}

[data-theme="dark"] .modern-select .el-input__wrapper.is-focus,
[data-theme="dark"] .modern-tree-select .el-input__wrapper.is-focus {
  box-shadow: 0 0 0 1px var(--accent-color) inset;
}

[data-theme="dark"] .el-select-dropdown__item.selected {
  background: linear-gradient(90deg, rgba(137, 180, 250, 0.12) 0%, transparent 100%);
  color: var(--accent-color);
}

/* ── Table Selector ── */
[data-theme="dark"] .table-selector-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .selector-badge {
  background: linear-gradient(135deg, rgba(137, 180, 250, 0.15) 0%, rgba(137, 180, 250, 0.08) 100%);
  color: var(--accent-color);
}

[data-theme="dark"] .selector-badge.ready {
  background: linear-gradient(135deg, rgba(166, 227, 161, 0.15) 0%, rgba(166, 227, 161, 0.08) 100%);
  color: var(--success-color);
}

[data-theme="dark"] .selector-skeleton {
  border-color: var(--border-primary);
}

[data-theme="dark"] .selector-skeleton::after {
  color: var(--text-placeholder);
}

[data-theme="dark"] .skeleton-shimmer {
  background: linear-gradient(90deg, var(--bg-hover) 0%, var(--bg-active) 35%, var(--bg-hover) 65%);
}

/* ── Option Items ── */
[data-theme="dark"] .table-option-name {
  color: var(--text-primary);
}

[data-theme="dark"] .table-option-comment {
  color: var(--text-tertiary);
}

[data-theme="dark"] .table-option-comment::before {
  color: var(--text-placeholder);
}

[data-theme="dark"] .table-option-schema {
  color: var(--accent-color);
  background: rgba(137, 180, 250, 0.12);
}

[data-theme="dark"] .model-option-name {
  color: var(--text-primary);
}

[data-theme="dark"] .model-option-provider {
  color: var(--text-tertiary);
  background: var(--bg-active);
}



/* ── Buttons ── */
[data-theme="dark"] .toolbar-btn.el-button--primary {
  background-color: rgba(137, 180, 250, 0.15);
  border-color: rgba(137, 180, 250, 0.25);
}

[data-theme="dark"] .toolbar-btn.el-button--danger {
  background-color: rgba(243, 139, 168, 0.15);
  border-color: rgba(243, 139, 168, 0.25);
}

[data-theme="dark"] .switch-view-link {
  color: var(--accent-color);
}

[data-theme="dark"] .switch-view-link:hover {
  color: #99c4ff;
  background-color: rgba(137, 180, 250, 0.08);
}

/* ── SQL Confirm ── */
[data-theme="dark"] .multi-sql-confirm {
  border-color: var(--warning-color);
  background: rgba(249, 226, 175, 0.08);
}

[data-theme="dark"] .sql-confirm-item {
  background: var(--bg-secondary);
  border-color: var(--border-primary);
}

/* ── Retry Confirm ── */
[data-theme="dark"] .retry-confirm-block {
  border-color: var(--warning-color);
  background: rgba(249, 226, 175, 0.08);
}

/* ── Uploaded File Info ── */
[data-theme="dark"] .uploaded-file-info {
  background: rgba(137, 180, 250, 0.1);
  color: var(--accent-color);
}

[data-theme="dark"] .uploaded-file-info .el-button--danger.is-text {
  color: var(--danger-color);
}

/* ── Session Items ── */
[data-theme="dark"] .session-item {
  background: var(--bg-secondary);
  border-color: var(--border-primary);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.15);
}

[data-theme="dark"] .session-item::before {
  background: linear-gradient(180deg, var(--accent-color) 0%, var(--text-tertiary) 100%);
}

[data-theme="dark"] .session-item:hover {
  background: var(--bg-hover);
  border-color: var(--border-secondary);
}

[data-theme="dark"] .session-title {
  color: var(--text-primary);
}

[data-theme="dark"] .session-time {
  color: var(--text-secondary);
}

/* ── Prompt Popover ── */
[data-theme="dark"] .prompt-popover-body .prompt-item:hover {
  background: var(--bg-hover);
}

[data-theme="dark"] .prompt-popover-body .prompt-item-title {
  color: var(--text-primary);
}

[data-theme="dark"] .prompt-popover-body .prompt-item-sub {
  color: var(--text-tertiary);
}

[data-theme="dark"] .prompt-popover-body .prompt-empty {
  color: var(--text-tertiary);
}

/* ── Popover ── */
[data-theme="dark"] .el-popover {
  --el-popover-bg-color: var(--bg-primary);
  background-color: var(--bg-primary);
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-popover .el-button--default {
  background-color: var(--bg-secondary);
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-popover .el-button--default:hover {
  background-color: var(--bg-hover);
}

[data-theme="dark"] .el-popover .el-empty__description {
  color: var(--text-tertiary);
}

[data-theme="dark"] .el-popover .el-skeleton__item {
  background: linear-gradient(90deg, var(--bg-hover) 25%, var(--bg-active) 50%, var(--bg-hover) 75%);
}

[data-theme="dark"] .el-popover .el-tabs__nav-wrap::after {
  background-color: var(--border-primary);
}

[data-theme="dark"] .el-popover .el-tabs__item {
  color: var(--text-secondary);
}

[data-theme="dark"] .el-popover .el-tabs__item.is-active {
  color: var(--accent-color);
}

/* ── Dialog ── */
[data-theme="dark"] .el-dialog {
  --el-dialog-bg-color: var(--bg-primary);
  --el-dialog-title-font-size: 18px;
  background-color: var(--bg-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-dialog .el-dialog__header {
  border-bottom-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-dialog .el-dialog__title {
  color: var(--text-primary);
}

[data-theme="dark"] .el-dialog .el-dialog__body {
  color: var(--text-secondary);
}

[data-theme="dark"] .el-dialog .el-dialog__footer {
  border-top-color: var(--border-primary);
}

/* ── Drawer ── */
[data-theme="dark"] .el-drawer {
  --el-drawer-bg-color: var(--bg-primary);
  background-color: var(--bg-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .el-drawer__header {
  color: var(--text-primary);
}

[data-theme="dark"] .el-drawer__body {
  scrollbar-color: var(--scrollbar-thumb) transparent;
}

/* ── Markdown Body (Dark) - 使用统一样式，删除重复定义 ── */

/* ── Prompt Detail Dialog (Dark) ── */
[data-theme="dark"] .prompt-detail-meta-label {
  color: var(--text-tertiary);
}

[data-theme="dark"] .prompt-detail-content {
  color: var(--text-secondary);
}

/* ── Global Search Popover (Dark) ── */
[data-theme="dark"] .global-search-popover {
  background-color: var(--bg-primary) !important;
  border-color: var(--border-primary) !important;
}

[data-theme="dark"] .global-search-popover .el-scrollbar {
  border-color: var(--border-primary);
}

/* ── Code Block Wrapper & Copy Button ── */
.code-block-wrapper {
  position: relative;
  margin: 12px 0;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  border: 1px solid rgba(0, 0, 0, 0.2);
}
.code-block-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 12px;
  background: linear-gradient(180deg, #2d2d2d 0%, #1e1e1e 100%);
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.code-block-lang {
  font-size: 11px;
  color: #9ca3af;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-family: 'Consolas', 'Monaco', monospace;
}
.code-copy-btn {
  font-size: 12px;
  color: #9ca3af;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  padding: 2px 10px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
  line-height: 1.4;
}
.code-copy-btn:hover {
  color: #e5e7eb;
  background: rgba(255, 255, 255, 0.12);
  border-color: rgba(255, 255, 255, 0.2);
}
.code-block-wrapper pre {
  margin: 0 !important;
  border-radius: 0 !important;
  border: none !important;
  box-shadow: none !important;
}

[data-theme="dark"] .code-block-header {
  background: linear-gradient(180deg, var(--bg-secondary) 0%, var(--bg-active) 100%);
  border-bottom-color: var(--border-primary);
}
[data-theme="dark"] .code-block-lang {
  color: var(--text-tertiary);
}
[data-theme="dark"] .code-copy-btn {
  color: var(--text-tertiary);
  background: rgba(255, 255, 255, 0.04);
  border-color: var(--border-primary);
}
[data-theme="dark"] .code-copy-btn:hover {
  color: var(--text-primary);
  background: rgba(255, 255, 255, 0.08);
}

/* ── Load More Messages ── */
.load-more-msgs {
  text-align: center;
  padding: 10px 0;
  margin: 4px 0;
  font-size: 13px;
  color: #909399;
  cursor: pointer;
  border-radius: 8px;
  transition: all 0.2s;
  user-select: none;
}
.load-more-msgs:hover {
  color: #1976d2;
  background: rgba(25, 118, 210, 0.06);
}
[data-theme="dark"] .load-more-msgs {
  color: var(--text-tertiary);
}
[data-theme="dark"] .load-more-msgs:hover {
  color: var(--accent-color);
  background: rgba(137, 180, 250, 0.06);
}

/* ── Mermaid Smooth Transition ── */
.mermaid-svg-wrap.smooth-transition {
  transition: transform 0.2s ease-out;
}

/* ── Mermaid 全屏模式 ── */
.mermaid-fullscreen-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 99999;
  background: rgba(220, 220, 220, 0.85);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: mermaid-fs-fadein 0.2s ease;
}
@keyframes mermaid-fs-fadein {
  from { opacity: 0; }
  to { opacity: 1; }
}
body.mermaid-fullscreen-active {
  overflow: hidden !important;
}
.mermaid-fullscreen-container {
  position: relative;
  width: 98vw;
  height: 96vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.mermaid-fullscreen-content {
  flex: 1;
  overflow: hidden;
  cursor: grab;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
}
.mermaid-fullscreen-content .mermaid-svg-wrap {
  transform-origin: center center;
  text-align: center;
}
.mermaid-fullscreen-content .mermaid-svg-wrap svg {
  max-width: none;
  max-height: none;
}
.mermaid-fullscreen-toolbar {
  position: absolute;
  top: 12px;
  right: 12px;
  display: flex;
  align-items: center;
  gap: 4px;
  z-index: 10;
  padding: 6px 8px;
}
.mermaid-fullscreen-toolbar .mermaid-tb-btn {
  width: 32px;
  height: 32px;
  font-size: 13px;
}

/* 全屏模式暗色主题 */
[data-theme="dark"] .mermaid-fullscreen-overlay {
  background: rgba(0, 0, 0, 0.92);
}
[data-theme="dark"] .mermaid-fullscreen-container {
  background: var(--bg-secondary, #1e1e2e);
  box-shadow: 0 8px 40px rgba(0, 0, 0, 0.6);
}
</style>
