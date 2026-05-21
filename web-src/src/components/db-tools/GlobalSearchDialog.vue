<template>
  <div class="global-search-content">
    <div class="search-filters">
      <el-select v-model="filterConnId" placeholder="不限连接" clearable filterable :teleported="false" style="width:220px" @change="onConnChange">
        <el-option v-for="c in connections" :key="c.id" :label="c.name || c.id" :value="c.id">
          <span>{{ c.name || c.id }}</span>
          <span class="option-extra">{{ c.dbType || '' }}</span>
        </el-option>
      </el-select>
      <el-select v-model="filterSchema" placeholder="不限Schema" clearable :teleported="false" style="width:180px" @change="onSchemaChange">
        <el-option v-for="s in schemas" :key="s.label" :label="s.label" :value="s.label">
          <span>{{ s.label }}</span>
          <span class="option-extra">{{ s.data?.dbType || '' }}</span>
        </el-option>
      </el-select>
      <el-button text size="small" @click="doSearch" :loading="searching" title="搜索">
        <el-icon :size="16"><Search /></el-icon>
      </el-button>
    </div>

    <div class="search-bar">
      <el-input ref="keywordInputRef" v-model="keyword" placeholder="输入搜索关键词..." size="default" clearable @keyup.enter="doSearch" @clear="onKeywordClear" style="flex:1">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-select v-model="searchType" :teleported="false" style="width:100px" @change="onSearchTypeChange">
        <el-option label="表" value="table" />
        <el-option label="视图" value="view" />
        <el-option label="列" value="column" />
        <el-option label="索引" value="index" />
      </el-select>
      <el-button v-if="searchType === 'column' || searchType === 'index'" type="primary" @click="doSearch" :loading="searching">搜索</el-button>
    </div>

    <div class="search-summary" v-if="lastQuery">
      搜索 "{{lastQuery}}" 找到 {{totalResults}} 个结果
    </div>

    <el-scrollbar max-height="280" v-if="results.length">
      <div v-for="r in results" :key="r.type+'_'+r.name+'_'+r.schema"
        class="search-result-item"
        @click="selectObject(r)">
        <el-tag :type="getTypeColor(r.type)" size="small" class="result-tag">{{r.typeLabel || r.type}}</el-tag>
        <span class="result-name" :title="r.name">{{r.name}}</span>
        <span v-if="r.schema" class="result-schema">{{r.schema}}</span>
        <span v-if="r.comment" class="result-comment" :title="r.comment">{{r.comment}}</span>
      </div>
    </el-scrollbar>

    <el-empty v-if="!searching && searched && !results.length" description="未找到结果" :image-size="60" />
  </div>
</template>

<script setup>
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import http from '@/utils/httpProxy.js'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()

const { visible, connId, schema } = defineProps({
  visible: Boolean,
  connId: String,
  schema: String
})
const emit = defineEmits(['select'])

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

let debounceTimer = null

const isTableOrView = computed(() => searchType.value === 'table' || searchType.value === 'view')

const typeLabelMap = {
  table: '表',
  view: '视图',
  column: '列',
  index: '索引'
}

watch(() => visible, (val) => {
  if (val) {
    init()
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

async function init() {
  keyword.value = ''
  results.value = []
  searched.value = false
  lastQuery.value = ''
  totalResults.value = 0
  searchType.value = 'table'
  filterConnId.value = ''
  filterSchema.value = ''
  schemas.value = []

  try {
    const res = await http.get('/listConn2', { params: { pageSize: 1000 } })
    const result = (res.data && res.data.data ? res.data.data : res.data) || {}
    connections.value = result.data || []
  } catch (e) {}

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
    const res = await http.get('/showTree', { params: { connId: filterConnId.value, key: '', type: 'conn', level: '2' } })
    schemas.value = res.data && res.data.data ? res.data.data : (Array.isArray(res.data) ? res.data : [])
  } catch (e) {}
}

async function onSchemaChange() {
  if (filterSchema.value && filterConnId.value) {
    await loadSchemaTables(filterSchema.value)
  }
}

async function loadSchemaTables(schemaName) {
  if (!filterConnId.value || !schemaName) return
  try {
    const res = await http.get('/showTree', { params: { connId: filterConnId.value, key: schemaName, type: 'schema', level: '3' } })
    const schemaObj = schemas.value.find(s => s.label === schemaName)
    const dbType = schemaObj?.data?.dbType || ''
    if (res.data && res.data.data) {
      dbSchemaProxy.addTable(schemaName, dbType, res.data.data)
    }
  } catch (e) {}
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
  return map[type] || ''
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
      if (!filterConnId.value && connections.value.length === 0) {
        ElMessage.warning('搜索列或索引需要先选择连接')
        return
      }
      await searchRemotely(keyword.value.trim(), searchType.value)
    }
    searched.value = true
  } catch (e) {
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
          connId: filterConnId.value || connId || ''
        })
      }
    }
  }
  return matched
}

async function searchTablesRemotely(keyword, type) {
  const connIds = filterConnId.value
    ? [filterConnId.value]
    : connections.value.map(c => c.id).filter(Boolean)

  if (connIds.length === 0) return

  const allResults = []
  const tasks = connIds.map(async (cid) => {
    try {
      const res = await http.get('/search/objects', {
        params: { connId: cid, schema: filterConnId.value ? filterSchema.value : '', keyword, searchType: type }
      })
      const payload = res.data
      const remoteResults = (payload && payload.results ? payload.results : (payload.data && payload.data.results ? payload.data.results : [])) || []
      return remoteResults.map(r => ({
        ...r,
        typeLabel: typeLabelMap[r.type] || r.type,
        connId: cid,
        schema: r.schema || (filterConnId.value ? filterSchema.value : '')
      }))
    } catch (e) {
      return []
    }
  })

  const settled = await Promise.allSettled(tasks)
  for (const item of settled) {
    if (item.status === 'fulfilled' && item.value) {
      allResults.push(...item.value)
    }
  }

  if (allResults.length > 0) {
    const existing = new Set(results.value.map(r => r.type + '_' + r.name + '_' + r.schema + '_' + r.connId))
    const newResults = allResults.filter(r => !existing.has(r.type + '_' + r.name + '_' + r.schema + '_' + r.connId))
    results.value = [...results.value, ...newResults]
    totalResults.value = results.value.length
  }
  if (!results.value.length) {
    searched.value = true
  }
}

async function searchRemotely(keyword, type) {
  const connIds = filterConnId.value
    ? [filterConnId.value]
    : connections.value.map(c => c.id).filter(Boolean)

  if (connIds.length === 0) return

  const allResults = []
  const tasks = connIds.map(async (cid) => {
    try {
      const res = await http.get('/search/objects', {
        params: { connId: cid, schema: filterConnId.value ? filterSchema.value : '', keyword, searchType: type }
      })
      const payload = res.data
      const remoteResults = (payload && payload.results ? payload.results : (payload.data && payload.data.results ? payload.data.results : [])) || []
      return remoteResults.map(r => ({
        ...r,
        typeLabel: typeLabelMap[r.type] || r.type,
        connId: cid,
        schema: r.schema || (filterConnId.value ? filterSchema.value : '')
      }))
    } catch (e) {
      return []
    }
  })

  const settled = await Promise.allSettled(tasks)
  for (const item of settled) {
    if (item.status === 'fulfilled' && item.value) {
      allResults.push(...item.value)
    }
  }

  results.value = allResults
  totalResults.value = results.value.length
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

.search-result-item {
  padding: 8px 12px;
  border-bottom: 1px solid #ebeef5;
  cursor: pointer;
  display: flex;
  align-items: center;
}
.search-result-item:hover {
  background: #f5f7fa;
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