<template>
  <!-- InnoDB 引擎状态 Tab（仅 MySQL/MariaDB 支持） -->
  <div v-loading="loading" :aria-busy="loading" style="min-height: 200px;" role="region" aria-label="InnoDB 引擎状态">
    <div class="vars-toolbar">
      <el-button size="small" @click="load" :loading="loading" aria-label="刷新 InnoDB 状态">刷新</el-button>
    </div>
    <el-empty v-if="!loading && !supported" description="当前数据库不支持 InnoDB 状态查看" :image-size="60" />
    <pre v-else-if="status" class="innodb-status-text">{{ status }}</pre>
    <el-empty v-else description="暂无 InnoDB 状态数据（可能缺少 PROCESS 权限）" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'
import { getInnodbStatus } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'

const props = defineProps<{
  connId?: string
  active: boolean
}>()

const loading = ref(false)
const supported = ref(false)
const status = ref('')
const loaded = ref(false)

async function load() {
  if (!props.connId) return
  loading.value = true
  try {
    const res = await getInnodbStatus(props.connId)
    const data = res.data?.data || {}
    supported.value = !!data.supported
    status.value = data.status || ''
  } catch (e) {
    handleError(e, 'InnoDB 状态')
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

onUnmounted(() => {
  // destroy-on-close 会销毁整个对话框内容，此处仅作防御性清理
})
</script>

<style scoped>
.vars-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.innodb-status-text {
  background: var(--db-bg-secondary);
  border: 1px solid var(--db-card-border);
  border-radius: 6px;
  padding: 12px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 440px;
  overflow-y: auto;
  color: var(--db-text-primary);
  margin: 0;
}
</style>
