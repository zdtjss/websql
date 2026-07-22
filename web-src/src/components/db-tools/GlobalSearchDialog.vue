<template>
  <div class="global-search-content" role="search" aria-label="全局搜索数据库对象">
    <div class="search-filters">
      <el-select v-model="filterConnId" placeholder="不限连接" clearable filterable :teleported="false" style="width:220px" aria-label="过滤连接" @change="onConnChange">
        <el-option v-for="c in connections" :key="c.id" :label="c.name || c.id" :value="c.id">
          <span>{{ c.name || c.id }}</span>
          <span class="option-extra">{{ c.dbType || '' }}</span>
        </el-option>
      </el-select>
      <el-select v-model="filterSchema" placeholder="不限Schema" clearable :teleported="false" style="width:180px" aria-label="过滤 Schema" @change="onSchemaChange">
        <el-option v-for="s in schemas" :key="s.label" :label="s.label" :value="s.label">
          <span>{{ s.label }}</span>
          <span class="option-extra">{{ s.data?.dbType || '' }}</span>
        </el-option>
      </el-select>
      <el-button text size="small" @click="doSearch" :loading="searching" title="搜索" aria-label="搜索">
        <el-icon :size="16"><Search /></el-icon>
      </el-button>
    </div>

    <div class="search-bar">
      <el-input ref="keywordInputRef" v-model="keyword" placeholder="输入搜索关键词..." size="default" clearable
        aria-label="搜索关键词" aria-keyshortcuts="Ctrl+F"
        @keyup.enter="doSearch" @clear="onKeywordClear" style="flex:1">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-select v-model="searchType" :teleported="false" style="width:100px" aria-label="搜索类型" @change="onSearchTypeChange">
        <el-option label="表" value="table" />
        <el-option label="视图" value="view" />
        <el-option label="列" value="column" />
        <el-option label="索引" value="index" />
      </el-select>
    </div>

    <!-- 搜索结果统计 -->
    <div class="search-summary" v-if="lastQuery" aria-live="polite">
      搜索 "{{lastQuery}}" 找到 {{totalResults}} 个结果
      <span v-if="searching" class="searching-hint">（搜索中...）</span>
    </div>

    <!-- 搜索结果列表：使用虚拟滚动优化大数据量渲染 -->
    <div v-if="results.length" class="search-result-container" :style="{ height: resultContainerHeight + 'px' }" role="listbox" aria-label="搜索结果列表" :aria-busy="searching">
      <el-auto-resizer>
        <template #default="{ height, width }">
          <FixedSizeList :data="results" :total="results.length" :item-size="RESULT_ITEM_SIZE" :height="height" :width="width" :cache="4">
            <template #default="{ data, index, style }">
              <div :style="style"
                :key="data[index].type+'_'+data[index].name+'_'+data[index].schema"
                class="search-result-item"
                role="option"
                tabindex="0"
                :aria-label="`${data[index].typeLabel || data[index].type}：${data[index].name}${data[index].schema ? '，属于 ' + data[index].schema : ''}${data[index].comment ? '，注释 ' + data[index].comment : ''}`"
                @click="selectObject(data[index])"
                @keyup.enter="selectObject(data[index])">
                <el-tag :type="getTypeColor(data[index].type)" size="small" class="result-tag">{{data[index].typeLabel || data[index].type}}</el-tag>
                <span class="result-name" :title="data[index].name">{{data[index].name}}</span>
                <span v-if="data[index].schema" class="result-schema" :title="connTitle(data[index])">{{data[index].schema}}</span>
                <span v-if="data[index].comment" class="result-comment" :title="data[index].comment">{{data[index].comment}}</span>
              </div>
            </template>
          </FixedSizeList>
        </template>
      </el-auto-resizer>
    </div>

    <el-empty v-if="!searching && searched && !results.length" description="未找到结果" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { ElMessage, FixedSizeList } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { showTree } from '@/api/conn'
import { searchObjectsBatchSSE } from '@/api/sql'
import type { BatchObjectResult, SearchSSEHandle } from '@/api/sql'
import { useDbSchemaStore } from '@/stores/dbSchema'
import { handleError } from '@/utils/errorHandler'
import { loadConnections } from '@/utils/dbMetadata'
const dbSchemaProxy = useDbSchemaStore()

const visible = defineModel({ default: false })
const { connId, schema } = defineProps({
  connId: String,
  schema: String
})
const emit = defineEmits(['select'])

// 虚拟滚动单项高度（px）：与 .search-result-item 的 padding/line-height 匹配
const RESULT_ITEM_SIZE = 44
// 结果列表最大高度
const RESULT_MAX_HEIGHT = 400

const keywordInputRef = useTemplateRef('keywordInputRef')
const filterConnId = ref('')
const filterSchema = ref('')
const connections = ref<any[]>([])
const schemas = ref<any[]>([])
const keyword = ref('')
const searchType = ref('table')
const searching = ref(false)
const searched = ref(false)
const lastQuery = ref('')
const results = ref<any[]>([])
const totalResults = ref(0)

// 当前活跃的 SSE 连接（用于中止上次搜索）
let activeSSEHandle: SearchSSEHandle | null = null

// 虚拟滚动容器高度：结果较少时按实际条数计算，避免留白；超过最大高度则按最大高度
const resultContainerHeight = computed(() =>
  Math.min(RESULT_MAX_HEIGHT, results.value.length * RESULT_ITEM_SIZE)
)

let debounceTimer: number | null = null

const isTableOrView = computed(() => searchType.value === 'table' || searchType.value === 'view')

// 按字段或索引搜索时需要指定连接和 schema
const requiresConnAndSchema = computed(() => searchType.value === 'column' || searchType.value === 'index')

const typeLabelMap: Record<string, string> = {
  table: '表',
  view: '视图',
  column: '列',
  index: '索引'
}

watch([visible, () => connId, () => schema], ([newVisible, newConnId, newSchema], [oldVisible]) => {
  if (newVisible) {
    init(!oldVisible)
  } else {
    cleanup()
  }
})

watch(keyword, (val) => {
  if (!isTableOrView.value) return
  if (debounceTimer !== null) clearTimeout(debounceTimer)
  if (!val.trim()) {
    results.value = []
    totalResults.value = 0
    return
  }
  debounceTimer = window.setTimeout(() => {
    results.value = searchTablesLocally(val.trim(), searchType.value)
    totalResults.value = results.value.length
  }, 200)
})

async function init(freshOpen = true) {
  if (freshOpen) {
    keyword.value = ''
    results.value = []
    searched.value = false
    lastQuery.value = ''
    totalResults.value = 0
    searchType.value = 'table'
  }
  filterConnId.value = ''
  filterSchema.value = ''
  schemas.value = []

  connections.value = await loadConnections({ pageSize: 1000 })

  if (connId) {
    filterConnId.value = connId
    await onConnChange()
    if (schema) {
      filterSchema.value = schema
      await loadSchemaTables(schema)
    }
  }
  await nextTick()
  keywordInputRef.value?.focus()
}

function cleanup() {
  if (debounceTimer !== null) clearTimeout(debounceTimer)
  activeSSEHandle?.abort()
  activeSSEHandle = null
  filterConnId.value = ''
  filterSchema.value = ''
  schemas.value = []
  results.value = []
  searched.value = false
  keyword.value = ''
  lastQuery.value = ''
  totalResults.value = 0
}

async function onConnChange() {
  schemas.value = []
  if (filterConnId.value) {
    filterSchema.value = schema && connId === filterConnId.value ? schema : ''
  } else {
    filterSchema.value = ''
  }
  if (!filterConnId.value) return
  try {
    const res = await showTree({ connId: filterConnId.value, key: '', type: 'conn', level: '2' })
    schemas.value = res.data && res.data.data ? res.data.data : (Array.isArray(res.data) ? res.data : [])
  } catch (e) { handleError(e, '加载Schema列表') }
}

async function onSchemaChange() {
  if (filterSchema.value && filterConnId.value) {
    await loadSchemaTables(filterSchema.value)
  }
}

async function loadSchemaTables(schemaName: string) {
  if (!filterConnId.value || !schemaName) return
  try {
    const res = await showTree({ connId: filterConnId.value, key: schemaName, type: 'schema', level: '3' })
    const schemaObj = schemas.value.find(s => s.label === schemaName)
    const dbType = schemaObj?.data?.dbType || ''
    if (res.data && res.data.data) {
      dbSchemaProxy.addTable(schemaName, dbType, res.data.data, filterConnId.value)
    }
  } catch (e) { handleError(e, '加载表结构') }
}

function onSearchTypeChange() {
  results.value = []
  totalResults.value = 0
  searched.value = false
  lastQuery.value = ''
  if (isTableOrView.value && keyword.value.trim()) {
    debounceTimer = window.setTimeout(() => {
      results.value = searchTablesLocally(keyword.value.trim(), searchType.value)
      totalResults.value = results.value.length
    }, 100)
  }
}

function onKeywordClear() {
  results.value = []
  totalResults.value = 0
  lastQuery.value = ''
}

function getTypeColor(type: string): 'primary' | 'success' | 'warning' | 'info' | 'danger' {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'info' | 'danger'> = { table: 'primary', view: 'success', column: 'warning', index: 'info' }
  return map[type] || 'info'
}

async function doSearch() {
  if (!keyword.value.trim()) { ElMessage.warning('请输入搜索关键词'); return }

  // 按字段或索引搜索时，必须指定连接和 schema
  if (requiresConnAndSchema.value) {
    if (!filterConnId.value) {
      ElMessage.warning('按字段或索引搜索时请先选择连接')
      return
    }
    if (!filterSchema.value) {
      ElMessage.warning('按字段或索引搜索时请先选择 Schema')
      return
    }
  }

  if (debounceTimer !== null) clearTimeout(debounceTimer)
  searching.value = true
  lastQuery.value = keyword.value
  searched.value = false

  try {
    if (isTableOrView.value) {
      // 先用本地缓存填充结果，确保即时反馈
      results.value = searchTablesLocally(keyword.value.trim(), searchType.value)
      totalResults.value = results.value.length
      // 再远程搜索补充
      await searchRemoteSSE(keyword.value.trim(), searchType.value, true)
    } else {
      results.value = []
      totalResults.value = 0
      await searchRemoteSSE(keyword.value.trim(), searchType.value, false)
    }
    searched.value = true
  } catch (e) {
    handleError(e, '搜索对象')
  } finally {
    searching.value = false
  }
}

function searchTablesLocally(keyword: string, type: string) {
  const keywordLower = keyword.toLowerCase()
  const matched = []
  let schemasToSearch = filterSchema.value
    ? [filterSchema.value]
    : Object.keys(dbSchemaProxy.schemaProxy)

  // 设置了连接过滤时，只搜索属于该连接的 schema
  if (filterConnId.value && !filterSchema.value) {
    schemasToSearch = schemasToSearch.filter(s => dbSchemaProxy.getConnId(s) === filterConnId.value)
  }

  for (const schemaName of schemasToSearch) {
    const allTables = dbSchemaProxy.getAll(schemaName)
    if (!allTables) continue
    const schemaConnId = dbSchemaProxy.getConnId(schemaName)
    const tableNames = Object.keys(allTables)
    for (const tableName of tableNames) {
      const tableInfo = allTables[tableName]
      const self = tableInfo.self || {}
      const nodeType = self.type || 'table'
      if (nodeType !== type) continue
      if (tableName.toLowerCase().indexOf(keywordLower) !== -1
        || (self.detail && self.detail.toLowerCase().indexOf(keywordLower) !== -1)) {
        matched.push({
          type: nodeType,
          typeLabel: typeLabelMap[nodeType] || '表',
          name: tableName,
          schema: schemaName,
          comment: self.detail || '',
          connId: schemaConnId || filterConnId.value || ''
        })
      }
    }
  }
  return matched
}

/**
 * 通过 SSE 流式远程搜索，结果逐条追加到 results 中。
 * @param mergeLocal 是否与已有本地结果去重合并（true = 表/视图场景）
 */
function searchRemoteSSE(keyword: string, type: string, mergeLocal: boolean): Promise<void> {
  // 中止上次未完成的搜索
  activeSSEHandle?.abort()

  return new Promise<void>((resolve) => {
    const existingSet = mergeLocal
      ? new Set(results.value.map(r => r.type + '_' + r.name + '_' + r.schema + '_' + r.connId))
      : null

    activeSSEHandle = searchObjectsBatchSSE({
      params: {
        connIds: filterConnId.value || '',
        schema: filterSchema.value || '',
        keyword,
        searchType: type
      },
      onResult(r: BatchObjectResult) {
        const item = {
          ...r,
          typeLabel: typeLabelMap[r.type] || r.type,
          schema: r.schema || filterSchema.value || ''
        }
        if (existingSet) {
          const key = item.type + '_' + item.name + '_' + item.schema + '_' + item.connId
          if (existingSet.has(key)) return
          existingSet.add(key)
        }
        results.value = [...results.value, item]
        totalResults.value = results.value.length
      },
      onDone() {
        activeSSEHandle = null
        resolve()
      },
      onError(err) {
        // 如果是参数校验错误（后端返回 event:error），提示用户
        if (err?.message) {
          ElMessage.warning(err.message)
        }
        activeSSEHandle = null
        resolve()
      }
    })
  })
}

function getConnName(connId: string) {
  if (!connId) return ''
  const conn = connections.value.find(c => c.id === connId)
  return conn ? (conn.name || conn.id) : connId
}

function connTitle(item: any) {
  const connName = getConnName(item.connId)
  return connName ? `${item.schema}（${connName}）` : item.schema
}

function selectObject(obj: any) {
  emit('select', {
    type: obj.type,
    name: obj.name,
    schema: obj.schema,
    comment: obj.comment,
    connId: obj.connId || filterConnId.value || connId || ''
  })
}
</script>

<style scoped>
.global-search-content {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.search-filters {
  display: flex;
  gap: 10px;
  align-items: center;
}

.search-bar {
  display: flex;
  gap: 10px;
}

.search-summary {
  color: #909399;
  font-size: 13px;
}

.searching-hint {
  color: #e6a23c;
}

/* 虚拟滚动结果容器 */
.search-result-container {
  border: 1px solid var(--db-border-light, #ebeef5);
  border-radius: 6px;
  overflow: hidden;
  background: var(--db-card-bg, #fff);
}

.search-result-item {
  padding: 8px 12px;
  border-bottom: 1px solid var(--db-border-light, #ebeef5);
  cursor: pointer;
  display: flex;
  align-items: center;
  box-sizing: border-box;
  background: var(--db-card-bg, #fff);
}
.search-result-item:hover {
  background: var(--db-bg-hover, #f5f7fa);
}
.search-result-item:last-child {
  border-bottom: none;
}

.result-tag {
  margin-right: 8px;
  min-width: 50px;
  text-align: center;
  flex-shrink: 0;
}

.result-name {
  flex: 1;
  font-weight: 500;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.result-schema {
  color: #909399;
  font-size: 12px;
  margin-left: 8px;
  flex-shrink: 0;
  text-align: right;
}

.result-comment {
  color: #909399;
  font-size: 12px;
  max-width: 200px;
  margin-left: 8px;
  flex-shrink: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.option-extra {
  color: #909399;
  font-size: 12px;
  margin-left: 6px;
}
</style>
