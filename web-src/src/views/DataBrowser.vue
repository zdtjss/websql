<template>
  <div class="data-browser" style="height: calc(100vh - 60px); display: flex; flex-direction: column;">
    <!-- Toolbar -->
    <div class="toolbar" style="padding: 6px 10px; border-bottom: 1px solid #e4e7ed; display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
      <span style="font-weight: 600; font-size: 14px; margin-right: 8px;">{{ tableName }}</span>
      <el-button size="small" @click="loadData" :loading="loading">刷新</el-button>
      <el-button size="small" type="primary" @click="openInsertDialog">新增</el-button>
      <el-input
        v-model="filterExpr"
        size="small"
        placeholder="过滤条件 (WHERE 子句)"
        style="width: 260px;"
        clearable
        @clear="onFilterClear"
        @keyup.enter="applyFilter"
      />
      <el-button size="small" type="primary" @click="applyFilter">应用</el-button>
      <el-button size="small" type="warning" :loading="exporting" @click="exportData">导出</el-button>
      <el-button size="small" type="success" @click="aiPanelVisible = !aiPanelVisible">AI 分析</el-button>
    </div>

    <!-- Table area -->
    <div style="flex: 1; overflow: hidden;">
      <el-table
        :data="rows"
        height="100%"
        style="width: 100%"
        :row-key="getRowKey"
        stripe
        @sort-change="onSortChange"
      >
        <!-- Row index column -->
        <el-table-column type="index" width="60" fixed :index="rowIndexOffset" />

        <!-- Data columns -->
        <el-table-column
          v-for="col in dataColumns"
          :key="col.name"
          :prop="col.name"
          :label="col.name"
          min-width="150"
          show-overflow-tooltip
          sortable="custom"
        >
          <template #header>
            <span :title="col.comment">{{ col.name }}</span>
          </template>
        </el-table-column>

        <!-- Action column -->
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="scope">
            <el-button size="small" type="primary" link @click="openEditDialog(scope.row, scope.$index)">
              编辑
            </el-button>
            <el-popconfirm
              title="确定删除这条记录吗？"
              @confirm="deleteRow(scope.row)"
            >
              <template #reference>
                <el-button size="small" type="danger" link>删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- Pagination -->
    <div style="padding: 8px 10px; border-top: 1px solid #e4e7ed; display: flex; justify-content: flex-end;">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        @current-change="onPageChange"
        @size-change="onSizeChange"
      />
    </div>

    <!-- AI Analysis Panel -->
    <AIAnalysisPanel
      :visible="aiPanelVisible"
      :connId="props.connId"
      :schema="props.schema"
      :tableName="props.tableName"
      :dataSample="rows"
    />
  </div>

  <!-- Edit dialog -->
  <el-dialog
    v-model="editDialogVisible"
    :title="'编辑 - ' + tableName"
    width="700px"
    :draggable="true"
    destroy-on-close
    style="max-height: 80vh; overflow-y: auto;"
  >
    <div style="max-height: 500px; overflow-y: auto;">
      <el-form :model="editRowData" label-width="auto" style="margin-right: 10px;">
        <el-form-item
          v-for="col in dataColumns"
          :key="col.name"
          :label="col.name"
          :title="col.comment"
        >
          <el-input
            v-model="editRowData[col.name]"
            type="textarea"
            autosize
            :disabled="pkColumns.includes(col.name)"
          />
        </el-form-item>
      </el-form>
    </div>
    <template #footer>
      <el-button @click="editDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="saveData">保存</el-button>
    </template>
  </el-dialog>

  <!-- Insert dialog -->
  <el-dialog
    v-model="insertDialogVisible"
    :title="'新增 - ' + tableName"
    width="700px"
    :draggable="true"
    destroy-on-close
    style="max-height: 80vh; overflow-y: auto;"
  >
    <div style="max-height: 500px; overflow-y: auto;">
      <el-form :model="insertRowData" label-width="auto" style="margin-right: 10px;">
        <el-form-item
          v-for="col in dataColumns"
          :key="col.name"
          :label="col.name"
          :title="col.comment"
        >
          <el-input
            v-model="insertRowData[col.name]"
            type="textarea"
            autosize
            :placeholder="col.type"
          />
        </el-form-item>
      </el-form>
    </div>
    <template #footer>
      <el-button @click="insertDialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="inserting" @click="insertData">提交</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../js/utils/httpProxy.js'
import AIAnalysisPanel from './components/AIAnalysisPanel.vue'

const props = defineProps({
  connId: String,
  schema: String,
  tableName: String,
})

const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const dataColumns = ref([])  // [{name, comment, type}]
const rows = ref([])

// Filter & sort state
const filterExpr = ref('')
const sortColumn = ref('')
const sortOrder = ref(null)  // 'ascending' | 'descending' | null

// Edit dialog state
const editDialogVisible = ref(false)
const editRowData = ref({})
const originRowData = ref({})
const pkColumns = ref([])
const saving = ref(false)

// Insert dialog state
const insertDialogVisible = ref(false)
const insertRowData = ref({})
const inserting = ref(false)

// AI panel state
const aiPanelVisible = ref(false)

const exporting = ref(false)

const rowIndexOffset = computed(() => (currentPage.value - 1) * pageSize.value + 1)

function getRowKey(row) {
  if (pkColumns.value.length > 0) {
    return pkColumns.value.map(k => row[k]).join('_')
  }
  return JSON.stringify(row)
}

async function fetchTotal() {
  let sql = `SELECT COUNT(*) as cnt FROM \`${props.tableName}\``
  if (filterExpr.value.trim()) {
    sql += ` WHERE ${filterExpr.value.trim()}`
  }
  const params = new URLSearchParams()
  params.append('connId', props.connId)
  params.append('schema', props.schema)
  params.append('sql', sql)
  const resp = await http.post('/execSQL', params)
  const data = resp.data.data
  if (data && data.data && data.data.length > 0) {
    const firstRow = data.data[0]
    total.value = Number(firstRow['cnt'] ?? firstRow['COUNT(*) as cnt'] ?? 0)
  }
}

async function fetchData() {
  const offset = (currentPage.value - 1) * pageSize.value
  let sql = `SELECT * FROM \`${props.tableName}\``
  if (filterExpr.value.trim()) {
    sql += ` WHERE ${filterExpr.value.trim()}`
  }
  if (sortColumn.value && sortOrder.value) {
    const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
    sql += ` ORDER BY \`${sortColumn.value}\` ${dir}`
  }
  sql += ` LIMIT ${pageSize.value} OFFSET ${offset}`
  const params = new URLSearchParams()
  params.append('connId', props.connId)
  params.append('schema', props.schema)
  params.append('sql', sql)
  const resp = await http.post('/execSQL', params)
  const data = resp.data.data

  if (data && data.columns) {
    dataColumns.value = data.columns.map((col) => ({
      name: col.name,
      comment: col.comment || '',
      type: col.type || '',
    }))
    // Capture primary key info from response
    if (data.keys && data.keys.length > 0) {
      pkColumns.value = data.keys
    } else {
      // Fallback: use 'id' column or first column as PK
      const colNames = dataColumns.value.map(c => c.name)
      const idCol = colNames.find(n => n.toLowerCase() === 'id')
      pkColumns.value = idCol ? [idCol] : colNames.slice(0, 1)
    }
  }

  rows.value = data?.data ?? []
}

async function loadData() {
  if (!props.connId || !props.schema || !props.tableName) return
  loading.value = true
  try {
    await fetchTotal()
    await fetchData()
  } catch (err) {
    ElMessage({ message: err?.message || '加载数据失败', type: 'error' })
  } finally {
    loading.value = false
  }
}

function onPageChange() {
  loadData()
}

function onSizeChange() {
  currentPage.value = 1
  loadData()
}

function onSortChange({ prop, order }) {
  sortColumn.value = prop || ''
  sortOrder.value = order  // 'ascending' | 'descending' | null
  loadData()
}

function applyFilter() {
  currentPage.value = 1
  loadData()
}

function onFilterClear() {
  filterExpr.value = ''
  currentPage.value = 1
  loadData()
}

function exportData() {
  let sql = `SELECT * FROM \`${props.tableName}\``
  if (filterExpr.value.trim()) {
    sql += ` WHERE ${filterExpr.value.trim()}`
  }
  if (sortColumn.value && sortOrder.value) {
    const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
    sql += ` ORDER BY \`${sortColumn.value}\` ${dir}`
  }
  const params = new URLSearchParams()
  params.append('connId', props.connId)
  params.append('schema', props.schema)
  params.append('filename', props.tableName)
  params.append('sql', sql)
  exporting.value = true
  http.post('/exportXlsxBySql', params, { responseType: 'blob' })
    .then((res) => {
      // If server returned an error JSON, surface it instead of saving corrupt file
      const contentType = res.headers['content-type'] || ''
      if (contentType.includes('application/json')) {
        return res.data.text().then(text => {
          const err = JSON.parse(text)
          ElMessage({ message: err.msg || '导出失败', type: 'error' })
        })
      }
      const blob = new Blob([res.data], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
      const url = window.URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = props.tableName + '.xlsx'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      window.URL.revokeObjectURL(url)
    })
    .catch(() => ElMessage({ message: '导出失败', type: 'error' }))
    .finally(() => exporting.value = false)
}

function openEditDialog(row) {
  editRowData.value = { ...row }
  originRowData.value = { ...row }
  editDialogVisible.value = true
}

async function saveData() {
  const origin = originRowData.value
  const current = editRowData.value

  // Build SET clause from changed columns (exclude PK columns)
  const changedCols = Object.keys(origin).filter(
    key => !pkColumns.value.includes(key) && origin[key] !== current[key]
  )

  if (changedCols.length === 0) {
    ElMessage({ message: '数据未修改', type: 'warning' })
    return
  }

  const setClauses = changedCols.map(key => `\`${key}\` = ${fmtVal(current[key])}`).join(', ')

  // Build WHERE clause from PK columns
  const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(origin).slice(0, 1)
  const whereClauses = pkCols.map(key => `\`${key}\` = ${fmtVal(origin[key])}`).join(' AND ')

  const sql = `UPDATE \`${props.tableName}\` SET ${setClauses} WHERE ${whereClauses}`

  saving.value = true
  try {
    const params = new URLSearchParams()
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('sql', sql)
    const resp = await http.post('/execSQL', params)
    const respData = resp.data.data

    if (respData && respData.msg) {
      // Server returned an error message
      ElMessage({ message: respData.msg, type: 'error' })
      // Keep dialog open — local data unchanged (automatic rollback)
    } else {
      ElMessage({ message: '保存成功', type: 'success' })
      editDialogVisible.value = false
      await loadData()
    }
  } catch (err) {
    // HTTP-level error — keep dialog open
    ElMessage({ message: err?.message || '保存失败', type: 'error' })
  } finally {
    saving.value = false
  }
}

function fmtVal(val) {
  if (val === null || val === undefined) {
    return 'NULL'
  } else if (typeof val === 'string' && val.length > 2 && val.startsWith("b'") && val.endsWith("'")) {
    return val
  } else if (typeof val === 'string') {
    return "'" + val.replace(/'/g, "''") + "'"
  }
  return val
}

function openInsertDialog() {
  // Initialize all columns to empty string
  const blank = {}
  dataColumns.value.forEach(col => { blank[col.name] = '' })
  insertRowData.value = blank
  insertDialogVisible.value = true
}

async function insertData() {
  const row = insertRowData.value
  // Only include columns with non-empty values
  const cols = Object.keys(row).filter(k => row[k] !== '' && row[k] !== null && row[k] !== undefined)

  if (cols.length === 0) {
    ElMessage({ message: '请至少填写一个字段', type: 'warning' })
    return
  }

  const colList = cols.map(k => `\`${k}\``).join(', ')
  const valList = cols.map(k => fmtVal(row[k])).join(', ')
  const sql = `INSERT INTO \`${props.tableName}\` (${colList}) VALUES (${valList})`

  inserting.value = true
  try {
    const params = new URLSearchParams()
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('sql', sql)
    const resp = await http.post('/execSQL', params)
    const respData = resp.data.data

    if (respData && respData.msg) {
      ElMessage({ message: respData.msg, type: 'error' })
    } else {
      ElMessage({ message: '新增成功', type: 'success' })
      insertDialogVisible.value = false
      await loadData()
    }
  } catch (err) {
    ElMessage({ message: err?.message || '新增失败', type: 'error' })
  } finally {
    inserting.value = false
  }
}

async function deleteRow(row) {
  const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(row).slice(0, 1)
  const whereClauses = pkCols.map(key => `\`${key}\` = ${fmtVal(row[key])}`).join(' AND ')
  const sql = `DELETE FROM \`${props.tableName}\` WHERE ${whereClauses}`

  try {
    const params = new URLSearchParams()
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('sql', sql)
    const resp = await http.post('/execSQL', params)
    const respData = resp.data.data

    if (respData && respData.msg) {
      ElMessage({ message: respData.msg, type: 'error' })
    } else {
      ElMessage({ message: '删除成功', type: 'success' })
      await loadData()
    }
  } catch (err) {
    ElMessage({ message: err?.message || '删除失败', type: 'error' })
  }
}

onMounted(() => {
  loadData()
})

watch(
  () => [props.connId, props.schema, props.tableName],
  () => {
    currentPage.value = 1
    loadData()
  }
)
</script>
