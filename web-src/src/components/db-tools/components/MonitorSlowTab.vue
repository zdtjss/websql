<template>
  <!-- 慢查询分析 Tab -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="慢查询分析">
    <div class="vars-toolbar">
      <el-button size="small" @click="load" :loading="loading" aria-label="刷新慢查询列表">刷新</el-button>
    </div>
    <el-table :data="list" max-height="440" size="small" stripe border aria-label="慢查询列表">
      <el-table-column prop="digestText" label="SQL 摘要" min-width="280" resizable show-overflow-tooltip />
      <el-table-column prop="avgMs" label="平均耗时(ms)" width="130" resizable>
        <template #default="scope">{{ scope.row.avgMs != null ? scope.row.avgMs.toFixed(2) : '-' }}</template>
      </el-table-column>
      <el-table-column prop="execCount" label="执行次数" width="110" resizable />
      <el-table-column prop="rowsExamined" label="扫描行数" width="110" resizable />
      <el-table-column prop="lastSeen" label="最后出现" width="160" resizable show-overflow-tooltip />
    </el-table>
    <el-empty v-if="!loading && list.length === 0" :description="emptyDesc" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { getSlowQueries } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'

interface SlowRow {
  digestText: string
  avgMs: number | null
  execCount: number
  rowsExamined: number
  lastSeen: string
  [key: string]: unknown
}

const props = defineProps<{
  connId?: string
  active: boolean
}>()

const loading = ref(false)
const list = ref<SlowRow[]>([])
const loaded = ref(false)
const reason = ref('')

const emptyDesc = computed(() => reason.value || '暂无慢查询数据')

async function load() {
  if (!props.connId) return
  loading.value = true
  try {
    const res = await getSlowQueries(props.connId, 20)
    // 后端返回 { queries: [...], count, supported, reason? }，取 queries 数组
    const data = res.data?.data
    list.value = (data?.queries || []) as SlowRow[]
    reason.value = data?.reason || ''
  } catch (e) {
    handleError(e, '慢查询分析')
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
