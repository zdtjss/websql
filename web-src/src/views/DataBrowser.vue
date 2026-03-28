<template>
  <div class="data-browser" style="height: calc(100vh - 60px); display: flex; flex-direction: column;">
    <!-- Toolbar -->
    <div class="toolbar" style="padding: 6px 10px; border-bottom: 1px solid #e4e7ed; display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
      <span style="font-weight: 600; font-size: 14px; margin-right: 8px;">{{ tableName }}</span>
      <el-button size="small" @click="loadData" :loading="loading">刷新</el-button>
      <el-button size="small" type="primary" @click="openInsertDialog">新增</el-button>
      <el-upload
        :file-list="fileList"
        :http-request="handleFileSelect"
        :show-file-list="false"
        :limit="1"
        accept=".xlsx,.xls"
      >
        <el-button size="small" type="success" :loading="importing">导入</el-button>
      </el-upload>
      <el-button size="small" type="warning" :loading="exporting" @click="exportData">导出</el-button>
    </div>

    <!-- Table area -->
    <div style="flex: 1; overflow: hidden;">
      <el-table
        :data="rows"
        height="100%"
        style="width: 100%"
        :row-key="getRowKey"
        stripe
        border
        @sort-change="onSortChange"
      >
        <!-- Row index column -->
        <el-table-column type="index" width="60" fixed :index="rowIndexOffset" />

        <!-- Data columns -->
        <el-table-column
          v-for="col in dataColumns"
          :key="col.name"
          :prop="col.name"
          :min-width="Math.max(150, col.name.length * 14 + 60)"
          show-overflow-tooltip
          resizable
        >
          <template #header>
            <div style="display: flex; align-items: center; gap: 5px;">
              <span 
                :title="col.comment" 
                style="cursor: pointer;"
                @click.stop="handleSort(col.name)"
              >{{ col.name }}</span>
              <el-icon
                :size="14"
                style="cursor: pointer; color: #409eff;"
                title="设置过滤条件"
                @click.stop="openColumnFilter(col)"
              >
                <Filter />
              </el-icon>
              <el-icon 
                :size="14" 
                style="cursor: pointer;"
                title="排序"
                @click.stop="handleSort(col.name)"
              >
                <component :is="getSortIcon(col.name)" />
              </el-icon>
            </div>
          </template>
          <template #default="scope">
            <span :title="col.comment">{{ scope.row[col.name] }}</span>
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

  <!-- Column filter dialog -->
  <el-dialog
    v-model="columnFilterDialogVisible"
    :title="'字段过滤 - ' + (currentColumn?.name || '')"
    width="500px"
    :draggable="true"
    destroy-on-close
  >
    <el-form label-width="80px">
      <el-form-item label="字段">
        <span>{{ currentColumn?.name }}</span>
        <span v-if="currentColumn?.comment" style="color: #909399; margin-left: 8px;">{{ currentColumn.comment }}</span>
      </el-form-item>
      <el-form-item label="操作符">
        <el-select v-model="columnFilterOperator" style="width: 100%;">
          <el-option label="等于" value="=" />
          <el-option label="不等于" value="!=" />
          <el-option label="大于" value=">" />
          <el-option label="大于等于" value=">=" />
          <el-option label="小于" value="<" />
          <el-option label="小于等于" value="<=" />
          <el-option label="LIKE" value="LIKE" />
          <el-option label="NOT LIKE" value="NOT LIKE" />
          <el-option label="IS NULL" value="IS NULL" />
          <el-option label="IS NOT NULL" value="IS NOT NULL" />
          <el-option label="IN (逗号分隔)" value="IN" />
          <el-option label="NOT IN (逗号分隔)" value="NOT IN" />
        </el-select>
      </el-form-item>
      <el-form-item 
        label="值" 
        v-if="!['IS NULL', 'IS NOT NULL'].includes(columnFilterOperator)"
      >
        <el-input
          v-model="columnFilterValue"
          type="textarea"
          :rows="2"
          :placeholder="getOperatorPlaceholder(columnFilterOperator)"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="columnFilterDialogVisible = false">取消</el-button>
      <el-button @click="clearColumnFilter">清除该字段过滤</el-button>
      <el-button type="primary" @click="applyColumnFilter">应用</el-button>
    </template>
  </el-dialog>

  <!-- Import preview dialog -->
  <el-dialog
    v-model="importPreviewVisible"
    title="导入预览"
    width="1000px"
    :draggable="true"
    destroy-on-close
  >
    <div style="margin-bottom: 15px; display: flex; align-items: center; gap: 12px; flex-wrap: wrap;">
      <el-form :inline="true" size="small">
        <el-form-item label="数据起始行">
          <el-input-number v-model="dataStartRow" :min="1" :step="1" style="width: 100px;" @change="previewData" />
        </el-form-item>
        <el-form-item label="预览行数">
          <el-input-number v-model="previewRows" :min="1" :max="100" :step="10" style="width: 100px;" @change="previewData" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="small" @click="previewData">
            <el-icon><Refresh /></el-icon>刷新
          </el-button>
        </el-form-item>
      </el-form>
    </div>

    <div style="margin-bottom: 10px; overflow-x: auto;">
      <el-table 
        :data="previewDataList" 
        border 
        max-height="400" 
        stripe 
        :header-cell-style="{background:'#f5f7fa'}"
        :resizable="true"
      >
        <el-table-column 
          v-for="(col, idx) in previewColumns" 
          :key="col.excelCol + '_' + idx"
          min-width="120"
          :width="150"
        >
          <template #header>
            <div style="display: flex; flex-direction: column; gap: 6px; padding: 8px 0;">
              <div style="display: flex; flex-direction: column; gap: 6px; width: 100%;">
                <!-- 未匹配字段：显示红色 Excel 列名 + 下拉框 -->
                <template v-if="!col.dbCol">
                  <div 
                    style="font-weight: 600; font-size: 13px; color: #f56c6c; white-space: nowrap;" 
                    :title="col.excelCol"
                  >
                    {{ col.excelCol }}
                  </div>
                  <el-select 
                    v-model="col.dbCol" 
                    size="small" 
                    placeholder="选择字段"
                    filterable
                    allow-create
                    clearable
                    style="width: 100%;"
                    @change="onMappingChange"
                  >
                    <el-option
                      v-for="dbCol in getAvailableColumns(col)"
                      :key="dbCol"
                      :label="dbCol"
                      :value="dbCol"
                    >
                      <span style="white-space: normal; word-break: break-all;">{{ dbCol }}</span>
                    </el-option>
                  </el-select>
                </template>
                <!-- 已匹配字段：显示绿色数据库字段名 + 重置按钮（仅自定义匹配） -->
                <template v-else>
                  <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;">
                    <div style="font-size: 13px; color: #67c23a; font-weight: 600; white-space: nowrap; flex: 1; overflow: hidden; text-overflow: ellipsis;">
                      <el-icon style="vertical-align: middle;"><CircleCheck /></el-icon>
                      {{ col.dbCol }}
                    </div>
                    <el-button 
                      v-if="!col.isAutoMatched"
                      size="small" 
                      type="warning" 
                      link
                      style="flex-shrink: 0; padding: 4px;"
                      @click="resetColumnMapping(col)"
                    >
                      <el-icon size="14"><RefreshLeft /></el-icon>
                    </el-button>
                  </div>
                </template>
              </div>
            </div>
          </template>
          <template #default="scope">
            <span :title="scope.row[col.excelCol]" style="font-size: 13px;">{{ scope.row[col.excelCol] }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
    
    <template #footer>
      <div style="display: flex; justify-content: space-between; align-items: center;">
        <div style="display: flex; gap: 12px;">
          <el-tag type="success" size="small">
            <el-icon><CircleCheck /></el-icon>
            已匹配 {{ mappingStatus.matched > 0 ? mappingStatus.matched : '0' }}
          </el-tag>
          <el-tag type="danger" size="small">
            <el-icon><Warning /></el-icon>
            未匹配 {{ mappingStatus.unmatchedExcel.length > 0 ? mappingStatus.unmatchedExcel.length : '0' }}
          </el-tag>
        </div>
        <div style="display: flex; gap: 10px;">
          <el-button @click="importPreviewVisible = false">取消</el-button>
          <el-button type="primary" :loading="importing" @click="confirmImport">
            <el-icon><Upload /></el-icon>导入
          </el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Filter, ArrowUp, ArrowDown, Sort, Warning, CircleCheck, Refresh, Upload, RefreshLeft } from '@element-plus/icons-vue'
import http from '../js/utils/httpProxy.js'
import * as XLSX from 'xlsx'

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

// Column filter dialog state
const columnFilterDialogVisible = ref(false)
const currentColumn = ref(null)
const columnFilterOperator = ref('=')
const columnFilterValue = ref('')

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

// Import state
const fileList = ref([])
const importing = ref(false)
const importPreviewVisible = ref(false)
const selectedFile = ref(null)
const excelData = ref([])
const excelHeaders = ref([])
const dataStartRow = ref(1)
const previewRows = ref(10)
const previewDataList = ref([])
const previewColumns = ref([])
const dbColumns = ref([])
const mappingStatus = ref({
  matched: 0,
  unmatchedExcel: [],
  unmatchedDb: []
})

const exporting = ref(false)

const unmatchedExcelColumns = computed(() => mappingStatus.value.unmatchedExcel)
const unmatchedDbColumns = computed(() => mappingStatus.value.unmatchedDb)

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

function handleSort(colName) {
  if (sortColumn.value === colName) {
    if (sortOrder.value === null) {
      sortOrder.value = 'ascending'
    } else if (sortOrder.value === 'ascending') {
      sortOrder.value = 'descending'
    } else {
      sortColumn.value = ''
      sortOrder.value = null
    }
  } else {
    sortColumn.value = colName
    sortOrder.value = 'ascending'
  }
  loadData()
}

function getSortIcon(colName) {
  if (sortColumn.value !== colName) {
    return Sort
  }
  return sortOrder.value === 'ascending' ? ArrowUp : ArrowDown
}

function openColumnFilter(col) {
  currentColumn.value = col
  columnFilterOperator.value = '='
  columnFilterValue.value = ''
  columnFilterDialogVisible.value = true
}

function getOperatorPlaceholder(op) {
  switch (op) {
    case 'LIKE':
    case 'NOT LIKE':
      return '例如：%keyword%'
    case 'IN':
    case 'NOT IN':
      return '例如：value1,value2,value3'
    default:
      return '请输入值'
  }
}

function buildColumnCondition() {
  if (!currentColumn.value) return ''
  
  const colName = `\`${currentColumn.value.name}\``
  const op = columnFilterOperator.value
  const val = columnFilterValue.value.trim()
  
  if (op === 'IS NULL') {
    return `${colName} IS NULL`
  }
  if (op === 'IS NOT NULL') {
    return `${colName} IS NOT NULL`
  }
  
  if (!val) {
    ElMessage({ message: '请输入值', type: 'warning' })
    return ''
  }
  
  if (op === 'IN' || op === 'NOT IN') {
    const values = val.split(',').map(v => v.trim()).filter(v => v)
    if (values.length === 0) {
      ElMessage({ message: '请至少输入一个值', type: 'warning' })
      return ''
    }
    const formatted = values.map(v => fmtVal(v)).join(', ')
    return `${colName} ${op} (${formatted})`
  }
  
  if (op === 'LIKE' || op === 'NOT LIKE') {
    return `${colName} ${op} ${fmtVal(val)}`
  }
  
  return `${colName} ${op} ${fmtVal(val)}`
}

function applyColumnFilter() {
  const condition = buildColumnCondition()
  if (!condition) return
  
  if (filterExpr.value.trim()) {
    filterExpr.value = filterExpr.value.trim() + ' AND ' + condition
  } else {
    filterExpr.value = condition
  }
  
  columnFilterDialogVisible.value = false
  currentPage.value = 1
  loadData()
  
  ElMessage({ message: '过滤条件已应用', type: 'success' })
}

function clearColumnFilter() {
  if (!currentColumn.value) return
  
  const colName = currentColumn.value.name
  const conditions = filterExpr.value.split(/\s+AND\s+/i).filter(c => {
    const trimmed = c.trim()
    return !trimmed.startsWith(`\`${colName}\``) && 
           !trimmed.startsWith(colName) &&
           !trimmed.includes(`\`${colName}\``) &&
           !trimmed.includes(colName)
  })
  
  filterExpr.value = conditions.join(' AND ')
  columnFilterDialogVisible.value = false
  currentPage.value = 1
  loadData()
  
  ElMessage({ message: '该字段过滤已清除', type: 'success' })
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

function upload(options) {
  let param = new FormData()
  param.append('file', options.file)
  param.append('connId', props.connId)
  param.append('schema', props.schema)
  param.append('table', options.data.table)

  importing.value = true

  http.post('/importXlsx', param, {
    headers: { 'content-type': 'multipart/form-data' }
  }).then((res) => {
    if (res && res.status === 200) {
      ElMessage({ message: '导入成功', type: 'success' })
      loadData()
    } else {
      if (res && res.data) {
        ElMessage({ message: res.data, type: 'error' })
      } else {
        ElMessage({ message: '导入失败', type: 'error' })
      }
    }
  }).catch((err) => {
    ElMessage({ message: err?.message || '导入失败', type: 'error' })
  }).finally(() => {
    fileList.value = []
    importing.value = false
  })
}

function handleFileSelect(options) {
  selectedFile.value = options.file
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const data = new Uint8Array(e.target.result)
      const workbook = XLSX.read(data, { type: 'array' })
      const firstSheetName = workbook.SheetNames[0]
      const worksheet = workbook.Sheets[firstSheetName]
      const jsonData = XLSX.utils.sheet_to_json(worksheet, { header: 1 })
      
      if (jsonData.length === 0) {
        ElMessage({ message: 'Excel 文件为空', type: 'warning' })
        return
      }
      
      excelHeaders.value = jsonData[0] || []
      excelData.value = jsonData.slice(1)
      
      // 确保 dbColumns 已加载
      fetchDbColumns().then(() => {
        console.log('数据库字段:', dbColumns.value)
        console.log('Excel 列:', excelHeaders.value)
        initMapping()
        previewData()
        importPreviewVisible.value = true
      })
    } catch (err) {
      ElMessage({ message: '读取 Excel 文件失败：' + err.message, type: 'error' })
    }
  }
  reader.readAsArrayBuffer(options.file)
}

function fetchDbColumns() {
  return new Promise((resolve) => {
    // 优先使用 dataColumns（已经在 loadData 时获取）
    if (dataColumns.value && dataColumns.value.length > 0) {
      dbColumns.value = dataColumns.value.map(col => col.name)
      resolve()
      return
    }
    
    // 如果 dataColumns 为空，尝试查询 INFORMATION_SCHEMA
    const sql = `SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = '${props.schema}' AND TABLE_NAME = '${props.tableName}'`
    const params = new URLSearchParams()
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('sql', sql)
    
    http.post('/execSQL', params).then((resp) => {
      const data = resp.data.data
      if (data && data.data) {
        dbColumns.value = data.data.map(row => Object.values(row)[0])
      }
      resolve()
    }).catch((err) => {
      console.warn('获取数据库字段失败:', err)
      // 如果查询失败，使用已有的 dataColumns
      dbColumns.value = dataColumns.value.map(col => col.name)
      resolve()
    })
  })
}

function initMapping() {
  previewColumns.value = excelHeaders.value.map((excelCol) => {
    const matchedDbCol = dbColumns.value.find(dbCol => dbCol.toLowerCase() === excelCol.toLowerCase())
    return {
      excelCol: excelCol,
      dbCol: matchedDbCol || '',
      isAutoMatched: !!matchedDbCol  // 标记是否为自动匹配
    }
  })
  console.log('previewColumns:', previewColumns.value)
  updateMappingStatus()
}

function onMappingChange() {
  updateMappingStatus()
}

function getAvailableColumns(currentCol) {
  // 获取所有未被其他列使用的数据库字段
  const usedColumns = previewColumns.value
    .filter(col => col.dbCol && col.excelCol !== currentCol.excelCol)
    .map(col => col.dbCol)
  
  return dbColumns.value.filter(dbCol => !usedColumns.includes(dbCol))
}

function resetColumnMapping(col) {
  col.dbCol = ''
  updateMappingStatus()
  ElMessage({ message: '已重置该列的映射关系', type: 'info' })
}

function resetMapping() {
  previewColumns.value.forEach(col => {
    col.dbCol = ''
  })
  initMapping()
  ElMessage({ message: '映射已重置', type: 'info' })
}

function updateMappingStatus() {
  const matched = previewColumns.value.filter(col => col.dbCol).length
  const unmatchedExcel = previewColumns.value.filter(col => !col.dbCol).map(col => col.excelCol)
  const matchedDbCols = previewColumns.value.filter(col => col.dbCol).map(col => col.dbCol)
  const unmatchedDb = dbColumns.value.filter(dbCol => !matchedDbCols.includes(dbCol))
  
  mappingStatus.value = {
    matched,
    unmatchedExcel,
    unmatchedDb
  }
}

function previewData() {
  const startIdx = dataStartRow.value - 1
  const endIdx = startIdx + previewRows.value
  const slicedData = excelData.value.slice(startIdx, endIdx)
  
  previewDataList.value = slicedData.map((row) => {
    const rowData = {}
    excelHeaders.value.forEach((header, idx) => {
      rowData[header] = row[idx] !== undefined ? row[idx] : ''
    })
    return rowData
  })
}

function confirmImport() {
  const validMapping = previewColumns.value.filter(col => col.dbCol)
  if (validMapping.length === 0) {
    ElMessage({ message: '请至少映射一个字段', type: 'warning' })
    return
  }
  
  importPreviewVisible.value = false
  executeImport()
}

function executeImport() {
  if (!selectedFile.value) return
  
  const mapping = {}
  previewColumns.value.forEach(col => {
    if (col.dbCol) {
      mapping[col.excelCol] = col.dbCol
    }
  })
  
  let param = new FormData()
  param.append('file', selectedFile.value)
  param.append('connId', props.connId)
  param.append('schema', props.schema)
  param.append('table', props.tableName)
  param.append('startRow', dataStartRow.value.toString())
  param.append('mapping', JSON.stringify(mapping))
  
  importing.value = true
  
  http.post('/importXlsx', param, {
    headers: { 'content-type': 'multipart/form-data' }
  }).then((res) => {
    if (res && res.status === 200) {
      ElMessage({ message: '导入成功', type: 'success' })
      importPreviewVisible.value = false
      loadData()
    } else {
      if (res && res.data) {
        ElMessage({ message: res.data, type: 'error' })
      } else {
        ElMessage({ message: '导入失败', type: 'error' })
      }
    }
  }).catch((err) => {
    ElMessage({ message: err?.message || '导入失败', type: 'error' })
  }).finally(() => {
    fileList.value = []
    importing.value = false
    selectedFile.value = null
  })
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
