<template>
  <div>
    <router-view v-if="['/system-management', '/role-permission', '/classical'].includes($route.path)" />
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
              <div class="bubble-content" style="white-space: pre-wrap;">{{ msg.content }}</div>
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
          <div v-for="(item, idx) in pendingSQLList" :key="idx" class="sql-confirm-item">
            <div class="sql-confirm-header">
              <el-tag :type="item.riskLevel === 'high' ? 'danger' : 'warning'" size="small">
                {{ item.riskLevel === 'high' ? '高危' : '中危' }} - {{ item.type }}
              </el-tag>
              <span v-if="item.tableName" style="font-size:12px;color:#909399;">表：{{ item.tableName }}</span>
            </div>
            <!-- <pre class="sql-preview-code">{{ item.sql }}</pre> -->
            <pre class="sql-pre"><code v-html="highlightSql(item.sql)" /></pre>
          </div>
          <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;">
            <el-button size="small" @click="handleCancelAllSQL">全部取消</el-button>
            <el-button size="small" type="danger" @click="handleConfirmAllSQL">
              确认执行全部 ({{ pendingSQLList.length }} 条)
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
                :on-change="handleExcelUpload">
                <el-button class="toolbar-btn" size="small" title="上传 Excel 导入数据">
                  <el-icon><Upload /></el-icon>
                </el-button>
              </el-upload>
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
            <div class="table-selector-container" v-if="connList.length > 1">
              <label class="table-selector-label">数据库</label>
              <el-tree-select
                v-model="connId"
                :data="processedConnList"
                :props="{ label: 'label', value: 'value', children: 'children', disabled: 'isDir' }"
                placeholder="选择数据库"
                class="table-selector"
                @change="handleConnChange"
                filterable
                check-strictly
                :check-on-click-node="false"
              />
            </div>
            <div class="table-selector-container" :class="{ 'full-width': connList.length <= 1 }">
              <label class="table-selector-label">相关表</label>
              <el-select v-model="selectedTables" multiple filterable placeholder="选择相关表（可多选）" class="table-selector">
                <el-option v-for="table in tableList" :key="table.name"
                  :label="table.comment ? table.name + '（' + table.comment + '）' : table.name"
                  :value="table.name" />
              </el-select>
            </div>
          </div>

          <div class="input-action-row">
            <div style="flex:1;display:flex;flex-direction:column;gap:6px;">
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
              <router-link to="/classical" class="switch-view-link" title="经典视图">
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
          <el-button v-if="loginSucc" circle size="small" @click="promptDrawerVisible = true" title="常用提示词">
            <el-icon>
              <ChatLineRound />
            </el-icon>
          </el-button>
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
    <PromptDrawer ref="promptDrawerRef" v-model="promptDrawerVisible" @add="handlePromptAdd" @edit="handlePromptEdit" @send-to-AI="handleSendPromptToAI" />
    <PromptEditDialog v-model="promptEditDialogVisible" :prompt-id="editingPromptId" @saved="handlePromptSaved" @send-to-AI="handleSendPromptToAI" />
  </div>
</template>

<script setup>
import SQLConfirmInline from '@/components/SQLConfirmInline.vue'
import PromptDrawer from '@/components/PromptDrawer.vue'
import PromptEditDialog from '@/components/PromptEditDialog.vue'
import { preloadVditor } from '@/utils/vditorLoader'
import LoginDialog from '@/components/LoginDialog.vue'
import http from '@/js/utils/httpProxy.js'
import { sanitizeError } from '@/utils/errorHandler.js'
import { analyzeSQL } from '@/utils/sqlRiskAssessment'
import { ChatLineRound, Clock, Delete, Document, DocumentAdd, Microphone, Promotion, Setting, SwitchButton, Upload, User, VideoPause } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import hljs from 'highlight.js/lib/core'
import hljsSql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/stackoverflow-light.css'
import MarkdownIt from 'markdown-it'
import mermaid from 'mermaid'
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

hljs.registerLanguage('sql', hljsSql)

// 初始化 mermaid
mermaid.initialize({
  startOnLoad: false,
  theme: 'default',
  securityLevel: 'loose',
  fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
})

let mermaidIdCounter = 0

// 获取 API 基础路径
const apiBase = import.meta.env.VITE_API_URL || ''

// 初始化 markdown-it
const md = new MarkdownIt({
  html: true,
  breaks: true,
  linkify: true,
  typographer: false,
})

// 自定义链接渲染
md.renderer.rules.link_open = function (tokens, idx, options, env, self) {
  const token = tokens[idx]
  const hrefIndex = token.attrIndex('href')

  if (hrefIndex >= 0) {
    let href = token.attrs[hrefIndex][1]

    // 处理相对路径：如果以 / 开头且不是 // 开头，添加 apiBase
    if (href && href.startsWith('/') && !href.startsWith('//')) {
      // 更新 href 属性
      href = apiBase + href
      token.attrs[hrefIndex][1] = href
    }

    // 自动添加登录 token：如果是导出链接，添加认证参数
    const authToken = sessionStorage.getItem('authentication')
    if (authToken && href && href.includes('/exports/')) {
      // 检查 URL 是否已有查询参数
      const separator = href.includes('?') ? '&' : '?'
      token.attrs[hrefIndex][1] = href + separator + 'token=' + encodeURIComponent(authToken)
    }

    // 所有链接都添加 target="_blank"
    const targetIndex = token.attrIndex('target')
    if (targetIndex < 0) {
      token.attrPush(['target', '_blank'])
    } else {
      token.attrs[targetIndex][1] = '_blank'
    }

    // 外部链接额外添加 rel 属性
    if (href.startsWith('http://') || href.startsWith('https://')) {
      token.attrPush(['rel', 'noopener noreferrer'])
    }
  }

  // 使用默认的 renderToken 方法渲染 token
  return self.renderToken(tokens, idx, options)
}

// 自定义表格渲染，添加滚动容器
const defaultTableRender = md.renderer.rules.table_open
md.renderer.rules.table_open = function (tokens, idx, options, env, self) {
  return '<div class="table-wrapper"><table>'
}
const defaultTableCloseRender = md.renderer.rules.table_close
md.renderer.rules.table_close = function (tokens, idx, options, env, self) {
  return '</table></div>'
}

// 自定义 fence 渲染：mermaid 代码块转为待渲染容器，其余保持默认
const defaultFenceRender = md.renderer.rules.fence || function (tokens, idx, options, env, self) {
  return self.renderToken(tokens, idx, options)
}
md.renderer.rules.fence = function (tokens, idx, options, env, self) {
  const token = tokens[idx]
  const info = token.info ? token.info.trim().toLowerCase() : ''
  if (info === 'mermaid') {
    const id = 'mermaid-' + (++mermaidIdCounter)
    // 对内容进行 HTML 转义，防止 XSS，mermaid.render 会处理原始文本
    const escaped = token.content
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
    // 输出占位容器：流式输出时显示源码预览，流结束后由 doRenderMermaidBlocks 替换为 SVG 图表
    return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-source="${escaped}" data-mermaid-processed="false"><pre class="mermaid-source-preview"><code>📊 Mermaid\n${escaped}</code></pre></div>`
  }
  return defaultFenceRender(tokens, idx, options, env, self)
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
const chatHistory = ref([])
const sessionId = ref('')
const lastSql = ref('')
const msgContainer = ref(null)
let speechRecognition = null

const isRemote = ref(sessionStorage.getItem("isRemote") === "true")

const promptDrawerVisible = ref(false)
const promptEditDialogVisible = ref(false)
const editingPromptId = ref('')
const promptDrawerRef = ref(null)

const showLoginBtn = ref(true)
const loginDialogVisible = ref(false)
const loginForm = ref({ name: "", password: "" })
const loginName = ref()
const loginFormRef = ref()
const currentUser = ref(sessionStorage.getItem("currentUser") ? JSON.parse(sessionStorage.getItem("currentUser")) : { id: "", name: "", isAdmin: false })
const loginSucc = ref(!!sessionStorage.getItem("authentication"))
const logining = ref(false)
const bioLocalStorageKey = "nway_websql_bio_credential_id"

const router = useRouter()

const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

// 数据库连接配置 - 需要根据实际情况设置
const connId = ref('')
const schema = ref('')
const tableList = ref([])
const connList = ref([])

// 递归查找第一个可选的连接节点
function findFirstSelectableConn(nodes) {
  for (const node of nodes) {
    if (!node.isDir) {
      return node
    }
    if (node.children && node.children.length > 0) {
      const found = findFirstSelectableConn(node.children)
      if (found) return found
    }
  }
  return null
}

// 根据 connId 递归查找连接节点
function findConnById(nodes, id) {
  for (const node of nodes) {
    if (!node.isDir && node.value === id) {
      return node
    }
    if (node.children && node.children.length > 0) {
      const found = findConnById(node.children, id)
      if (found) return found
    }
  }
  return null
}

// 递归查找节点的父目录名称
function findParentDirName(nodes, connId) {
  for (const node of nodes) {
    if (node.isDir) {
      // 检查该目录下是否有目标连接
      const foundChild = node.children?.find(child => !child.isDir && child.value === connId)
      if (foundChild) {
        return node.label
      }
      // 递归检查子目录
      if (node.children) {
        const found = findParentDirName(node.children, connId)
        if (found) return found
      }
    }
  }
  return null
}

// 递归处理树节点，为选中的连接添加目录名
function processTreeNodes(nodes) {
  return nodes.map(node => {
    const newNode = { ...node }
    if (node.isDir) {
      // 目录节点，递归处理子节点
      if (node.children) {
        newNode.children = processTreeNodes(node.children)
      }
    } else {
      // 连接节点，如果被选中则添加目录名
      if (node.value === connId.value) {
        const parentDirName = findParentDirName(connList.value, node.value)
        if (parentDirName) {
          newNode.label = node.label + '（' + parentDirName + '）'
        }
      }
    }
    return newNode
  })
}

// 计算属性：处理后的连接列表（选中的连接显示目录名）
const processedConnList = computed(() => {
  return processTreeNodes(connList.value)
})

async function loadConnList() {
  try {
    const resp = await http.get('/listUserConn')
    const rawList = resp.data.data || []

    // 将扁平的 [{connId, name, dbSchema, dirName}] 转为 el-tree-select 需要的树形结构
    const dirMap = new Map() // dirName -> children[]
    const noDir = []         // 没有目录的连接

    for (const item of rawList) {
      const node = {
        label: item.name,
        value: item.connId,
        dbSchema: item.dbSchema || '',
        isDir: false,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) {
          dirMap.set(dir, [])
        }
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
    }

    const tree = []
    // 有目录的连接，按目录分组
    for (const [dirName, children] of dirMap) {
      tree.push({
        label: dirName,
        value: '__dir__' + dirName,
        isDir: true,
        children,
      })
    }
    // 没有目录的连接放在顶层
    tree.push(...noDir)

    connList.value = tree

    // 如果有连接且当前未选择，自动选择第一个
    if (connList.value.length > 0 && !connId.value) {
      const firstConn = findFirstSelectableConn(connList.value)
      if (firstConn) {
        connId.value = firstConn.value
        schema.value = firstConn.dbSchema || ''
        handleConnChange()
      }
    }
  } catch (e) {
    console.error('加载连接列表失败:', e)
  }
}

async function loadTableList(connId, schema) {
  try {
    if (!connId) {
      tableList.value = []
      return
    }
    const resp = await http.get('/listTableNames', {
      params: { connId, schema: schema || '' }
    })
    const newTableList = resp.data.data || []
    
    // 保留已选择的表中在新表列表中仍然存在的部分
    if (selectedTables.value.length > 0) {
      const newNames = newTableList.map(t => typeof t === 'string' ? t : t.name)
      selectedTables.value = selectedTables.value.filter(name => newNames.includes(name))
    }
    
    // 兼容：如果后端返回字符串数组则转为对象
    tableList.value = newTableList.map(t => typeof t === 'string' ? { name: t, comment: '' } : t)
  } catch (e) {
    console.error('加载表列表失败:', e)
    tableList.value = []
  }
}

function handleConnChange() {
  // 当连接改变时，更新 schema 信息
  const selectedConn = findConnById(connList.value, connId.value)
  if (selectedConn) {
    schema.value = selectedConn.dbSchema || ''
  }
  // 加载表列表（会自动过滤保留已选择的表）
  loadTableList(connId.value, schema.value)
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
const confirmInterruptId = ref('')
const confirmCheckPointId = ref('')
const confirmOperationType = ref('SELECT')
const confirmRiskLevel = ref('low')
const confirmDescription = ref('')
const confirmTableName = ref('')
let pendingCallback = null
let hasShownConfirm = false  // 防止重复弹出

// 多条 SQL 批量确认
const pendingSQLList = ref([])

// 重试确认
const showRetryConfirm = ref(false)
const retryMessage = ref('')
const lastQuestion = ref('')

// Excel 上传
const uploadedExcel = ref(null) // { fileId, name, columns, rows, preview }
const excelUploadRef = ref(null)

function highlightSql(text) {
  if (!text) return ''
  try {
    return hljs.highlight(text, { language: 'sql' }).value
  } catch {
    return text
  }
}

function renderMarkdown(text) {
  if (!text) return ''
  try {
    let processed = text

    // 预处理 1：修复 **text** 包裹链接的情况
    // 将 **[text](url)** 转换为 [text](url)
    processed = processed.replace(/\*\*\[([^\]]+)\]\(([^)]+)\)\*\*/g, '[$1]($2)')

    // 预处理 2：将反引号包裹的文件路径转换为链接
    // 匹配 `/path/to/file` 格式，转换为 [filename](/path/to/file)
    processed = processed.replace(/`((\/|\.\/)[^`\s]+\.(xlsx|csv|pdf|txt|zip|json|md))`/g, (match, path) => {
      const filename = path.substring(path.lastIndexOf('/') + 1)
      return `[${filename}](${path})`
    })

    // 预处理 3：将 markdown 链接 [text](url) 转换为 HTML <a> 标签
    // 优化：支持流式输出场景，即使链接被拆分也能正确处理
    // 匹配规则：只要是 [text] 后面跟着 ( 开头的内容，就尝试转换为链接
    processed = processed.replace(/\[([^\]]+)\]\(([^)]*)\)/g, (match, linkText, url) => {
      // 如果 URL 不完整（没有闭合括号或没有文件扩展名），先保留原样
      if (!url || url.length === 0) {
        return match // 流式输出中，URL 还没完全接收，保持原样
      }

      // 处理相对路径：添加 apiBase
      let fullUrl = url
      if (url.startsWith('/') && !url.startsWith('//')) {
        fullUrl = apiBase + url
      }

      // 自动添加登录 token：如果是导出链接
      const authToken = sessionStorage.getItem('authentication')
      if (authToken && fullUrl && fullUrl.includes('/exports/')) {
        const separator = fullUrl.includes('?') ? '&' : '?'
        fullUrl = fullUrl + separator + 'token=' + encodeURIComponent(authToken)
      }

      return `<a href="${fullUrl}" target="_blank" rel="noopener noreferrer">${linkText}</a>`
    })

    return md.render(processed)
  } catch (e) {
    console.error('Markdown parse error:', e)
    return text
  }
}

// 缓存版本：用于历史消息，避免重复调用 renderMarkdown 导致 mermaid ID 变化
function getCachedHtml(msg) {
  if (!msg._renderedHtml || msg._lastContent !== msg.content) {
    msg._renderedHtml = renderMarkdown(msg.content)
    msg._lastContent = msg.content
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
        const { svg } = await mermaid.render(id, trimmed)
        const escapedSource = source.replace(/</g, '&lt;').replace(/>/g, '&gt;')
        el.innerHTML =
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
          `</div>` +
          `<div class="mermaid-content-wrapper">` +
            `<div class="mermaid-svg-wrap" data-scale="1" data-translate-x="0" data-translate-y="0">${svg}</div>` +
            `<pre class="mermaid-source-preview" style="display:none;"><code>${escapedSource}</code></pre>` +
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

  try {
    const body = {
      sessionId: sessionId.value,
      connId: connId.value,
      schema: schema.value,
      question: messageContent,
      tableContext: selectedTables.value,
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
      if (collectedDangerSQLs.length === 1) {
        showConfirmDialog(collectedDangerSQLs[0].sql, collectedDangerSQLs[0].interruptId, collectedDangerSQLs[0].checkPointId)
      } else {
        pendingSQLList.value = collectedDangerSQLs.map(item => {
          const analysis = analyzeSQL(item.sql)
          return { sql: item.sql, interruptId: item.interruptId, checkPointId: item.checkPointId, ...analysis }
        })
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
function showConfirmDialog(sql, interruptId, checkPointId) {
  const analysis = analyzeSQL(sql)

  confirmSQL.value = sql
  confirmInterruptId.value = interruptId || ''
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

  // 将 SQL 保留在聊天记录中
  chatHistory.value.push({ role: 'assistant', content: `⏳ 正在执行：\n\`\`\`sql\n${confirmSQL.value}\n\`\`\`` })
  scrollToBottom()

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  const controller = new AbortController()
  abortController.value = controller

  try {
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: connId.value,
        schema: schema.value,
        question: '执行已确认的 SQL',
        confirmed: true,
        interruptId: confirmInterruptId.value,
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
    let result = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        try {
          const chunk = JSON.parse(line.slice(6).trim())
          if (chunk.type === 'content') {
            result += chunk.content
          }
          if (chunk.type === 'error') {
            chatHistory.value.push({ role: 'assistant', content: '❌ ' + (sanitizeError(chunk.content) || '执行失败') })
            scrollToBottom()
          }
        } catch (_) { }
      }
    }

    if (result) {
      chatHistory.value.push({ role: 'assistant', content: result })
    }
  } catch (e) {
    if (e.name === 'AbortError') {
      chatHistory.value.push({ role: 'assistant', content: '⏹ 执行已被手动终止' })
      scrollToBottom()
    } else {
      ElMessage({ message: sanitizeError(e) || '执行失败', type: 'error' })
    }
  } finally {
    loading.value = false
    abortController.value = null
    scrollToBottom()
  }
}

// 处理取消确认
function handleConfirmCancel() {
  confirmVisible.value = false
  chatHistory.value.push({ role: 'assistant', content: `已取消执行：\n\`\`\`sql\n${confirmSQL.value}\n\`\`\`` })
  scrollToBottom()
}

// ── 多条 SQL 批量确认 ──
async function handleConfirmAllSQL() {
  const items = pendingSQLList.value.map(item => ({ sql: item.sql, interruptId: item.interruptId, checkPointId: item.checkPointId }))
  pendingSQLList.value = []
  loading.value = true

  for (const item of items) {
    await executeConfirmedSQL(item.interruptId, item.checkPointId)
  }

  loading.value = false
  scrollToBottom()
}

function handleCancelAllSQL() {
  const items = pendingSQLList.value
  pendingSQLList.value = []
  // 将每条 SQL 保留在聊天记录中
  for (const item of items) {
    chatHistory.value.push({ role: 'assistant', content: `已取消执行：\n\`\`\`sql\n${item.sql}\n\`\`\`` })
  }
  scrollToBottom()
}

async function executeConfirmedSQL(interruptId, checkPointId) {
  chatHistory.value.push({ role: 'assistant', content: '⏳ 正在执行...' })
  scrollToBottom()

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: connId.value,
        schema: schema.value,
        question: 'resume confirmed SQL',
        confirmed: true,
        interruptId: interruptId,
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
    let result = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        try {
          const chunk = JSON.parse(line.slice(6).trim())
          if (chunk.type === 'content') result += chunk.content
          if (chunk.type === 'error') {
            const lastMsg = chatHistory.value[chatHistory.value.length - 1]
            if (lastMsg) {
              lastMsg.content = `❌ ${sanitizeError(chunk.content) || '执行失败'}\n\`\`\`sql\n${sql}\n\`\`\``
            }
          }
        } catch (_) { }
      }
    }

    // 更新最后一条消息
    const lastMsg = chatHistory.value[chatHistory.value.length - 1]
    if (lastMsg) {
      lastMsg.content = `✅ ${result || '执行成功'}\n\`\`\`sql\n${sql}\n\`\`\``
    }
  } catch (e) {
    const lastMsg = chatHistory.value[chatHistory.value.length - 1]
    if (lastMsg) {
      lastMsg.content = `❌ 执行失败：${sanitizeError(e)}\n\`\`\`sql\n${sql}\n\`\`\``
    }
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
    if (connId.value && selectedTables.value.length === 1) {
      try {
        const matchResp = await fetch(apiBase + '/ai/agent/preMatchColumns', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'Authorization': auth },
          body: JSON.stringify({
            fileId: data.fileId,
            connId: connId.value,
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
  }
}

function clearUploadedExcel() {
  uploadedExcel.value = null
}

function handleSendPromptToAI(content) {
  promptDrawerVisible.value = false
  promptEditDialogVisible.value = false
  question.value = content
  nextTick(() => sendMessage())
}

function handlePromptAdd() {
  editingPromptId.value = ''
  promptEditDialogVisible.value = true
}

function handlePromptEdit(promptId) {
  editingPromptId.value = promptId
  promptEditDialogVisible.value = true
}

function handlePromptSaved() {
  if (promptDrawerRef.value) {
    promptDrawerRef.value.loadPrompts()
  }
}

function clearSession(showMsg) {
  chatHistory.value = []
  sessionId.value = ''
  thinkingText.value = ''
  streamingContent.value = ''
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

function toLogin() {
  const searchParams = new URLSearchParams(window.location.search);
  const authorization = searchParams.get('authorization');
  if (authorization) {
    loginByToken(authorization)
  } else {
    const credentialId = window.localStorage.getItem(bioLocalStorageKey)
    if (credentialId && client.isAvailable()) {
      loginBio()
    } else {
      loginDialogVisible.value = true
    }
  }
}

function loginByToken(token) {
  const params = new URLSearchParams();
  params.append("key", token);
  params.append("loginType", "token");
  http.post("/login", params).then((resp) => {
    if (resp.data.code == 200) {
      currentUser.value = resp.data.data
      sessionStorage.setItem("authentication", resp.data.data["authentication"])
      sessionStorage.setItem("currentUser", JSON.stringify(resp.data.data))
      loadConnList()
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
    } else {
      console.error('[App] 登录失败 - code:', resp.data.code)
      ElMessage("登录失败")
    }
  }).catch((error) => {
    console.error('[App] 登录异常:', error)
    ElMessage("登录失败")
  });
}

function login() {
  loginFormRef.value.validate(isValid => {
    if (isValid) {
      logining.value = true
      const params = new URLSearchParams();
      params.append("name", loginForm.value.name);
      params.append("password", loginForm.value.password);
      params.append("loginType", "pwd");
      http.post("/login", params, {
        headers: {
          "Content-Type": "application/x-www-form-urlencoded"
        }
      }).then((resp) => {
        currentUser.value = resp.data.data
        sessionStorage.setItem("authentication", resp.headers.get("authentication"))
        sessionStorage.setItem("currentUser", JSON.stringify(resp.data.data))
        loginForm.value = {}
        logining.value = false
        loginSucc.value = true
        loginDialogVisible.value = false
        loadConnList()
        ElMessage("登陆成功")
      }).finally(() => logining.value = false)
    }
  })
}

async function loginBio() {

   const credential = window.localStorage.getItem(bioLocalStorageKey)
  // 第一个参数指定值，可以简化用户选择的操作
  let authentication = await client.authenticate({
    allowCredentials: credential == null ? [] : [JSON.parse(credential)],
    challenge: server.randomChallenge()
  })

  const authenticationParsed = await parsers.parseAuthentication(authentication);

  const params = new URLSearchParams();
  params.append("key", authenticationParsed.credentialId);
  params.append("loginType", "bio");
  http.post("/login", params).then((resp) => {
    if (resp.data.code == 200) {
      currentUser.value = resp.data.data
      sessionStorage.setItem("authentication", resp.headers.get("authentication"))
      sessionStorage.setItem("currentUser", JSON.stringify(resp.data.data))
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      loadConnList()
      ElMessage("登陆成功")
    } else {
      console.error('[App] bio登录失败 - code:', resp.data.code)
      ElMessage("登录失败")
      loginDialogVisible.value = true
    }
  }).catch((error) => {
    console.error('[App] bio登录异常:', error)
    ElMessage("登录失败")
  });
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      currentUser.value = {}
      loginSucc.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
      sessionStorage.removeItem("currentUser")
      sessionStorage.removeItem("isRemote")
    })
}

const loginDialogRef = ref(null)

function showLoginDialog() {
  loginDialogVisible.value = true
}

function handleSessionExpired() {
  loginSucc.value = false
  sessionStorage.removeItem('authentication')
  sessionStorage.removeItem('currentUser')
  // 延迟一下再弹出登录对话框，确保 UI 状态已更新
  nextTick(() => {
    loginDialogVisible.value = true
  })
}

function handleSessionExpiredEvent(event) {
  const message = event.detail?.message || '登录已过期，请重新登录'
  ElMessage({ message: message, type: 'warning' })
  handleSessionExpired()
}

function handleLoginSuccess(userData) {
  currentUser.value = userData
  loginSucc.value = true
  loadConnList()
  loadPromptList()
}

function loadPromptList() {
  if (promptDrawerRef.value) {
    promptDrawerRef.value.loadPrompts()
  }
}

function openSystemManagement() {
  console.log('[App.vue] 打开系统管理页面，当前 currentUser:', currentUser.value)
  // 将 currentUser 存储到 sessionStorage
  sessionStorage.setItem('systemManagement_user', JSON.stringify(currentUser.value))
  router.push('/system-management')
}

function getSysModel() {
  http.get("/sysMode").then((resp) => {
    isRemote.value = resp.data.data.isRemote
    sessionStorage.setItem("isRemote", isRemote.value.toString())
    if (!loginSucc.value && isRemote.value) {
      toLogin()
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

// 登录相关方法
async function handleLogin() {
  if (!loginForm.value.username || !loginForm.value.password) {
    ElMessage({ message: '请输入用户名和密码', type: 'warning' })
    return
  }

  loginLoading.value = true
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/auth/login'

  try {
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(loginForm.value)
    })

    if (!resp.ok) {
      throw new Error(`登录失败：${resp.status}`)
    }

    const data = await resp.json()
    if (data.token) {
      sessionStorage.setItem('authentication', data.token)
      userInfo.value = data.user
      isLoggedIn.value = true
      loginDialogVisible.value = false
      ElMessage({ message: '登录成功', type: 'success' })
      loginForm.value = { username: '', password: '' }
    } else {
      throw new Error('未获取到 token')
    }
  } catch (e) {
    console.error('[App] 登录失败:', e)
    ElMessage({ message: '登录失败', type: 'error' })
  } finally {
    loginLoading.value = false
  }
}

function handleLogout() {
  sessionStorage.removeItem('authentication')
  isLoggedIn.value = false
  userInfo.value = null
  ElMessage({ message: '已退出登录', type: 'info' })
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

onMounted(() => {
  loadConnList()
  getSysModel()
  document.addEventListener('keydown', handleEscKey)
  document.addEventListener('keydown', handleMermaidKeyDown)
  document.addEventListener('keyup', handleMermaidKeyUp)
  document.addEventListener('mousemove', handleMermaidMouseMove)
  document.addEventListener('mouseup', handleMermaidMouseUp)
  window.addEventListener('session-expired', handleSessionExpiredEvent)
  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization
  nextTick(() => {
    if (msgContainer.value) {
      msgContainer.value.addEventListener('wheel', handleMermaidWheel, { passive: false })
      msgContainer.value.addEventListener('mousedown', handleMermaidMouseDown)
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
  window.removeEventListener('session-expired', handleSessionExpiredEvent)
  if (msgContainer.value) {
    msgContainer.value.removeEventListener('wheel', handleMermaidWheel)
    msgContainer.value.removeEventListener('mousedown', handleMermaidMouseDown)
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
  background: linear-gradient(135deg, #64b5f6 0%, #42a5f5 100%);
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

/* 相关表选择器容器 */
.table-selector-row {
  display: flex;
  gap: 16px;
  margin-bottom: 12px;
  flex-shrink: 0;
}

.table-selector-row .table-selector-container:first-child {
  flex: 0 0 20%;
}

.table-selector-row .table-selector-container:last-child {
  flex: 0 0 80%;
}

/* 当只有一个数据库时，相关表占满整行 */
.table-selector-row .table-selector-container.full-width {
  flex: 0 0 100%;
}

.table-selector-label {
  display: block;
  margin-bottom: 8px;
  font-size: 13px;
  color: #4a5568;
  font-weight: 600;
}

.table-selector {
  width: 100%;
}

.table-selector-label::before {
  content: '📊';
  margin-right: 6px;
}

.table-selector {
  width: 100%;
}

.table-selector :deep(.el-input__inner) {
  border-radius: 10px;
  border: 1.5px solid #e0e0e0;
  transition: all 0.3s ease;
  min-height: 40px;
}

.table-selector :deep(.el-input__inner:hover) {
  border-color: #bdbdbd;
  box-shadow: 0 0 0 3px rgba(189, 189, 189, 0.08);
}

.table-selector :deep(.el-input__inner:focus) {
  border-color: #90a4ae;
  box-shadow: 0 0 0 3px rgba(144, 164, 174, 0.12);
}

.table-selector :deep(.el-tag) {
  border-radius: 6px;
  background: linear-gradient(135deg, #e3f2fd 0%, #bbdefb 100%);
  border-color: #64b5f6;
  color: #0d47a1;
  font-weight: 500;
}

.table-selector :deep(.el-select__tags) {
  padding: 4px 8px;
}

/* 下拉选项美化 */
.table-selector :deep(.el-select-dropdown__item) {
  transition: all 0.2s ease;
}

.table-selector :deep(.el-select-dropdown__item:hover) {
  background: linear-gradient(90deg, rgba(25, 118, 210, 0.05) 0%, rgba(25, 118, 210, 0.15) 100%);
  color: #1565c0;
  font-weight: 600;
}

.table-selector :deep(.el-select-dropdown__item.selected) {
  background: linear-gradient(90deg, rgba(25, 118, 210, 0.1) 0%, rgba(25, 118, 210, 0.2) 100%);
  color: #1565c0;
  font-weight: 700;
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
  overflow: visible;
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
</style>
