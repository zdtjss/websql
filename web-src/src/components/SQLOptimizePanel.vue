<template>
  <el-drawer v-model="drawerVisible" title="SQL优化分析" direction="btt" size="45%" :before-close="handleClose">
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
                <div class="section-header">
                  <span class="section-title">执行计划</span>
                  <el-tag size="small" type="primary">EXPLAIN</el-tag>
                </div>
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
                      min-width="120"
                      :show-overflow-tooltip="true"
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
            <template v-if="optimizeResult && optimizeResult.suggestions && optimizeResult.suggestions.length">
              <h4 class="suggest-title">优化建议</h4>
              <el-alert v-for="(sug, idx) in optimizeResult.suggestions" :key="idx" :title="sug.title" :type="sug.severity === 'critical' ? 'error' : sug.severity === 'warning' ? 'warning' : 'info'" :description="sug.description" :closable="false" show-icon style="margin-bottom:8px">
                <template v-if="sug.fixSql" #default>
                  <div style="display:flex;align-items:center;gap:8px;margin-top:5px">
                    <pre class="fix-sql-pre"><code>{{ sug.fixSql }}</code></pre>
                    <el-button size="small" type="primary" link @click="copyText(sug.fixSql)">复制</el-button>
                  </div>
                </template>
              </el-alert>
            </template>
            <el-empty v-else-if="optimizeAttempted" description="该SQL无法生成优化建议" :image-size="60" />
            <el-empty v-else description="点击上方 AI优化建议 页签开始分析" :image-size="60" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading, ArrowRight } from '@element-plus/icons-vue'
import http from '@/js/utils/httpProxy.js'

const props = defineProps({
  visible: Boolean,
  connId: String,
  schema: String,
  sql: String,
  dbType: String
})
const emit = defineEmits(['update:visible'])

const drawerVisible = computed({ get: () => props.visible, set: v => emit('update:visible', v) })

const explaining = ref(false)
const optimizing = ref(false)
const explainResult = ref(null)
const optimizeResult = ref(null)
const activeTab = ref('explain')
const rawExpanded = ref(false)
const explainAttempted = ref(false)
const optimizeAttempted = ref(false)

function handleClose() {
  drawerVisible.value = false
}

function copyText(text) {
  navigator.clipboard.writeText(text).then(() => ElMessage.success('已复制到剪贴板'))
}

function onTabChange(name) {
  if (name === 'explain' && !explainResult.value && !explainAttempted.value) {
    runExplain()
  } else if (name === 'optimize' && !optimizeResult.value && !optimizeAttempted.value) {
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
    } catch {
      // not base64, return as-is
    }
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
    } catch {
      // keep raw format if decoding fails
    }
  }

  return {
    ...data,
    rows: decodedRows,
    raw: formattedRaw
  }
}

async function runExplain() {
  if (!props.sql?.trim()) { ElMessage.warning('SQL不能为空'); return }
  explaining.value = true
  try {
    const formData = new FormData()
    formData.append('connId', props.connId)
    formData.append('schema', props.schema)
    formData.append('sql', props.sql)
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
  if (!props.sql?.trim()) { ElMessage.warning('SQL不能为空'); return }
  optimizing.value = true
  try {
    const formData = new FormData()
    formData.append('connId', props.connId)
    formData.append('schema', props.schema)
    formData.append('sql', props.sql)
    formData.append('useExplain', 'true')
    const res = await http.post('/sqlopt/optimize', formData)
    const data = res.data.data
    if (data) {
      optimizeResult.value = data
      optimizeAttempted.value = false
    }
    if (data && data.explainPlan) {
      const planResult = decodeExplainData(data.explainPlan)
      if (planResult && planResult.rows && planResult.rows.length) {
        explainResult.value = planResult
      }
    }
    if (data && data.suggestions && data.suggestions.length) {
      ElMessage.success('发现 ' + data.suggestions.length + ' 条优化建议')
    } else {
      ElMessage.success('SQL看起来不错，没有发现需要优化的地方')
    }
  } catch (e) {
    optimizeResult.value = null
    optimizeAttempted.value = true
  } finally {
    optimizing.value = false
  }
}

watch(() => props.visible, (val) => {
  if (val && props.sql?.trim()) {
    runExplain()
  } else if (!val) {
    explainResult.value = null
    optimizeResult.value = null
    explainAttempted.value = false
    optimizeAttempted.value = false
  }
})

watch(() => props.sql, (newSql, oldSql) => {
  if (!props.visible || !newSql?.trim()) return
  if (newSql === oldSql) return
  explainResult.value = null
  optimizeResult.value = null
  explainAttempted.value = false
  optimizeAttempted.value = false
  runExplain()
})
</script>

<style scoped>
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

.suggest-title {
  margin: 0 0 8px;
  color: #303133;
  font-size: 14px;
}

.fix-sql-pre {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 8px;
  border-radius: 4px;
  font-size: 12px;
  flex: 1;
  margin: 0;
  max-height: 120px;
  overflow: auto;
}

.explain-section {
  background: #fafbfc;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  background: linear-gradient(135deg, #f0f5ff 0%, #e8ecff 100%);
  border-bottom: 1px solid #e4e7ed;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
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
  color: #cdd6f4;
  font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 180px;
  overflow: auto;
}
</style>