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
        <el-dropdown @command="handleImportCommand" style="margin-left: 12px;">
          <el-button type="success">
            导入<el-icon class="el-icon--right"><arrow-down /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="insert">新增</el-dropdown-item>
              <el-dropdown-item command="update">修改</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
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
                :title="col.comment || col.name" 
                style="cursor: pointer;"
                @click.stop="handleSort(col.name)"
              >
                {{ col.name }}
              </span>
              <el-icon
                :size="14"
                :style="{ cursor: 'pointer', color: isColumnFiltered(col.name) ? '#67c23a' : '#409eff' }"
                :title="isColumnFiltered(col.name) ? '已设置过滤条件' : '设置过滤条件'"
                @click.stop="openColumnFilter(col, $event)"
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

  <!-- Column filter popover -->
  <el-popover
    ref="columnFilterPopoverRef"
    v-model:visible="columnFilterDialogVisible"
    placement="bottom"
    :width="350"
    trigger="click"
    :virtual-ref="filterTriggerRef"
    virtual-triggering
    :title="'字段过滤 - ' + (currentColumn?.name || '')"
    @update:visible="handlePopoverVisibleChange"
  >
    <el-form label-width="60px" size="small" @click.stop>
      <el-form-item label="操作符">
        <el-select 
          v-model="columnFilterOperator" 
          style="width: 100%;" 
          size="small"
          @click.stop
        >
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
          size="small"
          @click.stop
        />
      </el-form-item>
      <div style="display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px;">
        <el-button size="small" @click="clearColumnFilter">清除</el-button>
        <el-button size="small" @click="columnFilterDialogVisible = false">取消</el-button>
        <el-button size="small" type="primary" @click="applyColumnFilter">应用</el-button>
      </div>
    </el-form>
  </el-popover>

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

  <!-- Import preview dialog -->
  <ImportPreviewDialog
    v-model="importPreviewVisible"
    :conn-id="connId"
    :schema="schema"
    :table-name="tableName"
    :db-columns="dbColumns"
    :on-import-success="loadData"
    ref="importDialogRef"
  />
</template>

<script setup>
import { ArrowDown, ArrowUp, Filter, Sort } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { computed, onMounted, ref, watch } from 'vue'
import * as XLSX from 'xlsx'
import ImportPreviewDialog from '../components/ImportPreviewDialog.vue'
import http from '../js/utils/httpProxy.js'

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
const columnFilterPopoverRef = ref(null)
const filterTriggerRef = ref(null)
// 存储每个字段的过滤条件：{ fieldName: { operator, value } }
const columnFilterConditions = ref({})

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
const dbColumns = ref([])
const importDialogRef = ref(null)
const importMode = ref('insert') // 存储当前选择的导入模式

function handleImportCommand(command) {
  // Store the import mode for later use when file is selected
  importMode.value = command
  // Trigger the hidden file input click
  const fileInput = document.querySelector('input[type="file"]')
  if (fileInput) {
    fileInput.click()
  }
}

const exporting = ref(false)

const unmatchedExcelColumns = computed(() => mappingStatus.value.unmatchedExcel)
const unmatchedDbColumns = computed(() => mappingStatus.value.unmatchedDb)

const rowIndexOffset = computed(() => (currentPage.value - 1) * pageSize.value + 1)

// 判断字段是否在过滤条件中
function isColumnFiltered(colName) {
  if (!filterExpr.value.trim()) return false
  // 使用单词边界精确匹配字段名，避免 parent_id 匹配到 id
  // 匹配模式：\`字段名\` 或 字段名（前后为非字母数字下划线或字符串边界）
  const escapedColName = colName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const pattern = new RegExp(`\\\`${escapedColName}\\\`|(?<![a-zA-Z0-9_])${escapedColName}(?![a-zA-Z0-9_])`, 'i')
  return pattern.test(filterExpr.value)
}

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
  params.append('tableName', props.tableName)
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
  params.append('tableName', props.tableName)
  params.append('sql', sql)
  const resp = await http.post('/execSQL', params)
  const data = resp.data.data

  if (data && data.columns) {
    dataColumns.value = data.columns.map((col) => ({
      name: col.name,
      comment: col.comment || '',
      type: col.type || '',
    }))
    console.log('[DataBrowser] columns:', dataColumns.value)
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
    console.error('[DataBrowser] 加载数据失败:', err)
    ElMessage({ message: '加载数据失败', type: 'error' })
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

function openColumnFilter(col, event) {
  currentColumn.value = col
  // 设置虚拟引用为点击的事件目标
  filterTriggerRef.value = event.currentTarget
  
  // 从存储中读取该字段的过滤条件，如果没有则使用默认值
  const savedCondition = columnFilterConditions.value[col.name]
  if (savedCondition) {
    columnFilterOperator.value = savedCondition.operator
    columnFilterValue.value = savedCondition.value
  } else {
    // 新字段，使用默认值
    columnFilterOperator.value = '='
    columnFilterValue.value = ''
  }
  
  columnFilterDialogVisible.value = true
}

function handlePopoverVisibleChange(visible) {
  // 当 popover 即将关闭时，如果有未完成的输入，保持显示
  if (!visible && currentColumn.value) {
    // 用户点击了外部区域或按了 ESC，允许关闭
  }
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
  
  // 保存该字段的过滤条件到存储
  if (currentColumn.value) {
    columnFilterConditions.value[currentColumn.value.name] = {
      operator: columnFilterOperator.value,
      value: columnFilterValue.value
    }
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
  
  // 清除该字段的存储条件
  delete columnFilterConditions.value[colName]
  
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
          ElMessage({ message: '导出失败', type: 'error' })
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
      console.error('[DataBrowser] 保存失败 - 后端返回:', respData.msg)
      ElMessage({ message: '保存失败，请检查数据', type: 'error' })
    } else {
      ElMessage({ message: '保存成功', type: 'success' })
      editDialogVisible.value = false
      await loadData()
    }
  } catch (err) {
    console.error('[DataBrowser] 保存失败:', err)
    ElMessage({ message: '保存失败', type: 'error' })
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
      console.error('[DataBrowser] 新增失败 - 后端返回:', respData.msg)
      ElMessage({ message: '操作失败，请检查数据', type: 'error' })
    } else {
      ElMessage({ message: '新增成功', type: 'success' })
      insertDialogVisible.value = false
      await loadData()
    }
  } catch (err) {
    console.error('[DataBrowser] 新增失败:', err)
    ElMessage({ message: '新增失败', type: 'error' })
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
      console.error('[DataBrowser] 删除失败 - 后端返回:', respData.msg)
      ElMessage({ message: '操作失败，请检查数据', type: 'error' })
    } else {
      ElMessage({ message: '删除成功', type: 'success' })
      await loadData()
    }
  } catch (err) {
    console.error('[DataBrowser] 删除失败:', err)
    ElMessage({ message: '删除失败', type: 'error' })
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
      console.error('[DataBrowser] 导入失败 - 响应:', res)
      if (res && res.data) {
        ElMessage({ message: '导入失败，请检查数据格式', type: 'error' })
      } else {
        ElMessage({ message: '导入失败', type: 'error' })
      }
    }
  }).catch((err) => {
    console.error('[DataBrowser] 导入失败:', err)
    ElMessage({ message: '导入失败', type: 'error' })
  }).finally(() => {
    fileList.value = []
    importing.value = false
  })
}

function handleFileSelect(options) {
  const file = options.file
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
      
      const headers = jsonData[0] || []
      const dataRows = jsonData.slice(1)
      
      // 获取数据库字段
      if (dataColumns.value && dataColumns.value.length > 0) {
        dbColumns.value = dataColumns.value.map(col => col.name)
      }
      
      // 设置文件数据并打开对话框（根据导入模式）
      importDialogRef.value?.setFileData(file, headers, dataRows)
      importDialogRef.value?.initMapping()
      importDialogRef.value?.previewData()
      if (importDialogRef.value?.setImportMode) {
        importDialogRef.value.setImportMode(importMode.value)
      }
      importPreviewVisible.value = true
    } catch (err) {
      console.error('[DataBrowser] 读取 Excel 文件失败:', err)
      ElMessage({ message: '读取 Excel 文件失败，请检查文件格式', type: 'error' })
    }
  }
  reader.readAsArrayBuffer(options.file)
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
