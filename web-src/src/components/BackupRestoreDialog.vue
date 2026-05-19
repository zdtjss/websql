<template>
  <el-dialog v-model="visible" title="备份与恢复" width="1000px" :close-on-click-modal="false" @opened="loadBackups">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="创建备份" name="create">
        <el-form label-position="top">
          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="备份名称">
                <el-input v-model="backupName" placeholder="默认自动生成" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="描述">
                <el-input v-model="backupDesc" placeholder="备份描述（可选）" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-row :gutter="16">
            <el-col :span="8">
              <el-form-item label="包含数据">
                <el-switch v-model="withData" active-text="是" inactive-text="仅结构" />
              </el-form-item>
            </el-col>
            <el-col :span="8">
              <el-form-item label="加密备份">
                <el-switch v-model="encrypt" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-form-item label="选择表（留空则全部）">
            <div style="max-height:200px;overflow:auto;border:1px solid #dcdfe6;border-radius:4px;padding:10px">
              <el-checkbox v-model="selectAll" @change="toggleAll" style="margin-bottom:8px">全选/取消全选</el-checkbox>
              <div v-for="t in backupTables" :key="t.table" style="display:inline-block;margin-right:15px;margin-bottom:4px">
                <el-checkbox v-model="t.checked">{{ t.table }}</el-checkbox>
                <span style="font-size:12px;color:#909399">({{ t.count || 0 }}行)</span>
              </div>
            </div>
          </el-form-item>
          <el-button type="primary" @click="createBackup" :loading="creating">
            开始备份
          </el-button>
        </el-form>
        <div v-if="backupResult" style="margin-top:15px">
          <el-result :icon="backupResult.success ? 'success' : 'error'" :sub-title="`备份完成: ${backupResult.tables} 张表, 大小 ${formatSize(backupResult.size)}`" />
        </div>
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
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '@/js/utils/httpProxy.js'

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
const selectAll = ref(true)
const creating = ref(false)
const backupResult = ref(null)
const backupList = ref([])
const selectedBackup = ref(null)
const restoringId = ref('')

function toggleAll(val) {
  backupTables.value.forEach(t => t.checked = val)
}

function formatSize(bytes) {
  if (!bytes) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1048576).toFixed(2) + ' MB'
}

async function loadBackups() {
  try {
    const res = await http.get('/backup/list', { params: { connId, schema } })
    backupList.value = res.data.data?.records || []
  } catch (e) {}
  try {
    const res = await http.get('/backup/tables', { params: { connId, schema } })
    backupTables.value = (res.data.data?.tables || []).map(t => ({ ...t, checked: true }))
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
    const res = await http.post('/backup/create', formData)
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
    const res = await http.get('/backup/download', { params: { backupId: row.id }, responseType: 'blob' })
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
    const res = await http.post('/backup/restore', formData)
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
    await http.post('/backup/delete', formData)
    ElMessage.success('删除成功')
    selectedBackup.value = null
    loadBackups()
  } catch (e) {}
}
</script>
