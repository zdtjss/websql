<template>
  <div class="data-browser classical-panel">
    <!-- Toolbar -->
    <div class="db-toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">
          <el-icon :size="16" color="#409eff"><Grid /></el-icon>
          {{ tableName }}
        </span>
        <el-divider direction="vertical" />
        <el-button size="small" @click="loadData" :loading="loading" :icon="Refresh">刷新</el-button>
        <el-button size="small" :icon="InfoFilled" @click="openTableStructure">表结构</el-button>
        <el-dropdown @command="handleAutoRefresh" style="margin-left: -4px;">
          <el-button size="small" :type="autoRefreshInterval > 0 ? 'warning' : ''" :icon="Timer">
            {{ autoRefreshInterval > 0 ? autoRefreshInterval + 's' : '' }}
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="0">关闭自动刷新</el-dropdown-item>
              <el-dropdown-item command="5">每 5 秒</el-dropdown-item>
              <el-dropdown-item command="15">每 15 秒</el-dropdown-item>
              <el-dropdown-item command="30">每 30 秒</el-dropdown-item>
              <el-dropdown-item command="60">每 60 秒</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button size="small" type="primary" @click="openInsertDialog" :icon="Plus">新增</el-button>
        <el-button size="small" type="success" @click="addBlankRow" :icon="Plus">添加行</el-button>
        <el-upload
          :file-list="fileList"
          :http-request="handleFileSelect"
          :show-file-list="false"
          :limit="1"
          :accept="importAccept"
        >
          <el-dropdown @command="handleImportCommand">
            <el-button size="small" type="success">
              导入<el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-item command="insert_xlsx">📊 新增导入 (Excel)</el-dropdown-item>
              <el-dropdown-item command="update_xlsx">📊 更新导入 (Excel)</el-dropdown-item>
              <el-dropdown-item command="insert_csv" divided>📄 新增导入 (CSV)</el-dropdown-item>
              <el-dropdown-item command="update_csv">📄 更新导入 (CSV)</el-dropdown-item>
              <el-dropdown-item command="insert_json" divided>📋 新增导入 (JSON)</el-dropdown-item>
              <el-dropdown-item command="update_json">📋 更新导入 (JSON)</el-dropdown-item>
          </template>
        </el-dropdown>
        </el-upload>
        <el-dropdown @command="handleExportCommand">
            <el-button size="small" type="warning" :loading="exporting">
              导出<el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="xlsx">Excel (.xlsx)</el-dropdown-item>
                <el-dropdown-item command="csv">CSV</el-dropdown-item>
                <el-dropdown-item command="json">JSON</el-dropdown-item>
                <el-dropdown-item command="sql">SQL INSERT</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
      </div>
      <div class="toolbar-right" v-if="filterExpr">
        <el-tag closable type="info" size="small" @close="filterExpr = ''; columnFilterConditions = {}; loadData()">
          过滤中
        </el-tag>
      </div>
    </div>

    <!-- Inline edit status bar -->
    <div v-if="inlineChangeCount > 0 || newRowUids.size > 0" class="db-inline-bar">
      <span class="inline-change-hint">
        <template v-if="inlineChangeCount > 0">{{ inlineChangeCount }} 个单元格已修改</template>
        <template v-if="inlineChangeCount > 0 && newRowUids.size > 0">，</template>
        <template v-if="newRowUids.size > 0">{{ newRowUids.size }} 行待新增</template>
      </span>
      <el-button size="small" type="primary" :loading="savingInline" @click="saveInlineChanges">保存更改</el-button>
      <el-button size="small" @click="discardInlineChanges">放弃更改</el-button>
    </div>

    <!-- Table area -->
    <div class="table-wrapper" style="flex: 1; overflow: hidden;" @paste="handlePaste" @keydown="onTableKeydown" @mouseup="onTableMouseUp" @mouseleave="onTableMouseUp" tabindex="0">
      <el-table
        :data="rows"
        height="100%"
        style="width: 100%"
        :row-key="getRowKey"
        stripe
        border
        :row-class-name="rowClassName"
        :cell-class-name="cellClassFn"
      >
        <!-- Row index column -->
        <el-table-column type="index" width="60" fixed :index="rowIndexOffset" resizable />

        <!-- Data columns -->
        <el-table-column
          v-for="col in dataColumns"
          :key="col.name"
          :prop="col.name"
          :min-width="Math.max(150, col.name.length * 14 + 60)"
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
            <div class="inline-cell"
              :class="{ 'cell-selected-sel': isCellInSelection(scope.$index, col.name) }"
              @mousedown="onCellMouseDown(scope.$index, col.name, $event)"
              @mouseenter="onCellMouseEnter(scope.$index, col.name)"
              @dblclick.stop="startInlineEdit(scope.row, col.name, $event)"
              @click="activeCellIndex = scope.$index; activeColName = col.name">
              <template v-if="isEditingCell(scope.row, col.name)">
                <el-date-picker
                  v-if="isDateColumn(col.name)"
                  v-model="editingValue"
                  type="datetime"
                  value-format="YYYY-MM-DDTHH:mm:ss"
                  placeholder="选择日期"
                  size="small"
                  class="inline-edit-input"
                  @keyup.escape="cancelInlineEdit()"
                  @visible-change="(visible) => { if (!visible) commitInlineEdit() }"
                />
                <el-input
                  v-else
                  :ref="(el) => setEditInputRef(el)"
                  v-model="editingValue"
                  size="small"
                  class="inline-edit-input"
                  @keyup.enter="commitInlineEdit()"
                  @keyup.escape="cancelInlineEdit()"
                  @blur="commitInlineEdit()"
                />
              </template>
              <span v-else :class="{ 'cell-changed': isCellChanged(scope.row, col.name) }" :title="String(scope.row[col.name] ?? '')">
                <template v-if="scope.row[col.name] !== null && scope.row[col.name] !== undefined && scope.row[col.name] !== ''">{{ scope.row[col.name] }}</template>
                <span v-else class="null-placeholder">-</span>
              </span>
            </div>
          </template>
        </el-table-column>

        <!-- Action column -->
        <el-table-column label="操作" width="180" fixed="right" resizable>
          <template #default="scope">
            <template v-if="isNewRow(scope.row)">
              <el-button size="small" type="danger" link @click="removeNewRow(scope.row)">
                移除
              </el-button>
            </template>
            <template v-else>
              <el-button size="small" type="primary" link @click="openEditDialog(scope.row, scope.$index)">
                详细
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
            <el-button size="small" type="success" link @click="copyRow(scope.row)">复制</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- Pagination -->
    <div class="db-pagination">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :page-sizes="[20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        @current-change="onPageChange"
        @size-change="onSizeChange"
        small
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
    width="720px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
  >
    <div style="max-height: 480px; overflow-y: auto; padding-right: 8px;">
      <el-form :model="editRowData" label-width="auto" size="default">
        <el-form-item
          v-for="col in dataColumns"
          :key="col.name"
          :label="col.name"
          :title="col.comment"
        >
          <el-date-picker
            v-if="isDateColumn(col.name)"
            v-model="editRowData[col.name]"
            type="datetime"
            value-format="YYYY-MM-DDTHH:mm:ss"
            :disabled="pkColumns.includes(col.name)"
          />
          <el-input
            v-else
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
    width="720px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
  >
    <div style="max-height: 480px; overflow-y: auto; padding-right: 8px;">
      <el-form :model="insertRowData" label-width="auto" size="default">
        <el-form-item
          v-for="col in dataColumns"
          :key="col.name"
          :label="col.name"
          :title="col.comment"
        >
          <el-date-picker
            v-if="isDateColumn(col.name)"
            v-model="insertRowData[col.name]"
            type="datetime"
            value-format="YYYY-MM-DDTHH:mm:ss"
          />
          <el-input
            v-else
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
    :import-format="importFormat"
    :on-import-success="loadData"
    ref="importDialogRef"
    @confirm-import-data="handleCsvJsonImport"
  />
</template>

<script setup>
import { ArrowDown, ArrowUp, Download, Filter, Grid, InfoFilled, Plus, Refresh, Sort, Timer } from '@element-plus/icons-vue'
import { ElLoading, ElMessage } from 'element-plus'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as XLSX from 'xlsx'
import ImportPreviewDialog from '../components/ImportPreviewDialog.vue'
import http from '../js/utils/httpProxy.js'
import { fmtVal } from '../js/utils/sqlHelper.ts'
import { exportToCsv, exportToJson, exportToSql, downloadBlob } from '../js/utils/exportHelper.ts'

const props = defineProps({
  connId: String,
  schema: String,
  tableName: String,
})

const emit = defineEmits(['viewTableInfo'])

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

// Inline editing state
const editingCell = ref(null)  // { rowKey: string, colName: string } | null
const editingValue = ref('')
const changedRows = ref({})  // { [rowKey]: { [colName]: newValue, ... } }
const originalRows = ref({})  // { [rowKey]: { [colName]: value, ... } }
const savingInline = ref(false)
let editInputRef = null

// Paste tracking
const activeCellIndex = ref(-1)
const activeColName = ref('')
const pasteSnapshot = ref(null)

const inlineChangeCount = computed(() => {
  let count = 0
  Object.values(changedRows.value).forEach(row => {
    count += Object.keys(row).length
  })
  return count
})

// Inline new row state
let nextRowUid = 1
const newRowUids = ref(new Set())  // track _rowUid values of unsaved new rows

// Excel-style range selection state
const selStart = ref({ row: -1, col: -1 })
const selEnd = ref({ row: -1, col: -1 })
const selAnchor = ref({ row: -1, col: -1 })
const isSelDragging = ref(false)

const selectionBounds = computed(() => {
  if (selStart.value.row < 0 || selEnd.value.row < 0) return null
  return {
    rowMin: Math.min(selStart.value.row, selEnd.value.row),
    rowMax: Math.max(selStart.value.row, selEnd.value.row),
    colMin: Math.min(selStart.value.col, selEnd.value.col),
    colMax: Math.max(selStart.value.col, selEnd.value.col),
  }
})

function colNameToIndex(colName) {
  return dataColumns.value.findIndex(c => c.name === colName)
}

function isCellInSelection(rowIdx, colName) {
  const bounds = selectionBounds.value
  if (!bounds) return false
  const colIdx = colNameToIndex(colName)
  return rowIdx >= bounds.rowMin && rowIdx <= bounds.rowMax &&
         colIdx >= bounds.colMin && colIdx <= bounds.colMax
}

function cellClassFn({ rowIndex, columnIndex }) {
  const bounds = selectionBounds.value
  if (!bounds) return ''
  // columnIndex includes index column and action column, need to offset
  const dataColIdx = columnIndex - 1  // -1 for the index column
  if (dataColIdx >= bounds.colMin && dataColIdx <= bounds.colMax &&
      rowIndex >= bounds.rowMin && rowIndex <= bounds.rowMax) {
    return 'cell-range-selected'
  }
  return ''
}

function clearRangeSelection() {
  selStart.value = { row: -1, col: -1 }
  selEnd.value = { row: -1, col: -1 }
  selAnchor.value = { row: -1, col: -1 }
  isSelDragging.value = false
}

function onCellMouseDown(rowIdx, colName, e) {
  if (isEditingCell.value) return
  const colIdx = colNameToIndex(colName)
  if (colIdx < 0) return

  if (e.shiftKey && selAnchor.value.row >= 0) {
    selEnd.value = { row: rowIdx, col: colIdx }
  } else {
    selStart.value = { row: rowIdx, col: colIdx }
    selEnd.value = { row: rowIdx, col: colIdx }
  }
  selAnchor.value = { row: selStart.value.row, col: selStart.value.col }
  isSelDragging.value = true
  // Prevent triggering click/dblclick on the inline edit
  e.preventDefault()
}

function onCellMouseEnter(rowIdx, colName) {
  if (!isSelDragging.value) return
  const colIdx = colNameToIndex(colName)
  if (colIdx < 0) return
  selEnd.value = { row: rowIdx, col: colIdx }
}

function onTableMouseUp() {
  isSelDragging.value = false
}

function copySelectedRange() {
  const bounds = selectionBounds.value
  if (!bounds) return

  const lines = []
  const cols = dataColumns.value.slice(bounds.colMin, bounds.colMax + 1)
  for (let r = bounds.rowMin; r <= bounds.rowMax; r++) {
    const row = rows.value[r]
    if (!row) continue
    const line = cols.map(c => {
      const val = row[c.name]
      return val != null ? String(val) : ''
    }).join('\t')
    lines.push(line)
  }
  if (lines.length > 0) {
    navigator.clipboard.writeText(lines.join('\n')).catch(() => {})
    ElMessage({ message: `已复制 ${lines.length} 行`, type: 'success' })
  }
}

async function pasteToSelectedRange(e) {
  const bounds = selectionBounds.value
  if (!bounds) return

  let text = ''
  try {
    text = await navigator.clipboard.readText()
  } catch {
    try {
      text = (e.clipboardData || window.clipboardData)?.getData('text') || ''
    } catch { return }
  }
  if (!text.trim()) return

  const lines = text.split(/\r?\n/).filter(l => l.trim() !== '' || l === '').map(l => l || '\t')
  const rows_data = lines.map(l => l.split('\t'))
  if (rows_data.length === 0) return

  const pasteRows = rows_data.length
  const pasteCols = Math.max(...rows_data.map(r => r.length))
  const availableCols = dataColumns.value.length - bounds.colMin

  // Ensure enough rows exist
  const neededRows = bounds.rowMin + pasteRows
  while (rows.value.length < neededRows) {
    addBlankRowSilent()
  }

  // Apply paste values
  for (let r = 0; r < pasteRows; r++) {
    const targetRow = rows.value[bounds.rowMin + r]
    if (!targetRow) continue
    const key = getRowKey(targetRow)
    if (!changedRows.value[key]) changedRows.value[key] = {}

    for (let c = 0; c < Math.min(pasteCols, availableCols); c++) {
      const colName = dataColumns.value[bounds.colMin + c]?.name
      if (!colName) continue
      const val = rows_data[r][c] !== undefined ? rows_data[r][c] : ''

      if (isNewRow(targetRow) && pkColumns.value.includes(colName)) {
        targetRow[colName] = val
      }
      if (String(targetRow[colName] ?? '') !== val) {
        changedRows.value[key][colName] = val
      } else {
        delete changedRows.value[key][colName]
      }
    }
    if (Object.keys(changedRows.value[key]).length === 0) {
      delete changedRows.value[key]
    }
  }
}

function addBlankRowSilent() {
  const blank = { _rowUid: nextRowUid++ }
  dataColumns.value.forEach(col => { blank[col.name] = '' })
  rows.value.push(blank)
  const key = getRowKey(blank)
  newRowUids.value = new Set([...newRowUids.value, blank._rowUid])
  originalRows.value[key] = {}
}

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
const importMode = ref('insert')
const importFormat = ref('xlsx')

const importAccept = computed(() => {
  switch (importFormat.value) {
    case 'csv': return '.csv'
    case 'json': return '.json'
    default: return '.xlsx,.xls'
  }
})

// Auto-refresh
const autoRefreshInterval = ref(0)
let autoRefreshTimer = null

function handleAutoRefresh(seconds) {
  const sec = parseInt(seconds)
  autoRefreshInterval.value = sec
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
  if (sec > 0) {
    autoRefreshTimer = setInterval(() => {
      if (!loading.value) {
        loadData()
      }
    }, sec * 1000)
  }
}

onBeforeUnmount(() => {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
  }
})

function handleImportCommand(command) {
  if (command.endsWith('_csv')) {
    importFormat.value = 'csv'
    importMode.value = command.startsWith('insert') ? 'insert' : 'update'
  } else if (command.endsWith('_json')) {
    importFormat.value = 'json'
    importMode.value = command.startsWith('insert') ? 'insert' : 'update'
  } else {
    importFormat.value = 'xlsx'
    importMode.value = command.startsWith('insert') ? 'insert' : 'update'
  }
  const fileInput = document.querySelector('.data-browser input[type="file"]')
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
  if (row._rowUid) return '_new_' + row._rowUid
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
  params.append('maxLine', '1')
  const resp = await http.post('/execSQL', params)
  const data = resp.data.data
  if (data && data.data && data.data.length > 0) {
    const firstRow = data.data[0]
    const firstValue = Object.values(firstRow)[0]
    total.value = Number(firstValue ?? 0)
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
  changedRows.value = {}
  newRowUids.value = new Set()
  rows.value.forEach(row => {
    originalRows.value[getRowKey(row)] = { ...row }
  })
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

function rowClassName({ row }) {
  const key = getRowKey(row)
  if (changedRows.value[key]) return 'row-changed'
  if (isNewRow(row)) return 'row-new'
  return ''
}

function setEditInputRef(el) {
  if (el) {
    editInputRef = el
    el.focus?.()
    el.select?.()
  }
}

function isDateColumn(colName) {
  const col = dataColumns.value.find((c) => c.name === colName)
  if (!col || !col.type) return false
  const upper = col.type.toUpperCase()
  return upper === 'DATETIME' || upper === 'DATE' || upper === 'TIMESTAMP' 
    || upper === 'TIMESTAMP(6)' || upper.includes('TIMESTAMP')
    || upper === 'TIMESTAMPTZ' || upper === 'TIMESTAMPLTZ'
}

function isEditingCell(row, colName) {
  if (!editingCell.value) return false
  return editingCell.value.rowKey === getRowKey(row) && editingCell.value.colName === colName
}

function isCellChanged(row, colName) {
  const key = getRowKey(row)
  return changedRows.value[key] && changedRows.value[key][colName] !== undefined
}

function startInlineEdit(row, colName, event) {
  if (!isNewRow(row) && pkColumns.value.includes(colName)) return
  const key = getRowKey(row)
  editingCell.value = { rowKey: key, colName }
  const changed = changedRows.value[key]
  const currentVal = changed && changed[colName] !== undefined ? changed[colName] : row[colName]
  editingValue.value = currentVal ?? ''
}

function commitInlineEdit() {
  if (!editingCell.value) return
  const { rowKey, colName } = editingCell.value
  const newVal = editingValue.value
  const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined
  const strNew = String(newVal ?? '')
  const strOrig = String(origVal ?? '')

  if (strNew !== strOrig) {
    if (!changedRows.value[rowKey]) {
      changedRows.value[rowKey] = {}
    }
    changedRows.value[rowKey][colName] = newVal
  } else {
    if (changedRows.value[rowKey]) {
      delete changedRows.value[rowKey][colName]
      if (Object.keys(changedRows.value[rowKey]).length === 0) {
        delete changedRows.value[rowKey]
      }
    }
  }

  editingCell.value = null
  editingValue.value = ''
}

function cancelInlineEdit() {
  editingCell.value = null
  editingValue.value = ''
}

function handlePaste(event) {
  const text = event.clipboardData?.getData('text/plain')
  if (!text) return

  let startRowIdx = -1
  let startColIdx = -1

  if (editingCell.value) {
    startRowIdx = rows.value.findIndex(r => getRowKey(r) === editingCell.value.rowKey)
    startColIdx = dataColumns.value.findIndex(c => c.name === editingCell.value.colName)
  } else if (activeCellIndex.value >= 0 && activeColName.value) {
    startRowIdx = activeCellIndex.value
    startColIdx = dataColumns.value.findIndex(c => c.name === activeColName.value)
  }

  if (startRowIdx < 0 || startColIdx < 0) return

  const lines = text.split('\n')
  const grid = []
  for (const line of lines) {
    const trimmed = line.trim()
    if (trimmed) {
      grid.push(trimmed.split('\t'))
    }
  }
  if (grid.length === 0) return

  event.preventDefault()

  // Save snapshot for Ctrl+Z undo
  const snapshot = {
    changedRows: JSON.parse(JSON.stringify(changedRows.value)),
    restoredCells: []
  }

  cancelInlineEdit()

  for (let ri = 0; ri < grid.length; ri++) {
    const targetRowIdx = startRowIdx + ri
    if (targetRowIdx >= rows.value.length) break
    const targetRow = rows.value[targetRowIdx]
    const rowKey = getRowKey(targetRow)

    for (let ci = 0; ci < grid[ri].length; ci++) {
      const targetColIdx = startColIdx + ci
      if (targetColIdx >= dataColumns.value.length) break
      const colName = dataColumns.value[targetColIdx].name
      const targetRow = rows.value[targetRowIdx]
      if (!isNewRow(targetRow) && pkColumns.value.includes(colName)) continue

      const newVal = grid[ri][ci].trim()
      const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined

      // Record old value for undo
      const oldChanged = changedRows.value[rowKey]?.[colName]
      snapshot.restoredCells.push({
        rowKey,
        colName,
        oldVal: oldChanged !== undefined ? oldChanged : origVal
      })

      const strNew = String(newVal ?? '')
      const strOrig = String(origVal ?? '')

      if (strNew !== strOrig) {
        if (!changedRows.value[rowKey]) {
          changedRows.value[rowKey] = {}
        }
        changedRows.value[rowKey][colName] = newVal
      } else {
        if (changedRows.value[rowKey]) {
          delete changedRows.value[rowKey][colName]
          if (Object.keys(changedRows.value[rowKey]).length === 0) {
            delete changedRows.value[rowKey]
          }
        }
      }
    }
  }

  pasteSnapshot.value = snapshot
  activeCellIndex.value = -1
  activeColName.value = ''
}

function onTableKeydown(event) {
  // Ctrl+Z: undo paste
  if ((event.ctrlKey || event.metaKey) && event.key === 'z') {
    if (pasteSnapshot.value) {
      event.preventDefault()
      undoPaste()
      return
    }
  }
  // Range selection keyboard shortcuts
  if (isEditingCell.value) return
  const bounds = selectionBounds.value
  if (!bounds) return

  if (event.ctrlKey && event.key === 'c') {
    event.preventDefault()
    copySelectedRange()
  } else if (event.ctrlKey && event.key === 'v') {
    event.preventDefault()
    pasteToSelectedRange(event)
  }
}

function undoPaste() {
  const snapshot = pasteSnapshot.value
  if (!snapshot) return

  changedRows.value = JSON.parse(JSON.stringify(snapshot.changedRows))

  for (const cell of snapshot.restoredCells) {
    const { rowKey, colName, oldVal } = cell
    const row = rows.value.find(r => getRowKey(r) === rowKey)
    if (row) {
      if (oldVal === undefined) {
        delete row[colName]
      } else {
        row[colName] = oldVal
      }
    }
  }

  pasteSnapshot.value = null
}

async function saveInlineChanges() {
  const rowKeys = Object.keys(changedRows.value)
  const newKeys = rowKeys.filter(k => k.startsWith('_new_'))
  const existingKeys = rowKeys.filter(k => !k.startsWith('_new_'))
  if (rowKeys.length === 0 && newRowUids.value.size === 0) return

  savingInline.value = true
  let successCount = 0
  let errorCount = 0

  try {
    for (const rowKey of newKeys) {
      const changed = changedRows.value[rowKey]
      const row = rows.value.find(r => getRowKey(r) === rowKey)
      if (!row) continue

      const merged = { ...row }
      if (changed) {
        Object.keys(changed).forEach(k => { merged[k] = changed[k] })
      }

      const insertCols = dataColumns.value
        .filter(col => {
          const val = merged[col.name]
          return val !== '' && val !== null && val !== undefined
        })
      if (insertCols.length === 0) continue

      const colList = insertCols.map(c => '`' + c.name + '`').join(', ')
      const valList = insertCols.map(c => fmtVal(merged[c.name])).join(', ')

      const sql = `INSERT INTO \`${props.tableName}\` (${colList}) VALUES (${valList})`

      const params = new URLSearchParams()
      params.append('connId', props.connId)
      params.append('schema', props.schema)
      params.append('sql', sql)

      try {
        const resp = await http.post('/execSQL', params)
        const respData = resp.data.data
        if (respData && respData.msg) {
          errorCount++
        } else {
          successCount++
        }
      } catch {
        errorCount++
      }
    }

    for (const rowKey of existingKeys) {
      const changed = changedRows.value[rowKey]
      const orig = originalRows.value[rowKey]
      if (!orig) continue

      const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(orig).slice(0, 1)
      const setClauses = Object.keys(changed)
        .map(k => `\`${k}\` = ${fmtVal(changed[k])}`)
        .join(', ')

      const allWhereCols = [
        ...pkCols,
        ...Object.keys(changed).filter(k => !pkCols.includes(k))
      ]
      const whereClauses = allWhereCols
        .map(k => `\`${k}\` = ${fmtVal(orig[k])}`)
        .join(' AND ')

      const sql = `UPDATE \`${props.tableName}\` SET ${setClauses} WHERE ${whereClauses}`

      const params = new URLSearchParams()
      params.append('connId', props.connId)
      params.append('schema', props.schema)
      params.append('sql', sql)

      try {
        const resp = await http.post('/execSQL', params)
        const respData = resp.data.data
        if (respData && respData.msg) {
          errorCount++
        } else {
          successCount++
        }
      } catch {
        errorCount++
      }
    }

    if (errorCount === 0) {
      ElMessage({ message: `成功保存 ${successCount} 条记录`, type: 'success' })
    } else {
      ElMessage({ message: `保存完成: ${successCount} 成功, ${errorCount} 失败`, type: 'warning' })
    }
    await loadData()
  } finally {
    savingInline.value = false
  }
}

function discardInlineChanges() {
  // Remove new rows
  const uidSet = newRowUids.value
  rows.value = rows.value.filter(r => !r._rowUid || !uidSet.has(r._rowUid))
  newRowUids.value = new Set()
  // Restore original values
  changedRows.value = {}
  rows.value.forEach(row => {
    if (!row._rowUid) {
      const key = getRowKey(row)
      if (originalRows.value[key]) {
        Object.assign(row, originalRows.value[key])
      }
    }
  })
  ElMessage({ message: '已放弃更改', type: 'info' })
}

function addBlankRow() {
  const blank = { _rowUid: nextRowUid++ }
  dataColumns.value.forEach(col => { blank[col.name] = '' })
  rows.value.push(blank)
  const key = getRowKey(blank)
  newRowUids.value = new Set([...newRowUids.value, blank._rowUid])
  originalRows.value[key] = {}
}

function copyRow(row) {
  const copied = { _rowUid: nextRowUid++ }
  dataColumns.value.forEach(col => {
    if (pkColumns.value.includes(col.name)) {
      copied[col.name] = ''
    } else {
      copied[col.name] = row[col.name] != null ? row[col.name] : ''
    }
  })
  rows.value.push(copied)
  const key = getRowKey(copied)
  newRowUids.value = new Set([...newRowUids.value, copied._rowUid])
  originalRows.value[key] = {}
  // Mark non-empty fields as changed so save bar appears
  const changed = {}
  dataColumns.value.forEach(col => {
    const val = copied[col.name]
    if (val !== '' && val !== null && val !== undefined && !pkColumns.value.includes(col.name)) {
      changed[col.name] = val
    }
  })
  if (Object.keys(changed).length > 0) {
    changedRows.value[key] = changed
  }
}

function removeNewRow(row) {
  if (!row._rowUid) return
  rows.value = rows.value.filter(r => r._rowUid !== row._rowUid)
  const uid = row._rowUid
  const nextSet = new Set(newRowUids.value)
  nextSet.delete(uid)
  newRowUids.value = nextSet
  const key = getRowKey(row)
  delete changedRows.value[key]
  delete originalRows.value[key]
}

function isNewRow(row) {
  return row._rowUid && newRowUids.value.has(row._rowUid)
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

async function handleExportCommand(format) {
  if (format === 'xlsx') {
    await exportToExcel()
    return
  }

  loading.value = true
  try {
    const resp = await fetchFullData()
    if (!resp) return

    const rows = resp.data?.data ?? []
    const cols = dataColumns.value.map(c => c.name)

    if (format === 'csv') {
      exportToCsv(cols, rows, props.tableName)
    } else if (format === 'json') {
      exportToJson(rows, props.tableName)
    } else if (format === 'sql') {
      const sqlText = exportToSql(cols, rows, props.tableName)
      downloadBlob(sqlText, props.tableName + '.sql', 'text/plain')
    }
    ElMessage({ message: '导出成功', type: 'success' })
  } catch (err) {
    console.error('[DataBrowser] 导出失败:', err)
    ElMessage({ message: '导出失败', type: 'error' })
  } finally {
    loading.value = false
  }
}

async function fetchFullData() {
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
  params.append('tableName', props.tableName)
  params.append('sql', sql)
  params.append('maxLine', '100000')
  return await http.post('/execSQL', params)
}

async function exportToExcel() {
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

  const changedCols = Object.keys(origin).filter(
    key => !pkColumns.value.includes(key) && origin[key] !== current[key]
  )

  if (changedCols.length === 0) {
    ElMessage({ message: '数据未修改', type: 'warning' })
    return
  }

  const setClauses = changedCols.map(key => `\`${key}\` = ${fmtVal(current[key])}`).join(', ')

  const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(origin).slice(0, 1)
  const allWhereCols = [
    ...pkCols,
    ...changedCols.filter(k => !pkCols.includes(k))
  ]
  const whereClauses = allWhereCols.map(key => `\`${key}\` = ${fmtVal(origin[key])}`).join(' AND ')

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
  if (isNewRow(row)) {
    removeNewRow(row)
    return
  }
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
  if (importFormat.value === 'csv') {
    handleCsvFile(file)
  } else if (importFormat.value === 'json') {
    handleJsonFile(file)
  } else {
    handleExcelFile(file)
  }
}

function handleExcelFile(file) {
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
      
      if (dataColumns.value && dataColumns.value.length > 0) {
        dbColumns.value = dataColumns.value.map(col => col.name)
      }
      
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
  reader.readAsArrayBuffer(file)
}

function handleCsvFile(file) {
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const text = e.target.result
      const lines = text.split(/\r?\n/).filter(line => line.trim())
      if (lines.length === 0) {
        ElMessage({ message: 'CSV 文件为空', type: 'warning' })
        return
      }
      const headers = parseCsvLine(lines[0])
      const dataRows = lines.slice(1).map(parseCsvLine)
      
      if (dataColumns.value && dataColumns.value.length > 0) {
        dbColumns.value = dataColumns.value.map(col => col.name)
      }
      
      importDialogRef.value?.setFileData(file, headers, dataRows)
      importDialogRef.value?.initMapping()
      importDialogRef.value?.previewData()
      if (importDialogRef.value?.setImportMode) {
        importDialogRef.value.setImportMode(importMode.value)
      }
      importPreviewVisible.value = true
    } catch (err) {
      console.error('[DataBrowser] 读取 CSV 文件失败:', err)
      ElMessage({ message: '读取 CSV 文件失败，请检查文件格式', type: 'error' })
    }
  }
  reader.readAsText(file)
}

function parseCsvLine(line) {
  const result = []
  let current = ''
  let inQuotes = false
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (inQuotes) {
      if (ch === '"') {
        if (i + 1 < line.length && line[i + 1] === '"') {
          current += '"'
          i++
        } else {
          inQuotes = false
        }
      } else {
        current += ch
      }
    } else {
      if (ch === '"') {
        inQuotes = true
      } else if (ch === ',') {
        result.push(current)
        current = ''
      } else {
        current += ch
      }
    }
  }
  result.push(current)
  return result
}

function handleJsonFile(file) {
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const json = JSON.parse(e.target.result)
      if (!Array.isArray(json) || json.length === 0) {
        ElMessage({ message: 'JSON 文件应为非空数组', type: 'warning' })
        return
      }
      const headers = Object.keys(json[0])
      const dataRows = json.map(obj => headers.map(h => obj[h] ?? ''))
      
      if (dataColumns.value && dataColumns.value.length > 0) {
        dbColumns.value = dataColumns.value.map(col => col.name)
      }
      
      importDialogRef.value?.setFileData(file, headers, dataRows)
      importDialogRef.value?.initMapping()
      importDialogRef.value?.previewData()
      if (importDialogRef.value?.setImportMode) {
        importDialogRef.value.setImportMode(importMode.value)
      }
      importPreviewVisible.value = true
    } catch (err) {
      console.error('[DataBrowser] 读取 JSON 文件失败:', err)
      ElMessage({ message: '读取 JSON 文件失败，请检查文件格式', type: 'error' })
    }
  }
  reader.readAsText(file)
}

async function handleCsvJsonImport({ data, mapping, mode }) {
  if (!data || data.length === 0) {
    ElMessage({ message: '没有可导入的数据', type: 'warning' })
    return
  }

  const loading = ElLoading.service({ fullscreen: false, text: `正在${mode === 'insert' ? '新增' : '更新'}导入 ${data.length} 条数据...` })

  let successCount = 0
  let errorCount = 0

  try {
    const batchSize = 50
    for (let i = 0; i < data.length; i += batchSize) {
      const batch = data.slice(i, i + batchSize)
      const sqlStatements = []

      for (const row of batch) {
        const cols = Object.keys(row).filter(k => row[k] !== null && row[k] !== undefined)
        if (cols.length === 0) continue

        if (mode === 'insert') {
          const colList = cols.map(k => '`' + k + '`').join(', ')
          const valList = cols.map(k => fmtVal(row[k])).join(', ')
          sqlStatements.push(`INSERT INTO \`${props.tableName}\` (${colList}) VALUES (${valList})`)
        } else {
          const setClauses = cols.map(k => '`' + k + '` = ' + fmtVal(row[k])).join(', ')
          const pkCols = pkColumns.value.length > 0 ? pkColumns.value : cols.slice(0, 1)
          const whereClauses = pkCols.filter(k => row[k] !== null).map(k => '`' + k + '` = ' + fmtVal(row[k])).join(' AND ')
          if (!whereClauses) continue
          sqlStatements.push(`UPDATE \`${props.tableName}\` SET ${setClauses} WHERE ${whereClauses}`)
        }
      }

      const batchSql = sqlStatements.join('; ')
      if (!batchSql) continue

      const params = new URLSearchParams()
      params.append('connId', props.connId)
      params.append('schema', props.schema)
      params.append('sql', batchSql)

      try {
        await http.post('/execSQL', params)
        successCount += batch.length
      } catch {
        errorCount += batch.length - sqlStatements.length
      }
    }
  } finally {
    loading.close()
    importPreviewVisible.value = false
    if (errorCount === 0) {
      ElMessage({ message: `${mode === 'insert' ? '新增' : '更新'}导入成功，共 ${successCount} 条`, type: 'success' })
    } else {
      ElMessage({ message: `导入完成: ${successCount} 成功, ${errorCount} 失败`, type: 'warning' })
    }
    await loadData()
  }
}

onMounted(() => {
  loadData()
})

function openTableStructure() {
  emit('viewTableInfo', {
    connId: props.connId,
    schema: props.schema,
    tableName: props.tableName,
  })
}

watch(
  () => [props.connId, props.schema, props.tableName],
  () => {
    currentPage.value = 1
    loadData()
  }
)
</script>


<style scoped>
.data-browser {
  height: calc(100vh - 60px);
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

.db-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 14px;
  background: var(--bg-toolbar);
  border-bottom: 1px solid var(--border-primary);
  gap: 8px;
  flex-wrap: wrap;
}

.db-toolbar .toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.db-toolbar .toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.db-toolbar .toolbar-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary);
  display: flex;
  align-items: center;
  gap: 6px;
  margin-right: 4px;
}

.db-toolbar .el-button {
  border-radius: 6px;
  font-size: 13px;
}

.db-toolbar .el-divider--vertical {
  margin: 0 4px;
  height: 16px;
}

.db-pagination {
  padding: 8px 14px;
  border-top: 1px solid var(--border-primary);
  display: flex;
  justify-content: flex-end;
  background: var(--bg-toolbar);
}

.db-inline-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 14px;
  background: var(--bg-inline-bar);
  border-bottom: 1px solid var(--border-inline);
}

.db-inline-bar .inline-change-hint {
  font-size: 13px;
  color: var(--accent-color);
  font-weight: 500;
}

.inline-cell {
  min-height: 24px;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.inline-cell > span {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: block;
  width: 100%;
}

.null-placeholder {
  color: var(--text-tertiary, #bbb);
  font-style: italic;
  user-select: none;
}

.inline-edit-input {
  width: 100%;
}

.inline-edit-input :deep(.el-input__inner) {
  padding: 0 4px;
  height: 28px;
}

.cell-changed {
  background: #fff7e6;
  padding: 2px 4px;
  border-radius: 3px;
  border-bottom: 1px dashed #faad14;
}

[data-theme="dark"] .cell-changed {
  background: #3d3520;
}

:deep(.row-new td) {
  border-left: 3px solid #67c23a;
  background-color: #f0f9eb !important;
}

[data-theme="dark"] :deep(.row-new td) {
  background-color: #1a2a1a !important;
  border-left-color: #4caf50;
}

:deep(.cell-range-selected) {
  background-color: #d4e6ff !important;
  outline: 2px solid #409eff;
  outline-offset: -1px;
}

[data-theme="dark"] :deep(.cell-range-selected) {
  background-color: #2a3a5a !important;
  outline-color: #89b4fa;
}

:deep(.cell-selected-sel) {
  background-color: rgba(64, 158, 255, 0.08);
}

[data-theme="dark"] :deep(.cell-selected-sel) {
  background-color: rgba(137, 180, 250, 0.12);
}

:deep(.row-changed td) {
  border-bottom: 2px solid #faad14;
}
</style>
