<template>
  <el-drawer
    :model-value="modelValue"
    title="AI SQL 智能助手"
    direction="rtl"
    :size="panelWidth"
    :fullscreen="isFullscreen"
    :close-on-press-escape="false"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div style="display: flex; flex-direction: column; height: 100%; gap: 12px;">
      <!-- 会话历史消息 -->
      <div ref="msgContainer" class="chat-messages">
        <!-- 思考过程（历史中的，可折叠） -->
        <div v-for="(msg, idx) in chatHistory" :key="'h'+idx">
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

        <!-- 危险操作确认 -->
        <div v-if="dangerConfirm.visible" class="danger-confirm">
          <el-alert type="warning" :closable="false" show-icon>
            <template #title>⚠️ 检测到危险操作</template>
            <pre class="sql-pre danger-sql"><code v-html="highlightSql(dangerConfirm.sql)" /></pre>
            <div style="margin-top: 10px; display: flex; gap: 8px;">
              <el-button type="danger" size="small" @click="confirmDangerExec">确认执行</el-button>
              <el-button size="small" @click="cancelDangerExec">取消</el-button>
            </div>
          </el-alert>
        </div>

        <div v-if="loading" style="color:#909399;font-size:13px;padding:4px 0;">AI 正在处理...</div>
      </div>

      <!-- 输入区域 -->
      <div style="border-top: 1px solid #e4e7ed; padding-top: 12px;">
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266; display: flex; justify-content: space-between; align-items: center;">
          <span>描述你的需求（SQL 生成 / 数据分析 / 导出）</span>
          <div style="display: flex; gap: 6px;">
            <el-button size="small" @click="toggleFullscreen" :title="isFullscreen ? '退出全屏' : '全屏展示'">
              <el-icon><component :is="isFullscreen ? 'ZoomOut' : 'FullScreen'" /></el-icon>
            </el-button>
            <el-button
              :type="isRecording ? 'danger' : 'primary'"
              size="small"
              @click="toggleRecording"
            >
              <el-icon style="vertical-align: middle;">
                <component :is="isRecording ? VideoPause : Microphone" />
              </el-icon>
            </el-button>
            <el-button size="small" @click="clearSession" title="清空会话">
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>
        <div>
          <div style="margin-bottom: 6px; font-size: 13px; color: #606266;">相关表</div>
          <el-select v-model="selectedTables" multiple filterable placeholder="选择相关表" style="width: 100%;" size="small">
            <el-option v-for="table in tableList" :key="table" :label="table" :value="table" />
          </el-select>
        </div>

        <div style="display: flex; gap: 8px; margin-top: 8px;">
          <el-input
            v-model="question"
            type="textarea"
            :rows="5"
            placeholder="描述你想查询的内容，或使用语音录入... (Ctrl+Enter 发送)"
            :disabled="loading"
            @keydown.ctrl.enter="sendMessage"
            style="flex: 1;"
          />
          <div style="display: flex; flex-direction: column; gap: 4px;">
            <el-button type="primary" :loading="loading" :disabled="!question.trim()" @click="sendMessage" size="small">
              发送
            </el-button>
            <el-button v-if="lastSql" type="success" size="small" @click="insertToEditor" title="将最后生成的SQL加入编辑器">
              加入编辑器
            </el-button>
          </div>
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import hljs from 'highlight.js/lib/core'
import hljsSql from 'highlight.js/lib/languages/sql'
import MarkdownIt from 'markdown-it'
import 'highlight.js/styles/stackoverflow-light.css'
import { Microphone, VideoPause, CopyDocument, Delete, FullScreen } from '@element-plus/icons-vue'

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
md.renderer.rules.link_open = function(tokens, idx, options, env, self) {
  const token = tokens[idx]
  const hrefIndex = token.attrIndex('href')
  let href = hrefIndex >= 0 ? token.attrs[hrefIndex][1] : ''
  
  // 处理 href：如果是相对路径且以 / 开头，添加 apiBase
  if (href && href.startsWith('/') && !href.startsWith('//')) {
    // 更新 href 属性
    token.attrs[hrefIndex][1] = apiBase + href
  }
  
  // 所有链接都添加 target="_blank"
  token.attrPush(['target', '_blank'])
  
  // 外部链接额外添加 rel 属性
  if (href.startsWith('http')) {
    token.attrPush(['rel', 'noopener noreferrer'])
  }
  
  return self.renderToken(tokens, idx, options)
}

// 自定义表格渲染，添加滚动容器
const defaultTableRender = md.renderer.rules.table_open
md.renderer.rules.table_open = function(tokens, idx, options, env, self) {
  return '<div class="table-wrapper"><table>'
}
const defaultTableCloseRender = md.renderer.rules.table_close
md.renderer.rules.table_close = function(tokens, idx, options, env, self) {
  return '</table></div>'
}

const props = defineProps({
  connId: String,
  schema: String,
  tableList: { type: Array, default: () => [] },
  modelValue: Boolean,
})

const emit = defineEmits(['update:modelValue', 'insertSql'])

const question = ref('')
const selectedTables = ref([])
const loading = ref(false)
const isRecording = ref(false)
const thinkingText = ref('')
const streamingContent = ref('')
const chatHistory = ref([])
const sessionId = ref('')
const lastSql = ref('')
const isToolCalling = ref(false) // 标记是否正在调用工具
const msgContainer = ref(null)
const panelWidth = ref('720px')
const isFullscreen = ref(false)
let speechRecognition = null

const dangerConfirm = ref({ visible: false, sql: '' })

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
    
    return md.render(processed)
  } catch (e) {
    console.error('Markdown parse error:', e)
    return text
  }
}

function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value
  if (isFullscreen.value) {
    panelWidth.value = '100%'
  } else {
    panelWidth.value = '720px'
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

  chatHistory.value.push({ role: 'user', content: text })
  question.value = ''
  loading.value = true
  thinkingText.value = ''
  streamingContent.value = ''
  isToolCalling.value = false
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
        connId: props.connId,
        schema: props.schema,
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
              break
            case 'content':
              // 如果正在调用工具，所有内容都添加到思考区域
              if (isToolCalling.value) {
                thinkingText.value += chunk.content
              } else {
                streamingContent.value += chunk.content
              }
              break
            case 'tool_call':
              // 工具调用，标记为工具调用中，添加到思考内容
              isToolCalling.value = true
              thinkingText.value += `\n[调用工具] ${chunk.content}\n`
              break
            case 'tool_result':
              // 工具执行结果，添加到思考内容
              if (chunk.toolResult) {
                thinkingText.value += `[工具结果] ${chunk.toolResult.name}: ${chunk.content || '执行完成'}\n`
              }
              // 工具调用结束，重置标志，让后续 content 回到结果区域
              isToolCalling.value = false
              break
            case 'danger_confirm':
              dangerConfirm.value = { visible: true, sql: chunk.sql || chunk.content }
              break
            case 'error':
              ElMessage({ message: chunk.content || 'AI 服务错误', type: 'error' })
              break
            case 'done':
              break
          }
        } catch (_) {}
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
    // 重置工具调用标志
    isToolCalling.value = false
  } catch (e) {
    ElMessage({ message: e.message || '请求失败', type: 'error' })
  } finally {
    loading.value = false
    scrollToBottom()
  }
}

async function confirmDangerExec() {
  const sql = dangerConfirm.value.sql
  dangerConfirm.value = { visible: false, sql: '' }
  loading.value = true

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/chatStream'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        sessionId: sessionId.value,
        connId: props.connId,
        schema: props.schema,
        question: '执行已确认的SQL',
        confirmed: true,
        pendingSQL: sql,
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
          if (chunk.type === 'content') result += chunk.content
          if (chunk.type === 'error') ElMessage({ message: chunk.content, type: 'error' })
        } catch (_) {}
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

function cancelDangerExec() {
  dangerConfirm.value = { visible: false, sql: '' }
  chatHistory.value.push({ role: 'assistant', content: '已取消执行危险操作。' })
  scrollToBottom()
}

function clearSession() {
  chatHistory.value = []
  sessionId.value = ''
  thinkingText.value = ''
  streamingContent.value = ''
  lastSql.value = ''
  isToolCalling.value = false
  dangerConfirm.value = { visible: false, sql: '' }
}

function insertToEditor() {
  if (!lastSql.value) return
  emit('insertSql', lastSql.value.trim())
  emit('update:modelValue', false)
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
    if (isFullscreen.value) {
      // 全屏状态：只退出全屏，不关闭面板
      isFullscreen.value = false
      panelWidth.value = '720px'
    } else {
      // 非全屏状态：关闭面板
      emit('update:modelValue', false)
    }
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleEscKey)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscKey)
})

watch(() => props.modelValue, (v) => { if (v) scrollToBottom() })
</script>

<style scoped>
.chat-messages {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 4px 0;
  min-height: 0;
}
.chat-bubble {
  max-width: 90%;
  border-radius: 8px;
  padding: 8px 12px;
  font-size: 13px;
}
.chat-bubble.user {
  align-self: flex-end;
  background: #409eff;
  color: #fff;
}
.chat-bubble.assistant {
  align-self: flex-start;
  background: #f5f7fa;
  color: #303133;
}
.bubble-label {
  font-size: 11px;
  opacity: 0.7;
  margin-bottom: 2px;
}
.bubble-content { word-break: break-word; }
.thinking-block {
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  background: #fafafa;
  padding: 8px;
}
.thinking-label { font-size: 12px; color: #909399; margin-bottom: 4px; }
.thinking-content {
  font-size: 12px; color: #606266; white-space: pre-wrap;
  word-break: break-all; max-height: 150px; overflow-y: auto; margin: 0;
}
.tool-call-block {
  font-size: 12px; color: #67c23a; padding: 4px 8px;
  background: #f0f9eb; border-radius: 4px;
}
.sql-pre {
  margin: 0; padding: 8px; overflow: auto;
  font-family: 'Courier New', monospace; font-size: 13px;
  line-height: 1.5; white-space: pre-wrap; word-break: break-all;
}
.danger-sql { max-height: 200px; overflow-y: auto; background: #fff8e6; border-radius: 4px; }
.danger-confirm { padding: 8px 0; }
.cursor-blink { animation: blink 1s step-start infinite; font-size: 14px; }
@keyframes blink { 50% { opacity: 0; } }

/* Markdown 样式 */
.markdown-body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
  font-size: 14px;
  line-height: 1.6;
  color: #24292e;
  word-wrap: break-word;
  overflow-wrap: break-word;
}

.markdown-body p {
  margin-top: 0;
  margin-bottom: 10px;
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
  margin-top: 16px;
  margin-bottom: 8px;
  font-weight: 600;
  line-height: 1.25;
}

.markdown-body h1 { font-size: 20px; }
.markdown-body h2 { font-size: 18px; }
.markdown-body h3 { font-size: 16px; }
.markdown-body h4 { font-size: 14px; }
.markdown-body h5 { font-size: 13px; }
.markdown-body h6 { font-size: 12px; }

.markdown-body ul,
.markdown-body ol {
  padding-left: 2em;
  margin-top: 0;
  margin-bottom: 10px;
}

.markdown-body ul {
  list-style-type: disc;
}

.markdown-body ol {
  list-style-type: decimal;
}

.markdown-body li {
  margin-top: 4px;
  margin-bottom: 4px;
}

.markdown-body li + li {
  margin-top: 4px;
}

.markdown-body code {
  padding: 0.2em 0.4em;
  margin: 0;
  font-size: 12px;
  background-color: rgba(27, 31, 35, 0.05);
  border-radius: 4px;
  font-family: 'Courier New', monospace;
}

.markdown-body pre {
  padding: 12px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.45;
  background-color: #f6f8fa;
  border-radius: 4px;
  margin-top: 8px;
  margin-bottom: 8px;
  max-width: 100%;
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
  color: inherit;
  white-space: pre;
}

.markdown-body blockquote {
  padding: 0 1em;
  color: #6a737d;
  border-left: 0.25em solid #dfe2e5;
  margin: 0;
  margin-bottom: 10px;
}

/* 表格样式优化 */
.markdown-body table {
  border-collapse: collapse;
  width: 100%;
  max-width: 100%;
  margin-top: 8px;
  margin-bottom: 8px;
  font-size: 13px;
  display: block;
  overflow-x: auto;
}

.markdown-body table th,
.markdown-body table td {
  padding: 8px 12px;
  border: 1px solid #e1e4e8;
  white-space: nowrap;
}

.markdown-body table th {
  font-weight: 600;
  background-color: #f6f8fa;
  position: sticky;
  top: 0;
}

.markdown-body table tr:nth-child(2n) {
  background-color: #f6f8fa;
}

/* 宽表滚动容器 */
.markdown-body .table-wrapper {
  overflow-x: auto;
  margin: 8px 0;
}

.markdown-body a {
  color: #0366d6;
  text-decoration: none;
  cursor: pointer;
}

.markdown-body a:hover {
  text-decoration: underline;
}

.markdown-body a[target="_blank"]::after {
  content: " ↗";
  font-size: 12px;
}

.markdown-body hr {
  height: 0.25em;
  padding: 0;
  margin: 16px 0;
  background-color: #e1e4e8;
  border: 0;
}

.markdown-body strong {
  font-weight: 600;
}

.markdown-body em {
  font-style: italic;
}

.markdown-body img {
  max-width: 100%;
  height: auto;
}
</style>
