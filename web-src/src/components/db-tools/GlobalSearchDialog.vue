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
      <!-- 图标按钮：仅图标无文字，需补充 aria-label（复用 title 的值） -->
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
      <el-button v-if="searchType === 'column' || searchType === 'index'" type="primary" @click="doSearch" :loading="searching">搜索</el-button>
    </div>

    <!-- 搜索结果统计动态变化，aria-live 通知屏幕阅读器 -->
    <div class="search-summary" v-if="lastQuery" aria-live="polite">
      搜索 "{{lastQuery}}" 找到 {{totalResults}} 个结果
    </div>

    <!-- 搜索结果列表：使用虚拟滚动（FixedSizeList）优化大数据量渲染性能 -->
    <!-- el-auto-resizer 自动撑满父容器并传入可用宽高，FixedSizeList 仅渲染可见区域内的项 -->
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
                <span v-if="data[index].schema" class="result-schema">{{data[index].schema}}</span>
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

<script setup>
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { ElMessage, FixedSizeList } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { showTree } from '@/api/conn'
import { searchObjectsBatch } from '@/api/sql'
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
// 结果列表最大高度（与原 el-scrollbar max-height 保持一致）
const RESULT_MAX_HEIGHT = 280

const keywordInputRef = useTemplateRef('keywordInputRef')
const filterConnId = ref('')
const filterSchema = ref('')
const connections = ref([])
const schemas = ref([])
const keyword = ref('')
const searchType = ref('table')
const searching = ref(false)
const searched = ref(false)
const lastQuery = ref('')
const results = ref([])
const totalResults = ref(0)

// 虚拟滚动容器高度：结果较少时按实际条数计算，避免留白；超过最大高度则按最大高度
const resultContainerHeight = computed(() =>
  Math.min(RESULT_MAX_HEIGHT, results.value.length * RESULT_ITEM_SIZE)
)

let debounceTimer = null

const isTableOrView = computed(() => searchType.value === 'table' || searchType.value === 'view')

const typeLabelMap = {
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
  clearTimeout(debounceTimer)
  if (!val.trim()) {
    results.value = []
    totalResults.value = 0
    return
  }
  debounceTimer = setTimeout(() => {
    results.value = searchTablesLocally(val.trim(), searchType.value)
    totalResults.value = results.value.length
  }, 200)
})

async function init(freshOpen = true) {
  // 新打开时做完整重置；已可见时仅更新过滤器
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

  // loadConnections 内部已统一处理错误并返回空数组，无需外层 try/catch
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
  clearTimeout(debounceTimer)
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

async function loadSchemaTables(schemaName) {
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
    debounceTimer = setTimeout(() => {
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

function getTypeColor(type) {
  const map = { table: 'primary', view: 'success', column: 'warning', index: 'info' }
  return map[type] || 'info'
}

async function doSearch() {
  if (!keyword.value.trim()) { ElMessage.warning('请输入搜索关键词'); return }

  results.value = []
  totalResults.value = 0
  searching.value = true
  lastQuery.value = keyword.value
  searched.value = false

  try {
    if (isTableOrView.value) {
      await searchTablesRemotely(keyword.value.trim(), searchType.value)
    } else {
      await searchRemotely(keyword.value.trim(), searchType.value)
    }
    searched.value = true
  } catch (e) {
    handleError(e, '搜索对象')
  } finally {
    searching.value = false
  }
}

function searchTablesLocally(keyword, type) {
  const keywordLower = keyword.toLowerCase()
  const matched = []
  const schemasToSearch = filterSchema.value
    ? [filterSchema.value]
    : Object.keys(dbSchemaProxy.schemaProxy)

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
          connId: schemaConnId || filterConnId.value || connId || ''
        })
      }
    }
  }
  return matched
}

async function searchTablesRemotely(keyword, type) {
  try {
    const res = await searchObjectsBatch({
      connIds: filterConnId.value || '',
      schema: filterSchema.value || '',
      keyword,
      searchType: type
    })
    const payload = res.data
    const remoteResults = (payload?.results || payload?.data?.results || []).map(r => ({
      ...r,
      typeLabel: typeLabelMap[r.type] || r.type,
      schema: r.schema || filterSchema.value || ''
    }))

    if (remoteResults.length > 0) {
      const existing = new Set(results.value.map(r => r.type + '_' + r.name + '_' + r.schema + '_' + r.connId))
      const newResults = remoteResults.filter(r => !existing.has(r.type + '_' + r.name + '_' + r.schema + '_' + r.connId))
      results.value = [...results.value, ...newResults]
      totalResults.value = results.value.length
    }
    if (!results.value.length) {
      searched.value = true
    }
  } catch (e) {
    // 批量接口出错时静默处理
  }
}

async function searchRemotely(keyword, type) {
  try {
    const res = await searchObjectsBatch({
      connIds: filterConnId.value || '',
      schema: filterSchema.value || '',
      keyword,
      searchType: type
    })
    const payload = res.data
    const remoteResults = (payload?.results || payload?.data?.results || []).map(r => ({
      ...r,
      typeLabel: typeLabelMap[r.type] || r.type,
      schema: r.schema || filterSchema.value || ''
    }))
    results.value = remoteResults
    totalResults.value = results.value.length
  } catch (e) {
    // 批量接口出错时静默处理
  }
}

function selectObject(obj) {
  emit('select', {
    type: obj.type,
    name: obj.name,
    schema: obj.schema,
    comment: obj.comment,
    connId: obj.connId || filterConnId.value
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

/* 虚拟滚动结果容器：需提供明确高度供 el-auto-resizer 读取 */
.search-result-container {
  border: 1px solid var(--db-border-light, #ebeef5);
  border-radius: 6px;
  overflow: hidden;
  background: var(--db-card-bg, #fff);
}

.search-result-item {
  /* FixedSizeList 会通过 inline style 注入 position/top/height/width，
     此处仅补充外观与布局，display:flex 与 absolute 定位兼容 */
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