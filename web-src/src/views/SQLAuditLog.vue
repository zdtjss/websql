<template>
  <div class="audit-log-container">
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
      <span style="font-size: 16px; font-weight: 600; color: #303133;">SQL 审计日志</span>
      <el-button size="small" @click="loadLogs" :loading="loading">刷新</el-button>
    </div>

    <el-table :data="logs" stripe border style="width: 100%" max-height="calc(100vh - 280px)" v-loading="loading">
      <el-table-column prop="execTime" label="执行时间" width="170" sortable>
        <template #default="{ row }">
          {{ formatDate(row.execTime) }}
        </template>
      </el-table-column>
      <el-table-column prop="userName" label="用户" width="100" />
      <el-table-column prop="sqlType" label="类型" width="90">
        <template #default="{ row }">
          <el-tag :type="getTypeTag(row.sqlType)" size="small">{{ row.sqlType }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="riskLevel" label="风险" width="80">
        <template #default="{ row }">
          <el-tag :type="row.riskLevel === 'high' ? 'danger' : 'warning'" size="small">{{ row.riskLevel }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">
            {{ row.status === 'success' ? '成功' : '失败' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="affectedRows" label="影响行数" width="100" />
      <el-table-column prop="sqlText" label="SQL 语句" min-width="300" show-overflow-tooltip />
      <el-table-column prop="errorMsg" label="错误信息" width="200" show-overflow-tooltip />
    </el-table>

    <el-empty v-if="!loading && logs.length === 0" description="暂无审计日志" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'

const logs = ref([])
const loading = ref(false)
const apiBase = import.meta.env.VITE_API_URL || ''

async function loadLogs() {
  loading.value = true
  try {
    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch(apiBase + '/ai/agent/audit/logs', {
      headers: { 'Authorization': auth }
    })
    if (!resp.ok) throw new Error(`请求失败：${resp.status}`)
    const data = await resp.json()
    logs.value = data.data || []
  } catch (e) {
    ElMessage.error(e.message || '加载审计日志失败')
  } finally {
    loading.value = false
  }
}

function formatDate(isoString) {
  if (!isoString) return ''
  const d = new Date(isoString)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleString('zh-CN')
}

function getTypeTag(type_) {
  const map = { DROP: 'danger', TRUNCATE: 'danger', DELETE: 'warning', ALTER: 'warning', UPDATE: 'info', INSERT: 'success', CREATE: '' }
  return map[type_] || ''
}

onMounted(loadLogs)
</script>

<style scoped>
.audit-log-container {
  padding: 16px;
}
</style>
