<template>
  <el-dialog v-model="visible" title="全局搜索" width="900px" :close-on-click-modal="false" @opened="onOpen">
    <div style="margin-bottom:15px;display:flex;gap:10px;align-items:center">
      <el-select v-model="connId" placeholder="选择连接" style="width:180px" @change="onConnChange">
        <el-option v-for="c in connections" :key="c.id" :label="c.name" :value="c.id" />
      </el-select>
      <el-select v-model="schema" placeholder="选择Schema" style="width:150px">
        <el-option v-for="s in schemas" :key="s" :label="s" :value="s" />
      </el-select>
    </div>

    <div style="margin-bottom:15px;display:flex;gap:10px">
      <el-input v-model="keyword" placeholder="输入搜索关键词..." size="large" clearable @keyup.enter="doSearch" style="flex:1">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-select v-model="searchType" style="width:130px">
        <el-option label="全部" value="all" />
        <el-option label="表" value="table" />
        <el-option label="列" value="column" />
        <el-option label="索引" value="index" />
        <el-option label="视图" value="view" />
      </el-select>
      <el-button type="primary" @click="doSearch" :loading="searching">搜索</el-button>
    </div>

    <div style="margin-bottom:10px;color:#909399;font-size:13px" v-if="lastQuery">
      搜索 "{{lastQuery}}" 找到 {{totalResults}} 个结果
    </div>

    <el-tabs v-model="resultTab" v-if="results.length || dataResults.length">
      <el-tab-pane label="对象" name="objects">
        <el-scrollbar max-height="400">
          <div v-for="r in results" :key="r.type+r.name" style="padding:8px 12px;border-bottom:1px solid #ebeef5;cursor:pointer;display:flex;align-items:center" @click="selectObject(r)">
            <el-tag :type="getTypeColor(r.type)" size="small" style="margin-right:8px;width:50px;text-align:center">{{r.type}}</el-tag>
            <span style="flex:1;font-weight:500">{{r.name}}</span>
            <span style="color:#909399;font-size:12px;max-width:300px;text-align:right">{{r.comment}}</span>
          </div>
        </el-scrollbar>
      </el-tab-pane>
      <el-tab-pane label="数据" name="data">
        <el-scrollbar max-height="400">
          <div v-for="r in dataResults" :key="r.table+'.'+r.column" style="padding:8px 12px;border-bottom:1px solid #ebeef5;cursor:pointer" @click="selectDataResult(r)">
            <el-tag type="primary" size="small" style="margin-right:8px">data</el-tag>
            <span style="font-weight:500">{{r.table}}</span>
            <span style="color:#909399;margin:0 4px">.</span>
            <span style="color:#409EFF">{{r.column}}</span>
            <el-tag size="small" type="info" style="margin-left:8px">{{r.rowCount}} 行匹配</el-tag>
            <span style="color:#909399;font-size:12px;margin-left:8px">匹配: {{r.matchText}}</span>
          </div>
        </el-scrollbar>
      </el-tab-pane>
    </el-tabs>

    <el-empty v-if="!searching && searched && !results.length && !dataResults.length" description="未找到结果" />
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import http from '@/js/utils/httpProxy.js'

const props = defineProps({
  modelValue: Boolean,
  connId: String,
  schema: String
})
const emit = defineEmits(['update:modelValue', 'select'])
const visible = computed({ get: () => props.modelValue, set: v => emit('update:modelValue', v) })

const connId = ref('')
const schema = ref('')
const connections = ref([])
const schemas = ref([])
const keyword = ref('')
const searchType = ref('all')
const searching = ref(false)
const searched = ref(false)
const lastQuery = ref('')
const results = ref([])
const dataResults = ref([])
const resultTab = ref('objects')
const totalResults = ref(0)

async function onOpen() {
  keyword.value = ''
  results.value = []
  dataResults.value = []
  searched.value = false
  try {
    const res = await http.get('/listConn2')
    connections.value = res.data || []
  } catch (e) {}
  if (props.connId) {
    connId.value = props.connId
    onConnChange()
  }
  if (props.schema) schema.value = props.schema
}

async function onConnChange() {
  if (!connId.value) return
  try {
    const res = await http.get('/sync/targets', { params: { connId: connId.value } })
    schemas.value = res.data.schemas || []
  } catch (e) {}
}

function getTypeColor(type) {
  const map = { table: 'primary', column: 'success', index: 'warning', view: 'info' }
  return map[type] || ''
}

async function doSearch() {
  if (!keyword.value.trim()) { ElMessage.warning('请输入搜索关键词'); return }
  if (!connId.value) { ElMessage.warning('请选择连接'); return }
  searching.value = true
  searched.value = false
  lastQuery.value = keyword.value
  try {
    if (searchType.value === 'all') {
      const res = await http.get('/search/all', { params: { connId: connId.value, schema: schema.value, keyword: keyword.value } })
      results.value = res.data.objectResults || []
      dataResults.value = res.data.dataResults || []
      totalResults.value = res.data.totalResults || 0
    } else {
      const res = await http.get('/search/objects', { params: { connId: connId.value, schema: schema.value, keyword: keyword.value, searchType: searchType.value } })
      results.value = res.data.results || []
      dataResults.value = []
      totalResults.value = res.data.totalResults || 0
    }
    searched.value = true
    if (dataResults.value.length) resultTab.value = 'data'
    else resultTab.value = 'objects'
  } catch (e) {
    ElMessage.error('搜索失败')
  } finally {
    searching.value = false
  }
}

function selectObject(obj) {
  emit('select', { ...obj, connId: connId.value, schema: schema.value })
  visible.value = false
}

function selectDataResult(r) {
  emit('select', { type: 'table', name: r.table, connId: connId.value, schema: schema.value })
  visible.value = false
}
</script>
