<template>
  <!-- 表统计 TOP N Tab -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="表统计 TOP N">
    <div class="vars-toolbar">
      <el-button size="small" @click="load" :loading="loading" aria-label="刷新表统计">刷新</el-button>
    </div>
    <el-table :data="list" max-height="440" size="small" stripe border aria-label="表统计列表">
      <el-table-column prop="tableName" label="表名" min-width="180" resizable show-overflow-tooltip />
      <el-table-column prop="engine" label="引擎" width="90" resizable />
      <el-table-column prop="tableRows" label="行数" width="110" resizable>
        <template #default="scope">{{ formatNum(scope.row.tableRows) }}</template>
      </el-table-column>
      <el-table-column prop="dataSize" label="数据大小" width="110" resizable>
        <template #default="scope">{{ formatBytes(scope.row.dataSize) }}</template>
      </el-table-column>
      <el-table-column prop="indexSize" label="索引大小" width="110" resizable>
        <template #default="scope">{{ formatBytes(scope.row.indexSize) }}</template>
      </el-table-column>
      <el-table-column prop="dataFree" label="碎片空间" width="110" resizable>
        <template #default="scope">{{ formatBytes(scope.row.dataFree) }}</template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && list.length === 0" description="暂无表统计数据" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { getTopTables } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'
import { formatBytes, formatNum } from '../composables/useMonitorFormat'

interface TopTableRow {
  tableName: string
  engine: string
  tableRows: number
  dataSize: number
  indexSize: number
  dataFree: number
  [key: string]: unknown
}

const props = defineProps<{
  connId?: string
  schema?: string
  active: boolean
}>()

const loading = ref(false)
const list = ref<TopTableRow[]>([])
const loaded = ref(false)

async function load() {
  if (!props.connId) return
  loading.value = true
  try {
    const res = await getTopTables(props.connId, props.schema || '', 20)
    // 后端返回 { tables: [...], count, supported }，取 tables 数组
    list.value = (res.data?.data?.tables || []) as TopTableRow[]
  } catch (e) {
    handleError(e, '表统计')
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
