<template>
  <!-- 锁与事务等待 Tab -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="锁与事务等待">
    <div class="vars-toolbar">
      <el-button size="small" @click="load" :loading="loading" aria-label="刷新锁等待列表">刷新</el-button>
    </div>
    <el-table :data="list" max-height="440" size="small" stripe border aria-label="锁与事务等待列表">
      <el-table-column prop="waitingId" label="等待事务/会话" width="140" resizable show-overflow-tooltip />
      <el-table-column prop="blockingId" label="阻塞会话" width="120" resizable show-overflow-tooltip />
      <el-table-column prop="lockType" label="锁类型/事件" min-width="160" resizable show-overflow-tooltip />
      <el-table-column prop="waitSeconds" label="等待(秒)" width="100" resizable />
      <el-table-column prop="tableName" label="表名" width="140" resizable show-overflow-tooltip />
      <el-table-column prop="query" label="SQL" min-width="180" resizable show-overflow-tooltip />
    </el-table>
    <el-empty v-if="!loading && list.length === 0" description="当前无锁等待" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { getLocks } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'

interface LockRow {
  waitingId: string | number
  blockingId: string | number
  lockType: string
  waitSeconds: number | string
  tableName: string
  query: string
  [key: string]: unknown
}

const props = defineProps<{
  connId?: string
  active: boolean
}>()

const loading = ref(false)
const list = ref<LockRow[]>([])
const loaded = ref(false)

async function load() {
  if (!props.connId) return
  loading.value = true
  try {
    const res = await getLocks(props.connId)
    // 后端返回 { locks: [...], count, supported }，取 locks 数组
    list.value = (res.data?.data?.locks || []) as LockRow[]
  } catch (e) {
    handleError(e, '锁与等待')
  } finally {
    loading.value = false
    loaded.value = true
  }
}

// 首次激活时加载数据
watch(
  () => props.active,
  (active) => {
    if (active && !loaded.value) load()
  },
  { immediate: true },
)

defineExpose({ refresh: load })
</script>

<style scoped>
.vars-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}
</style>
