<template>
  <!-- 概览 Tab：关键指标卡片 + 服务器基础信息 -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="监控概览">
    <el-row :gutter="10" style="margin-bottom: 12px;">
      <el-col :span="8">
        <div class="stat-card" role="group" :aria-label="`连接数：${metrics?.connections ?? 0}`">
          <div class="stat-value">{{ metrics?.connections ?? 0 }}</div>
          <div class="stat-label">连接数（活跃 {{ metrics?.activeConnections ?? 0 }}）</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="stat-card" role="group" :aria-label="`QPS：${(metrics?.qps ?? 0).toFixed(1)}`">
          <div class="stat-value">{{ (metrics?.qps ?? 0).toFixed(1) }}</div>
          <div class="stat-label">QPS</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="stat-card" role="group" :aria-label="`TPS：${(metrics?.tps ?? 0).toFixed(1)}`">
          <div class="stat-value">{{ (metrics?.tps ?? 0).toFixed(1) }}</div>
          <div class="stat-label">TPS</div>
        </div>
      </el-col>
    </el-row>
    <el-row :gutter="10" style="margin-bottom: 12px;">
      <el-col :span="8">
        <div class="stat-card" role="group" :aria-label="`线程：${metrics?.threadsRunning ?? 0}`">
          <div class="stat-value">{{ metrics?.threadsRunning ?? 0 }} <span class="stat-sub">/ {{ metrics?.threadsConnected ?? 0 }}</span></div>
          <div class="stat-label">线程（运行 / 连接）</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="stat-card" role="group" :aria-label="`慢查询：${metrics?.slowQueries ?? 0}`">
          <div class="stat-value" :style="{ color: (metrics?.slowQueries ?? 0) > 0 ? 'var(--db-danger)' : 'var(--db-success)' }">{{ metrics?.slowQueries ?? 0 }}</div>
          <div class="stat-label">慢查询</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="stat-card" role="group" :aria-label="`锁等待：${metrics?.lockWaits ?? 0}`">
          <div class="stat-value" :style="{ color: (metrics?.lockWaits ?? 0) > 0 ? 'var(--db-danger)' : 'var(--db-success)' }">{{ metrics?.lockWaits ?? 0 }}</div>
          <div class="stat-label">锁等待</div>
        </div>
      </el-col>
    </el-row>

    <!-- Buffer Pool 命中率与使用情况 -->
    <div v-if="resources" class="buffer-section">
      <div class="buffer-row">
        <span class="buffer-label">Buffer Pool 命中率</span>
        <span class="buffer-value" :style="{ color: (resources.bufferPoolHitRate ?? 0) > 95 ? 'var(--db-success)' : 'var(--db-warning)' }" aria-live="polite">{{ (resources.bufferPoolHitRate ?? 0).toFixed(1) }}%</span>
      </div>
      <el-progress
        :percentage="resources.bufferPoolHitRate ?? 0"
        :stroke-width="10"
        :color="(resources.bufferPoolHitRate ?? 0) > 95 ? '#67c23a' : '#e6a23c'"
        role="progressbar"
        :aria-valuenow="Math.round(resources.bufferPoolHitRate ?? 0)"
        aria-valuemin="0"
        aria-valuemax="100"
        aria-label="Buffer Pool 命中率"
      />
      <div v-if="resources.bufferPoolSize" class="buffer-row" style="margin-top: 10px;">
        <span class="buffer-label">Buffer Pool 使用</span>
        <span class="buffer-value">{{ formatBytes(resources.bufferPoolUsed ?? 0) }} / {{ formatBytes(resources.bufferPoolSize ?? 0) }}</span>
      </div>
      <el-progress
        v-if="resources.bufferPoolSize"
        :percentage="resources.bufferPoolSize ? Math.round((resources.bufferPoolUsed ?? 0) / resources.bufferPoolSize * 100) : 0"
        :stroke-width="10"
        role="progressbar"
        :aria-valuenow="resources.bufferPoolSize ? Math.round((resources.bufferPoolUsed ?? 0) / resources.bufferPoolSize * 100) : 0"
        aria-valuemin="0"
        aria-valuemax="100"
        aria-label="Buffer Pool 使用率"
      />
    </div>

    <!-- 资源概览：数据/索引大小、表行数、InnoDB 行操作 -->
    <el-row v-if="resources" :gutter="10" style="margin: 12px 0;">
      <el-col :span="8">
        <div class="mini-stat">
          <div class="mini-label">数据大小</div>
          <div class="mini-value">{{ formatBytes(resources.dataSize) }}</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="mini-stat">
          <div class="mini-label">索引大小</div>
          <div class="mini-value">{{ formatBytes(resources.indexSize) }}</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="mini-stat">
          <div class="mini-label">表 / 行数</div>
          <div class="mini-value">{{ resources.tableCount ?? 0 }} <span class="stat-sub">/ {{ formatNum(resources.totalRows) }} 行</span></div>
        </div>
      </el-col>
    </el-row>
    <el-row v-if="resources" :gutter="10" style="margin-bottom: 12px;">
      <el-col :span="8">
        <div class="mini-stat">
          <div class="mini-label">InnoDB 读</div>
          <div class="mini-value">{{ formatNum(resources.innodbRowsRead ?? 0) }}</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="mini-stat">
          <div class="mini-label">InnoDB 插入</div>
          <div class="mini-value">{{ formatNum(resources.innodbRowsInserted ?? 0) }}</div>
        </div>
      </el-col>
      <el-col :span="8">
        <div class="mini-stat">
          <div class="mini-label">InnoDB 更新</div>
          <div class="mini-value">{{ formatNum(resources.innodbRowsUpdated ?? 0) }}</div>
        </div>
      </el-col>
    </el-row>

    <!-- 服务器基础信息（来自 SHOW STATUS / VARIABLES） -->
    <el-descriptions v-if="Object.keys(serverInfo).length > 0" :column="2" border size="small" aria-label="服务器基础信息">
      <el-descriptions-item v-for="(val, key) in serverInfo" :key="key" :label="key">{{ val }}</el-descriptions-item>
    </el-descriptions>

    <div class="overview-toolbar">
      <el-button type="primary" size="small" @click="refresh" :loading="loading" aria-label="刷新监控数据">刷新</el-button>
      <el-button size="small" :type="autoRefresh ? 'success' : ''" :aria-pressed="autoRefresh" :aria-label="autoRefresh ? '停止自动刷新' : '每 5 秒自动刷新'" @click="toggleAutoRefresh">
        {{ autoRefresh ? '停止自动' : '自动刷新' }}
      </el-button>
      <span v-if="metrics" class="update-time" aria-live="polite">更新于 {{ metrics.timestamp }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'
import { getMonitorMetrics, getMonitorResources, getMonitorAllStatus, getMonitorAllVariables } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'
import { formatBytes, formatNum, formatUptime } from '../composables/useMonitorFormat'

interface MonitorMetrics {
  connections?: number
  activeConnections?: number
  qps?: number
  tps?: number
  threadsRunning?: number
  threadsConnected?: number
  slowQueries?: number
  lockWaits?: number
  timestamp?: string
  [key: string]: unknown
}

interface MonitorResources {
  bufferPoolHitRate?: number
  bufferPoolSize?: number
  bufferPoolUsed?: number
  dataSize?: number
  indexSize?: number
  tableCount?: number
  totalRows?: number
  innodbRowsRead?: number
  innodbRowsInserted?: number
  innodbRowsUpdated?: number
  [key: string]: unknown
}

const props = defineProps<{
  connId?: string
  schema?: string
  active: boolean
}>()

const loading = ref(false)
const metrics = ref<MonitorMetrics | null>(null)
const resources = ref<MonitorResources | null>(null)
const serverInfo = ref<Record<string, string>>({})

const autoRefresh = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

// 当前数据库类型与版本（由 /monitor/variables/all 等接口返回，供本 Tab 内部使用）
const dbType = ref('')
const dbVersion = ref('')

async function loadMetrics() {
  if (!props.connId) return
  try {
    const res = await getMonitorMetrics(props.connId)
    // 后端 response.WriteOK 包成 {code,msg,data}，前端拦截器返回完整 response，
    // 故真实快照在 res.data.data；之前取 res.data 拿到外壳导致指标卡片为 0、"更新于"为空
    metrics.value = res.data?.data
  } catch (e) { handleError(e, '加载监控指标') }
}

async function loadResources() {
  if (!props.connId) return
  try {
    const res = await getMonitorResources(props.connId, props.schema || '')
    resources.value = res.data?.data?.dbResources
  } catch (e) { handleError(e, '加载资源监控') }
}

// 加载服务器基础信息（运行时间、版本、字符集等），通过带方言适配的监控接口获取。
// MySQL/MariaDB: 从 SHOW STATUS/VARIABLES 提取；Oracle: 从 v$parameter/v$sysstat 提取对应字段；
// 不支持的数据库（如 SQLite）跳过，serverInfo 保持为空。
async function loadServerInfo() {
  if (!props.connId) return
  try {
    const [statusRes, varsRes] = await Promise.all([
      getMonitorAllStatus(props.connId),
      getMonitorAllVariables(props.connId, 'global'),
    ])
    const statusData = statusRes.data?.data || {}
    const varsData = varsRes.data?.data || {}

    // 同步数据库类型与版本
    if (varsData.dbType) dbType.value = varsData.dbType
    if (varsData.version) dbVersion.value = varsData.version

    // 不支持的数据库类型：保持 serverInfo 为空，不报错
    if (statusData.supported === false || varsData.supported === false) {
      serverInfo.value = {}
      return
    }

    const statusMap: Record<string, string> = {}
    const varsMap: Record<string, string> = {}
    ;(statusData.items || []).forEach((r: { name: string; value: string }) => { statusMap[r.name] = r.value })
    ;(varsData.items || []).forEach((r: { name: string; value: string }) => { varsMap[r.name] = r.value })

    const type = varsData.dbType || dbType.value
    if (type === 'oracle') {
      // Oracle 字段映射：v$parameter 小写命名，无 datadir/Uptime/Slow_queries 概念。
      serverInfo.value = {
        '数据库版本': varsData.version || '-',
        '字符集': varsMap['nls_characterset'] || varsMap['nls_language'] || '-',
        '会话/进程上限': varsMap['processes'] || varsMap['sessions'] || '-',
        'SGA 目标': varsMap['sga_target'] || '-',
        'PGA 聚合目标': varsMap['pga_aggregate_target'] || '-',
      }
    } else {
      // MySQL / MariaDB：沿用原 SHOW STATUS/VARIABLES 字段名
      const uptime = parseInt(statusMap['Uptime'] || '0')
      serverInfo.value = {
        '运行时间': formatUptime(uptime),
        '数据库版本': varsMap['version'] || '-',
        '数据目录': varsMap['datadir'] || '-',
        '字符集': varsMap['character_set_server'] || '-',
        '连接数(活跃/上限)': (statusMap['Threads_connected'] || '?') + ' / ' + (varsMap['max_connections'] || '?'),
        '慢查询数': statusMap['Slow_queries'] || '0',
      }
    }
  } catch (e) { handleError(e, '加载服务器信息') }
}

async function refresh() {
  loading.value = true
  await Promise.all([loadMetrics(), loadResources(), loadServerInfo()])
  loading.value = false
}

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
}

function startAutoRefresh() {
  stopAutoRefresh()
  timer = setInterval(refresh, 5000)
}

function stopAutoRefresh() {
  if (timer) { clearInterval(timer); timer = null }
}

watch(autoRefresh, (val) => {
  if (val) startAutoRefresh()
  else stopAutoRefresh()
})

// 首次激活时加载数据
watch(
  () => props.active,
  (active) => {
    if (active) refresh()
  },
  { immediate: true },
)

// 切换离开本 Tab 时停止自动刷新，避免后台无用轮询
watch(
  () => props.active,
  (active) => {
    if (!active && autoRefresh.value) {
      // 保留 autoRefresh 状态但停止定时器，切回时恢复
      stopAutoRefresh()
    } else if (active && autoRefresh.value && !timer) {
      startAutoRefresh()
    }
  },
)

onUnmounted(() => {
  stopAutoRefresh()
})

defineExpose({ refresh })
</script>

<style scoped>
/* 指标卡片：使用 db-tools.css 中的 CSS 变量，支持深色模式 */
.stat-card {
  background: var(--db-card-bg);
  border: 1px solid var(--db-card-border);
  border-radius: 8px;
  padding: 14px;
  text-align: center;
  box-shadow: var(--db-card-shadow);
}

.stat-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--db-accent);
  font-family: 'JetBrains Mono', monospace;
  line-height: 1.2;
}

.stat-label {
  font-size: 12px;
  color: var(--db-text-tertiary);
  margin-top: 4px;
}

.stat-sub {
  font-size: 12px;
  font-weight: 400;
  color: var(--db-text-tertiary);
}

.buffer-section {
  margin: 8px 0 4px;
}

.buffer-row {
  display: flex;
  justify-content: space-between;
  margin-bottom: 4px;
  font-size: 12px;
}

.buffer-label {
  color: var(--db-text-tertiary);
}

.buffer-value {
  font-weight: 600;
  color: var(--db-text-primary);
}

.mini-stat {
  text-align: center;
  padding: 8px;
  background: var(--db-bg-secondary);
  border-radius: 6px;
}

.mini-label {
  font-size: 12px;
  color: var(--db-text-tertiary);
}

.mini-value {
  font-size: 15px;
  font-weight: 600;
  color: var(--db-text-primary);
}

.overview-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.update-time {
  color: var(--db-text-tertiary);
  font-size: 12px;
}
</style>
