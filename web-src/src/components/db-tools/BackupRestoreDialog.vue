<template>
  <el-dialog v-model="visible" title="备份与恢复" width="900px" :close-on-click-modal="false" @opened="loadBackups">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="创建备份" name="create">
        <el-form label-position="top" :inline="false" class="backup-form">
          <el-row :gutter="12">
            <el-col :span="14">
              <el-form-item label="备份名称" prop="name">
                <el-input v-model="backupName" placeholder="默认自动生成" clearable size="default" />
              </el-form-item>
            </el-col>
            <el-col :span="10">
              <el-form-item label="描述" prop="desc">
                <el-input v-model="backupDesc" placeholder="备份描述（可选）" clearable size="default" />
              </el-form-item>
            </el-col>
          </el-row>

          <div class="options-bar">
            <div class="opt-item">
              <span class="opt-label">包含数据</span>
              <el-switch v-model="withData" inline-prompt active-text="是" inactive-text="否" />
            </div>
            <div class="opt-item">
              <span class="opt-label">加密备份</span>
              <el-switch v-model="encrypt" inline-prompt active-text="开" inactive-text="关" />
            </div>
          </div>

          <el-card shadow="never" class="section-card" :body-style="{ padding: '10px' }">
            <div class="table-filter">
              <el-icon class="filter-icon"><Grid /></el-icon>
              <span class="filter-title">选择表</span>
              <el-input v-model="tableFilter" placeholder="搜索表名..." clearable size="small" style="flex:1;margin:0 12px">
                <template #prefix><el-icon><Search /></el-icon></template>
              </el-input>
              <el-button size="small" :disabled="!filteredBackupTables.length" @click="toggleAll(true)">全选</el-button>
              <el-button size="small" :disabled="!filteredBackupTables.length" @click="toggleAll(false)">清空</el-button>
              <el-tag size="small" type="info" effect="plain">已选 {{ selectedCount }} / {{ backupTables.length }}</el-tag>
            </div>
            <div class="table-grid">
              <div v-for="t in filteredBackupTables" :key="t.table" class="table-item" :class="{ active: t.checked }" @click="toggleTable(t)">
                <el-checkbox :model-value="t.checked" @change="(v) => onCheckChange(t, v)" @click.stop />
                <el-tooltip :content="t.table" placement="top" :show-after="300" :disabled="!needsTooltip(t.table)">
                  <span class="table-name">{{ t.table }}</span>
                </el-tooltip>
                <span class="table-count">{{ t.count || 0 }} 行</span>
              </div>
              <el-empty v-if="!filteredBackupTables.length" description="未找到匹配的表" :image-size="50" />
            </div>
          </el-card>

          <div v-if="backupResult" class="backup-result">
            <el-result :icon="backupResult.success ? 'success' : 'error'" :sub-title="`备份完成: ${backupResult.tables} 张表, 大小 ${formatSize(backupResult.size)}`" />
          </div>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="备份列表" name="list">
        <el-table :data="backupList" stripe max-height="400" highlight-current-row @row-click="selectBackup">
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
              <el-button type="primary" size="small" link @click.stop="downloadBackup(row)">下载</el-button>
              <el-button type="warning" size="small" link @click.stop="restoreBackup(row)" :loading="restoringId===row.id">恢复</el-button>
              <el-button type="danger" size="small" link @click.stop="deleteBackup(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!backupList.length" description="暂无备份记录" :image-size="60" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="visible=false">关闭</el-button>
      <el-button v-if="activeTab === 'create'" type="primary" @click="createBackup" :loading="creating">
        <el-icon><VideoPlay /></el-icon>
        <span>开始备份</span>
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Grid, Search, VideoPlay } from '@element-plus/icons-vue'
import {
  listBackups,
  listBackupTables,
  createBackup as createBackupApi,
  downloadBackup as downloadBackupApi,
  restoreBackup as restoreBackupApi,
  deleteBackup as deleteBackupApi,
} from '@/api/sql'

const visible = defineModel({ default: false })

const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const activeTab = ref('create')
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
  } catch (e) {}
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
  } catch (e) {}
}

async function createBackup() {
  creating.value = true
  backupResult.value = null
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
    const res = await createBackupApi(formData)
    backupResult.value = res.data.data
    if (res.data.data?.success) ElMessage.success('备份创建成功')
    else ElMessage.warning('备份创建完成，但有部分错误')
    loadBackups()
  } catch (e) {
    ElMessage.error('备份失败: ' + (e.message || '未知错误'))
  } finally {
    creating.value = false
  }
}

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
    ElMessage.error('下载失败')
  }
}

function restoreBackup(row) {
  ElMessageBox.confirm(
    `确定要恢复备份 "${row.name}" 吗？此操作将覆盖当前数据。`,
    '警告',
    { type: 'warning', confirmButtonText: '确定恢复', cancelButtonText: '取消' }
  ).then(() => doRestore(row)).catch(() => {})
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
    ElMessage.error('恢复失败: ' + (e.message || '未知错误'))
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
  } catch (e) {}
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
</style>
