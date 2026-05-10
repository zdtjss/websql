<template>
  <el-dialog
    v-model="visible"
    title="服务器状态监控"
    width="900px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
    @opened="loadAll"
  >
    <el-tabs v-model="activeTab" type="card">
      <el-tab-pane label="概览" name="overview">
        <div v-loading="loading" style="min-height: 200px;">
          <el-row :gutter="16" style="margin-bottom: 16px;">
            <el-col :span="6" v-for="card in overviewCards" :key="card.label">
              <div class="stat-card">
                <div class="stat-value">{{ card.value }}</div>
                <div class="stat-label">{{ card.label }}</div>
              </div>
            </el-col>
          </el-row>
          <el-descriptions :column="2" border v-if="Object.keys(overviewData).length > 0">
            <el-descriptions-item v-for="(val, key) in overviewData" :key="key" :label="key">
              {{ val }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </el-tab-pane>

      <el-tab-pane label="连接" name="connections">
        <div v-loading="connLoading" style="min-height: 200px;">
          <div style="margin-bottom:12px;display:flex;gap:8px;align-items:center;">
            <span style="font-weight:600;">活跃连接: {{ processList.length }}</span>
            <span style="color: var(--text-tertiary);font-size:12px;">总连接上限: {{ maxConnections }}</span>
            <el-progress :percentage="connPercentage" :stroke-width="8" style="flex:1;max-width:200px;" />
          </div>
          <el-table :data="processList" max-height="400" size="small" stripe>
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="user" label="用户" width="120" />
            <el-table-column prop="host" label="来源" width="180" />
            <el-table-column prop="db" label="数据库" width="120" />
            <el-table-column prop="command" label="命令" width="90">
              <template #default="scope">
                <el-tag size="small" :type="scope.row.command === 'Sleep' ? 'info' : 'warning'">{{ scope.row.command }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="time" label="时间(s)" width="80" />
            <el-table-column prop="state" label="状态" min-width="180" show-overflow-tooltip />
          </el-table>
        </div>
      </el-tab-pane>

      <el-tab-pane label="性能" name="performance">
        <div v-loading="perfLoading" style="min-height: 200px;">
          <el-row :gutter="16" style="margin-bottom: 16px;">
            <el-col :span="8" v-for="card in perfCards" :key="card.label">
              <div class="stat-card">
                <div class="stat-value">{{ card.value }}</div>
                <div class="stat-label">{{ card.label }}</div>
              </div>
            </el-col>
          </el-row>
          <el-descriptions :column="2" border v-if="Object.keys(perfData).length > 0">
            <el-descriptions-item v-for="(val, key) in perfData" :key="key" :label="key">
              {{ val }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import http from '../js/utils/httpProxy.js'

const props = defineProps({
  modelValue: Boolean,
  connId: String,
  schema: String,
})

const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const activeTab = ref('overview')
const loading = ref(false)
const connLoading = ref(false)
const perfLoading = ref(false)

const overviewData = ref({})
const processList = ref([])
const maxConnections = ref(0)
const perfData = ref({})

const overviewCards = ref([])
const perfCards = ref([])

const connPercentage = computed(() => {
  if (maxConnections.value === 0) return 0
  return Math.round((processList.value.length / maxConnections.value) * 100)
})

async function execQuery(sql) {
  const params = new URLSearchParams()
  params.append('connId', props.connId)
  params.append('schema', props.schema)
  params.append('sql', sql)
  params.append('maxLine', '500')
  const resp = await http.post('/execSQL', params)
  return resp.data.data?.data || []
}

async function loadAll() {
  await Promise.all([loadOverview(), loadConnections(), loadPerformance()])
}

async function loadOverview() {
  loading.value = true
  try {
    const statusRows = await execQuery('SHOW STATUS')
    const varsRows = await execQuery('SHOW VARIABLES')
    const statusMap = {}
    const varsMap = {}
    statusRows.forEach(r => { statusMap[r.Variable_name || r.variable_name] = r.Value || r.value })
    varsRows.forEach(r => { varsMap[r.Variable_name || r.variable_name] = r.Value || r.value })

    const uptime = parseInt(statusMap['Uptime'] || '0')
    overviewData.value = {
      '运行时间': formatUptime(uptime),
      '数据库版本': varsMap['version'] || '-',
      '数据目录': varsMap['datadir'] || '-',
      '字符集': varsMap['character_set_server'] || '-',
      '连接数(活跃/上限)': (statusMap['Threads_connected'] || '?') + ' / ' + (varsMap['max_connections'] || '?'),
      '慢查询数': statusMap['Slow_queries'] || '0',
    }

    overviewCards.value = [
      { label: 'QPS', value: statusMap['Queries'] || '0' },
      { label: 'TPS', value: formatNumber(statusMap['Com_commit'] + statusMap['Com_rollback'] || 0) },
      { label: '运行时间', value: formatUptime(uptime) },
      { label: '缓冲池命中率', value: calcBufferHitRate(statusMap) + '%' },
    ]
  } catch {} finally { loading.value = false }
}

async function loadConnections() {
  connLoading.value = true
  try {
    processList.value = await execQuery('SHOW PROCESSLIST')
    processList.value = processList.value.map(p => ({
      id: p.Id || p.id,
      user: p.User || p.user,
      host: p.Host || p.host,
      db: p.db || p.database || '-',
      command: p.Command || p.command,
      time: p.Time || p.time,
      state: p.State || p.state || '',
    }))
    const varsRows = await execQuery("SHOW VARIABLES LIKE 'max_connections'")
    if (varsRows.length > 0) {
      maxConnections.value = parseInt(varsRows[0].Value || varsRows[0].value || '151')
    }
  } catch {} finally { connLoading.value = false }
}

async function loadPerformance() {
  perfLoading.value = true
  try {
    const statusRows = await execQuery('SHOW STATUS')
    const statusMap = {}
    statusRows.forEach(r => { statusMap[r.Variable_name || r.variable_name] = r.Value || r.value })

    perfData.value = {
      '表锁等待(立即/等待)': (statusMap['Table_locks_immediate'] || '0') + ' / ' + (statusMap['Table_locks_waited'] || '0'),
      'InnoDB行锁等待': statusMap['Innodb_row_lock_waits'] || '0',
      'InnoDB平均锁等待(ms)': statusMap['Innodb_row_lock_time_avg'] || '0',
      '临时表(磁盘/总计)': (statusMap['Created_tmp_disk_tables'] || '0') + ' / ' + (statusMap['Created_tmp_tables'] || '0'),
      '全表扫描(Select_scan)': statusMap['Select_scan'] || '0',
      'Handler读(读首行/随机/下一页)': formatHandlerReads(statusMap),
    }

    perfCards.value = [
      { label: 'InnoDB缓冲池读/写', value: (statusMap['Innodb_buffer_pool_read_requests'] || '0') + ' / ' + (statusMap['Innodb_buffer_pool_write_requests'] || '0') },
      { label: 'InnoDB磁盘页读取', value: statusMap['Innodb_buffer_pool_reads'] || '0' },
      { label: '打开的表', value: statusMap['Open_tables'] || '0'},
    ]
  } catch {} finally { perfLoading.value = false }
}

function formatUptime(seconds) {
  if (!seconds || seconds <= 0) return '-'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const parts = []
  if (d > 0) parts.push(d + '天')
  if (h > 0) parts.push(h + '时')
  parts.push(m + '分')
  return parts.join(' ')
}

function formatNumber(n) {
  if (!n) return '0'
  const num = parseInt(n)
  if (num >= 1000000000) return (num / 1000000000).toFixed(1) + 'B'
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K'
  return num.toString()
}

function calcBufferHitRate(map) {
  const read = parseInt(map['Innodb_buffer_pool_reads'] || '0')
  const req = parseInt(map['Innodb_buffer_pool_read_requests'] || '0')
  if (req === 0) return '100'
  return Math.round((1 - read / req) * 100)
}

function formatHandlerReads(map) {
  return (map['Handler_read_first'] || '0') + ' / ' + (map['Handler_read_rnd'] || '0') + ' / ' + (map['Handler_read_next'] || '0')
}
</script>

<style scoped>
.stat-card {
  background: var(--bg-secondary);
  border: 1px solid var(--border-primary);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}
.stat-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--accent-color);
  font-family: 'JetBrains Mono', monospace;
}
.stat-label {
  font-size: 12px;
  color: var(--text-tertiary);
  margin-top: 4px;
}
</style>