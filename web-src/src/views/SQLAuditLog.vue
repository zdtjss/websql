<template>
  <div class="audit-log-container">
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
      <span style="font-size: 16px; font-weight: 600; color: #303133;">SQL 审计日志</span>
      <el-button size="small" @click="loadLogs" :loading="loading">刷新</el-button>
    </div>

    <el-form :inline="true" :model="queryParams" style="margin-bottom: 16px;">
      <el-form-item label="执行时间">
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          format="YYYY-MM-DD HH:mm:ss"
          value-format="YYYY-MM-DD HH:mm:ss"
          :default-time="[new Date(), new Date()]"
        />
      </el-form-item>
      <el-form-item label="用户">
        <el-select v-model="queryParams.userId" placeholder="请选择用户" clearable filterable style="width: 200px;">
          <el-option v-for="user in userList" :key="user.id" :label="`${user.name} (${user.loginName})`" :value="user.id" />
        </el-select>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="handleSearch">查询</el-button>
        <el-button @click="handleReset">重置</el-button>
      </el-form-item>
    </el-form>

    <el-table :data="logs" stripe border style="width: 100%" max-height="calc(100vh - 340px)" v-loading="loading">
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
import { ElMessage } from 'element-plus'
import { onMounted, ref } from 'vue'

const logs = ref([])
const loading = ref(false)
const userList = ref([])
const apiBase = import.meta.env.VITE_API_URL || ''
const queryParams = ref({ userId: '' })
const dateRange = ref([])

async function loadLogs() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (queryParams.value.userId) {
      params.append('userId', queryParams.value.userId)
    }
    if (dateRange.value && dateRange.value.length === 2) {
      params.append('startTime', dateRange.value[0])
      params.append('endTime', dateRange.value[1])
    }
    const queryString = params.toString()
    const url = apiBase + '/ai/agent/audit/logs' + (queryString ? `?${queryString}` : '')
    
    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch(url, {
      headers: { 'Authorization': auth }
    })
    if (!resp.ok) throw new Error(`请求失败：${resp.status}`)
    const data = await resp.json()
    logs.value = data.data || []
  } catch (e) {
    console.error('[SQLAuditLog] 加载审计日志失败:', e)
    ElMessage.error('加载审计日志失败')
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  loadLogs()
}

async function loadUsers() {
  try {
    const auth = sessionStorage.getItem('authentication') || ''
    const resp = await fetch(apiBase + '/findUser', {
      headers: { 'Authorization': auth }
    })
    if (!resp.ok) throw new Error(`请求失败：${resp.status}`)
    const data = await resp.json()
    userList.value = data.data || []
  } catch (e) {
    console.error('[SQLAuditLog] 加载用户列表失败:', e)
  }
}

function handleReset() {
  queryParams.value.userId = ''
  dateRange.value = []
  loadLogs()
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

onMounted(() => {
  loadUsers()
  loadLogs()
})
</script>

<style scoped>
.audit-log-container {
  padding: 16px;
}
</style>
