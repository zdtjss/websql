<template>
  <!-- 会话与进程 Tab：合并进程列表，支持搜索与 Kill -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="会话与进程列表">
    <div class="session-toolbar">
      <el-input
        v-model="filter"
        placeholder="按 user / host / state / db 过滤"
        size="small"
        clearable
        style="width: 280px;"
        aria-label="过滤会话列表"
      />
      <span class="session-count" aria-live="polite">共 {{ filteredList.length }} / {{ list.length }} 个连接</span>
      <div style="flex: 1;"></div>
      <el-select v-model="interval" size="small" style="width: 110px;" aria-label="自动刷新间隔" @change="onIntervalChange">
        <el-option label="不自动" :value="0" />
        <el-option label="每 5 秒" :value="5000" />
        <el-option label="每 10 秒" :value="10000" />
        <el-option label="每 30 秒" :value="30000" />
      </el-select>
      <el-button size="small" @click="load" :loading="loading" aria-label="刷新会话列表">刷新</el-button>
    </div>
    <el-table :data="filteredList" max-height="420" size="small" stripe border aria-label="数据库会话与进程列表">
      <el-table-column prop="id" label="ID" width="70" resizable />
      <el-table-column prop="user" label="用户" width="110" resizable show-overflow-tooltip />
      <el-table-column prop="host" label="来源" width="180" resizable show-overflow-tooltip />
      <el-table-column prop="db" label="数据库" width="120" resizable show-overflow-tooltip>
        <template #default="scope">{{ scope.row.db || '-' }}</template>
      </el-table-column>
      <el-table-column prop="command" label="命令" width="90" resizable>
        <template #default="scope">
          <el-tag size="small" :type="scope.row.command === 'Sleep' ? 'info' : 'warning'">{{ scope.row.command }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="time" label="时间(s)" width="80" resizable />
      <el-table-column prop="state" label="状态" min-width="150" show-overflow-tooltip resizable />
      <el-table-column prop="info" label="SQL" min-width="180" show-overflow-tooltip resizable />
      <el-table-column label="操作" width="80" fixed="right" resizable>
        <template #default="scope">
          <el-button size="small" link type="danger" :aria-label="`终止连接 ${scope.row.id}`" @click="confirmKill(scope.row as SessionRow)">Kill</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && filteredList.length === 0" description="没有符合条件的连接" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { execSQL } from '@/api/sql'
import { getMonitorProcesses } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'

interface SessionRow {
  id: number | string
  user: string
  host: string
  db: string
  command: string
  time: number
  state: string
  info: string
}

const props = defineProps<{
  connId?: string
  schema?: string
  active: boolean
}>()

const loading = ref(false)
const list = ref<SessionRow[]>([])
const filter = ref('')
const interval = ref(0) // 0 表示不自动
let timer: ReturnType<typeof setInterval> | null = null
const loaded = ref(false)

// 通过 /monitor/processes 接口获取（后端已做方言适配：MySQL SHOW PROCESSLIST / Oracle v$session）
async function load() {
  if (!props.connId) return
  loading.value = true
  try {
    const res = await getMonitorProcesses(props.connId)
    const rows = res.data?.data?.processes || []
    list.value = rows.map((p: any) => ({
      id: p.id,
      user: p.user || '',
      host: p.host || '',
      db: p.db || '',
      command: p.command || '',
      time: p.time ?? 0,
      state: p.state || '',
      info: p.info || '',
    }))
  } catch (e) {
    handleError(e, '加载会话列表')
  } finally {
    loading.value = false
    loaded.value = true
  }
}

const filteredList = computed(() => {
  const kw = filter.value.trim().toLowerCase()
  if (!kw) return list.value
  return list.value.filter(s =>
    String(s.user || '').toLowerCase().includes(kw) ||
    String(s.host || '').toLowerCase().includes(kw) ||
    String(s.state || '').toLowerCase().includes(kw) ||
    String(s.db || '').toLowerCase().includes(kw)
  )
})

function onIntervalChange(val: number) {
  stopAutoRefresh()
  if (val > 0) {
    timer = setInterval(load, val)
  }
}

function stopAutoRefresh() {
  if (timer) { clearInterval(timer); timer = null }
}

// Kill 连接：弹确认对话框，确认后通过 execSQL 执行 KILL <id>
function confirmKill(row: SessionRow) {
  ElMessageBox.confirm(
    `确定要终止连接 ID ${row.id}（用户 ${row.user}）吗？该操作会强制中断对应会话。`,
    '终止连接确认',
    { type: 'warning', confirmButtonText: '终止', cancelButtonText: '取消' }
  ).then(() => doKill(row)).catch((e: unknown) => {
    if (e !== 'cancel' && e !== 'close') handleError(e, '终止连接')
  })
}

async function doKill(row: SessionRow) {
  if (!props.connId) return
  try {
    await execSQL({ connId: props.connId, schema: props.schema || '', sql: `KILL ${row.id}`, maxLine: '1' })
    ElMessage.success(`已终止连接 ${row.id}`)
    await load()
  } catch (e) { handleError(e, '终止连接') }
}

// 首次激活时加载数据
watch(
  () => props.active,
  (active) => {
    if (active && !loaded.value) load()
  },
  { immediate: true },
)

// 切换离开本 Tab 时停止自动刷新
watch(
  () => props.active,
  (active) => {
    if (!active) {
      stopAutoRefresh()
    } else if (interval.value > 0 && !timer) {
      timer = setInterval(load, interval.value)
    }
  },
)

onUnmounted(() => {
  stopAutoRefresh()
})

defineExpose({ refresh: load })
</script>

<style scoped>
/* 会话 Tab 工具栏 */
.session-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.session-count {
  color: var(--db-text-tertiary);
  font-size: 12px;
}
</style>
