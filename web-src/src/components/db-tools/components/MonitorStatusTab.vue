<template>
  <!-- 状态指标 Tab：状态计数器，可重置 -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="状态指标">
    <!-- 不支持提示（如 Oracle 10g 以下、SQLite） -->
    <el-alert v-if="unsupported" :title="unsupported" type="warning" :closable="false" show-icon />
    <template v-else>
      <div class="vars-toolbar">
        <el-input v-model="filter" placeholder="按状态名或值过滤" size="small" clearable style="width: 260px;" aria-label="过滤状态列表" />
        <el-button size="small" @click="load" :loading="loading" aria-label="刷新状态列表">刷新</el-button>
        <el-button size="small" type="warning" @click="confirmFlush" aria-label="重置状态计数器">重置状态计数器</el-button>
        <div style="flex: 1;"></div>
        <el-button
          size="small"
          type="primary"
          @click="ai.runAIAnalyze"
          :loading="ai.aiAnalyzing.value"
          :disabled="filteredList.length === 0"
          aria-label="AI 分析当前显示的状态指标"
        >AI 分析</el-button>
      </div>
      <el-table :data="filteredList" max-height="440" size="small" stripe border aria-label="状态计数器列表">
        <el-table-column prop="name" label="状态名" min-width="240" resizable show-overflow-tooltip />
        <el-table-column prop="value" label="值" min-width="180" resizable show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!loading && filteredList.length === 0" description="没有符合条件的状态" :image-size="60" />
      <!-- AI 分析结果区域 -->
      <div v-if="ai.aiAnalyzing.value || ai.aiContent.value || ai.aiError.value || ai.aiThinking.value" class="ai-result-section">
        <div class="ai-result-header" @click="ai.toggleAIExpand">
          <span>
            <el-icon class="ai-arrow" :class="{ expanded: ai.aiExpanded.value }"><ArrowRight /></el-icon>
            {{ ai.aiAnalyzing.value ? 'AI 正在分析...' : 'AI 分析结果' }}
          </span>
          <el-button v-if="ai.aiAnalyzing.value" size="small" link type="danger" @click.stop="ai.stopAIAnalyze">停止</el-button>
        </div>
        <el-collapse-transition>
          <div v-show="ai.aiExpanded.value" class="ai-result-body">
            <el-alert v-if="ai.aiError.value" :title="ai.aiError.value" type="error" :closable="false" show-icon />
            <div v-if="ai.aiThinking.value" class="ai-thinking">{{ ai.aiThinking.value }}</div>
            <div v-if="ai.aiContent.value" class="markdown-body" v-html="ai.renderedAIContent.value"></div>
          </div>
        </el-collapse-transition>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { ArrowRight } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { execSQL } from '@/api/sql'
import { getMonitorAllStatus } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'
import { useMonitorAI } from '../composables/useMonitorAI'

interface StatusItem {
  name: string
  value: string
}

const props = defineProps<{
  connId?: string
  schema?: string
  active: boolean
}>()

const loading = ref(false)
const list = ref<StatusItem[]>([])
const filter = ref('')
const unsupported = ref('')
const loaded = ref(false)

// 当前数据库类型与版本（由接口返回，供 AI 分析使用）
const dbType = ref('')
const dbVersion = ref('')

// AI 分析（本 Tab 专属实例）
const ai = useMonitorAI({
  kind: 'status',
  connId: () => props.connId,
  dbType: () => dbType.value,
  dbVersion: () => dbVersion.value,
  getSourceList: () => filteredList.value,
})

// 通过 /monitor/status/all 获取（后端按 dbType 适配：MySQL SHOW STATUS / Oracle v$sysstat）
async function load() {
  if (!props.connId) return
  loading.value = true
  try {
    const res = await getMonitorAllStatus(props.connId)
    const data = res.data?.data || {}
    if (data.dbType) dbType.value = data.dbType
    if (data.version) dbVersion.value = data.version
    if (data.supported === false) {
      list.value = []
      unsupported.value = data.unsupportedMessage || '当前数据库不支持查看状态指标'
    } else {
      list.value = (data.items || []).map((r: any) => ({
        name: r.name || '',
        value: r.value ?? '',
      }))
      unsupported.value = ''
    }
  } catch (e) {
    handleError(e, '加载状态指标')
  } finally {
    loading.value = false
    loaded.value = true
  }
}

const filteredList = computed(() => {
  const kw = filter.value.trim().toLowerCase()
  if (!kw) return list.value
  return list.value.filter(s =>
    String(s.name).toLowerCase().includes(kw) ||
    String(s.value).toLowerCase().includes(kw)
  )
})

// 重置状态计数器：FLUSH STATUS，需用户确认
function confirmFlush() {
  ElMessageBox.confirm(
    '确定要执行 FLUSH STATUS 重置大部分状态计数器吗？该操作会将会话级状态计数器清零。',
    '重置状态确认',
    { type: 'warning', confirmButtonText: '重置', cancelButtonText: '取消' }
  ).then(() => doFlush()).catch((e: unknown) => {
    if (e !== 'cancel' && e !== 'close') handleError(e, '重置状态')
  })
}

async function doFlush() {
  if (!props.connId) return
  try {
    await execSQL({ connId: props.connId, schema: props.schema || '', sql: 'FLUSH STATUS', maxLine: '1' })
    ElMessage.success('状态计数器已重置')
    await load()
  } catch (e) { handleError(e, '重置状态') }
}

// 首次激活时加载数据
watch(
  () => props.active,
  (active) => {
    if (active && !loaded.value && !unsupported.value) load()
  },
  { immediate: true },
)

onUnmounted(() => {
  ai.stopAIAnalyze()
})

defineExpose({ refresh: load })
</script>

<style scoped>
/* 服务器变量 / 状态指标 Tab 工具栏 */
.vars-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

/* AI 分析结果区域 */
.ai-result-section {
  margin-top: 12px;
  border: 1px solid var(--db-card-border);
  border-radius: 6px;
  background: var(--db-card-bg);
  overflow: hidden;
}

.ai-result-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  background: var(--db-bg-secondary);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  color: var(--db-text-primary);
}

.ai-arrow {
  transition: transform 0.2s;
  margin-right: 6px;
  vertical-align: middle;
}

.ai-arrow.expanded {
  transform: rotate(90deg);
}

.ai-result-body {
  padding: 12px;
  max-height: 360px;
  overflow-y: auto;
}

.ai-thinking {
  margin-bottom: 8px;
  padding: 8px 10px;
  background: var(--db-bg-secondary);
  border-radius: 4px;
  font-size: 12px;
  color: var(--db-text-tertiary);
  white-space: pre-wrap;
  max-height: 180px;
  overflow-y: auto;
}

.ai-result-body .markdown-body {
  font-size: 13px;
  line-height: 1.6;
}

.ai-result-body .markdown-body :deep(h3) {
  margin: 10px 0 6px;
  font-size: 14px;
  font-weight: 600;
}

.ai-result-body .markdown-body :deep(h4) {
  margin: 8px 0 4px;
  font-size: 13px;
  font-weight: 600;
}

.ai-result-body .markdown-body :deep(p) {
  margin: 6px 0;
}

.ai-result-body .markdown-body :deep(ul),
.ai-result-body .markdown-body :deep(ol) {
  padding-left: 20px;
  margin: 6px 0;
}

.ai-result-body .markdown-body :deep(code) {
  padding: 1px 4px;
  background: var(--db-bg-secondary);
  border-radius: 3px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
}

.ai-result-body .markdown-body :deep(pre) {
  padding: 8px 10px;
  background: var(--db-bg-secondary);
  border-radius: 4px;
  overflow-x: auto;
}

.ai-result-body .markdown-body :deep(pre code) {
  padding: 0;
  background: transparent;
}
</style>
