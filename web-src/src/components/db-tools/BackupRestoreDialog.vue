<template>
  <el-dialog v-model="visible" title="备份与恢复" width="900px" :close-on-click-modal="false" aria-label="备份与恢复对话框" @opened="onDialogOpened">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="创建备份" name="create">
        <el-form label-position="top" :inline="false" class="backup-form">
          <el-row :gutter="12">
            <el-col :span="14">
              <el-form-item label="备份名称" prop="name">
                <el-input ref="backupNameInputRef" v-model="backupName" placeholder="默认自动生成" clearable size="default" aria-label="备份名称" />
              </el-form-item>
            </el-col>
            <el-col :span="10">
              <el-form-item label="描述" prop="desc">
                <el-input v-model="backupDesc" placeholder="备份描述（可选）" clearable size="default" aria-label="备份描述" />
              </el-form-item>
            </el-col>
          </el-row>

          <div class="options-bar">
            <div class="opt-item">
              <span class="opt-label">包含数据</span>
              <el-switch v-model="withData" inline-prompt active-text="是" inactive-text="否" aria-label="备份是否包含数据" />
            </div>
            <div class="opt-item">
              <span class="opt-label">加密备份</span>
              <el-switch v-model="encrypt" inline-prompt active-text="开" inactive-text="关" aria-label="是否加密备份" />
            </div>
          </div>

          <el-card shadow="never" class="section-card" :body-style="{ padding: '10px' }">
            <div class="table-filter">
              <el-icon class="filter-icon"><Grid /></el-icon>
              <span class="filter-title">选择表</span>
              <el-input v-model="tableFilter" placeholder="搜索表名..." clearable size="small" style="flex:1;margin:0 12px" aria-label="过滤表名">
                <template #prefix><el-icon><Search /></el-icon></template>
              </el-input>
              <el-button size="small" :disabled="!filteredBackupTables.length" @click="toggleAll(true)" aria-label="全选当前过滤结果">全选</el-button>
              <el-button size="small" :disabled="!filteredBackupTables.length" @click="toggleAll(false)" aria-label="清空选择">清空</el-button>
              <!-- 已选数量动态更新，aria-live 通知屏幕阅读器 -->
              <el-tag size="small" type="info" effect="plain" aria-live="polite">已选 {{ selectedCount }} / {{ backupTables.length }}</el-tag>
            </div>
            <div class="table-grid" role="group" aria-label="表选择列表">
              <div v-for="t in filteredBackupTables" :key="t.table" class="table-item" :class="{ active: t.checked }"
                role="checkbox" :aria-checked="t.checked" :aria-label="`${t.table}，${t.count || 0} 行`" tabindex="0"
                @click="toggleTable(t)" @keyup.enter="toggleTable(t)" @keyup.space.prevent="toggleTable(t)">
                <el-checkbox :model-value="t.checked" @change="(v) => onCheckChange(t, v)" @click.stop :aria-label="`选择表 ${t.table}`" />
                <el-tooltip :content="t.table" placement="top" :show-after="300" :disabled="!needsTooltip(t.table)">
                  <span class="table-name">{{ t.table }}</span>
                </el-tooltip>
                <span class="table-count">{{ t.count || 0 }} 行</span>
              </div>
              <el-empty v-if="!filteredBackupTables.length" description="未找到匹配的表" :image-size="50" />
            </div>
          </el-card>

          <div v-if="backupProgress" class="backup-progress">
            <div class="progress-header">
              <el-icon class="is-loading" color="#409eff"><Loading /></el-icon>
              <span class="progress-title">正在备份...</span>
            </div>
            <el-progress :percentage="backupProgressPercent" :status="backupProgress.status === 'failed' ? 'exception' : undefined" :stroke-width="18" :text-inside="true" :format="() => `${backupProgressPercent}%`" />
            <div class="progress-detail">
              <span>已处理 {{ backupProgress.processedTables || 0 }} / {{ backupProgress.totalTables || 0 }} 表</span>
              <span v-if="backupProgress.currentTable" class="current-table">当前: {{ backupProgress.currentTable }}</span>
            </div>
            <div class="progress-meta">
              <span>已导出 {{ formatSize(backupProgress.exportedBytes || 0) }}</span>
              <span v-if="backupEta">预计剩余 {{ backupEta }}</span>
            </div>
          </div>

          <div v-if="backupResult" class="backup-result">
            <el-result :icon="backupResult.success ? 'success' : 'error'" :sub-title="`备份完成: ${backupResult.tables} 张表, 大小 ${formatSize(backupResult.size)}`" />
          </div>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="备份列表" name="list">
        <el-table :data="backupList" stripe max-height="400" highlight-current-row aria-label="备份记录列表" @row-click="selectBackup">
          <el-table-column prop="name" label="名称" width="200" />
          <el-table-column prop="schema" label="Schema" width="120" />
          <el-table-column label="大小" width="100">
            <template #default="{row}">
              {{ formatSize(row.size || 0) }}
            </template>
          </el-table-column>
          <el-table-column label="类型" width="80">
            <template #default="{row}">
              <el-tag size="small">{{ row.type }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="加密" width="70">
            <template #default="{row}">
              <el-tag v-if="row.encrypted" type="success" size="small">是</el-tag>
              <el-tag v-else type="info" size="small">否</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="createdAt" label="创建时间" width="170" />
          <el-table-column prop="description" label="描述" min-width="140" :show-overflow-tooltip="true" />
          <el-table-column label="操作" width="200" fixed="right">
            <template #default="{row}">
              <el-button type="primary" size="small" link @click.stop="downloadBackup(row)" :aria-label="`下载备份 ${row.name}`">下载</el-button>
              <el-button type="warning" size="small" link @click.stop="restoreBackup(row)" :loading="restoringId===row.id" :aria-label="`恢复备份 ${row.name}`">恢复</el-button>
              <el-button type="danger" size="small" link @click.stop="deleteBackup(row)" :aria-label="`删除备份 ${row.name}`">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!backupList.length" description="暂无备份记录" :image-size="60" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="visible=false">关闭</el-button>
      <el-button v-if="activeTab === 'create'" type="primary" @click="createBackup" :loading="creating" aria-keyshortcuts="Alt+Enter">
        <el-icon><VideoPlay /></el-icon>
        <span>开始备份</span>
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, onBeforeUnmount, useTemplateRef, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Grid, Search, VideoPlay, Loading } from '@element-plus/icons-vue'
import {
  listBackups,
  listBackupTables,
  createBackup as createBackupApi,
  getBackupProgress,
  downloadBackup as downloadBackupApi,
  restoreBackup as restoreBackupApi,
  deleteBackup as deleteBackupApi,
} from '@/api/sql'
import { handleError } from '@/utils/errorHandler'

const visible = defineModel({ default: false })

const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const activeTab = ref('create')
const backupNameInputRef = useTemplateRef('backupNameInputRef')
const backupName = ref('')
const backupDesc = ref('')
const withData = ref(true)
const encrypt = ref(false)
const backupTables = ref([])
const tableFilter = ref('')
const creating = ref(false)
const backupResult = ref(null)
const backupList = ref([])
const selectedBackup = ref(null)
const restoringId = ref('')

// 备份进度相关状态
const backupProgress = ref(null)     // 后端返回的进度对象
let progressTimer = null              // 轮询定时器
let progressStartTime = 0             // 备份开始时间戳（用于估算剩余时间）

// 进度百分比：后端未返回 totalTables 时按 status 推断（0 或 100）
const backupProgressPercent = computed(() => {
  const p = backupProgress.value
  if (!p) return 0
  if (p.status === 'completed') return 100
  if (p.status === 'failed') return p.processedTables && p.totalTables ? Math.round(p.processedTables / p.totalTables * 100) : 0
  if (!p.totalTables) return 0
  return Math.min(99, Math.round((p.processedTables || 0) / p.totalTables * 100))
})

// 预计剩余时间：基于已处理表数和已用时间估算
const backupEta = computed(() => {
  const p = backupProgress.value
  if (!p || p.status !== 'running') return ''
  const processed = p.processedTables || 0
  const total = p.totalTables || 0
  if (processed < 1 || total <= processed) return ''
  const elapsedMs = Date.now() - progressStartTime
  if (elapsedMs < 500) return ''
  const msPerTable = elapsedMs / processed
  const remainingMs = (total - processed) * msPerTable
  return formatDuration(remainingMs)
})

function formatDuration(ms) {
  if (ms < 1000) return '<1秒'
  const sec = Math.round(ms / 1000)
  if (sec < 60) return `${sec}秒`
  const min = Math.floor(sec / 60)
  const remSec = sec % 60
  if (min < 60) return `${min}分${remSec}秒`
  const hr = Math.floor(min / 60)
  return `${hr}小时${min % 60}分`
}

const filteredBackupTables = computed(() => {
  if (!tableFilter.value) return backupTables.value
  const kw = tableFilter.value.toLowerCase().trim()
  if (!kw) return backupTables.value
  return backupTables.value.filter(t => t.table.toLowerCase().includes(kw))
})

const selectedCount = computed(() => backupTables.value.filter(t => t.checked).length)

function needsTooltip(name) {
  return name && name.length > 18
}

function toggleTable(t) {
  t.checked = !t.checked
}

function onCheckChange(t, val) {
  t.checked = val
}

function toggleAll(val) {
  filteredBackupTables.value.forEach(t => { t.checked = val })
}

function formatSize(bytes) {
  if (!bytes) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1048576).toFixed(2) + ' MB'
}

async function loadBackups() {
  try {
    const res = await listBackups(connId, schema)
    backupList.value = res.data.data?.records || []
  } catch (e) { handleError(e, '加载备份列表') }
  try {
    const res = await listBackupTables(connId, schema)
    const tables = res.data.data?.tables || []
    const tableCounts = res.data.data?.tableCounts || []
    const countMap = new Map(tableCounts.map(c => [c.table, c.rows ?? c.count ?? 0]))
    backupTables.value = tables.map(t => ({
      table: t.table,
      checked: t.checked !== false,
      count: countMap.get(t.table) || 0
    }))
  } catch (e) { handleError(e, '加载备份表列表') }
}

// 对话框打开时聚焦到第一个输入框，便于键盘操作
function onDialogOpened() {
  loadBackups()
  nextTick(() => {
    backupNameInputRef.value?.focus?.()
  })
}

async function createBackup() {
  creating.value = true
  backupResult.value = null
  backupProgress.value = null
  progressStartTime = Date.now()
  try {
    const selected = backupTables.value.filter(t => t.checked).map(t => t.table).join(',')
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('name', backupName.value)
    formData.append('description', backupDesc.value)
    formData.append('tables', selected)
    formData.append('withData', withData.value.toString())
    formData.append('encrypt', encrypt.value.toString())
    formData.append('compress', 'false')
    // 异步接口：立即返回 taskId，实际备份在后台执行
    const res = await createBackupApi(formData)
    const taskId = res.data.data?.taskId
    if (!taskId) {
      throw new Error('未获取到备份任务 ID')
    }
    // 开始轮询进度（每 500ms 一次）
    startProgressPolling(taskId)
  } catch (e) {
    handleError(e, '创建备份')
    creating.value = false
    backupProgress.value = null
  }
}

// 轮询备份进度，直到任务完成或失败
function startProgressPolling(taskId) {
  stopProgressPolling()
  progressTimer = setInterval(async () => {
    try {
      const res = await getBackupProgress(taskId)
      const data = res.data.data
      if (!data) {
        stopProgressPolling()
        creating.value = false
        return
      }
      // not_found 表示进度已过期被清理
      if (data.status === 'not_found') {
        stopProgressPolling()
        creating.value = false
        ElMessage.warning('备份进度信息已过期，请刷新列表查看结果')
        loadBackups()
        return
      }
      backupProgress.value = data
      if (data.status === 'completed') {
        stopProgressPolling()
        creating.value = false
        backupResult.value = data.result || null
        if (data.result?.success) ElMessage.success('备份创建成功')
        else ElMessage.warning('备份创建完成，但有部分错误')
        // 延迟清除进度显示，让用户看到 100%
        setTimeout(() => { backupProgress.value = null }, 1500)
        loadBackups()
      } else if (data.status === 'failed') {
        stopProgressPolling()
        creating.value = false
        ElMessage.error('备份失败: ' + (data.error || '未知错误'))
        setTimeout(() => { backupProgress.value = null }, 2000)
        loadBackups()
      }
    } catch (e) {
      // 网络抖动等异常不停止轮询，下次重试
    }
  }, 500)
}

function stopProgressPolling() {
  if (progressTimer) {
    clearInterval(progressTimer)
    progressTimer = null
  }
}

// 组件卸载时清理定时器，避免内存泄漏
onBeforeUnmount(() => {
  stopProgressPolling()
})

function selectBackup(row) { selectedBackup.value = row }

async function downloadBackup(row) {
  try {
    const res = await downloadBackupApi(row.id)
    const blob = new Blob([res.data])
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${row.name}.sql`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    handleError(e, '下载备份')
  }
}

function restoreBackup(row) {
  ElMessageBox.confirm(
    `确定要恢复备份 "${row.name}" 吗？此操作将覆盖当前数据。`,
    '警告',
    { type: 'warning', confirmButtonText: '确定恢复', cancelButtonText: '取消' }
  ).then(() => doRestore(row)).catch((e) => {
    if (e !== 'cancel' && e !== 'close') handleError(e, '恢复备份')
  })
}

async function doRestore(row) {
  restoringId.value = row.id
  try {
    const formData = new FormData()
    formData.append('backupId', row.id)
    formData.append('connId', connId)
    formData.append('schema', schema)
    const res = await restoreBackupApi(formData)
    if (res.data.data?.success) ElMessage.success(`恢复成功，执行 ${res.data.data?.executed || 0} 条语句`)
    else ElMessage.error(`恢复部分失败: ${(res.data.data?.failedCount || 0)} 条错误`)
    loadBackups()
  } catch (e) {
    handleError(e, '恢复备份')
  } finally {
    restoringId.value = ''
  }
}

async function deleteBackup(row) {
  try {
    await ElMessageBox.confirm(`确定删除备份 "${row.name}" 吗？`, '确认删除', { type: 'warning' })
    const formData = new FormData()
    formData.append('backupId', row.id)
    await deleteBackupApi(formData)
    ElMessage.success('删除成功')
    selectedBackup.value = null
    loadBackups()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') handleError(e, '删除备份')
  }
}
</script>

<style scoped>
.backup-form :deep(.el-form-item) {
  margin-bottom: 12px;
}

.options-bar {
  display: flex;
  align-items: center;
  gap: 28px;
  padding: 6px 0 10px;
  margin-bottom: 4px;
}

.opt-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.opt-label {
  color: #606266;
  font-size: 13px;
}

.section-card {
  border: 1px solid #ebeef5;
  border-radius: 4px;
}

.table-filter {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 8px;
  border-bottom: 1px dashed #ebeef5;
  margin-bottom: 8px;
}

.filter-icon {
  color: #409eff;
  font-size: 14px;
}

.filter-title {
  font-weight: 600;
  color: #303133;
  font-size: 13px;
}

.table-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 8px;
  max-height: 300px;
  overflow-y: auto;
  padding: 4px;
}

.table-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0px 3px;
  background: #fafbfc;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s;
  user-select: none;
}

.table-item:hover {
  border-color: #409eff;
  background: #ecf5ff;
}

.table-item.active {
  border-color: #409eff;
  background: #ecf5ff;
}

.table-item :deep(.el-checkbox) {
  margin-right: 0;
  flex-shrink: 0;
}

.table-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 500;
  color: #303133;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.table-count {
  font-size: 11px;
  color: #909399;
  white-space: nowrap;
  flex-shrink: 0;
  padding-left: 4px;
}

.backup-result {
  margin-top: 12px;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
}

.backup-progress {
  margin-top: 12px;
  padding: 16px;
  background: #f5f7fa;
  border: 1px solid #d9ecff;
  border-radius: 6px;
}

.progress-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.progress-title {
  font-weight: 600;
  color: #303133;
  font-size: 14px;
}

.progress-detail {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-top: 10px;
  font-size: 13px;
  color: #606266;
}

.progress-detail .current-table {
  color: #409eff;
  font-weight: 500;
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.progress-meta {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-top: 6px;
  font-size: 12px;
  color: #909399;
}
</style>
