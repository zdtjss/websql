<template>
  <el-dialog v-model="visible" width="1100px" :close-on-click-modal="false" :fullscreen="isFullscreen" :draggable="!isFullscreen" :show-close="false" @opened="loadTables">
    <template #header="{ close }">
      <div style="display:flex;justify-content:space-between;align-items:center">
        <span style="font-size:18px;font-weight:600">数据字典</span>
        <div style="display:flex;gap:4px">
          <el-button text @click="toggleFullscreen" :title="isFullscreen ? '还原' : '全屏'">
            <el-icon><FullScreen /></el-icon>
          </el-button>
          <el-button text @click="close" title="关闭">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
      </div>
    </template>
    <el-row style="margin-bottom:15px">
      <el-col :span="12">
        <el-button type="primary" @click="generateDict" :loading="generating" :disabled="!selectedCount">生成字典</el-button>
        <el-button @click="toggleSelectAll">{{selectAllTables ? '取消全选' : '全选'}}</el-button>
        <span style="margin-left:10px;color:#909399;font-size:13px">已选 {{selectedCount}} / {{tables.length}} 张表</span>
      </el-col>
      <el-col :span="12" style="text-align:right">
        <el-button type="success" @click="exportHTML" :disabled="!dictData">导出HTML</el-button>
        <el-button type="warning" @click="exportPDF" :disabled="!dictData">导出PDF</el-button>
      </el-col>
    </el-row>

    <el-row :gutter="15">
      <el-col :span="6">
        <el-card shadow="never" :style="{maxHeight: contentMaxHeight, overflow: 'auto'}">
          <template #header><span style="font-weight:bold">表列表</span></template>
          <el-input v-model="tableFilter" placeholder="搜索表..." size="small" clearable style="margin-bottom:10px" />
          <el-checkbox v-for="t in filteredTables" :key="t.name" v-model="t.checked" style="display:block;margin:4px 0">
            {{ t.name }}
            <span v-if="t.comment" style="font-size:11px;color:#606266"> - {{ t.comment }}</span>
            <span style="font-size:11px;color:#909399">({{ t.rows || 0 }}行)</span>
          </el-checkbox>
        </el-card>
      </el-col>
      <el-col :span="18">
        <div v-if="!dictData" style="text-align:center;padding:60px;color:#909399">
          <el-icon :size="50"><Document /></el-icon>
          <p style="margin-top:10px">选择表后点击"生成字典"</p>
        </div>
        <div v-else :style="{maxHeight: dictMaxHeight, overflow: 'auto', paddingRight: '10px'}">
          <div v-for="table in dictData.tables" :key="table.name" style="margin-bottom:25px">
            <h3 style="color:#409EFF;border-bottom:2px solid #409EFF;padding-bottom:5px">
              {{ table.name }}
              <span style="font-size:13px;color:#909399;font-weight:normal;margin-left:8px">{{ table.comment }}</span>
            </h3>
            <p style="color:#909399;font-size:12px">引擎: {{ table.engine }} | 行数: {{ table.rows }}</p>

            <el-table :data="table.columns" stripe size="small" style="margin-bottom:8px" border>
              <el-table-column prop="position" label="#" width="50" />
              <el-table-column prop="name" label="列名" width="140">
                <template #default="{row}">
                  <span>{{ row.name }}</span>
                  <el-tag v-if="row.primaryKey" type="warning" size="small" style="margin-left:4px">PK</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="type" label="类型" width="140">
                <template #default="{row}">
                  <code>{{ row.type }}</code>
                </template>
              </el-table-column>
              <el-table-column label="可空" width="60">
                <template #default="{row}">
                  <span :style="{color: row.nullable ? '#67c23a' : '#f56c6c'}">{{ row.nullable ? 'YES' : 'NO' }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="defaultValue" label="默认值" width="100" />
              <el-table-column prop="comment" label="注释" min-width="150" />
            </el-table>

            <div v-if="table.indexes && table.indexes.length" style="margin-top:5px">
              <strong style="font-size:12px;color:#909399">索引:</strong>
              <el-tag v-for="idx in table.indexes" :key="idx.name" size="small" :type="idx.unique ? 'success' : 'info'" style="margin:2px 4px">
                {{ idx.name }}({{ idx.columns.join(',') }})
              </el-tag>
            </div>
          </div>
        </div>
      </el-col>
    </el-row>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Document, FullScreen, Close } from '@element-plus/icons-vue'
import http from '@/utils/httpProxy.js'

const visible = defineModel({ default: false })
const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const tables = ref([])
const dictData = ref(null)
const generating = ref(false)
const tableFilter = ref('')
const selectAllTables = ref(false)
const isFullscreen = ref(false)

function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value
}

const filteredTables = computed(() => {
  if (!tableFilter.value) return tables.value
  const kw = tableFilter.value.toLowerCase()
  return tables.value.filter(t => 
    t.name.toLowerCase().includes(kw) || 
    (t.comment && t.comment.toLowerCase().includes(kw))
  )
})

const selectedCount = computed(() => tables.value.filter(t => t.checked).length)

const contentMaxHeight = computed(() => isFullscreen.value ? 'calc(100vh - 150px)' : '600px')
const dictMaxHeight = computed(() => isFullscreen.value ? 'calc(100vh - 160px)' : '560px')

function toggleSelectAll() {
  selectAllTables.value = !selectAllTables.value
  tables.value.forEach(t => t.checked = selectAllTables.value)
}

async function loadTables() {
  try {
    const res = await http.get('/datadict/tables', { params: { connId, schema } })
    const result = res.data.data || res.data
    tables.value = (result.tables || []).map(t => ({ ...t, checked: false }))
  } catch (e) {
    ElMessage.error('加载表失败')
  }
}

async function generateDict() {
  const selected = tables.value.filter(t => t.checked).map(t => t.name).join(',')
  if (!selected) { ElMessage.warning('请选择要生成字典的表'); return }
  generating.value = true
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('tables', selected)
    const res = await http.post('/datadict/generate', formData)
    dictData.value = res.data.data || res.data
    ElMessage.success('字典生成成功')
  } catch (e) {
    ElMessage.error('生成失败')
  } finally {
    generating.value = false
  }
}

async function exportHTML() {
  const selected = tables.value.filter(t => t.checked).map(t => t.name).join(',')
  if (!selected && dictData.value) {
    ElMessage.info('使用当前字典数据导出')
  }
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('tables', selected)
    const res = await http.post('/datadict/export/html', formData, { responseType: 'blob' })
    const blob = new Blob([res.data], { type: 'text/html;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `datadict_${schema}.html`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('HTML导出成功')
  } catch (e) {
    ElMessage.error('导出失败')
  }
}

async function exportPDF() {
  const selected = tables.value.filter(t => t.checked).map(t => t.name).join(',')
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('tables', selected)
    const res = await http.post('/datadict/export/pdf', formData, { responseType: 'blob' })
    const blob = new Blob([res.data], { type: 'text/html;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const win = window.open(url, '_blank')
    if (!win) {
      ElMessage.warning('弹窗被浏览器拦截，请允许弹窗后重试')
    }
    setTimeout(() => URL.revokeObjectURL(url), 60000)
  } catch (e) {
    ElMessage.error('导出失败')
  }
}
</script>
