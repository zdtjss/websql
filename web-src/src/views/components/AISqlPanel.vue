<template>
  <el-drawer
    :model-value="modelValue"
    title="AI SQL 生成"
    direction="rtl"
    size="640px"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div style="display: flex; flex-direction: column; gap: 16px;">
      <div>
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266;">描述你的查询需求</div>
        <el-input
          v-model="question"
          type="textarea"
          :rows="4"
          placeholder="描述你想查询的内容..."
          @keydown.ctrl.enter="generateSql"
        />
      </div>

      <div>
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266;">相关表（可选）</div>
        <el-select v-model="selectedTables" multiple filterable placeholder="选择相关表（可选）" style="width: 100%;">
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
        <div style="margin-bottom: 6px; font-size: 13px; color: #606266;">生成结果</div>
        <div class="sql-result-wrap">
          <pre class="sql-pre"><code v-html="highlightedSql" /><span v-if="loading" class="cursor-blink">▌</span></pre>
        </div>
        <el-button v-if="rawContent && !loading" type="success" style="margin-top:10px;width:100%;" @click="insertToEditor">
          插入编辑器
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

hljs.registerLanguage('sql', hljsSql)

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
