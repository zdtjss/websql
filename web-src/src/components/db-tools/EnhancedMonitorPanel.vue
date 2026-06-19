<template>
  <el-dialog v-model="drawerVisible" title="数据库监控" width="560px" :close-on-click-modal="false" @opened="onOpen" @close="onClose">
    <div style="padding:0 5px">
      <el-row :gutter="10" style="margin-bottom:12px">
        <el-col :span="12">
          <el-button type="primary" size="small" @click="refreshAll" :loading="refreshing">刷新</el-button>
          <el-button size="small" @click="toggleAutoRefresh" :type="autoRefresh ? 'success' : ''">
            {{ autoRefresh ? '停止自动' : '自动刷新' }}
          </el-button>
        </el-col>
        <el-col :span="12" style="text-align:right">
          <span v-if="autoRefresh" style="color:#67c23a;font-size:12px">每5秒自动刷新</span>
          <span v-if="metrics" style="color:#909399;font-size:12px">更新于 {{metrics.timestamp}}</span>
        </el-col>
      </el-row>

      <div v-if="metrics">
        <h4 style="color:#303133;margin-bottom:10px">实时指标</h4>
        <el-row :gutter="10" style="margin-bottom:15px">
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">连接数</div>
              <div style="font-size:22px;font-weight:bold;color:#409EFF">{{metrics.connections}}</div>
              <div style="font-size:11px;color:#67c23a">活跃 {{metrics.activeConnections}}</div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">QPS</div>
              <div style="font-size:22px;font-weight:bold;color:#409EFF">{{(metrics.qps||0).toFixed(1)}}</div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">TPS</div>
              <div style="font-size:22px;font-weight:bold;color:#409EFF">{{(metrics.tps||0).toFixed(1)}}</div>
            </el-card>
          </el-col>
        </el-row>
        <el-row :gutter="10" style="margin-bottom:15px">
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">线程</div>
              <div style="font-size:22px;font-weight:bold">{{metrics.threadsRunning||0}}</div>
              <div style="font-size:11px;color:#909399">/ {{metrics.threadsConnected||0}}</div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">慢查询</div>
              <div style="font-size:22px;font-weight:bold" :style="{color: metrics.slowQueries > 0 ? '#f56c6c' : '#67c23a'}">{{metrics.slowQueries||0}}</div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">锁等待</div>
              <div style="font-size:22px;font-weight:bold" :style="{color: metrics.lockWaits > 0 ? '#f56c6c' : '#67c23a'}">{{metrics.lockWaits||0}}</div>
            </el-card>
          </el-col>
        </el-row>
      </div>

      <el-divider />

      <div v-if="resources">
        <h4 style="color:#303133;margin-bottom:10px">资源监控</h4>
        <el-row :gutter="10" style="margin-bottom:12px">
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">数据大小</div>
              <div style="font-size:16px;font-weight:bold">{{formatBytes(resources.dataSize)}}</div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">索引大小</div>
              <div style="font-size:16px;font-weight:bold">{{formatBytes(resources.indexSize)}}</div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card shadow="hover" body-style="padding:12px;text-align:center">
              <div style="color:#909399;font-size:12px;margin-bottom:4px">表/行数</div>
              <div style="font-size:16px;font-weight:bold">{{resources.tableCount||0}}</div>
              <div style="font-size:11px;color:#909399">{{formatNum(resources.totalRows)}}行</div>
            </el-card>
          </el-col>
        </el-row>

        <div style="margin-bottom:12px">
          <div style="display:flex;justify-content:space-between;margin-bottom:4px">
            <span style="font-size:12px;color:#909399">Buffer Pool 命中率</span>
            <span style="font-size:12px;font-weight:bold" :style="{color: (resources.bufferPoolHitRate||0) > 95 ? '#67c23a' : '#e6a23c'}">{{(resources.bufferPoolHitRate||0).toFixed(1)}}%</span>
          </div>
          <el-progress :percentage="resources.bufferPoolHitRate||0" :stroke-width="10" :color="(resources.bufferPoolHitRate||0) > 95 ? '#67c23a' : '#e6a23c'" />
        </div>

        <div v-if="resources.bufferPoolSize" style="margin-bottom:12px">
          <div style="display:flex;justify-content:space-between;margin-bottom:4px">
            <span style="font-size:12px;color:#909399">Buffer Pool 使用</span>
            <span style="font-size:12px;color:#606266">{{formatBytes(resources.bufferPoolUsed||0)}} / {{formatBytes(resources.bufferPoolSize||0)}}</span>
          </div>
          <el-progress :percentage="resources.bufferPoolSize ? Math.round((resources.bufferPoolUsed||0) / resources.bufferPoolSize * 100) : 0" :stroke-width="10" />
        </div>

        <el-row :gutter="10" style="margin-bottom:12px">
          <el-col :span="8">
            <div style="text-align:center">
              <div style="color:#909399;font-size:12px">InnoDB读</div>
              <div style="font-weight:bold">{{formatNum(resources.innodbRowsRead||0)}}</div>
            </div>
          </el-col>
          <el-col :span="8">
            <div style="text-align:center">
              <div style="color:#909399;font-size:12px">InnoDB插入</div>
              <div style="font-weight:bold">{{formatNum(resources.innodbRowsInserted||0)}}</div>
            </div>
          </el-col>
          <el-col :span="8">
            <div style="text-align:center">
              <div style="color:#909399;font-size:12px">InnoDB更新</div>
              <div style="font-weight:bold">{{formatNum(resources.innodbRowsUpdated||0)}}</div>
            </div>
          </el-col>
        </el-row>
      </div>

      <el-divider />

      <div v-if="processes && processes.length">
        <h4 style="color:#303133;margin-bottom:10px">进程列表 (前10)</h4>
        <el-table :data="processes.slice(0,10)" size="small" max-height="200" border>
          <el-table-column prop="id" label="ID" width="60" />
          <el-table-column prop="user" label="用户" width="80" />
          <el-table-column prop="command" label="命令" width="80" />
          <el-table-column prop="time" label="时间" width="60" />
          <el-table-column prop="state" label="状态" width="100" :show-overflow-tooltip="true" />
          <el-table-column prop="info" label="SQL" min-width="150" :show-overflow-tooltip="true" />
        </el-table>
      </div>

      <el-empty v-if="!metrics && !resources && !refreshing" description="点击刷新获取监控数据" :image-size="60" />
    </div>
  </el-dialog>
</template>

<script setup>
import { ref, watch, onUnmounted } from 'vue'
import { getMonitorMetrics, getMonitorResources, getMonitorProcesses } from '@/api/conn'

const drawerVisible = defineModel('visible', { default: false })
const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const metrics = ref(null)
const resources = ref(null)
const processes = ref([])
const refreshing = ref(false)
const autoRefresh = ref(false)
let timer = null

function formatBytes(val) {
  if (!val || val === 0) return '0 B'
  val = Number(val)
  if (val < 1024) return val + ' B'
  if (val < 1048576) return (val / 1024).toFixed(1) + ' KB'
  if (val < 1073741824) return (val / 1048576).toFixed(2) + ' MB'
  return (val / 1073741824).toFixed(2) + ' GB'
}

function formatNum(val) {
  if (!val) return '0'
  val = Number(val)
  if (val >= 1000000) return (val / 1000000).toFixed(1) + 'M'
  if (val >= 1000) return (val / 1000).toFixed(1) + 'K'
  return val.toString()
}

async function loadMetrics() {
  if (!connId) return
  try {
    const res = await getMonitorMetrics(connId)
    metrics.value = res.data
  } catch (e) {}
}

async function loadResources() {
  if (!connId) return
  try {
    const res = await getMonitorResources(connId, schema)
    resources.value = res.data.dbResources || res.data
  } catch (e) {}
}

async function loadProcesses() {
  if (!connId) return
  try {
    const res = await getMonitorProcesses(connId)
    processes.value = res.data.processes || []
  } catch (e) {}
}

async function refreshAll() {
  refreshing.value = true
  await Promise.all([loadMetrics(), loadResources(), loadProcesses()])
  refreshing.value = false
}

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
}

function startAutoRefresh() {
  stopAutoRefresh()
  timer = setInterval(refreshAll, 5000)
}

function stopAutoRefresh() {
  if (timer) { clearInterval(timer); timer = null }
}

function onOpen() {
  refreshAll()
}

function onClose() {
  autoRefresh.value = false
  stopAutoRefresh()
}

watch(autoRefresh, (val) => {
  if (val) startAutoRefresh()
  else stopAutoRefresh()
})

onUnmounted(() => stopAutoRefresh())
</script>
