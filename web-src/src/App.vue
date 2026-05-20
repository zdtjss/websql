<template>
  <el-config-provider :locale="zhCn">
  <div>
    <router-view v-if="systemRoutes.includes(route.path)" />
    <div v-else>

    <div class="ai-sql-panel-container">
      <div class="container">
        <!-- 会话历史消息 -->
        <div ref="msgContainer" class="chat-messages">
          <!-- 思考过程（历史中的，可折叠） -->
          <div v-for="(msg, idx) in chatHistory" :key="'h' + idx">
            <div v-if="msg.role === 'thinking'" class="thinking-block">
              <div class="thinking-label" style="cursor:pointer;" @click="toggleThinking(msg)">
                💭 思考过程 <span style="font-size:11px;">{{ msg.collapsed ? '▶ 展开' : '▼ 折叠' }}</span>
              </div>
              <div v-show="!msg.collapsed" class="thinking-content markdown-body" v-html="getCachedHtml(msg)"></div>
            </div>
            <div v-else-if="msg.role === 'user'" :class="['chat-bubble', 'user']">
              <div class="bubble-label">你</div>
              <div class="bubble-content markdown-body" v-html="getCachedHtml(msg)"></div>
            </div>
            <div v-else-if="msg.role === 'assistant'" :class="['chat-bubble', 'assistant']">
              <div class="bubble-label">AI</div>
              <div v-if="msg.hasSql" class="bubble-content">
                <pre class="sql-pre"><code v-html="highlightSql(msg.content)" /></pre>
              </div>
              <div v-else class="bubble-content markdown-body" v-html="getCachedHtml(msg)"></div>
            </div>
            <div v-else-if="msg.role === 'tool_call'" class="tool-call-block">
              <span>🔧 {{ msg.content }}</span>
            </div>
          </div>

          <!-- 实时思考过程（流式中） -->
          <div v-if="thinkingText && loading" class="thinking-block">
            <div class="thinking-label">💭 思考中...</div>
            <div class="thinking-content markdown-body" v-html="renderMarkdown(thinkingText)"></div>
          </div>

          <!-- 流式输出中 -->
          <div v-if="streamingContent" class="chat-bubble assistant">
            <div class="bubble-label">AI</div>
            <div class="bubble-content markdown-body" v-html="renderMarkdown(streamingContent)"></div>
          </div>

          <!-- 危险SQL确认后的流式输出 -->
          <div v-if="streamingExecContent" class="chat-bubble assistant">
            <div class="bubble-label">AI</div>
            <div class="bubble-content markdown-body" v-html="renderMarkdown(streamingExecContent)"></div>
          </div>

          <div v-if="loading" style="color:#909399;font-size:13px;padding:4px 0;">AI 正在处理...</div>
        </div>

        <!-- 内联 SQL 确认区域 -->
        <SQLConfirmInline v-model="confirmVisible" :sql="confirmSQL" :operation-type="confirmOperationType"
          :risk-level="confirmRiskLevel" :description="confirmDescription" :table-name="confirmTableName"
          @confirm="handleConfirmExec" @cancel="handleConfirmCancel" />

        <!-- 多条 SQL 批量确认区域 -->
        <div v-if="pendingSQLList.length > 0" class="multi-sql-confirm">
          <div style="font-weight:600;margin-bottom:8px;font-size:14px;">
            检测到 {{ pendingSQLList.length }} 条需要确认的 SQL：
          </div>
          <div style="margin-bottom:8px;">
            <el-checkbox v-model="selectAllChecked" @change="handleSelectAllChange">
              全选
            </el-checkbox>
            <span style="font-size:12px;color:#909399;margin-left:8px;">
              已选择 {{ pendingSQLList.filter(i => i.selected).length }} 条
            </span>
          </div>
          <div v-for="(item, idx) in pendingSQLList" :key="idx" class="sql-confirm-item">
            <div class="sql-confirm-header">
              <el-checkbox v-model="item.selected" style="margin-right:8px;">
                <el-tag :type="item.riskLevel === 'high' ? 'danger' : 'warning'" size="small">
                  {{ item.riskLevel === 'high' ? '高危' : '中危' }} - {{ item.type }}
                </el-tag>
              </el-checkbox>
              <span v-if="item.tableName" style="font-size:12px;color:#909399;">表：{{ item.tableName }}</span>
            </div>
            <pre class="sql-pre"><code v-html="highlightSql(item.sql)" /></pre>
          </div>
          <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;">
            <el-button size="small" @click="handleCancelAllSQL">全部取消</el-button>
            <el-button size="small" type="danger" @click="handleConfirmSelectedSQL" :disabled="selectedSQLCount === 0">
              确认执行选中 ({{ selectedSQLCount }} 条)
            </el-button>
          </div>
        </div>

        <!-- 重试确认区域 -->
        <div v-if="showRetryConfirm" class="retry-confirm-block">
          <div style="color:#e6a23c;font-weight:600;margin-bottom:8px;">⚠️ 工具调用多次失败</div>
          <div style="font-size:13px;color:#606266;margin-bottom:12px;">{{ retryMessage }}</div>
          <div style="display:flex;gap:8px;">
            <el-button size="small" type="primary" @click="handleRetryContinue">继续尝试</el-button>
            <el-button size="small" @click="showRetryConfirm = false">放弃</el-button>
          </div>
        </div>

        <!-- 输入区域 -->
        <div class="input-area">
          <div class="input-label">
            <span>描述你的需求（数据查询 / 数据分析 / SQL 生成 / 数据导出 / 数据导入）</span>
            <div style="display: flex; gap: 0px;">
              <!-- 已上传的 Excel 文件信息 -->
            <div v-if="uploadedExcel" class="uploaded-file-info" style="margin-left: 12px;">
              <span>{{ uploadedExcel.name }} ({{ uploadedExcel.rows }} 行, {{ uploadedExcel.columns.length }} 列)</span>
              <el-button size="small" text type="danger" @click="clearUploadedExcel">✕</el-button>
            </div>
               <el-upload ref="excelUploadRef" :auto-upload="false" :show-file-list="false" style="margin-left: 12px;" accept=".xlsx,.xls"
                :on-change="handleExcelUpload" :disabled="excelUploading">
                <el-button class="toolbar-btn" size="small" title="上传 Excel 导入数据" :loading="excelUploading">
                  <el-icon v-if="!excelUploading"><Upload /></el-icon>
                </el-button>
              </el-upload>
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="promptPopoverVisible"
                @show="loadPrompts()">
                <div class="prompt-popover-body">
                  <el-tabs v-model="activeTab" class="prompt-tabs">
                    <el-tab-pane name="mine">
                      <template #label>
                        <span style="display: inline-flex; align-items: center; gap: 6px; width: 100%;">
                          我的
                          <el-icon v-if="promptLoading" class="is-loading" size="14"><Loading /></el-icon>
                          <el-icon :size="10" style="top: -10px;" @click="handlePromptAdd"><Plus /></el-icon>
                        </span>
                      </template>
                      <div class="prompt-list">
                        <div v-if="myPrompts.length === 0" class="prompt-empty">暂无提示词</div>
                        <div v-for="prompt in myPrompts" :key="prompt.id" class="prompt-item" @click.stop="handlePromptSendToAI(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })">
                          <div class="prompt-item-info">
                            <div class="prompt-item-title">{{ prompt.title }}</div>
                            <div v-if="prompt.connSchemas && prompt.connSchemas.length > 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas.length }} 个 Schema
                            </div>
                            <div v-else-if="prompt.connSchemas && prompt.connSchemas.length === 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas[0].schema }}
                            </div>
                            <div v-if="prompt.isShared" class="prompt-item-sub">
                              <el-icon size="12"><Share /></el-icon>
                              {{ prompt.sharedByName || '他人分享' }}
                            </div>
                          </div>
                          <div class="prompt-item-actions">
                            <el-button text type="primary">
                              <el-icon v-if="!prompt.isShared" @click.stop="handlePromptEdit(prompt.id)" title="编辑"><Edit /></el-icon>
                            </el-button>
                            <el-button v-if="!prompt.isShared" style="margin-left: -10px;" text type="danger" @click.stop="handleDeletePrompt(prompt)" title="删除">
                              <el-icon><Delete /></el-icon>
                            </el-button>
                          </div>
                        </div>
                      </div>
                    </el-tab-pane>
                    <el-tab-pane label="系统" name="system">
                      <div class="prompt-list">
                        <div v-if="promptLoading" style="text-align: center; padding: 10px;">
                          <el-icon class="is-loading"><Loading /></el-icon>
                        </div>
                        <div v-else-if="systemPrompts.length === 0" class="prompt-empty">暂无系统提示词</div>
                        <div v-for="prompt in systemPrompts" :key="prompt.id" class="prompt-item" @click.stop="handlePromptSendToAI(prompt.content, { connSchemas: prompt.connSchemas, tables: prompt.tables })">
                          <div class="prompt-item-info">
                            <div class="prompt-item-title">{{ prompt.title }}</div>
                            <div v-if="prompt.connSchemas && prompt.connSchemas.length > 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas.length }} 个 Schema
                            </div>
                            <div v-else-if="prompt.connSchemas && prompt.connSchemas.length === 1" class="prompt-item-sub">
                              <el-icon size="12"><Coin /></el-icon>
                              {{ prompt.connSchemas[0].schema }}
                            </div>
                          </div>
                          <div class="prompt-item-actions">
                            <el-button size="small" text type="info" @click.stop="handleViewPromptDetail(prompt)" title="查看">
                              <el-icon><View /></el-icon>
                            </el-button>
                          </div>
                        </div>
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
                    <div class="prompt-detail-content markdown-body" v-html="md.render(promptDetail.content)"></div>
                  </div>
                  <template #footer>
                    <el-button @click="promptDetailVisible = false">关闭</el-button>
                    <el-button type="primary" @click="handleSendFromDetail">
                      <el-icon><Promotion /></el-icon>
                      发送给大模型
                    </el-button>
                  </template>
                </el-dialog>
                <template #reference>
                  <el-button class="toolbar-btn" circle size="small" title="常用提示词" style="margin-left: 12px;">
                    <el-icon><ChatLineRound /></el-icon>
                  </el-button>
                </template>
              </el-popover>
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="sessionHistoryVisible" 
                @show="loadSessionList()">
                <div style="max-height: 400px; overflow-y: auto;">
                  <el-empty v-if="sessionList.length === 0 && !loadingSessions" description="暂无历史会话" />
                  <el-skeleton v-if="loadingSessions" :rows="4" animated />
                  <div v-else style="display: flex; flex-direction: column; gap: 8px;">
                    <div v-for="sess in sessionList" :key="sess.id" class="session-item">
                      <div class="session-content" @click="handleClickSession(sess.id)">
                        <div class="session-title">{{ sess.title || '未命名会话' }}</div>
                        <div class="session-time">
                          <el-icon>
                            <Clock />
                          </el-icon>
                          {{ formatDate(sess.createdAt) }}
                        </div>
                      </div>
                      <div class="session-actions">
                        <el-button type="danger" size="small" text @click.stop="confirmDeleteSession(sess.id)">
                          <el-icon>
                            <Delete />
                          </el-icon>  
                        </el-button>
                      </div>
                    </div>
                  </div>
                </div>
                <template #reference>
                  <el-button class="toolbar-btn" size="small" title="历史会话" style="margin-left: 12px;">
                    <el-icon>
                      <Document />
                    </el-icon>
                  </el-button>
                </template>
              </el-popover>
              <el-button class="toolbar-btn" :type="isRecording ? 'danger' : 'primary'" size="small"
                @click="toggleRecording">
                <el-icon style="vertical-align: middle;">
                  <component :is="isRecording ? VideoPause : Microphone" />
                </el-icon>
              </el-button>
              <el-button class="toolbar-btn" size="small" @click="clearSession" title="清空并新建会话">
                <el-icon>
                  <Delete />
                </el-icon>
              </el-button>
            </div>
          </div>
          <div class="table-selector-row">
            <div class="table-selector-container" v-if="shouldShowSchemaSelector">
              <div class="selector-header">
                <label class="table-selector-label">数据库 / Schema</label>
                <span v-if="schemasLoading" class="selector-badge loading">加载中...</span>
                <span v-else-if="selectedSchemas.length" class="selector-badge">{{ selectedSchemas.length }} 已选</span>
              </div>
              <div v-if="schemasLoading" class="selector-skeleton">
                <div class="skeleton-shimmer"></div>
              </div>
              <el-tree-select v-else
                v-model="selectedSchemas"
                :data="processedConnList"
                :props="{ label: 'label', value: 'value', children: 'children', disabled: 'disabled' }"
                placeholder="搜索数据库 / Schema..."
                class="modern-tree-select"
                @change="handleSchemaChange"
                filterable
                multiple
                :check-on-click-node="true"
                collapse-tags
                collapse-tags-tooltip
              />
            </div>
            <div class="table-selector-container" :class="{ 'full-width': !shouldShowSchemaSelector }">
              <div class="selector-header">
                <label class="table-selector-label" style="padding-top: 2px;">相关表</label>
                <span v-if="tablesLoading" class="selector-badge loading">加载中...</span>
                <span v-else-if="selectedTables.length" class="selector-badge">{{ selectedTables.length }} 已选</span>
                <span v-else-if="tableList.length" class="selector-badge ready">{{ tableList.length }} 可用</span>
              </div>
              <div v-if="tablesLoading" class="selector-skeleton">
                <div class="skeleton-shimmer"></div>
              </div>
              <el-select v-else v-model="selectedTables" multiple filterable placeholder="搜索表名..." class="modern-select">
                <template #tag="{ data, deleteTag, selectDisabled }">
                  <el-tag
                    v-for="item in data.slice(0, 2)"
                    :key="item.value"
                    :closable="!selectDisabled && !item.isDisabled"
                    @close="deleteTag($event, item)"
                    size="small"
                    disable-transitions
                  >
                    <el-tooltip :content="getTableComment(item.currentLabel)" :disabled="!getTableComment(item.currentLabel)" placement="top">
                      <span>{{ item.currentLabel }}</span>
                    </el-tooltip>
                  </el-tag>
                  <el-tooltip v-if="data.length > 2" placement="bottom">
                    <template #content>
                      <div v-for="item in data.slice(2)" :key="'c-' + item.value" style="line-height: 2;">
                        {{ item.currentLabel }}<span v-if="getTableComment(item.currentLabel)" style="color: var(--el-text-color-secondary); margin-left: 6px;">{{ getTableComment(item.currentLabel) }}</span>
                      </div>
                    </template>
                    <el-tag size="small" type="info" disable-transitions>+ {{ data.length - 2 }}</el-tag>
                  </el-tooltip>
                </template>
                <el-option v-for="table in tableList" :key="table.name + (table.schema || '')"
                  :label="table.label || table.name"
                  :value="table.label || table.name">
                  <div class="table-option-content">
                    <span class="table-option-name">{{ table.name }}</span>
                    <span v-if="table.comment" class="table-option-comment">{{ table.comment }}</span>
                    <span v-if="table.schema && selectedSchemas.length > 1" class="table-option-schema">{{ table.schema }}</span>
                  </div>
                </el-option>
              </el-select>
            </div>
            <div class="table-selector-container model-selector-container">
              <div class="selector-header">
                <label class="table-selector-label" style="padding-top: 2px;">AI 模型</label>
                <span v-if="selectedModel" class="selector-badge ready">{{ selectedModelName }}</span>
              </div>
              <el-select v-model="selectedModel" filterable placeholder="选择模型..." class="modern-select">
                <el-option v-for="model in aiModelList" :key="model.id"
                  :label="model.model"
                  :value="model.id">
                  <div class="model-option-content">
                    <span class="model-option-name">{{ model.model }}</span>
                    <span v-if="model.provider" class="model-option-provider">{{ model.provider }}</span>
                  </div>
                </el-option>
              </el-select>
            </div>
          </div>

          <div class="input-action-row">
            <div style="flex:1;display:flex;flex-direction:column;gap:6px;">
              <div v-if="extractedSchemaHints.length > 0" class="schema-hints">
                <span class="schema-hints-label">检测到跨库引用：</span>
                <template v-for="hint in extractedSchemaHints" :key="hint.schema">
                  <el-tag :type="hint.validated ? 'success' : 'danger'" size="small" effect="plain">
                    {{ hint.schema }}.{{ hint.tables.join(', ') }}
                  </el-tag>
                </template>
                <span v-if="extractedSchemaHints.some(h => !h.validated)" class="schema-hints-warn">
                  {{ extractedSchemaHints.filter(h => !h.validated).map(h => h.schema).join(', ') }} 不在授权中
                </span>
              </div>
              <el-input v-model="question" type="textarea" :rows="5" placeholder="描述你想查询的内容，或使用语音录入... (Ctrl+Enter 发送)"
                :disabled="loading" @keydown.ctrl.enter="sendMessage" class="question-input" />
            </div>
            <div class="action-buttons">
              <el-button v-if="loading" type="danger" @click="stopGeneration"
                class="stop-btn" size="default">
                <el-icon>
                  <VideoPause />
                </el-icon>
              </el-button>
              <el-button v-else type="primary" :disabled="!question.trim() && !uploadedExcel" @click="sendMessage"
                class="send-btn" size="default">
                <el-icon>
                  <Promotion />
                </el-icon>
              </el-button>
              <el-button v-if="lastSql" type="success" @click="insertToEditor" title="将最后生成的 SQL 加入编辑器"
                class="insert-btn" size="default">
                <el-icon>
                  <DocumentAdd />
                </el-icon>
              </el-button>
              <router-link v-if="canUseClassicView" to="/classical" class="switch-view-link" title="经典视图">
                <el-icon>
                  <Switch />
                </el-icon>
                <span>经典视图</span>
              </router-link>
            </div>
          </div>
        </div>
      </div>
    </div>
    <div class="login-button-container">
        <div style="display: flex; flex-direction: column; gap: 8px; align-items: center;">
          <el-button v-if="(currentUser.isAdmin || !isRemote) && loginSucc" circle size="small" @click="openSystemManagement" title="系统管理">
            <el-icon>
              <Setting />
            </el-icon>
          </el-button>
          <el-button v-if="!loginSucc && isRemote" circle size="small" @click="showLoginDialog" title="登录">
            <el-icon>
              <User />
            </el-icon>
          </el-button>
          <el-button v-else circle size="small" @click="logout" title="退出登录">
            <el-icon>
              <SwitchButton />
            </el-icon>
          </el-button>
        </div>
    </div>
    </div>
    <LoginDialog ref="loginDialogRef" v-model="loginDialogVisible" @login-success="handleLoginSuccess" />
  </div>
  </el-config-provider>
</template>

<script setup>
import SQLConfirmInline from '@/components/SQLConfirmInline.vue'
import PromptEditDialog from '@/components/PromptEditDialog.vue'
import { preloadVditor } from '@/utils/vditorLoader'
import { usePromptEditDialog } from '@/composables/usePromptEditDialog'
import LoginDialog from '@/components/LoginDialog.vue'
import http from '@/js/utils/httpProxy.js'
import { sanitizeError } from '@/utils/errorHandler.js'
import { analyzeSQL } from '@/utils/sqlRiskAssessment'
import { ChatLineRound, Clock, Coin, Delete, Document, DocumentAdd, Loading, Microphone, Plus, Promotion, Setting, Share, SwitchButton, Upload, User, VideoPause } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getMarkdownRenderer, getMermaid, getNextMermaidId, getHljs, preloadHeavyDeps } from '@/utils/lazyDeps'
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, useTemplateRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

const route = useRoute()
const systemRoutes = ['/system-management', '/role-permission', '/classical']

const apiBase = import.meta.env.VITE_API_URL || ''

let md = null

async function ensureMd() {
  if (md) return md
  md = await getMarkdownRenderer(apiBase)

  const defaultFenceRender = md.renderer.rules.fence || function (tokens, idx, options, env, self) {
    return self.renderToken(tokens, idx, options)
  }
  md.renderer.rules.fence = function (tokens, idx, options, env, self) {
    const token = tokens[idx]
    const info = token.info ? token.info.trim().toLowerCase() : ''
    if (info === 'mermaid') {
      const id = getNextMermaidId()
      const escaped = token.content
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
      return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-source="${escaped}" data-mermaid-processed="false"><pre class="mermaid-source-preview"><code>📊 Mermaid\n${escaped}</code></pre></div>`
    }
    return defaultFenceRender(tokens, idx, options, env, self)
  }

  return md
}

const props = defineProps({})

const emit = defineEmits([])

const question = ref('')
const selectedTables = ref([])
const loading = ref(false)
const abortController = ref(null)
const isRecording = ref(false)
const thinkingText = ref('')
const streamingContent = ref('')
const streamingExecContent = ref('') // 用于危险SQL确认后的流式响应
const chatHistory = ref([])
const sessionId = ref('')
const lastSql = ref('')
const msgContainer = useTemplateRef('msgContainer')
let speechRecognition = null

const isRemote = ref(sessionStorage.getItem("isRemote") === "true")
const canUseClassicView = ref(false)

const { visible: promptEditDialogVisible, promptId: editingPromptId, roleId: editingRoleId, openDialog: openPromptEditDialog, closeDialog: closePromptEditDialog, triggerSaved: triggerPromptSaved, setSendToAIHandler, handleSendToAI: handleSendToAIFromDialog } = usePromptEditDialog()

const promptPopoverVisible = ref(false)

const prompts = ref([])
const promptLoading = ref(false)
const activeTab = ref('mine')
const promptDetailVisible = ref(false)
const promptDetail = ref(null)

const myPrompts = computed(() =>
  prompts.value.filter(p => !p.isRolePrompt)
)

const systemPrompts = computed(() =>
  prompts.value.filter(p => p.isRolePrompt)
)

const showLoginBtn = ref(true)
const loginDialogVisible = ref(false)
const loginForm = ref({ name: "", password: "" })
const loginName = ref()
const loginFormRef = useTemplateRef('loginFormRef')
function parseCurrentUser() {
  try {
    const stored = sessionStorage.getItem("currentUser")
    return stored ? JSON.parse(stored) : { id: "", name: "", isAdmin: false }
  } catch {
    return { id: "", name: "", isAdmin: false }
  }
}

const currentUser = ref(parseCurrentUser())
const loginSucc = ref(!!sessionStorage.getItem("authentication"))
const logining = ref(false)

const router = useRouter()

const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

// 数据库连接配置 - 多 schema 选择模式
const selectedSchemas = ref([])
const tableList = ref([])
const connList = ref([])
const connSchemaList = ref([])
const schemasLoading = ref(false)
const tablesLoading = ref(false)

// AI 模型配置
const aiModelList = ref([])
const selectedModel = ref('')
const modelLoading = ref(false)

// 计算属性：处理后的 schema 级树结构
const processedConnList = computed(() => {
  return buildSchemaTree(connSchemaList.value)
})

// 计算属性：是否显示 schema 选择器（单连接单 schema 时隐藏）
const shouldShowSchemaSelector = computed(() => {
  // 加载中时显示
  if (schemasLoading.value) return true
  // 多个连接时显示
  if (connSchemaList.value.length > 1) return true
  // 单个连接但有多个 schema 时显示
  if (connSchemaList.value.length === 1) {
    const conn = connSchemaList.value[0]
    const schemaCount = (conn.schemas || []).length
    if (schemaCount > 1) return true
  }
  return false
})

// 将 [{connId, name, dbSchema, dirName, schemas: [{name}]}] 转为 el-tree-select 支持的 schema 级树
function buildSchemaTree(rawList) {
  const dirMap = new Map()
  const noDir = []

  for (const item of rawList) {
    const schemas = item.schemas || []
    const dbType = item.dbType || ''
    // 连接不可用时，创建禁用的叶子节点
    if (item.available === false) {
      const node = {
        label: item.name,
        value: item.connId + '::',
        connId: item.connId,
        schemaName: '',
        disabled: true,
        isSchemaLeaf: false,
        dbType,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
      continue
    }
    // 如果连接只有一个 schema，折叠到连接名下
    if (schemas.length <= 1) {
      const singleSchema = schemas.length === 1 ? schemas[0].name : (item.dbSchema || '')
      const node = {
        label: item.name,
        value: item.connId + '::' + singleSchema,
        connId: item.connId,
        schemaName: singleSchema,
        disabled: false,
        isSchemaLeaf: true,
        dbType,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
    } else {
      // 多个 schema：连接作为目录，schema 作为可选子节点
      const schemaChildren = schemas.map(s => ({
        label: s.name,
        value: item.connId + '::' + s.name,
        connId: item.connId,
        schemaName: s.name,
        disabled: false,
        isSchemaLeaf: true,
        dbType,
      }))
      const connNode = {
        label: item.name,
        value: '__conn__' + item.connId,
        disabled: true,
        children: schemaChildren,
        dbType,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(connNode)
      } else {
        noDir.push(connNode)
      }
    }
  }

  const tree = []
  for (const [dirName, children] of dirMap) {
    tree.push({ label: dirName, value: '__dir__' + dirName, disabled: true, children })
  }
  tree.push(...noDir)
  return tree
}

async function loadConnList() {
  schemasLoading.value = true
  const auth = sessionStorage.getItem('authentication') || ''
  const apiBase = import.meta.env.VITE_API_URL || ''

  try {
    const resp = await fetch(apiBase + '/listUserConnSchemasStream', {
      headers: { 'Authorization': auth }
    })

    if (!resp.ok) {
      if (resp.status === 401) {
        handleSessionExpired()
        return
      }
      throw new Error('HTTP ' + resp.status)
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    const rawList = []

    while (true) {
      const { done, value } = await reader.read()
      if (value && value.length > 0) {
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop()

        for (const line of lines) {
          if (!line.startsWith('data:')) continue
          const data = line.slice(5).trim()
          if (!data) continue
          if (data === '"ok"' || data === '"empty"') continue

          try {
            const item = JSON.parse(data)
            if (item.connId) {
              rawList.push(item)
              connSchemaList.value = [...rawList]
              connList.value = buildSchemaTree(connSchemaList.value)
              if (schemasLoading.value) {
                schemasLoading.value = false
              }
            }
          } catch (_) {}
        }
      }
      if (done) break
    }

    if (connList.value.length > 0 && selectedSchemas.value.length === 0) {
      const allSchemaValues = connList.value.flatMap(node => {
        if (node.children && node.children.length > 0) {
          return node.children.filter(c => !c.disabled).map(c => c.value)
        }
        if (!node.disabled && node.isSchemaLeaf) {
          return [node.value]
        }
        return []
      })
      if (allSchemaValues.length === 1) {
        selectedSchemas.value = allSchemaValues
        loadTableListForSchemas()
      }
    }
  } catch (e) {
    if (e.response && e.response.status === 401) {
      handleSessionExpired()
      return
    }
    console.error('加载连接列表失败:', e)
  } finally {
    schemasLoading.value = false
  }
}

function parseSchemaValue(value) {
  const idx = value.indexOf('::')
  if (idx === -1) return null
  return { connId: value.substring(0, idx), schema: value.substring(idx + 2) }
}

function getTableComment(value) {
  const found = tableList.value.find(t => (t.label || t.name) === value)
  return found?.comment || ''
}

async function loadTableListForSchemas() {
  tablesLoading.value = true
  if (selectedSchemas.value.length === 0) {
    tableList.value = []
    tablesLoading.value = false
    return
  }
  try {
    const schemaRefs = selectedSchemas.value
      .map(v => parseSchemaValue(v))
      .filter(Boolean)

    if (schemaRefs.length === 0) {
      tableList.value = []
      tablesLoading.value = false
      return
    }

    const resp = await http.get('/listTableNames', {
      params: { schemas: JSON.stringify(schemaRefs) }
    })
    const tables = resp.data.data || []
    const allTables = tables.map(t => {
      const hasSchema = t.schema && selectedSchemas.value.length > 1
      return {
        name: t.name,
        comment: t.comment || '',
        schema: t.schema || '',
        label: hasSchema ? t.schema + '.' + t.name : t.name,
      }
    })
    tableList.value = allTables

    if (selectedTables.value.length > 0) {
      const newValues = allTables.map(t => t.label || t.name)
      selectedTables.value = selectedTables.value.filter(name => newValues.includes(name))
    }
  } catch (e) {
    tableList.value = []
    if (e.response && e.response.status === 401) {
      handleSessionExpired()
    }
  } finally {
    tablesLoading.value = false
  }
}

async function loadTableList(connId, schema) {
  tablesLoading.value = true
  try {
    if (!connId) {
      tableList.value = []
      tablesLoading.value = false
      return
    }
    const resp = await http.get('/listTableNames', {
      params: { connId, schema: schema || '' }
    })
    const newTableList = resp.data.data || []

    if (selectedTables.value.length > 0) {
      const newNames = newTableList.map(t => typeof t === 'string' ? t : t.name)
      selectedTables.value = selectedTables.value.filter(name => newNames.includes(name))
    }

    tableList.value = newTableList.map(t => typeof t === 'string' ? { name: t, comment: '', schema: '' } : t)
  } catch (e) {
    console.error('加载表列表失败:', e)
    tableList.value = []
  } finally {
    tablesLoading.value = false
  }
}

function handleSchemaChange() {
  loadTableListForSchemas()
}

// 从 selectedSchemas 获取主连接的 connId（向后兼容）
function getPrimaryConnId() {
  if (selectedSchemas.value.length > 0) {
    const parsed = parseSchemaValue(selectedSchemas.value[0])
    if (parsed) return parsed.connId
  }
  return ''
}

// 构建发送给后端的 schema 引用数组
function buildRequestSchemas() {
  return selectedSchemas.value
    .map(v => parseSchemaValue(v))
    .filter(p => p && p.schema !== '')
    .map(p => ({ connId: p.connId, schema: p.schema }))
}

// AI 模型相关
function loadModelList() {
  modelLoading.value = true
  http.get("/system/config/ai/models").then((resp) => {
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

const selectedModelName = computed(() => {
  if (!selectedModel.value || aiModelList.value.length === 0) return ''
  const model = aiModelList.value.find(m => m.id === selectedModel.value)
  return model ? model.model : ''
})

// 从用户输入中自动提取 schema.表名 引用（数据权限验证辅助）
const extractedSchemaHints = computed(() => {
  const text = question.value || ''
  const hints = []
  // 匹配 schema.table 模式，排除 URL（http/https）、文件路径、版本号
  const regex = /(?:^|[^/\w])([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)(?=[^a-zA-Z0-9_]|$)/g
  let match
  while ((match = regex.exec(text)) !== null) {
    const schemaName = match[1]
    const tableName = match[2]
    // 排除常见非数据关键词
    const excluded = ['www', 'http', 'https', 'ftp', 'com', 'org', 'net', 'io', 'cn', 'js', 'ts', 'vue', 'py', 'go', 'java']
    if (excluded.includes(schemaName.toLowerCase()) || excluded.includes(tableName.toLowerCase())) {
      continue
    }
    const existing = hints.find(h => h.schema === schemaName)
    if (existing) {
      if (!existing.tables.includes(tableName)) {
        existing.tables.push(tableName)
      }
    } else {
      // 验证 schema 是否在已授权连接中存在
      const schemaExists = connSchemaList.value.some(c =>
        (c.schemas || []).some(s => s.name === schemaName) ||
        (c.dbSchema === schemaName)
      )
      hints.push({ schema: schemaName, tables: [tableName], validated: schemaExists })
    }
  }
  return hints
})

function validateExtractedSchemas() {
  const hints = extractedSchemaHints.value
  const unvalidated = hints.filter(h => !h.validated)
  if (unvalidated.length > 0) {
    const names = unvalidated.map(h => h.schema).join(', ')
    ElMessage.warning(`Schema [${names}] 不在您授权的连接中，将无法访问相关表`)
    return false
  }
  return true
}

// 历史会话相关
const sessionHistoryVisible = ref(false)
const sessionList = ref([])
const loadingSessions = ref(false)

// 用于记录已经渲染过的链接，避免重复处理
const processedLinks = new Set()

// SQL 确认相关
const confirmVisible = ref(false)
const confirmSQL = ref('')
const confirmInterruptIds = ref([])
const confirmCheckPointId = ref('')
const confirmOperationType = ref('SELECT')
const confirmRiskLevel = ref('low')
const confirmDescription = ref('')
const confirmTableName = ref('')
let hasShownConfirm = false  // 防止重复弹出

// 多条 SQL 批量确认
const pendingSQLList = ref([])
const selectAllChecked = ref(false)

// 计算选中的SQL数量
const selectedSQLCount = computed(() => {
  return pendingSQLList.value.filter(item => item.selected).length
})

// 全选/取消全选
function handleSelectAllChange(val) {
  pendingSQLList.value.forEach(item => {
    item.selected = val
  })
}

// 重试确认
const showRetryConfirm = ref(false)
const retryMessage = ref('')
const lastQuestion = ref('')

// Excel 上传
const uploadedExcel = ref(null) // { fileId, name, columns, rows, preview }
const excelUploadRef = useTemplateRef('excelUploadRef')
const excelUploading = ref(false)

const mdReady = ref(false)
const hljsReady = ref(false)
let hljsLib = null

async function initHeavyDeps() {
  const [, hljsResult] = await Promise.all([
    ensureMd().then(() => { mdReady.value = true }),
    getHljs().then(h => { hljsLib = h; hljsReady.value = true }),
  ])
}

function highlightSql(text) {
  if (!text) return ''
  void hljsReady.value
  if (hljsLib) {
    try {
      return hljsLib.highlight(text, { language: 'sql' }).value
    } catch {
      return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    }
  }
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function preprocessMarkdown(text) {
  if (!text) return ''
  let processed = text
  processed = processed.replace(/\$\\(?:text|textbf|textit)\{([^}]+)\}\$/g, (match, inner) => {
    return inner
  })
  processed = processed.replace(/\$\\(?:bm|mathit|mathrm|mathsf|mathtt)\{([^}]+)\}\$/g, (match, inner) => {
    return inner
  })
  processed = processed.replace(/\*\*\[([^\]]+)\]\(([^)]+)\)\*\*/g, '[$1]($2)')
  processed = processed.replace(/`((\/|\.\/)[^`\s]+\.(xlsx|csv|pdf|txt|zip|json|md))`/g, (match, path) => {
    const filename = path.substring(path.lastIndexOf('/') + 1)
    return `[${filename}](${path})`
  })
  processed = processed.replace(/\[([^\]]+)\]\(([^)]*)\)/g, (match, linkText, url) => {
    if (!url || url.length === 0) {
      return match
    }
    let fullUrl = url
    if (url.startsWith('/') && !url.startsWith('//')) {
      fullUrl = apiBase + url
    }
    let exportAttr = ''
    if (fullUrl && fullUrl.includes('/exports/')) {
      exportAttr = ' data-export-link="true"'
    }
    return `<a href="${fullUrl}" target="_blank" rel="noopener noreferrer"${exportAttr}>${linkText}</a>`
  })
  return processed
}

function renderMarkdown(text) {
  if (!text) return ''
  void mdReady.value
  if (!md) {
    return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n/g, '<br>')
  }
  try {
    const processed = preprocessMarkdown(text)
    return md.render(processed)
  } catch (e) {
    console.error('Markdown parse error:', e)
    return text
  }
}

// 缓存版本：用于历史消息，避免重复调用 renderMarkdown 导致 mermaid ID 变化
function getCachedHtml(msg) {
  if (!msg._renderedHtml || msg._lastContent !== msg.content || (mdReady.value && !msg._renderedWithMd)) {
    msg._renderedHtml = renderMarkdown(msg.content)
    msg._lastContent = msg.content
    msg._renderedWithMd = mdReady.value
  }
  return msg._renderedHtml
}

// 渲染 DOM 中所有未处理的 mermaid 容器
let isMermaidRendering = false

async function doRenderMermaidBlocks(scrollAfter = true) {
  if (isMermaidRendering) return
  isMermaidRendering = true
  try {
    await nextTick()
    const containers = document.querySelectorAll('.mermaid-container[data-mermaid-processed="false"]')
    for (const el of containers) {
      // 跳过隐藏的容器（如折叠的思考区域）
      if (el.offsetParent === null) continue
      const id = el.getAttribute('data-mermaid-id')
      const source = el.getAttribute('data-mermaid-source')
        ?.replace(/&quot;/g, '"')
        .replace(/&gt;/g, '>')
        .replace(/&lt;/g, '<')
        .replace(/&amp;/g, '&')
      if (!id || !source) continue
      const trimmed = source.trim()
      if (!trimmed || trimmed.length < 5) continue
      el.setAttribute('data-mermaid-processed', 'true')
      try {
        const mermaidLib = await getMermaid()
        const { svg } = await mermaidLib.render(id, trimmed)
        const escapedSource = source.replace(/</g, '&lt;').replace(/>/g, '&gt;')
        el.innerHTML =
          `<div class="mermaid-content-wrapper">` +
            `<div class="mermaid-svg-wrap" data-scale="1" data-translate-x="0" data-translate-y="0">${svg}</div>` +
            `<pre class="mermaid-source-preview" style="display:none;"><code>${escapedSource}</code></pre>` +
          `</div>` +
          `<div class="mermaid-resize-handle" title="拖拽调整高度">` +
            `<span class="mermaid-resize-dots">⋯</span>` +
          `</div>` +
          `<div class="mermaid-toolbar">` +
            `<button class="mermaid-tb-btn" data-action="zoom-out" title="缩小">` +
              `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="8" y1="11" x2="14" y2="11"/></svg>` +
            `</button>` +
            `<button class="mermaid-tb-btn" data-action="zoom-reset" title="重置">1:1</button>` +
            `<button class="mermaid-tb-btn" data-action="zoom-in" title="放大">` +
              `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>` +
            `</button>` +
            `<span class="mermaid-tb-sep"></span>` +
            `<button class="mermaid-tb-btn" data-action="toggle-source" title="源码/图表">` +
              `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>` +
            `</button>` +
            `<button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">` +
              `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>` +
            `</button>` +
          `</div>`
      } catch (e) {
        console.warn('Mermaid render error:', e)
        el.innerHTML = `<pre class="mermaid-error"><code>${source.replace(/</g, '&lt;').replace(/>/g, '&gt;')}</code></pre>`
      }
    }
    if (scrollAfter && containers.length > 0) {
      await nextTick()
      if (msgContainer.value) {
        msgContainer.value.scrollTop = msgContainer.value.scrollHeight
      }
    }
  } finally {
    isMermaidRendering = false
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (msgContainer.value) {
      msgContainer.value.scrollTop = msgContainer.value.scrollHeight
    }
  })
}

// 流式输出中检测已完成的 mermaid 代码块并立即渲染
// 原理：统计 streamingContent 中 ```mermaid 开始标记和对应的 ``` 结束标记数量，
// 当有新的完整代码块闭合时触发渲染（带防抖）
let lastRenderedMermaidCount = 0
let mermaidRenderTimer = null

function tryRenderStreamingMermaid() {
  const content = streamingContent.value
  if (!content.includes('```mermaid')) return

  // 统计已闭合的 mermaid 代码块数量
  let closedCount = 0
  let searchFrom = 0
  while (true) {
    const startIdx = content.indexOf('```mermaid', searchFrom)
    if (startIdx === -1) break
    const afterStart = startIdx + '```mermaid'.length
    // 找到对应的闭合 ```（跳过开始标记同行的内容）
    const lineEnd = content.indexOf('\n', afterStart)
    if (lineEnd === -1) break
    const closeIdx = content.indexOf('\n```', lineEnd)
    if (closeIdx === -1) break
    closedCount++
    searchFrom = closeIdx + 4
  }

  if (closedCount > lastRenderedMermaidCount) {
    lastRenderedMermaidCount = closedCount
    // 防抖：300ms 内不重复触发
    if (mermaidRenderTimer) clearTimeout(mermaidRenderTimer)
    mermaidRenderTimer = setTimeout(() => {
      doRenderMermaidBlocks(true)
      mermaidRenderTimer = null
    }, 300)
  }
}

function toggleThinking(msg) {
  msg.collapsed = !msg.collapsed
  if (!msg.collapsed) {
    nextTick(() => doRenderMermaidBlocks(false))
  }
}

const mermaidDragState = {
  isDragging: false,
  startX: 0,
  startY: 0,
  startTx: 0,
  startTy: 0,
  activeContainer: null,
}

function updateMermaidWrapTransform(wrap) {
  const s = parseFloat(wrap.dataset.scale || 1)
  const tx = parseFloat(wrap.dataset.translateX || 0)
  const ty = parseFloat(wrap.dataset.translateY || 0)
  wrap.style.transform = `translate(${tx}px, ${ty}px) scale(${s})`
}

function handleMermaidWheel(e) {
  if (!e.ctrlKey) return
  const container = e.target.closest('.mermaid-container')
  if (!container) return
  e.preventDefault()
  e.stopPropagation()

  const wrap = container.querySelector('.mermaid-svg-wrap')
  if (!wrap) return

  const oldScale = parseFloat(wrap.dataset.scale || 1)
  const delta = e.deltaY > 0 ? -0.1 : 0.1
  const newScale = Math.min(5, Math.max(0.25, +(oldScale + delta).toFixed(2)))
  if (newScale === oldScale) return

  const rect = container.getBoundingClientRect()
  const mx = e.clientX - rect.left
  const my = e.clientY - rect.top

  const oldTx = parseFloat(wrap.dataset.translateX || 0)
  const oldTy = parseFloat(wrap.dataset.translateY || 0)

  const ratio = newScale / oldScale
  const newTx = mx - (mx - oldTx) * ratio
  const newTy = my - (my - oldTy) * ratio

  wrap.dataset.scale = newScale
  wrap.dataset.translateX = +newTx.toFixed(1)
  wrap.dataset.translateY = +newTy.toFixed(1)
  updateMermaidWrapTransform(wrap)
}

function handleMermaidMouseDown(e) {
  if (e.button !== 0) return
  const container = e.target.closest('.mermaid-container')
  if (!container) return
  if (e.target.closest('.mermaid-toolbar')) return
  if (e.target.closest('.mermaid-resize-handle')) return

  // 处理工具栏按钮点击（事件委托）
  const btn = e.target.closest('.mermaid-tb-btn')
  if (btn) return // 工具栏按钮不触发拖拽

  const wrap = container.querySelector('.mermaid-svg-wrap')
  if (!wrap) return

  e.preventDefault()
  mermaidDragState.isDragging = true
  mermaidDragState.startX = e.clientX
  mermaidDragState.startY = e.clientY
  mermaidDragState.startTx = parseFloat(wrap.dataset.translateX || 0)
  mermaidDragState.startTy = parseFloat(wrap.dataset.translateY || 0)
  mermaidDragState.activeContainer = container

  document.body.classList.add('mermaid-dragging')
}

// 工具栏按钮事件委托
function handleMermaidToolbarClick(e) {
  const btn = e.target.closest('.mermaid-tb-btn[data-action]')
  if (!btn) return
  const container = btn.closest('.mermaid-container')
  if (!container) return
  const wrap = container.querySelector('.mermaid-svg-wrap')
  const action = btn.dataset.action

  switch (action) {
    case 'zoom-in': {
      if (!wrap) break
      const s = Math.min(5, +(parseFloat(wrap.dataset.scale || 1) + 0.25).toFixed(2))
      wrap.dataset.scale = s
      updateMermaidWrapTransform(wrap)
      break
    }
    case 'zoom-out': {
      if (!wrap) break
      const s = Math.max(0.25, +(parseFloat(wrap.dataset.scale || 1) - 0.25).toFixed(2))
      wrap.dataset.scale = s
      updateMermaidWrapTransform(wrap)
      break
    }
    case 'zoom-reset': {
      if (!wrap) break
      wrap.dataset.scale = 1
      wrap.dataset.translateX = 0
      wrap.dataset.translateY = 0
      updateMermaidWrapTransform(wrap)
      break
    }
    case 'toggle-source': {
      const src = container.querySelector('.mermaid-source-preview')
      if (!src || !wrap) break
      const showing = src.style.display !== 'none'
      src.style.display = showing ? 'none' : 'block'
      wrap.style.display = showing ? 'block' : 'none'
      break
    }
    case 'copy-source': {
      const code = container.querySelector('.mermaid-source-preview code')
      if (!code) break
      navigator.clipboard.writeText(code.textContent).then(() => {
        const origHtml = btn.innerHTML
        btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#52c41a" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>'
        setTimeout(() => { btn.innerHTML = origHtml }, 1200)
      }).catch(() => {})
      break
    }
  }
}

function handleMermaidMouseMove(e) {
  if (!mermaidDragState.isDragging) return
  const container = mermaidDragState.activeContainer
  if (!container) return

  const wrap = container.querySelector('.mermaid-svg-wrap')
  if (!wrap) return

  const dx = e.clientX - mermaidDragState.startX
  const dy = e.clientY - mermaidDragState.startY

  wrap.dataset.translateX = +(mermaidDragState.startTx + dx).toFixed(1)
  wrap.dataset.translateY = +(mermaidDragState.startTy + dy).toFixed(1)
  updateMermaidWrapTransform(wrap)
}

function handleMermaidMouseUp() {
  if (!mermaidDragState.isDragging) return
  mermaidDragState.isDragging = false
  mermaidDragState.activeContainer = null
  document.body.classList.remove('mermaid-dragging')
}

function handleMermaidKeyDown(e) {
  if (e.key === 'Control' && !e.repeat) {
    document.body.classList.add('mermaid-ctrl-held')
  }
}

function handleMermaidKeyUp(e) {
  if (e.key === 'Control') {
    document.body.classList.remove('mermaid-ctrl-held')
  }
}

// ── Mermaid 容器高度拖拽调整 ──
const mermaidResizeState = {
  isResizing: false,
  startY: 0,
  startHeight: 0,
  activeWrapper: null,
  activeContainer: null,
}

function handleMermaidResizeDown(e) {
  if (e.button !== 0) return
  const handle = e.target.closest('.mermaid-resize-handle')
  if (!handle) return
  const container = handle.closest('.mermaid-container')
  if (!container) return
  const wrapper = container.querySelector('.mermaid-content-wrapper')
  if (!wrapper) return

  e.preventDefault()
  e.stopPropagation()

  mermaidResizeState.isResizing = true
  mermaidResizeState.startY = e.clientY
  mermaidResizeState.startHeight = wrapper.offsetHeight
  mermaidResizeState.activeWrapper = wrapper
  mermaidResizeState.activeContainer = container

  document.body.classList.add('mermaid-resizing')
}

function handleMermaidResizeMove(e) {
  if (!mermaidResizeState.isResizing) return
  const wrapper = mermaidResizeState.activeWrapper
  if (!wrapper) return

  const dy = e.clientY - mermaidResizeState.startY
  const newHeight = Math.max(100, mermaidResizeState.startHeight + dy)
  wrapper.style.maxHeight = newHeight + 'px'

  // 同步更新容器的 max-height（wrapper + toolbar + padding）
  const container = mermaidResizeState.activeContainer
  if (container) {
    container.style.maxHeight = (newHeight + 60) + 'px'
  }
}

function handleMermaidResizeUp() {
  if (!mermaidResizeState.isResizing) return
  mermaidResizeState.isResizing = false
  mermaidResizeState.activeWrapper = null
  mermaidResizeState.activeContainer = null
  document.body.classList.remove('mermaid-resizing')
}

function stopGeneration() {
  if (abortController.value) {
    abortController.value.abort()
    abortController.value = null
  }
}

async function sendMessage() {
  const text = question.value.trim()
  if (!text && !uploadedExcel.value) return
  if (loading.value) return

  // 重置状态
  resetDetectFlag()
  pendingSQLList.value = []
  showRetryConfirm.value = false
  lastQuestion.value = text

  // 构建消息内容
  let messageContent = text
  let excelContext = null

  if (uploadedExcel.value) {
    const excel = uploadedExcel.value
    excelContext = {
      fileId: excel.fileId,
      columns: excel.columns,
      totalRows: excel.rows,
    }
    if (!text) {
      messageContent = `请将上传的 Excel 数据导入数据库。文件ID：${excel.fileId}，Excel 列名：${excel.columns.join(', ')}，共 ${excel.rows} 行数据。`
    } else {
      messageContent += `\n\n[Excel 文件上下文] 文件ID：${excel.fileId}，列名：${excel.columns.join(', ')}，共 ${excel.rows} 行数据。`
    }
  }

  chatHistory.value.push({ role: 'user', content: text || '导入 Excel 数据' })
  question.value = ''
  loading.value = true
  thinkingText.value = ''
  streamingContent.value = ''
  lastRenderedMermaidCount = 0
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
    chatHistory.value.pop()
    return
  }

  if (!validateExtractedSchemas()) {
    // 不阻止发送，但已给出警告
  }

  try {
    const primaryConnId = getPrimaryConnId()
    const primarySchema = schemas.length > 0 ? schemas[0].schema : ''
    const body = {
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

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    const collectedDangerSQLs = []

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })

      const lines = buf.split('\n')
      buf = lines.pop()

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
              collectedDangerSQLs.push({ sql: chunk.sql || chunk.content, interruptId: chunk.interruptId || '', checkPointId: chunk.checkPointId || '' })
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
                const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(content.trim())
                chatHistory.value.push({ role: 'assistant', content, hasSql: isSql })
                if (isSql) lastSql.value = content
                streamingContent.value = ''
              }
              // 然后再添加错误消息，确保显示在结果区域下方
              chatHistory.value.push({ role: 'assistant', content: '❌ ' + (sanitizeError(chunk.content) || 'AI 服务错误') })
              scrollToBottom()
              break
            case 'done':
              break
          }
        } catch (_) { }
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
      const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(content.trim())
      chatHistory.value.push({ role: 'assistant', content, hasSql: isSql })
      if (isSql) lastSql.value = content
      streamingContent.value = ''
    }

    // 处理收集到的危险 SQL
    if (collectedDangerSQLs.length > 0) {
      // 所有 danger_confirm 来自同一个 checkpoint，收集所有 interruptId
      const checkPointId = collectedDangerSQLs[0].checkPointId
      const allInterruptIds = collectedDangerSQLs.map(d => d.interruptId)

      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, allInterruptIds, checkPointId)
      } else {
        // 多条 SQL：合并显示，支持逐条选择和批量确认
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        // 保存 interruptIds 和 checkPointId 供批量确认使用
        pendingSQLList.interruptIds = allInterruptIds
        pendingSQLList.checkPointId = checkPointId
      }
    }

    // 清除已上传的 Excel
    if (uploadedExcel.value) {
      uploadedExcel.value = null
    }
  } catch (e) {
    if (e.name === 'AbortError') {
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
    // 流结束后渲染 mermaid 图表
    scrollToBottom()
    doRenderMermaidBlocks()
  }
}

// 显示确认区域
function showConfirmDialog(sql, interruptIds, checkPointId) {
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

// 重置检测标记
function resetDetectFlag() {
  hasShownConfirm = false
}

// 处理确认执行
async function handleConfirmExec(confirmedSql) {
  loading.value = true
  confirmVisible.value = false

  // 将已确认的 SQL 保留在聊天记录中（无论后续执行成功与否）
  const sqlForDisplay = confirmSQL.value
  chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`` })
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

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    streamingExecContent.value = '' // 清空之前的内容
    const collectedDangerSQLs = []
    let hasError = false
    let errorMsg = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
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
            // Agent 恢复执行后又遇到新的危险 SQL
            collectedDangerSQLs.push({
              sql: chunk.sql || chunk.content,
              interruptId: chunk.interruptId || '',
              checkPointId: chunk.checkPointId || ''
            })
          }
          if (chunk.type === 'error') {
            hasError = true
            errorMsg = chunk.content || '执行失败'
          }
        } catch (_) { }
      }
    }

    // 将流式内容添加到聊天记录
    const execContent = streamingExecContent.value
    streamingExecContent.value = '' // 清空流式内容

    // 更新执行结果到聊天记录
    if (hasError) {
      chatHistory.value.push({ role: 'assistant', content: `❌ 执行失败：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`\n${sanitizeError(errorMsg)}` })
    } else {
      chatHistory.value.push({ role: 'assistant', content: `✅ 已执行：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`` })
    }

    // 如果 Agent 继续输出了内容（比如执行结果说明），追加到聊天记录
    if (execContent) {
      chatHistory.value.push({ role: 'assistant', content: execContent })
    }

    scrollToBottom()

    // 如果恢复执行后又遇到新的危险 SQL，弹出确认框
    if (collectedDangerSQLs.length > 0) {
      loading.value = false
      abortController.value = null
      const cpId = collectedDangerSQLs[0].checkPointId
      const ids = collectedDangerSQLs.map(d => d.interruptId)
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, ids, cpId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        pendingSQLList.interruptIds = ids
        pendingSQLList.checkPointId = cpId
      }
      return
    }
  } catch (e) {
    if (e.name === 'AbortError') {
      chatHistory.value.push({ role: 'assistant', content: `⏹ 已终止：\n\`\`\`sql\n${sqlForDisplay}\n\`\`\`` })
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

// 处理取消确认 — 向后端发送 confirmed=false 的 Resume 请求，清理 checkpoint 状态
async function handleConfirmCancel() {
  confirmVisible.value = false
  chatHistory.value.push({ role: 'assistant', content: `已取消执行：\n\`\`\`sql\n${confirmSQL.value}\n\`\`\`` })
  scrollToBottom()

  // 向后端发送取消请求，确保 checkpoint 状态被正确处理
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
  } catch (_) {
    // 取消请求失败不影响用户体验
  }
}

// ── 多条 SQL 批量确认 ──
async function handleConfirmSelectedSQL() {
  const selectedItems = pendingSQLList.value.filter(item => item.selected)
  if (selectedItems.length === 0) {
    ElMessage.warning('请至少选择一条SQL')
    return
  }

  const allItems = [...pendingSQLList.value]
  const allInterruptIds = pendingSQLList.interruptIds || []
  const checkPointId = pendingSQLList.checkPointId || ''

  // 获取选中项对应的 interruptIds
  const selectedInterruptIds = []
  for (let i = 0; i < allItems.length; i++) {
    if (allItems[i].selected && allInterruptIds[i]) {
      selectedInterruptIds.push(allInterruptIds[i])
    }
  }

  pendingSQLList.value = []
  selectAllChecked.value = false
  loading.value = true

  // 将选中的 SQL 保留在聊天记录中
  for (const item of selectedItems) {
    chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${item.sql}\n\`\`\`` })
  }
  scrollToBottom()

  // 一次性 Resume 所有选中的 interruptId
  await executeBatchResume(selectedItems, selectedInterruptIds, checkPointId)

  loading.value = false
  scrollToBottom()
}

// 批量恢复执行：一次 Resume 传入所有 interruptId
async function executeBatchResume(sqlItems, interruptIds, checkPointId) {
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

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    streamingExecContent.value = ''
    const collectedDangerSQLs = []
    let hasError = false
    let errorMsg = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
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
              checkPointId: chunk.checkPointId || ''
            })
          }
          if (chunk.type === 'error') { hasError = true; errorMsg = chunk.content || 'exec failed' }
        } catch (_) { }
      }
    }

    // 将流式内容添加到聊天记录
    const execContent = streamingExecContent.value
    streamingExecContent.value = ''

    // 更新所有 SQL 的执行状态
    for (const item of sqlItems) {
      chatHistory.value.push({ role: 'assistant', content: hasError
          ? `❌ 执行失败：\n\`\`\`sql\n${item.sql}\n\`\`\``
          : `✅ 已执行：\n\`\`\`sql\n${item.sql}\n\`\`\``
      })
    }

    if (execContent) {
      chatHistory.value.push({ role: 'assistant', content: execContent })
    }

    if (collectedDangerSQLs.length > 0) {
      const cpId = collectedDangerSQLs[0].checkPointId
      const ids = collectedDangerSQLs.map(d => d.interruptId)
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, ids, cpId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        pendingSQLList.interruptIds = ids
        pendingSQLList.checkPointId = cpId
      }
    }
  } catch (e) {
    streamingExecContent.value = ''
    ElMessage({ message: sanitizeError(e) || 'exec failed', type: 'error' })
  }
}

async function handleCancelAllSQL() {
  const items = pendingSQLList.value
  const allInterruptIds = pendingSQLList.interruptIds || []
  const checkPointId = pendingSQLList.checkPointId || ''
  pendingSQLList.value = []
  selectAllChecked.value = false
  // 将每条 SQL 保留在聊天记录中
  for (const item of items) {
    chatHistory.value.push({ role: 'assistant', content: `已取消执行：\n\`\`\`sql\n${item.sql}\n\`\`\`` })
  }
  scrollToBottom()

  // 向后端发送取消请求，确保 checkpoint 状态被正确处理
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
    } catch (_) {
      // 取消请求失败不影响用户体验
    }
  }
}

async function executeConfirmedSQL(sqlText, interruptId, checkPointId) {
  // 将 SQL 保留在聊天记录中
  chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${sqlText}\n\`\`\`` })
  scrollToBottom()

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
        interruptIds: [interruptId],
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

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    streamingExecContent.value = ''
    const collectedDangerSQLs = []
    let hasError = false
    let errorMsg = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
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
              checkPointId: chunk.checkPointId || ''
            })
          }
          if (chunk.type === 'error') {
            hasError = true
            errorMsg = chunk.content || 'exec failed'
          }
        } catch (_) { }
      }
    }

    // 将流式内容添加到聊天记录
    const execContent = streamingExecContent.value
    streamingExecContent.value = ''

    // 更新执行结果
    if (hasError) {
      chatHistory.value.push({ role: 'assistant', content: `❌ 执行失败：\n\`\`\`sql\n${sqlText}\n\`\`\`\n${sanitizeError(errorMsg)}` })
    } else {
      chatHistory.value.push({ role: 'assistant', content: `✅ 已执行：\n\`\`\`sql\n${sqlText}\n\`\`\`` })
    }

    if (execContent) {
      chatHistory.value.push({ role: 'assistant', content: execContent })
    }

    // 如果恢复执行后又遇到新的危险 SQL，弹出确认框
    if (collectedDangerSQLs.length > 0) {
      const cpId = collectedDangerSQLs[0].checkPointId
      const ids = collectedDangerSQLs.map(d => d.interruptId)
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, ids, cpId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, ...analysis, selected: true }
        })
        selectAllChecked.value = true
        pendingSQLList.interruptIds = ids
        pendingSQLList.checkPointId = cpId
      }
    }
  } catch (e) {
    streamingExecContent.value = ''
    chatHistory.value.push({ role: 'assistant', content: `❌ 执行失败：\n\`\`\`sql\n${sqlText}\n\`\`\`\n${sanitizeError(e)}` })
  }
}

function getCurrentUser() {
  const userStr = sessionStorage.getItem('currentUser')
  if (userStr) {
    try { const user = JSON.parse(userStr); return user.name || 'anonymous' }
    catch { return 'anonymous' }
  }
  return 'anonymous'
}

// ── 重试 ──
function handleRetryContinue() {
  showRetryConfirm.value = false
  if (lastQuestion.value) {
    question.value = lastQuestion.value
    nextTick(() => sendMessage())
  }
}

// ── Excel 上传 ──
async function handleExcelUpload(file) {
  const rawFile = file.raw || file
  const formData = new FormData()
  formData.append('file', rawFile)

  // 前端文件大小校验（20MB）
  if (rawFile.size > 20 * 1024 * 1024) {
    ElMessage.error('文件大小不能超过 20MB')
    return
  }

  excelUploading.value = true
  try {
    const apiBase = import.meta.env.VITE_API_URL || ''
    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch(apiBase + '/ai/agent/uploadExcel', {
      method: 'POST',
      headers: { 'Authorization': auth },
      body: formData,
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
      const errData = await resp.json().catch(() => ({}))
      throw new Error(sanitizeError(errData.error) || `上传失败：${resp.status}`)
    }
    const data = await resp.json()

    uploadedExcel.value = {
      fileId: data.fileId,
      name: data.fileName,
      columns: data.columns,
      rows: data.totalRows,
    }

    // 在聊天区显示预览
    let previewText = `📎 已上传文件：**${data.fileName}**\n`
    previewText += `共 ${data.totalRows} 行数据，${data.columns.length} 列\n\n`
    previewText += `列名：\`${data.columns.join('`, `')}\`\n\n`
    const previewRows = data.preview || []
    if (previewRows.length > 0) {
      previewText += `前 ${previewRows.length} 行原始数据预览：\n`
      previewText += '| ' + data.columns.join(' | ') + ' |\n'
      previewText += '| ' + data.columns.map(() => '---').join(' | ') + ' |\n'
      for (const row of previewRows) {
        const cells = data.columns.map((_, i) => {
          const val = row[i] !== undefined && row[i] !== null ? String(row[i]) : ''
          // 保留原始内容，仅对超长值截断（50字符）
          return val.length > 50 ? val.substring(0, 50) + '…' : (val || ' ')
        })
        previewText += '| ' + cells.join(' | ') + ' |\n'
      }
      if (data.totalRows > previewRows.length) {
        previewText += `\n*共 ${data.totalRows} 行，以上仅展示前 ${previewRows.length} 行*\n`
      }
    }

    // 如果已选择了相关表，自动预匹配字段
    const primaryConnId = getPrimaryConnId()
    if (primaryConnId && selectedTables.value.length === 1) {
      try {
        const matchResp = await fetch(apiBase + '/ai/agent/preMatchColumns', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': auth },
          body: JSON.stringify({
            fileId: data.fileId,
            connId: primaryConnId,
            tableName: selectedTables.value[0],
          }),
        })
        if (matchResp.ok) {
          const matchData = await matchResp.json()
          previewText += `\n### 字段自动匹配预览（目标表：\`${selectedTables.value[0]}\`）\n`
          previewText += `匹配 ${matchData.matchedCount}/${matchData.totalExcel} 列\n\n`
          previewText += '| Excel 列 | 数据库字段 | 状态 |\n'
          previewText += '| --- | --- | --- |\n'
          for (const m of matchData.matches) {
            const status = m.matched ? '✅ 已匹配' : '❌ 未匹配'
            previewText += `| ${m.excelColumn} | ${m.dbColumn || '-'} | ${status} |\n`
          }
          if (matchData.matchedCount < matchData.totalExcel) {
            previewText += `\n⚠️ 有 ${matchData.totalExcel - matchData.matchedCount} 列未匹配，这些列的数据将不会导入。\n`
          }
        }
      } catch (matchErr) {
        console.warn('[App] 预匹配失败，不影响上传:', matchErr)
      }
    }

    chatHistory.value.push({ role: 'assistant', content: previewText, hasSql: false })
    scrollToBottom()
    doRenderMermaidBlocks()
    ElMessage.success(`已上传 ${data.fileName}，请输入导入指令（如：将数据导入到 xxx 表）`)
  } catch (e) {
    console.error('[App] 上传 Excel 文件失败:', e)
    ElMessage.error('上传 Excel 文件失败，请检查文件格式')
  } finally {
    excelUploading.value = false
  }
}

function clearUploadedExcel() {
  uploadedExcel.value = null
}

function handlePromptSendToAI(content, connInfo) {
  promptPopoverVisible.value = false
  promptEditDialogVisible.value = false
  question.value = content
  if (connInfo && connInfo.connSchemas && connInfo.connSchemas.length > 0) {
    selectedSchemas.value = connInfo.connSchemas.map(cs => cs.connId + '::' + cs.schema)
  }
  if (connInfo && connInfo.tables && connInfo.tables.length > 0) {
    const tableNames = connInfo.tables.map(t => typeof t === 'string' ? t : t.name)
    nextTick(() => {
      loadTableListForSchemas().then(() => {
        const availableNames = tableList.value.map(t => t.label || t.name)
        selectedTables.value = tableNames.filter(t => availableNames.includes(t))
        nextTick(() => sendMessage())
      })
    })
  } else {
    selectedTables.value = []
    nextTick(() => sendMessage())
  }
}

function handlePromptAdd() {
  openPromptEditDialog()
}

function handlePromptEdit(promptId) {
  openPromptEditDialog({ promptId })
}

function handlePromptSaved() {
  loadPrompts()
  triggerPromptSaved()
}

async function loadPrompts() {
  promptLoading.value = true
  try {
    const resp = await http.get('/promptList')
    prompts.value = (resp.data.data || []).map(p => ({
      ...p,
      isShared: p.createdBy !== p.currentUserId && !p.isRolePrompt,
    }))
  } catch (e) {
    console.error('加载提示词列表失败:', e)
  } finally {
    promptLoading.value = false
  }
}

function handleDeletePrompt(prompt) {
  ElMessageBox.confirm(
    `确定要删除提示词 "${prompt.title}" 吗？`,
    '确认删除',
    { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' }
  ).then(async () => {
    try {
      await http.get('/delPrompt', { params: { id: prompt.id } })
      ElMessage.success('删除成功')
      loadPrompts()
    } catch (e) {
      ElMessage.error('删除失败')
    }
  }).catch(() => {})
}

function handleViewPromptDetail(prompt) {
  promptDetail.value = prompt
  promptDetailVisible.value = true
}

function handleSendFromDetail() {
  if (promptDetail.value) {
    handlePromptSendToAI(promptDetail.value.content, {
      connSchemas: promptDetail.value.connSchemas,
      tables: promptDetail.value.tables,
    })
    promptDetailVisible.value = false
  }
}

function clearSession(showMsg) {
  stopGeneration()
  chatHistory.value = []
  sessionId.value = ''
  thinkingText.value = ''
  streamingContent.value = ''
  streamingExecContent.value = ''
  lastSql.value = ''
  confirmVisible.value = false
  confirmSQL.value = ''
  pendingSQLList.value = []
  showRetryConfirm.value = false
  uploadedExcel.value = null
  processedLinks.clear()
  resetDetectFlag()
  if(showMsg) {
    ElMessage({ message: '已新建会话', type: 'success' })
  }
}

function insertToEditor() {
  if (!lastSql.value) return
  // emit('insertSql', lastSql.value.trim())
  // emit('update:modelValue', false)
  console.log('SQL 待插入:', lastSql.value.trim())
}

// --- 语音识别 ---
function initSpeechRecognition() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition
  if (!SR) {
    ElMessage({ message: '浏览器不支持语音识别', type: 'warning' })
    return null
  }
  const recognition = new SR()
  recognition.lang = 'zh-CN'
  recognition.continuous = true
  recognition.interimResults = true
  recognition.onstart = () => { isRecording.value = true }
  recognition.onresult = (event) => {
    let finalTranscript = ''
    for (let i = event.resultIndex; i < event.results.length; i++) {
      if (event.results[i].isFinal) finalTranscript += event.results[i][0].transcript
    }
    if (finalTranscript) question.value += (question.value ? ' ' : '') + finalTranscript
  }
  recognition.onerror = (event) => {
    if (event.error === 'not-allowed') ElMessage({ message: '请允许使用麦克风', type: 'error' })
    isRecording.value = false
  }
  recognition.onend = () => { isRecording.value = false }
  return recognition
}

function toggleRecording() {
  if (isRecording.value) {
    speechRecognition?.stop()
    isRecording.value = false
  } else {
    if (!speechRecognition) speechRecognition = initSpeechRecognition()
    if (!speechRecognition) return
    try {
      speechRecognition.start()
      ElMessage({ message: '开始语音录入...', type: 'info' })
    } catch { ElMessage({ message: '无法启动语音识别', type: 'error' }) }
  }
}

function handleEscKey(e) {
  if (e.key === 'Escape' || e.keyCode === 27) {
    // ESC 键关闭面板的逻辑已移除
  }
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      currentUser.value = {}
      loginSucc.value = false
      canUseClassicView.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
      sessionStorage.removeItem("currentUser")
      sessionStorage.removeItem("isRemote")
    })
}

const loginDialogRef = useTemplateRef('loginDialogRef')

function showLoginDialog() {
  loginDialogVisible.value = true
}

function handleSessionExpired() {
  loginSucc.value = false
  canUseClassicView.value = false
  sessionStorage.removeItem('authentication')
  sessionStorage.removeItem('currentUser')
  sessionStorage.removeItem('isRemote')
  // 延迟一下再尝试登录，确保 UI 状态已更新
  nextTick(() => {
    loginDialogVisible.value = true
  })
}

function handleSessionExpiredEvent(event) {
  if (window.location.pathname === '/classical') {
    return
  }
  const message = event.detail?.message || ''
  if (message) {
    ElMessage({ message: message, type: 'warning' })
  }
  handleSessionExpired()
}

function handleLoginSuccess(userData) {
  currentUser.value = userData
  loginSucc.value = true
  loadConnList().then(() => {
    if (selectedSchemas.value.length > 0) {
      loadTableListForSchemas()
    }
  })
  loadModelList()
  checkClassicViewPermission()
  loadPromptList()
}

function checkClassicViewPermission() {
  http.get('/canUseClassicView').then(resp => {
    canUseClassicView.value = !!(resp.data.data && resp.data.data.allowed)
  }).catch(() => {
    canUseClassicView.value = false
  })
}

function loadPromptList() {
  loadPrompts()
}

function openSystemManagement() {
  console.log('[App.vue] 打开系统管理页面，当前 currentUser:', currentUser.value)
  // 将 currentUser 存储到 sessionStorage
  sessionStorage.setItem('systemManagement_user', JSON.stringify(currentUser.value))
  router.push('/system-management')
}

function getSysModel() {
  http.get("/sysMode").then((resp) => {
    isRemote.value = resp.data?.isRemote ?? resp.data?.data?.isRemote ?? false
    sessionStorage.setItem("isRemote", isRemote.value.toString())
    if (!loginSucc.value && isRemote.value) {
      loginDialogVisible.value = true
    }
  })
}

// --- 历史会话管理 ---
function formatDate(isoString) {
  if (!isoString) {
    return '未知时间'
  }

  const date = new Date(isoString)
  if (isNaN(date.getTime())) {
    return '未知时间'
  }

  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  const seconds = String(date.getSeconds()).padStart(2, '0')

  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}

async function loadSessionList() {
  loadingSessions.value = true
  sessionList.value = []

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/sessions'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'GET',
      headers: { 'Authorization': auth }
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
      throw new Error(`请求失败：${resp.status}`)
    }

    const data = await resp.json()
    const sessions = data.sessions || []
    // 按时间倒序排列（最新的在最上面）
    sessions.sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
    sessionList.value = sessions
  } catch (e) {
    console.error('[App] 加载历史会话失败:', e)
    ElMessage({ message: '加载历史会话失败', type: 'error' })
  } finally {
    loadingSessions.value = false
  }
}

function confirmDeleteSession(id) {
  ElMessageBox.confirm(
    '确定要删除这个会话吗？删除后无法恢复！',
    '删除确认',
    {
      confirmButtonText: '确定删除',
      cancelButtonText: '取消',
      type: 'warning',
    }
  )
    .then(async () => {
      await deleteSession(id)
    })
    .catch(() => {
      // 用户取消
    })
}

async function deleteSession(id) {
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/session/delete?sessionId=' + encodeURIComponent(id)
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'GET',
      headers: { 'Authorization': auth }
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
      throw new Error(`请求失败：${resp.status}`)
    }

    ElMessage({ message: '会话已删除', type: 'success' })
    await loadSessionList() // 刷新列表
  } catch (e) {
    console.error('[App] 删除会话失败:', e)
    ElMessage({ message: '删除会话失败', type: 'error' })
  }
}

function handleClickSession(id) {
  // 先关闭 popover，然后加载会话
  sessionHistoryVisible.value = false
  // 延迟一点时间加载会话，让 popover 先关闭
  setTimeout(() => {
    loadSession(id)
  }, 100)
}

async function loadSession(id) {
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/session?sessionId=' + encodeURIComponent(id)
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'GET',
      headers: { 'Authorization': auth }
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
      throw new Error(`请求失败：${resp.status}`)
    }

    const data = await resp.json()
    if (data.session) {
      // 清空当前会话
      clearSession()

      // 加载历史消息
      sessionId.value = data.session.id
      for (const msg of data.session.messages) {
        const isSql = /^\s*(SELECT|INSERT|UPDATE|DELETE|ALTER|CREATE|DROP|SHOW|DESCRIBE|EXPLAIN|WITH)\s/i.test(msg.content.trim())
        chatHistory.value.push({
          role: msg.role,
          content: msg.content,
          hasSql: isSql,
          collapsed: true
        })
        if (isSql) lastSql.value = msg.content
      }

      // 恢复会话上下文（当时选择的 schemas 和 tables）
      const ctx = data.session.context
      if (ctx && ctx.schemas && ctx.schemas.length > 0) {
        const schemaValues = ctx.schemas
          .filter(s => s.connId && s.schema)
          .map(s => s.connId + '::' + s.schema)
        if (schemaValues.length > 0) {
          selectedSchemas.value = schemaValues
          await loadTableListForSchemas()
          if (ctx.tables && ctx.tables.length > 0) {
            selectedTables.value = ctx.tables.filter(t => tableList.value.some(tl => tl.label === t || tl.name === t))
          }
        }
      }

      ElMessage({ message: '已加载历史会话', type: 'success' })
      scrollToBottom()
      // 历史会话加载完成后立即渲染 mermaid
      doRenderMermaidBlocks()
    }
  } catch (e) {
    console.error('[App] 加载会话失败:', e)
    ElMessage({ message: '加载会话失败', type: 'error' })
  }
}

watch(selectedTables, (val) => {
  try {
    sessionStorage.setItem('lastSelectedTables', JSON.stringify(val))
  } catch (_) {}
}, { deep: true })

function handleExportLinkClick(e) {
  const link = e.target.closest('a[data-export-link]')
  if (!link) return
  const authToken = sessionStorage.getItem('authentication')
  if (!authToken) return
  e.preventDefault()
  let href = link.getAttribute('href')
  const separator = href.includes('?') ? '&' : '?'
  href = href + separator + 'token=' + encodeURIComponent(authToken)
  window.open(href, '_blank')
}

onMounted(() => {
  initHeavyDeps()
  preloadHeavyDeps()
  setSendToAIHandler(handlePromptSendToAI)
  getSysModel()
  loadModelList()
  loadConnList().then(() => {
    if (loginSucc.value || !isRemote.value) {
      loadTableListForSchemas().then(() => {
        const savedTables = sessionStorage.getItem('lastSelectedTables')
        if (savedTables) {
          try {
            const parsedTables = JSON.parse(savedTables)
            if (Array.isArray(parsedTables) && parsedTables.length > 0) {
              selectedTables.value = parsedTables.filter(t => tableList.value.some(tl => tl.label === t || tl.name === t))
            }
          } catch (_) {}
        }
      })
    }
  })
  if (loginSucc.value) {
    checkClassicViewPermission()
  }
  document.addEventListener('keydown', handleEscKey)
  document.addEventListener('keydown', handleMermaidKeyDown)
  document.addEventListener('keyup', handleMermaidKeyUp)
  document.addEventListener('mousemove', handleMermaidMouseMove)
  document.addEventListener('mouseup', handleMermaidMouseUp)
  document.addEventListener('mousemove', handleMermaidResizeMove)
  document.addEventListener('mouseup', handleMermaidResizeUp)
  window.addEventListener('session-expired', handleSessionExpiredEvent)
  document.addEventListener('click', handleExportLinkClick)
  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization
  nextTick(() => {
    if (msgContainer.value) {
      msgContainer.value.addEventListener('wheel', handleMermaidWheel, { passive: false })
      msgContainer.value.addEventListener('mousedown', handleMermaidMouseDown)
      msgContainer.value.addEventListener('mousedown', handleMermaidResizeDown)
      msgContainer.value.addEventListener('click', handleMermaidToolbarClick)
    }
  })
  // 空闲时预加载 Vditor 模块，用户打开编辑器时无需等待
  if (window.requestIdleCallback) {
    window.requestIdleCallback(() => preloadVditor(), { timeout: 5000 })
  } else {
    setTimeout(() => preloadVditor(), 3000)
  }
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscKey)
  document.removeEventListener('keydown', handleMermaidKeyDown)
  document.removeEventListener('keyup', handleMermaidKeyUp)
  document.removeEventListener('mousemove', handleMermaidMouseMove)
  document.removeEventListener('mouseup', handleMermaidMouseUp)
  document.removeEventListener('mousemove', handleMermaidResizeMove)
  document.removeEventListener('mouseup', handleMermaidResizeUp)
  window.removeEventListener('session-expired', handleSessionExpiredEvent)
  document.removeEventListener('click', handleExportLinkClick)
  if (msgContainer.value) {
    msgContainer.value.removeEventListener('wheel', handleMermaidWheel)
    msgContainer.value.removeEventListener('mousedown', handleMermaidMouseDown)
    msgContainer.value.removeEventListener('mousedown', handleMermaidResizeDown)
    msgContainer.value.removeEventListener('click', handleMermaidToolbarClick)
  }
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

/* ========== SQL 代码块 - 深空灰 ========== */
.sql-pre {
  margin: 0;
  padding: 12px;
  overflow: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  background: linear-gradient(180deg, #263238 0%, #1c282c 100%);
  color: #cfd8dc;
  border-radius: 8px;
  border: 1px solid rgba(0, 0, 0, 0.2);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.5);
}

.sql-pre::-webkit-scrollbar {
  height: 6px;
}

.sql-pre::-webkit-scrollbar-thumb {
  background: #546e7a;
  border-radius: 3px;
}

.cursor-blink {
  animation: blink 1s step-start infinite;
  font-size: 14px;
  color: #4fc3f7;
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
  color: #2d3748;
  word-wrap: break-word;
  overflow-wrap: break-word;
}

/* ========== 历史会话项样式 ========== */
.session-item {
  display: flex;
  align-items: center;
  padding: 10px 16px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  background: linear-gradient(135deg, #ffffff 0%, #f8fafc 100%);
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
  background: linear-gradient(180deg, #546e7a 0%, #78909c 100%);
  opacity: 0;
  transition: opacity 0.3s ease;
}

.session-item:hover {
  border-color: #90a4ae;
  background: linear-gradient(135deg, #f5f5f5 0%, #eceff1 100%);
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
  color: #2d3748;
  margin-bottom: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 0.3s ease;
}

.session-item:hover .session-title {
  color: #37474f;
}

.session-time {
  font-size: 12px;
  color: #718096;
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
  border-top: 1px solid rgba(226, 232, 240, 0.8);
  padding: 12px 16px;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  box-shadow: 0 -2px 8px rgba(0, 0, 0, 0.05);
  box-sizing: border-box;
}

.input-label {
  margin-bottom: 8px;
  font-size: 13px;
  color: #4a5568;
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
  box-shadow: 0 4px 12px rgba(84, 110, 122, 0.2);
}

/* 输入框美化 */
:deep(.el-textarea__inner) {
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  transition: all 0.3s ease;
  font-size: 14px;
}

:deep(.el-textarea__inner:hover) {
  border-color: #bdbdbd;
  box-shadow: 0 0 0 3px rgba(189, 189, 189, 0.08);
}

:deep(.el-textarea__inner:focus) {
  border-color: #90a4ae;
  box-shadow: 0 0 0 3px rgba(144, 164, 174, 0.12);
}

/* 选择器美化 */
:deep(.el-select) {
  width: 100%;
}

:deep(.el-select .el-input__inner) {
  border-radius: 10px;
}

:deep(.el-select-dropdown__item.selected) {
  background: linear-gradient(90deg, rgba(66, 153, 225, 0.1) 0%, transparent 100%);
  color: #4299e1;
  font-weight: 600;
}

/* 空状态美化 */
:deep(.el-empty) {
  padding: 20px 0;
}

:deep(.el-empty__description) {
  color: #718096;
  font-size: 13px;
}

/* 骨架屏美化 */
:deep(.el-skeleton) {
  border-radius: 8px;
}

:deep(.el-skeleton__item) {
  background: linear-gradient(90deg, #f0f0f0 25%, #e0e0e0 50%, #f0f0f0 75%);
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

/* 滚动条全局美化 - 蓝灰色 */
:deep(.el-drawer__body) {
  padding: 3px;
  scrollbar-width: thin;
  scrollbar-color: #78909c rgba(84, 110, 122, 0.05);
}

:deep(.el-drawer__header) {
  margin-bottom: 3px;
}

/* Popover 美化 */
:deep(.el-popover) {
  border-radius: 12px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

/* ========== 输入区域布局 ========== */
.input-action-row {
  display: flex;
  gap: 12px;
  margin-top: 12px;
  flex-shrink: 0;
}

.schema-hints {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
}

.schema-hints-label {
  font-size: 12px;
  color: #909399;
}

.schema-hints-warn {
  font-size: 12px;
  color: #f56c6c;
}

.question-input {
  flex: 1;
}

.question-input :deep(.el-textarea__inner) {
  border-radius: 12px;
  border: 1.5px solid #e0e0e0;
  transition: all 0.3s ease;
  font-size: 14px;
  line-height: 1.6;
  background: rgba(255, 255, 255, 0.98);
  backdrop-filter: blur(10px);
}

.question-input :deep(.el-textarea__inner:hover) {
  border-color: #bdbdbd;
  box-shadow: 0 0 0 3px rgba(189, 189, 189, 0.08);
}

.question-input :deep(.el-textarea__inner:focus) {
  border-color: #90a4ae;
  box-shadow: 0 0 0 3px rgba(144, 164, 174, 0.12);
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

/* 加入编辑器按钮美化 - 柔和青绿 */
.insert-btn {
  border-radius: 8px;
  font-weight: 500;
  font-size: 13px;
  padding: 8px 12px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  border: 1.5px solid #26a69a;
  background: linear-gradient(135deg, #4db6ac 0%, #26a69a 100%);
  color: #fff;
  box-shadow: 0 2px 8px rgba(77, 182, 172, 0.2);
  min-width: 38px;
  min-height: 38px;
}

.insert-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(77, 182, 172, 0.3);
  background: linear-gradient(135deg, #26a69a 0%, #00897b 100%);
  border-color: #00897b;
}

.insert-btn .el-icon {
  margin-right: 4px;
  font-size: 16px;
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
  flex: 0 0 calc(50% - 8px);
}

.table-selector-row .model-selector-container {
  flex: 0 0 calc(30% - 8px);
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
  color: #4a5568;
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
  background: linear-gradient(135deg, #e8eaf6 0%, #c5cae9 100%);
  color: #283593;
  letter-spacing: 0.3px;
  transition: all 0.3s ease;
  white-space: nowrap;
}

.selector-badge.ready {
  background: linear-gradient(135deg, #e8f5e9 0%, #c8e6c9 100%);
  color: #2e7d32;
}

@keyframes badgePulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.selector-skeleton {
  width: 100%;
  height: 32px;
  border-radius: 10px;
  border: 1.5px solid #e0e0e0;
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
  color: #c0c4cc;
  pointer-events: none;
  letter-spacing: 0.3px;
}

.skeleton-shimmer {
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, #f5f7fa 0%, #e8eaed 35%, #f5f7fa 65%);
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
  color: #1a202c;
  font-size: 13px;
  flex-shrink: 0;
}

.table-option-comment {
  font-size: 12px;
  color: #909399;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.table-option-comment::before {
  content: '— ';
  color: #c0c4cc;
}

.table-option-schema {
  font-size: 11px;
  color: #409eff;
  background: rgba(64, 158, 255, 0.1);
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
  color: #1a202c;
  font-size: 13px;
  flex: 1;
}

.model-option-provider {
  font-size: 11px;
  color: #909399;
  background: #f0f0f0;
  padding: 1px 6px;
  border-radius: 4px;
  flex-shrink: 0;
}

/* 多条 SQL 批量确认 */
.multi-sql-confirm {
  border: 2px solid #e6a23c;
  border-radius: 8px;
  padding: 16px;
  background: #fdf6ec;
  margin: 8px 16px;
  flex-shrink: 0;
}
.sql-confirm-item {
  margin: 8px 0;
  padding: 8px;
  background: #fff;
  border-radius: 6px;
  border: 1px solid #ebeef5;
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
  background: #f5f7fa;
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
  border: 1px solid #e6a23c;
  border-radius: 8px;
  padding: 16px;
  background: #fdf6ec;
  margin: 8px 16px;
  flex-shrink: 0;
}

/* Excel 上传 */
.uploaded-file-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background: #ecf5ff;
  border-radius: 6px;
  font-size: 13px;
  color: #409eff;
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

.prompt-popover-body .prompt-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
}

.prompt-popover-body .prompt-empty {
  text-align: center;
  color: #909399;
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
  background: #f5f7fa;
}

.prompt-popover-body .prompt-item-info {
  flex: 1;
  min-width: 0;
}

.prompt-popover-body .prompt-item-title {
  font-size: 14px;
  color: #303133;
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
  color: #909399;
  margin-top: 2px;
}

.prompt-popover-body .prompt-item-actions {
  display: flex;
  gap: 0;
  opacity: 0;
  transition: opacity 0.2s;
  flex-shrink: 0;
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
}
.markdown-body pre code {
  display: block;
  padding: 5px;
  margin: 0;
  overflow: visible;
  line-height: inherit;
  word-wrap: normal;
  background-color: transparent;
  border-radius: 0;
  /* color: #cfd8dc; */
  white-space: pre;
  border: none;
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
  background: #ffffff;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
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
  max-height: 540px;
  overflow: auto;
  position: relative;
  z-index: 0;
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
.mermaid-error {
  margin: 0;
  padding: 12px;
  background: #fff5f5;
  border: 1px solid #fed7d7;
  border-radius: 6px;
  color: #c53030;
  font-size: 12px;
  text-align: left;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 300px;
  overflow: auto;
}
.mermaid-toolbar {
  position: absolute;
  top: 8px;
  right: 8px;
  display: flex;
  align-items: center;
  gap: 2px;
  z-index: 100;
  background: rgba(255,255,255,0.98);
  backdrop-filter: blur(8px);
  border-radius: 6px;
  padding: 4px;
  border: 1px solid #e2e8f0;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
  opacity: 0;
  transition: opacity 0.2s ease;
}
.mermaid-container:hover .mermaid-toolbar {
  opacity: 1;
}
.mermaid-tb-sep {
  width: 1px;
  height: 16px;
  background: #e2e8f0;
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
  color: #64748b;
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
  color: #1976d2;
  background: #eff6ff;
  border-color: #bfdbfe;
}
.mermaid-tb-btn:active {
  background: #dbeafe;
}
.mermaid-tb-btn svg {
  flex-shrink: 0;
}
.mermaid-svg-wrap {
  text-align: center;
  transform-origin: 0 0;
  transition: transform 0.15s ease;
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
  border-top: 1px solid #e2e8f0;
  margin-top: 4px;
  opacity: 0;
  transition: opacity 0.2s ease;
}
.mermaid-container:hover .mermaid-resize-handle {
  opacity: 1;
}
.mermaid-resize-dots {
  font-size: 14px;
  color: #94a3b8;
  letter-spacing: 2px;
  line-height: 1;
}
.mermaid-resize-handle:hover .mermaid-resize-dots {
  color: #1976d2;
}
body.mermaid-resizing,
body.mermaid-resizing * {
  cursor: ns-resize !important;
  user-select: none !important;
}
</style>
