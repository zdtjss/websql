<template>
  <div>

    <div class="ai-sql-panel-container">
      <div class="container">
        <!-- 会话历史消息 -->
        <div ref="msgContainer" class="chat-messages">
          <!-- 思考过程（历史中的，可折叠） -->
          <div v-for="(msg, idx) in chatHistory" :key="'h' + idx">
            <div v-if="msg.role === 'thinking'" class="thinking-block">
              <div class="thinking-label" style="cursor:pointer;" @click="msg.collapsed = !msg.collapsed">
                💭 思考过程 <span style="font-size:11px;">{{ msg.collapsed ? '▶ 展开' : '▼ 折叠' }}</span>
              </div>
              <pre v-show="!msg.collapsed" class="thinking-content">{{ msg.content }}</pre>
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
              <div v-else class="bubble-content markdown-body" v-html="renderMarkdown(msg.content)"></div>
            </div>
            <div v-else-if="msg.role === 'tool_call'" class="tool-call-block">
              <span>🔧 {{ msg.content }}</span>
            </div>
          </div>

          <!-- 实时思考过程（流式中） -->
          <div v-if="thinkingText && loading" class="thinking-block">
            <div class="thinking-label">💭 思考中...</div>
            <pre class="thinking-content">{{ thinkingText }}</pre>
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

        <!-- 输入区域 -->
        <div class="input-area">
          <div class="input-label">
            <span>描述你的需求（数据查询 / 数据分析 / SQL 生成 / 数据导出）</span>
            <div style="display: flex; gap: 0px;">
              <el-popover placement="top" :width="380" trigger="click" v-model:visible="sessionHistoryVisible"
                @show="loadSessionList()">
                <div style="max-height: 400px; overflow-y: auto;">
                  <el-empty v-if="sessionList.length === 0 && !loadingSessions" description="暂无历史会话" />
                  <el-skeleton v-if="loadingSessions" :rows="4" animated />
                  <div v-else style="display: flex; flex-direction: column; gap: 8px;">
                    <div v-for="sess in sessionList" :key="sess.id" class="session-item">
                      <div class="session-content" @click="handleClickSession(sess.id)">
                        <div class="session-title">{{ sess.title }}</div>
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
                  <el-button class="toolbar-btn" size="small" title="历史会话">
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
              <el-button class="toolbar-btn" size="small" @click="clearSession" title="清空会话">
                <el-icon>
                  <Delete />
                </el-icon>
              </el-button>
            </div>
          </div>
          <div class="table-selector-container">
            <label class="table-selector-label">相关表</label>
            <el-select v-model="selectedTables" multiple filterable placeholder="选择相关表（可多选）" class="table-selector">
              <el-option v-for="table in tableList" :key="table" :label="table" :value="table" />
            </el-select>
          </div>

          <div class="input-action-row">
            <el-input v-model="question" type="textarea" :rows="5" placeholder="描述你想查询的内容，或使用语音录入... (Ctrl+Enter 发送)"
              :disabled="loading" @keydown.ctrl.enter="sendMessage" class="question-input" />
            <div class="action-buttons">
              <el-button type="primary" :loading="loading" :disabled="!question.trim()" @click="sendMessage"
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
            </div>
          </div>
        </div>
      </div>
    </div>
    <div class="login-button-container">
      <el-button v-if="!loginSucc && isRemote" circle size="small" @click="toLogin" title="登录">
        <el-icon>
          <User />
        </el-icon>
      </el-button>
      <el-button v-else circle size="small" @click="logout" title="退出登录">
        <el-icon>
          <SwitchButton />
        </el-icon>
      </el-button>
      <!-- 登录对话框 -->
      <el-dialog v-model="loginDialogVisible" @close="loginDialogVisible = false" width="350px" @keyup.enter="login"
        @opened="loginName.focus()">
        <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" label-width="80px">
          <el-form-item label="用户名" prop="name">
            <el-input ref="loginName" v-model="loginForm.name" />
          </el-form-item>
          <el-form-item label="密&nbsp;&nbsp;&nbsp;码" prop="password">
            <el-input v-model="loginForm.password" type="password" />
          </el-form-item>
        </el-form>
        <template #footer>
          <span class="dialog-footer">
            <el-button type="primary" @click="login" :loading="logining">登录</el-button>
            <el-button @click="loginDialogVisible = false">关闭</el-button>
          </span>
        </template>
      </el-dialog>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, nextTick } from 'vue'
import { client, parsers, server } from '@passwordless-id/webauthn'
import { ElMessage, ElMessageBox } from 'element-plus'
import hljs from 'highlight.js/lib/core'
import hljsSql from 'highlight.js/lib/languages/sql'
import MarkdownIt from 'markdown-it'
import 'highlight.js/styles/stackoverflow-light.css'
import { Microphone, VideoPause, CopyDocument, Delete, FullScreen, Document, Clock, Promotion, DocumentAdd, User, SwitchButton } from '@element-plus/icons-vue'
import SQLConfirmInline from '@/components/SQLConfirmInline.vue'
import { analyzeSQL, extractAllSQL, needsConfirmation } from '@/utils/sqlRiskAssessment'
import http from '@/js/utils/httpProxy.js'

hljs.registerLanguage('sql', hljsSql)

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
      token.attrs[hrefIndex][1] = apiBase + href
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

const props = defineProps({})

const emit = defineEmits([])

const question = ref('')
const selectedTables = ref([])
const loading = ref(false)
const isRecording = ref(false)
const thinkingText = ref('')
const streamingContent = ref('')
const chatHistory = ref([])
const sessionId = ref('')
const lastSql = ref('')
const msgContainer = ref(null)
let speechRecognition = null

const isRemote = ref(null)

const showLoginBtn = ref(true)
const loginDialogVisible = ref(false)
const loginForm = ref({ name: "", password: "" })
const loginName = ref()
const loginFormRef = ref()
const currentUser = ref({
  id: "",
  name: "",
  isAdmin: false
})
const loginSucc = ref(!!sessionStorage.getItem("authentication"))
const logining = ref(false)
const bioLocalStorageKey = "nway_websql_bio_credential_id"

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

// 历史会话相关
const sessionHistoryVisible = ref(false)
const sessionList = ref([])
const loadingSessions = ref(false)

// 用于记录已经渲染过的链接，避免重复处理
const processedLinks = new Set()

// SQL 确认相关
const confirmVisible = ref(false)
const confirmSQL = ref('')
const confirmOperationType = ref('SELECT')
const confirmRiskLevel = ref('low')
const confirmDescription = ref('')
const confirmTableName = ref('')
let pendingCallback = null
let hasShownConfirm = false  // 防止重复弹出

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

      return `<a href="${fullUrl}" target="_blank" rel="noopener noreferrer">${linkText}</a>`
    })

    return md.render(processed)
  } catch (e) {
    console.error('Markdown parse error:', e)
    return text
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (msgContainer.value) {
      msgContainer.value.scrollTop = msgContainer.value.scrollHeight
    }
  })
}

async function sendMessage() {
  const text = question.value.trim()
  if (!text || loading.value) return

  // 重置检测标记
  resetDetectFlag()

  chatHistory.value.push({ role: 'user', content: text })
  question.value = ''
  loading.value = true
  thinkingText.value = ''
  streamingContent.value = ''
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
        question: text,
        tableContext: selectedTables.value,
      }),
    })

    if (!resp.ok) {
      ElMessage({ message: `请求失败: ${resp.status}`, type: 'error' })
      return
    }

    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''

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
              scrollToBottom()
              break
            case 'danger_confirm':
              // 后端推送的危险 SQL 确认
              console.log('收到 danger_confirm 事件:', chunk)
              if (!hasShownConfirm) {
                hasShownConfirm = true
                const sqlToConfirm = chunk.sql || chunk.content
                console.log('准备显示确认对话框，SQL:', sqlToConfirm)
                showConfirmDialog(sqlToConfirm)
              }
              break
            case 'error':
              ElMessage({ message: chunk.content || 'AI 服务错误', type: 'error' })
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

      // 关键检测：如果内容包含 [CONFIRM_REQUIRED] 标记，立即触发确认
      if (!hasShownConfirm && content.includes('[CONFIRM_REQUIRED]')) {
        const sqlStatements = extractAllSQL(content)
        for (const sql of sqlStatements) {
          const analysis = analyzeSQL(sql)
          if (analysis.riskLevel === 'medium' || analysis.riskLevel === 'high') {
            showConfirmDialog(sql)
            break
          }
        }
      }

      streamingContent.value = ''
    }
  } catch (e) {
    ElMessage({ message: e.message || '请求失败', type: 'error' })
  } finally {
    loading.value = false
    scrollToBottom()
  }
}

// 显示确认区域
function showConfirmDialog(sql) {
  // 分析 SQL
  const analysis = analyzeSQL(sql)

  confirmSQL.value = sql
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
        question: '执行已确认的 SQL',
        confirmed: true,
        pendingSQL: confirmedSql,
      }),
    })

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
            ElMessage({ message: chunk.content, type: 'error' })
          }
        } catch (_) { }
      }
    }

    if (result) {
      chatHistory.value.push({ role: 'assistant', content: result })
    }
  } catch (e) {
    ElMessage({ message: e.message || '执行失败', type: 'error' })
  } finally {
    loading.value = false
    scrollToBottom()
  }
}

// 处理取消确认
function handleConfirmCancel() {
  confirmVisible.value = false
  chatHistory.value.push({ role: 'assistant', content: '已取消执行危险操作。' })
  scrollToBottom()
}

function clearSession() {
  chatHistory.value = []
  sessionId.value = ''
  thinkingText.value = ''
  streamingContent.value = ''
  lastSql.value = ''
  confirmVisible.value = false
  confirmSQL.value = ''
  processedLinks.clear()
  resetDetectFlag()
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
      refreshTree()
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
    } else {
      ElMessage(data.msg)
    }
  }).catch((error) => {
    ElMessage(error)
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
        loginForm.value = {}
        logining.value = false
        loginSucc.value = true
        loginDialogVisible.value = false
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
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
    } else {
      ElMessage(data.msg)
      loginDialogVisible.value = true
    }
  }).catch((error) => {
    ElMessage(error)
  });
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      currentUser.value = {}
      loginSucc.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
    })
}

function getSysModel() {
  http.get("/sysMode").then((resp) => {
    isRemote.value = resp.data.data.isRemote
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

    if (!resp.ok) {
      throw new Error(`请求失败：${resp.status}`)
    }

    const data = await resp.json()
    const sessions = data.sessions || []
    // 按时间倒序排列（最新的在最上面）
    sessions.sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
    sessionList.value = sessions
  } catch (e) {
    ElMessage({ message: e.message || '加载历史会话失败', type: 'error' })
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

    if (!resp.ok) {
      throw new Error(`请求失败：${resp.status}`)
    }

    ElMessage({ message: '会话已删除', type: 'success' })
    await loadSessionList() // 刷新列表
  } catch (e) {
    ElMessage({ message: e.message || '删除会话失败', type: 'error' })
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
    ElMessage({ message: e.message || '登录失败', type: 'error' })
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
    }
  } catch (e) {
    ElMessage({ message: e.message || '加载会话失败', type: 'error' })
  }
}

onMounted(() => {
  getSysModel()
  document.addEventListener('keydown', handleEscKey)
  // 检查登录状态
  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscKey)
})
</script>

<style scoped>
/* ========== 外层容器 - 填满整个视口 ========== */
.ai-sql-panel-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding-right: 15%;
  padding-left: 15%;
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
  max-width: 85%;
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
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  margin: 0;
  padding: 8px 12px;
  background: rgba(255, 255, 255, 0.7);
  border-radius: 8px;
  /* border-left: 3px solid #546e7a; */
  font-family: 'Consolas', 'Monaco', monospace;
  line-height: 1.5;
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

/* ========== Markdown 样式 ========== */
.markdown-body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif;
  font-size: 14px;
  line-height: 1.7;
  color: #2d3748;
  word-wrap: break-word;
  overflow-wrap: break-word;
}

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

.markdown-body h1 {
  font-size: 24px;
  border-bottom: 2px solid #e2e8f0;
  padding-bottom: 6px;
}

.markdown-body h2 {
  font-size: 20px;
  border-bottom: 1px solid #e2e8f0;
  padding-bottom: 4px;
}

.markdown-body h3 {
  font-size: 18px;
}

.markdown-body h4 {
  font-size: 16px;
}

.markdown-body h5 {
  font-size: 14px;
}

.markdown-body h6 {
  font-size: 13px;
}

.markdown-body ul,
.markdown-body ol {
  padding-left: 2em;
  margin-top: 8px;
  margin-bottom: 12px;
}

.markdown-body ul {
  list-style-type: disc;
}

.markdown-body ul ul {
  list-style-type: circle;
}

.markdown-body ul ul ul {
  list-style-type: square;
}

.markdown-body ol {
  list-style-type: decimal;
}

.markdown-body li {
  margin-top: 6px;
  margin-bottom: 6px;
  line-height: 1.6;
}

.markdown-body li+li {
  margin-top: 6px;
}

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
  padding: 16px;
  overflow: auto;
  font-size: 13px;
  line-height: 1.6;
  background: linear-gradient(180deg, #263238 0%, #1c282c 100%);
  border-radius: 10px;
  margin-top: 12px;
  margin-bottom: 12px;
  max-width: 100%;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
  border: 1px solid rgba(0, 0, 0, 0.2);
}

.markdown-body pre code {
  display: block;
  padding: 0;
  margin: 0;
  overflow: visible;
  line-height: inherit;
  word-wrap: normal;
  background-color: transparent;
  border-radius: 0;
  color: #cfd8dc;
  white-space: pre;
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

/* 表格样式优化 */
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

.markdown-body table tr:nth-child(2n) {
  background-color: #f5f5f5;
}

.markdown-body table tr:hover {
  background-color: #eceff1;
}

/* 宽表滚动容器 */
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
  opacity: 0;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  transform: translateX(-8px);
}

.session-item:hover .session-actions {
  opacity: 1;
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
  scrollbar-width: thin;
  scrollbar-color: #78909c rgba(84, 110, 122, 0.05);
}

/* Popover 美化 */
:deep(.el-popover) {
  border-radius: 12px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

/* 消息气泡中的链接 */
.chat-bubble a {
  color: inherit;
  text-decoration: underline;
  font-weight: 600;
}

.chat-bubble.user a {
  color: #ffffff;
  text-decoration-color: rgba(255, 255, 255, 0.6);
}

.chat-bubble.user a:hover {
  color: #e2e8f0;
  text-decoration-color: #ffffff;
}

/* ========== 输入区域布局 ========== */
.input-action-row {
  display: flex;
  gap: 12px;
  margin-top: 12px;
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

/* 相关表选择器容器 */
.table-selector-container {
  margin-bottom: 12px;
}

.table-selector-label {
  display: block;
  margin-bottom: 8px;
  font-size: 13px;
  color: #4a5568;
  font-weight: 600;
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
</style>
