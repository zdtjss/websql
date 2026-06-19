<template>
  <div class="audit-log-container">
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
      <span style="font-size: 16px; font-weight: 600; color: var(--text-primary);">SQL 审计日志</span>
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
        />
      </el-form-item>
      <el-form-item label="来源">
        <el-select v-model="queryParams.source" placeholder="全部" clearable style="width: 130px;">
          <el-option label="Agent" value="agent" />
          <el-option label="SQL编辑器" value="sqleditor" />
        </el-select>
      </el-form-item>
      <el-form-item label="SQL类型">
        <el-select v-model="queryParams.sqlType" placeholder="全部" clearable style="width: 120px;">
          <el-option label="SELECT" value="SELECT" />
          <el-option label="INSERT" value="INSERT" />
          <el-option label="UPDATE" value="UPDATE" />
          <el-option label="DELETE" value="DELETE" />
          <el-option label="DROP" value="DROP" />
          <el-option label="ALTER" value="ALTER" />
          <el-option label="TRUNCATE" value="TRUNCATE" />
          <el-option label="IMPORT" value="IMPORT" />
        </el-select>
      </el-form-item>
      <el-form-item label="风险等级">
        <el-select v-model="queryParams.riskLevel" placeholder="全部" clearable style="width: 100px;">
          <el-option label="高" value="high" />
          <el-option label="中" value="medium" />
          <el-option label="低" value="low" />
        </el-select>
      </el-form-item>
      <el-form-item label="用户">
        <el-select
          v-model="queryParams.userId"
          placeholder="输入用户名搜索"
          clearable
          filterable
          remote
          :remote-method="searchUsers"
          :loading="userLoading"
          style="width: 220px;"
        >
          <el-option v-for="user in userList" :key="user.id" :label="`${user.name} (${user.loginName})`" :value="user.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="关键词">
        <el-input v-model="queryParams.keyword" placeholder="SQL 关键字" clearable style="width: 180px;" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="handleSearch">查询</el-button>
        <el-button @click="handleReset">重置</el-button>
      </el-form-item>
    </el-form>

    <div class="table-wrapper">
      <el-table :data="logs" stripe border style="width: 100%" height="100%" v-loading="loading">
        <el-table-column prop="execTime" label="执行时间" width="160" sortable resizable />
        <el-table-column prop="source" label="来源" width="80" resizable>
          <template #default="{ row }">
            <el-tag :type="row.source === 'agent' ? 'primary' : 'info'" size="small">
              {{ row.source === 'agent' ? 'Agent' : '编辑器' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="userName" label="用户" width="90" resizable />
        <el-table-column prop="sqlType" label="类型" width="90" resizable>
          <template #default="{ row }">
            <el-tag :type="getTypeTag(row.sqlType)" size="small">{{ row.sqlType }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="riskLevel" label="风险" width="70" resizable>
          <template #default="{ row }">
            <el-tag :type="getRiskTag(row.riskLevel)" size="small">
              {{ getRiskLabel(row.riskLevel) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="70" resizable>
          <template #default="{ row }">
            <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">
              {{ row.status === 'success' ? '成功' : '失败' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="execTimeMs" label="耗时(ms)" width="90" resizable />
        <el-table-column prop="affectedRows" label="影响行数" width="90" resizable />
        <el-table-column prop="sqlText" label="SQL 语句" min-width="300" resizable>
          <template #default="{ row }">
            <div class="sql-cell" @click="showSqlDetail(row)" style="cursor: pointer;">
              <span class="sql-preview" :title="row.sqlText">{{ row.sqlText }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="errorMsg" label="错误信息" min-width="200" resizable>
          <template #default="{ row }">
            <div v-if="row.errorMsg" class="error-cell" @click="showErrorDetail(row)" style="cursor: pointer;">
              <span class="error-preview" :title="row.errorMsg">{{ row.errorMsg }}</span>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && logs.length === 0" description="暂无审计日志" />
    </div>

    <div v-if="total > 0" class="pagination-bar">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        @current-change="loadLogs"
        @size-change="onSizeChange"
        size="small"
      />
    </div>

    <el-dialog v-model="sqlDialogVisible" title="SQL 详情" width="700px" destroy-on-close class="sql-detail-dialog">
      <div style="margin-bottom: 8px; color: var(--text-tertiary); font-size: 13px;">
        {{ sqlDetail.userName }} | {{ sqlDetail.execTime || '' }} | {{ sqlDetail.source === 'agent' ? 'Agent' : '编辑器' }}
      </div>
      <pre class="sql-full-text" v-html="highlightedSql"></pre>
    </el-dialog>

    <el-dialog v-model="errorDialogVisible" title="错误详情" width="700px" destroy-on-close>
      <pre class="error-full-text">{{ errorDetail }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { getAuditLogs, findUserBase } from '@/api/system'
import { highlightSql } from '@/utils/lazyDeps.js'
import { ElMessage } from 'element-plus'
import { onMounted, ref, reactive, computed, watch } from 'vue'

const logs = ref([])
const loading = ref(false)
const userList = ref([])
const userLoading = ref(false)
const queryParams = reactive({ userId: '', source: '', sqlType: '', riskLevel: '', keyword: '' })
const dateRange = ref([])
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const sqlDialogVisible = ref(false)
const sqlDetail = ref({})
const highlightedSql = ref('')

watch(sqlDialogVisible, async (visible) => {
  if (visible && sqlDetail.value.sqlText) {
    highlightedSql.value = await highlightSql(sqlDetail.value.sqlText)
  } else {
    highlightedSql.value = ''
  }
})

const errorDialogVisible = ref(false)
const errorDetail = ref('')

async function loadLogs() {
  loading.value = true
  try {
    const params = { page: currentPage.value, pageSize: pageSize.value }
    if (queryParams.userId) params.userId = queryParams.userId
    if (queryParams.source) params.source = queryParams.source
    if (queryParams.sqlType) params.sqlType = queryParams.sqlType
    if (queryParams.riskLevel) params.riskLevel = queryParams.riskLevel
    if (queryParams.keyword) params.keyword = queryParams.keyword
    if (dateRange.value && dateRange.value.length === 2) {
      params.startTime = dateRange.value[0]
      params.endTime = dateRange.value[1]
    }
    const resp = await getAuditLogs(params)
    logs.value = resp.data.data || []
    total.value = resp.data.total || 0
  } catch (e) {
    console.error('[SQLAuditLog] 加载审计日志失败:', e)
    ElMessage.error('加载审计日志失败')
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  currentPage.value = 1
  loadLogs()
}

async function searchUsers(query) {
  if (!query) {
    userList.value = []
    return
  }
  userLoading.value = true
  try {
    const resp = await findUserBase(query)
    userList.value = resp.data.data || []
  } catch (e) {
    console.error('[SQLAuditLog] 搜索用户失败:', e)
  } finally {
    userLoading.value = false
  }
}

function handleReset() {
  queryParams.userId = ''
  queryParams.source = ''
  queryParams.sqlType = ''
  queryParams.riskLevel = ''
  queryParams.keyword = ''
  dateRange.value = []
  currentPage.value = 1
  loadLogs()
}

function onSizeChange() {
  currentPage.value = 1
  loadLogs()
}

function getTypeTag(type_) {
  const map = { DROP: 'danger', TRUNCATE: 'danger', DELETE: 'warning', ALTER: 'warning', UPDATE: 'info', INSERT: 'success', SELECT: 'info', CREATE: 'info', IMPORT: 'warning', SHOW: 'info', DESCRIBE: 'info', EXPLAIN: 'info' }
  return map[type_] || 'info'
}

function getRiskTag(level) {
  const map = { high: 'danger', medium: 'warning', low: 'info' }
  return map[level] || 'info'
}

function getRiskLabel(level) {
  const map = { high: '高', medium: '中', low: '低' }
  return map[level] || level
}

function showSqlDetail(row) {
  sqlDetail.value = row
  sqlDialogVisible.value = true
}

function showErrorDetail(row) {
  errorDetail.value = row.errorMsg
  errorDialogVisible.value = true
}

onMounted(() => {
  loadLogs()
})
</script>

<style scoped>
.audit-log-container {
  padding: 16px;
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.table-wrapper {
  flex: 1;
  overflow: hidden;
}

.pagination-bar {
  display: flex;
  justify-content: flex-end;
  height: 60px;
  flex-shrink: 0;
}

.sql-cell {
  max-width: 100%;
  overflow: hidden;
}

.sql-preview {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--accent-color);
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 13px;
}

.sql-preview:hover {
  text-decoration: underline;
}

.sql-full-text {
  background: var(--bg-secondary);
  border: 1px solid var(--border-primary);
  border-radius: 4px;
  padding: 12px;
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 520px;
  overflow-y: auto;
  color: var(--text-primary);
}

.error-cell {
  max-width: 100%;
  overflow: hidden;
}

.error-preview {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--el-color-danger);
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 13px;
}

.error-preview:hover {
  text-decoration: underline;
}

.error-full-text {
  background: var(--bg-secondary);
  border: 1px solid var(--border-primary);
  border-radius: 4px;
  padding: 12px;
  font-family: 'Consolas', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 500px;
  overflow-y: auto;
  color: var(--el-color-danger);
}
</style>

<style>
.sql-detail-dialog .el-dialog__body {
  max-height: 70vh;
  overflow-y: auto;
}
</style>