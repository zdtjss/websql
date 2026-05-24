<template>
  <el-drawer
    ref="drawerRef"
    v-model="drawerVisible"
    title="SQL优化分析"
    direction="btt"
    :size="drawerHeight + '%'"
    :before-close="handleClose"
    :close-on-click-modal="false"
  >
    <div class="drag-handle" @mousedown="startDrag"></div>
    <div class="toolbar-area">
      <el-tabs v-model="activeTab" @tab-change="onTabChange" class="optimize-tabs">
        <el-tab-pane name="explain">
          <template #label>
            <span class="tab-label">
              <el-icon v-if="explaining" class="is-loading"><Loading /></el-icon>
              执行计划
            </span>
          </template>
          <div class="tab-body">
            <template v-if="explainResult">
              <div class="explain-section">
                <div class="explain-table-wrapper">
                  <el-table
                    v-if="explainResult.rows && explainResult.rows.length"
                    :data="explainResult.rows"
                    stripe
                    size="small"
                    style="width: 100%"
                    border
                    :header-cell-style="{ background: '#f0f5ff', color: '#1d39c4', fontWeight: '600' }"
                  >
                    <el-table-column
                      v-for="col in explainResult.columns"
                      :key="col.name"
                      :prop="col.name"
                      :label="col.name"
                      :width="getColumnWidth(col.name)"
                      :show-overflow-tooltip="true"
                      resizable
                    />
                  </el-table>
                </div>
                <div v-if="explainResult.raw" class="explain-raw">
                  <div class="raw-header" @click="rawExpanded = !rawExpanded" style="cursor:pointer">
                    <span>
                      <el-icon class="raw-arrow" :class="{ expanded: rawExpanded }"><ArrowRight /></el-icon>
                      原始数据
                    </span>
                    <el-button size="small" type="primary" link @click.stop="copyText(explainResult.raw)">复制</el-button>
                  </div>
                  <el-collapse-transition>
                    <pre v-show="rawExpanded" class="raw-content">{{ explainResult.raw }}</pre>
                  </el-collapse-transition>
                </div>
              </div>
            </template>
            <el-empty v-else-if="explainAttempted" description="该SQL无法生成执行计划" :image-size="60" />
            <el-empty v-else description="点击上方 执行计划 页签开始分析" :image-size="60" />
          </div>
        </el-tab-pane>

        <el-tab-pane name="optimize">
          <template #label>
            <span class="tab-label">
              <el-icon v-if="optimizing" class="is-loading"><Loading /></el-icon>
              AI优化建议
            </span>
          </template>
          <div class="tab-body">
            <div v-if="optimizeError" class="optimize-error">
              <el-alert :title="optimizeError" type="error" :closable="false" show-icon />
            </div>

            <div v-if="thinkingText" class="thinking-section">
              <div class="thinking-header" @click="thinkingExpanded = !thinkingExpanded" style="cursor:pointer">
                <span>
                  <el-icon class="thinking-arrow" :class="{ expanded: thinkingExpanded }"><ArrowRight /></el-icon>
                  {{ optimizing ? 'AI 正在思考...' : 'AI 推理过程' }}
                </span>
              </div>
              <el-collapse-transition>
                <div v-show="thinkingExpanded" class="thinking-content">{{ thinkingText }}</div>
              </el-collapse-transition>
            </div>

            <div v-if="optimizeContent" class="markdown-body optimize-content" v-html="renderedMarkdown"></div>

            <div v-if="!optimizing && !optimizeContent && !optimizeError && optimizeAttempted" class="optimize-empty">
              <el-empty description="AI 未返回优化建议，请重试" :image-size="60" />
            </div>

            <div v-if="!optimizing && !optimizeContent && !optimizeError && !optimizeAttempted" class="optimize-empty">
              <el-empty description="点击上方 AI优化建议 页签开始分析" :image-size="60" />
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, computed, watch, onUnmounted, useTemplateRef } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading, ArrowRight } from '@element-plus/icons-vue'
import { getMarkdownRenderer, getHljs } from '@/utils/lazyDeps'
import http from '@/utils/httpProxy.js'

let md = null
let hljsLib = null
const mdReady = ref(false)

async function initDeps() {
  const [mdInstance, hljsInstance] = await Promise.all([
    getMarkdownRenderer(),
    getHljs(),
  ])
  hljsLib = hljsInstance
  hljsLib.registerLanguage('mysql', hljsLib.getLanguage('sql'))
  hljsLib.registerLanguage('mariadb', hljsLib.getLanguage('sql'))
  md = mdInstance
  mdReady.value = true
}

const drawerVisible = defineModel('visible', { default: false })

const { connId, schema, sql } = defineProps({
  connId: String,
  schema: String,
  sql: String,
  dbType: String
})

const drawerRef = useTemplateRef('drawerRef')

const explaining = ref(false)
const optimizing = ref(false)
const explainResult = ref(null)
const activeTab = ref('explain')
const rawExpanded = ref(false)
const explainAttempted = ref(false)
const optimizeAttempted = ref(false)

const thinkingText = ref('')
const thinkingExpanded = ref(false)
const optimizeContent = ref('')
const optimizeError = ref('')
const abortController = ref(null)

const explainColumnWidths = {
  id: 100,
  select_type: 150,
  table: 350,
  partitions: 100,
  type: 100,
  possible_keys: 170,
  key: 150,
  key_len: 100,
  ref: 200,
  rows: 100,
  filtered: 100,
  Extra: 380,
}

function getColumnWidth(colName) {
  return explainColumnWidths[colName] || 130
}

const drawerHeight = ref(45)
let dragging = false
let dragStartY = 0
let dragStartHeight = 0

function startDrag(e) {
  dragging = true
  dragStartY = e.clientY
  dragStartHeight = drawerHeight.value
  document.addEventListener('mousemove', onDrag)
  document.addEventListener('mouseup', stopDrag)
  document.body.style.userSelect = 'none'
  e.preventDefault()
}

function onDrag(e) {
  if (!dragging) return
  const dy = dragStartY - e.clientY
  const vh = window.innerHeight
  const newPct = dragStartHeight + (dy / vh) * 100
  drawerHeight.value = Math.max(15, Math.min(90, newPct))
}

function stopDrag() {
  dragging = false
  document.removeEventListener('mousemove', onDrag)
  document.removeEventListener('mouseup', stopDrag)
  document.body.style.userSelect = ''
}

const renderedMarkdown = computed(() => {
  void mdReady.value
  if (!optimizeContent.value) return ''
  if (!md) {
    return optimizeContent.value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n/g, '<br>')
  }
  let processed = optimizeContent.value

  processed = processed.replace(/\$\\(?:text|textbf|textit)\{([^}]+)\}\$/g, (match, inner) => {
    return inner
  })
  processed = processed.replace(/\$\\(?:bm|mathit|mathrm|mathsf|mathtt)\{([^}]+)\}\$/g, (match, inner) => {
    return inner
  })

  let rendered = md.render(processed)
  rendered = addCopyButtonsToCodeBlocks(rendered)
  return rendered
})

function addCopyButtonsToCodeBlocks(html) {
  return html.replace(
    /(<pre class="hljs"><code[^>]*>[\s\S]*?<\/code><\/pre>)/g,
    '<div class="code-block-wrapper">$1<div class="code-copy-btn" onclick="this.previousElementSibling.querySelector(\'code\').textContent && navigator.clipboard.writeText(this.previousElementSibling.querySelector(\'code\').textContent).then(()=>{this.textContent=\'已复制\';})">复制</div></div>'
  )
}

function handleClose() {
  stopOptimize()
  drawerVisible.value = false
}

function copyText(text) {
  navigator.clipboard.writeText(text).then(() => ElMessage.success('已复制到剪贴板'))
}

function onTabChange(name) {
  if (name === 'explain' && !explainResult.value && !explainAttempted.value && !explaining.value) {
    runExplain()
  } else if (name === 'optimize' && !optimizeContent.value && !optimizeAttempted.value && !optimizing.value) {
    runOptimize()
  }
}

function decodeExplainData(data) {
  if (!data || !data.rows) return data

  function tryDecodeBase64(val) {
    if (typeof val !== 'string' || !val) return val
    try {
      const decoded = atob(val)
      if (/^[\x20-\x7E\s]+$/.test(decoded)) return decoded
      const bytes = new Uint8Array(decoded.length)
      for (let i = 0; i < decoded.length; i++) bytes[i] = decoded.charCodeAt(i)
      const text = new TextDecoder().decode(bytes)
      if (text) return text
    } catch {}
    return val
  }

  const decodedRows = data.rows.map(row => {
    const newRow = {}
    for (const [key, value] of Object.entries(row)) {
      newRow[key] = tryDecodeBase64(value)
    }
    return newRow
  })

  let formattedRaw = data.raw || ''
  if (formattedRaw) {
    try {
      formattedRaw = data.raw.split('\n').map(line => {
        const eqIdx = line.indexOf('=')
        if (eqIdx === -1) return line
        const fieldName = line.substring(0, eqIdx)
        let rawValue = line.substring(eqIdx + 1).trim()
        rawValue = rawValue.replace(/^\[|\]$/g, '').trim()
        if (rawValue === '[]') return fieldName + '='
        const bytes = rawValue.split(/\s+/).map(s => parseInt(s, 10)).filter(n => !isNaN(n) && n >= 0 && n <= 255)
        if (bytes.length > 0) {
          const decoded = new TextDecoder().decode(new Uint8Array(bytes))
          return fieldName + '=' + decoded
        }
        return line
      }).join('\n')
    } catch {}
  }

  return { ...data, rows: decodedRows, raw: formattedRaw }
}

async function runExplain() {
  if (!sql?.trim()) { ElMessage.warning('SQL不能为空'); return }
  explaining.value = true
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('sql', sql)
    const res = await http.post('/sqlopt/explain', formData)
    const result = decodeExplainData(res.data.data)
    if (result && result.rows && result.rows.length) {
      explainResult.value = result
      explainAttempted.value = false
    } else {
      explainResult.value = null
      explainAttempted.value = true
      ElMessage.warning('该SQL无法生成执行计划')
    }
  } catch (e) {
    console.error('explain error:', e)
    explainResult.value = null
    explainAttempted.value = true
  } finally {
    explaining.value = false
  }
}

async function runOptimize() {
  if (!sql?.trim()) { ElMessage.warning('SQL不能为空'); return }
  if (optimizing.value) return

  stopOptimize()
  optimizing.value = true
  optimizeContent.value = ''
  thinkingText.value = ''
  optimizeError.value = ''
  optimizeAttempted.value = false
  thinkingExpanded.value = true

  const controller = new AbortController()
  abortController.value = controller

  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('sql', sql)
    if (explainResult.value) {
      formData.append('explainResult', JSON.stringify(explainResult.value))
    }

    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch('/api/sqlopt/optimize', {
      method: 'POST',
      headers: { 'Authorization': auth },
      body: formData,
      signal: controller.signal,
    })

    if (!resp.ok) {
      optimizeError.value = 'AI 服务请求失败 (HTTP ' + resp.status + ')'
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

        let chunk
        try {
          chunk = JSON.parse(data)
        } catch {
          continue
        }

        switch (chunk.type) {
          case 'thinking':
            thinkingText.value += chunk.content
            break
          case 'content':
            optimizeContent.value += chunk.content
            break
          case 'error':
            optimizeError.value = chunk.content || 'AI 处理出错'
            break
          case 'done':
            break
        }
      }
    }
  } catch (e) {
    if (e.name !== 'AbortError') {
      optimizeError.value = 'AI 服务请求失败: ' + (e.message || '未知错误')
      console.error('optimize stream error:', e)
    }
  } finally {
    optimizing.value = false
    optimizeAttempted.value = true
    abortController.value = null
    if (!thinkingText.value) {
      thinkingExpanded.value = false
    }
  }
}

function stopOptimize() {
  if (abortController.value) {
    abortController.value.abort()
    abortController.value = null
  }
}

watch(drawerVisible, (val) => {
  if (val) {
    if (!mdReady.value) initDeps()
    if (sql?.trim()) {
      activeTab.value = 'explain'
      runExplain()
    }
  } else if (!val) {
    stopOptimize()
    explainResult.value = null
    optimizeContent.value = ''
    thinkingText.value = ''
    optimizeError.value = ''
    explainAttempted.value = false
    optimizeAttempted.value = false
    activeTab.value = 'explain'
    optimizing.value = false
  }
})

watch(() => sql, (newSql, oldSql) => {
  if (!drawerVisible.value || !newSql?.trim()) return
  if (newSql === oldSql) return
  explainResult.value = null
  optimizeContent.value = ''
  thinkingText.value = ''
  optimizeError.value = ''
  explainAttempted.value = false
  optimizeAttempted.value = false
  activeTab.value = 'explain'
})

onUnmounted(() => {
  stopOptimize()
  document.removeEventListener('mousemove', onDrag)
  document.removeEventListener('mouseup', stopDrag)
})
</script>

<style scoped>
.drag-handle {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 6px;
  cursor: ns-resize;
  z-index: 10;
}

.toolbar-area {
  display: flex;
  align-items: flex-start;
  position: relative;
}

.optimize-tabs {
  flex: 1;
}

.optimize-tabs :deep(.el-tabs__header) {
  margin-bottom: 0;
}

.optimize-tabs :deep(.el-tabs__nav-wrap::after) {
  display: none;
}

.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.tab-body {
  padding-top: 12px;
  min-height: 120px;
}

.explain-section {
  background: #fafbfc;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  overflow: hidden;
}

.explain-table-wrapper {
  padding: 12px;
  background: #fff;
}

.explain-raw {
  margin: 0 12px 12px;
  background: #1e1e2e;
  border-radius: 6px;
  overflow: hidden;
}

.raw-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: #2d2d3f;
  color: #a6adc8;
  font-size: 12px;
  font-weight: 500;
  user-select: none;
}

.raw-arrow {
  font-size: 12px;
  margin-right: 4px;
  transition: transform 0.3s ease;
  vertical-align: middle;
}

.raw-arrow.expanded {
  transform: rotate(90deg);
}

.raw-content {
  margin: 0;
  padding: 12px;
  color: #67c23a;
  font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 180px;
  overflow: auto;
}

.thinking-section {
  margin-bottom: 12px;
  background: #f5f3ff;
  border: 1px solid #e0d8f0;
  border-radius: 8px;
  overflow: hidden;
}

.thinking-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: #ede9fe;
  color: #6d5c9e;
  font-size: 13px;
  font-weight: 500;
  user-select: none;
}

.thinking-arrow {
  font-size: 12px;
  margin-right: 4px;
  transition: transform 0.3s ease;
  vertical-align: middle;
}

.thinking-arrow.expanded {
  transform: rotate(90deg);
}

.thinking-content {
  padding: 10px 14px;
  color: #5b4a8a;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 200px;
  overflow: auto;
}

.optimize-error {
  margin-bottom: 12px;
}

.optimize-content {
  padding: 4px 0;
  margin: 0px 15px;
}

.markdown-body {
  font-size: 14px;
  line-height: 1.7;
  color: #303133;
}

.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3),
.markdown-body :deep(h4) {
  margin: 16px 0 8px;
  font-weight: 600;
  color: #1d1e1f;
}

.markdown-body :deep(h3) {
  font-size: 15px;
}

.markdown-body :deep(h4) {
  font-size: 14px;
}

.markdown-body :deep(p) {
  margin: 6px 0;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  padding-left: 20px;
  margin: 6px 0;
}

.markdown-body :deep(li) {
  margin: 3px 0;
}

.markdown-body :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 8px 0;
  font-size: 13px;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid #e4e7ed;
  padding: 6px 12px;
  text-align: left;
}

.markdown-body :deep(th) {
  background: #f0f5ff;
  font-weight: 600;
}

.markdown-body :deep(strong) {
  font-weight: 600;
  color: #1d1e1f;
}

.code-block-wrapper {
  position: relative;
  margin: 8px 0;
}

.code-block-wrapper .code-copy-btn {
  position: absolute;
  top: 6px;
  right: 8px;
  padding: 2px 8px;
  font-size: 11px;
  color: #909399;
  background: #f0f2f5;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  cursor: pointer;
  user-select: none;
  z-index: 2;
  transition: all 0.2s;
}

.code-block-wrapper .code-copy-btn:hover {
  color: #409eff;
  background: #ecf5ff;
  border-color: #c6e2ff;
  cursor: pointer;
}

.markdown-body :deep(.hljs) {
  background: #f8f9fa;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  padding: 14px;
  font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.5;
  overflow-x: auto;
}

.markdown-body :deep(code) {
  font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
}

.markdown-body :deep(blockquote) {
  margin: 8px 0;
  padding: 4px 12px;
  border-left: 3px solid #e0d8f0;
  background: #f5f3ff;
  color: #6d5c9e;
}

.optimize-empty {
  padding: 20px 0;
}
</style>