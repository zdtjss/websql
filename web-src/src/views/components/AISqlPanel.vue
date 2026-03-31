<template>
  <el-drawer
    :model-value="modelValue"
    title="AI SQL 生成"
    direction="rtl"
    size="680px"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div style="display: flex; flex-direction: column; gap: 16px;">
      <div>
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266; display: flex; justify-content: space-between; align-items: center;">
          <span>描述你的查询需求</span>
          <el-button 
            :type="isRecording ? 'danger' : 'primary'" 
            size="small"
            @click="toggleRecording"
          >
            <el-icon style="vertical-align: middle;">
              <component :is="isRecording ? VideoPauseIcon : MicrophoneIcon" />
            </el-icon>
          </el-button>
        </div>
        <el-input
          v-model="question"
          type="textarea"
          :rows="10"
          placeholder="描述你想查询的内容，或使用语音录入..."
          @keydown.ctrl.enter="generateSql"
        />
      </div>

      <div>
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266;">相关表</div>
        <el-select v-model="selectedTables" multiple filterable placeholder="选择相关表" style="width: 100%;">
          <el-option v-for="table in tableList" :key="table" :label="table" :value="table" />
        </el-select>
      </div>

      <el-button type="primary" :loading="loading" :disabled="!question.trim()" @click="generateSql">
        生成 SQL
      </el-button>

      <!-- Thinking block -->
      <div v-if="thinkingText" class="thinking-block">
        <div class="thinking-label">💭 思考过程</div>
        <pre class="thinking-content">{{ thinkingText }}</pre>
      </div>

      <!-- Result block -->
      <div v-if="rawContent || loading">
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266; display: flex; justify-content: space-between; align-items: center;">
          <span>生成结果</span>
          <el-button 
            v-if="rawContent && !loading" 
            type="info" 
            size="small" 
            @click="copySql"
            title="复制 SQL"
          >
            <el-icon><CopyDocument /></el-icon>
          </el-button>
        </div>
        <div class="sql-result-wrap">
          <pre class="sql-pre"><code v-html="highlightedSql" /><span v-if="loading" class="cursor-blink">▌</span></pre>
        </div>
        <el-button v-if="rawContent && !loading" type="success" style="margin-top:10px;width:100%;" @click="insertToEditor">
          加入编辑器
        </el-button>
      </div>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import hljs from 'highlight.js/lib/core'
import hljsSql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/stackoverflow-light.css'
import { Microphone, VideoPause, CopyDocument } from '@element-plus/icons-vue'

hljs.registerLanguage('sql', hljsSql)

// 注册图标组件
const MicrophoneIcon = Microphone
const VideoPauseIcon = VideoPause
const CopyDocumentIcon = CopyDocument

const props = defineProps({
  connId: String,
  schema: String,
  tableList: { type: Array, default: () => [] },
  modelValue: Boolean,
})

const emit = defineEmits(['update:modelValue', 'insertSql'])

const question = ref('')
const selectedTables = ref([])
const rawContent = ref('')
const thinkingText = ref('')
const loading = ref(false)
const isRecording = ref(false)
const speechRecognitionLoading = ref(false)
let speechRecognition = null

function initSpeechRecognition() {
  const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition
  if (!SpeechRecognition) {
    ElMessage({ message: '您的浏览器不支持语音识别功能', type: 'warning' })
    return null
  }

  const recognition = new SpeechRecognition()
  recognition.lang = 'zh-CN'
  recognition.continuous = true
  recognition.interimResults = true

  recognition.onstart = () => {
    isRecording.value = true
  }

  recognition.onresult = (event) => {
    let interimTranscript = ''
    let finalTranscript = ''

    for (let i = event.resultIndex; i < event.results.length; i++) {
      const transcript = event.results[i][0].transcript
      if (event.results[i].isFinal) {
        finalTranscript += transcript
      } else {
        interimTranscript += transcript
      }
    }

    if (finalTranscript) {
      question.value = question.value + (question.value ? ' ' : '') + finalTranscript
    }
  }

  recognition.onerror = (event) => {
    console.error('语音识别错误:', event.error)
    if (event.error === 'not-allowed') {
      ElMessage({ message: '请允许使用麦克风', type: 'error' })
    } else if (event.error === 'no-speech') {
      ElMessage({ message: '未检测到语音', type: 'warning' })
    } else {
      ElMessage({ message: `语音识别错误：${event.error}`, type: 'error' })
    }
    isRecording.value = false
  }

  recognition.onend = () => {
    isRecording.value = false
  }

  return recognition
}

function toggleRecording() {
  if (isRecording.value) {
    if (speechRecognition) {
      speechRecognition.stop()
    }
    isRecording.value = false
  } else {
    if (!speechRecognition) {
      speechRecognition = initSpeechRecognition()
      if (!speechRecognition) return
    }
    try {
      speechRecognition.start()
      ElMessage({ message: '开始语音录入，请说话...', type: 'info' })
    } catch (e) {
      ElMessage({ message: '无法启动语音识别', type: 'error' })
    }
  }
}

const highlightedSql = computed(() => {
  if (!rawContent.value) return ''
  try {
    return hljs.highlight(rawContent.value, { language: 'sql' }).value
  } catch {
    return rawContent.value
  }
})

async function generateSql() {
  if (!question.value.trim()) return
  loading.value = true
  rawContent.value = ''
  thinkingText.value = ''

  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/generateSqlStream'
  const auth = sessionStorage.getItem('authentication') || ''

  try {
    const resp = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': auth },
      body: JSON.stringify({
        connId: props.connId,
        schema: props.schema,
        question: question.value.trim(),
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
          if (chunk.type === 'thinking') {
            thinkingText.value += chunk.content
          } else if (chunk.type === 'content') {
            rawContent.value += chunk.content
          } else if (chunk.type === 'error') {
            ElMessage({ message: chunk.content || 'AI 服务错误', type: 'error' })
          }
        } catch (_) {}
      }
    }
  } catch (e) {
    ElMessage({ message: e.message || '请求失败', type: 'error' })
  } finally {
    loading.value = false
  }
}

function copySql() {
  if (!rawContent.value) return
  navigator.clipboard.writeText(rawContent.value.trim()).then(() => {
    ElMessage({ message: 'SQL 已复制到剪贴板', type: 'success' })
  }).catch(() => {
    ElMessage({ message: '复制失败', type: 'error' })
  })
}

function insertToEditor() {
  if (!rawContent.value) return
  emit('insertSql', rawContent.value.trim())
  emit('update:modelValue', false)
}
</script>

<style scoped>
.thinking-block {
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  background: #fafafa;
  padding: 10px;
}
.thinking-label {
  font-size: 12px;
  color: #909399;
  margin-bottom: 4px;
}
.thinking-content {
  font-size: 12px;
  color: #606266;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  margin: 0;
}

.sql-result-wrap {
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  overflow: hidden;
  background: #fff;
}
.sql-pre {
  margin: 0;
  padding: 12px;
  max-height: 420px;
  overflow: auto;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

.cursor-blink {
  animation: blink 1s step-start infinite;
  font-size: 14px;
}
@keyframes blink { 50% { opacity: 0; } }
</style>
