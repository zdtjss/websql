<template>
  <div class="data-browser classical-panel">
    <!-- Toolbar -->
    <div class="db-toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">
          <el-icon :size="16" color="#409eff"><Grid /></el-icon>
          {{ schema + "." + tableName }}
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
              @mousemove="onCellMouseMove(scope.$index, col.name)"
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
              <span v-else :class="{ 'cell-changed': isCellChanged(scope.row, col.name) }" :title="formatCellTitle(getRowValue(scope.row, col.name))">
                <template v-if="getRowValue(scope.row, col.name) !== null && getRowValue(scope.row, col.name) !== undefined && getRowValue(scope.row, col.name) !== ''">{{ getRowValue(scope.row, col.name) }}</template>
                <span v-else-if="getRowValue(scope.row, col.name) === null || getRowValue(scope.row, col.name) === undefined" class="null-placeholder">NULL</span>
                <span v-else class="empty-placeholder"></span>
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
        size="small"
      />
    </div>

  </div>

  <!-- Column filter popover -->
  <el-popover
    ref="columnFilterPopoverRef"
    :visible="columnFilterDialogVisible"
    placement="bottom"
    :width="350"
    trigger="manual"
    :virtual-ref="filterTriggerRef"
    virtual-triggering
    :title="'字段过滤 - ' + (currentColumn?.name || '')"
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
          <div style="display: flex; align-items: flex-start; gap: 6px; width: 100%;">
            <el-date-picker
              v-if="isDateColumn(col.name)"
              v-model="editRowData[col.name]"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm:ss"
              :disabled="pkColumns.includes(col.name)"
              :placeholder="editRowData[col.name] === null ? 'NULL' : ''"
              style="flex: 1;"
            />
            <el-input
              v-else
              v-model="editRowData[col.name]"
              type="textarea"
              autosize
              :disabled="pkColumns.includes(col.name)"
              :placeholder="editRowData[col.name] === null ? 'NULL' : ''"
              style="flex: 1;"
            />
            <el-button
              v-if="!pkColumns.includes(col.name)"
              size="small"
              :type="editRowData[col.name] === null ? 'warning' : 'default'"
              link
              @click="editRowData[col.name] = editRowData[col.name] === null ? '' : null"
              :title="editRowData[col.name] === null ? '当前为 NULL，点击设为空字符串' : '点击设为 NULL'"
            >∅</el-button>
          </div>
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
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import * as XLSX from 'xlsx'
import ImportPreviewDialog from '@/components/data/ImportPreviewDialog.vue'
import http from '@/utils/httpProxy.js'
import { buildCountSQL, buildPagedSQL, buildSelectSQL, buildWhereCondition, fmtVal, quoteId } from '@/utils/sqlHelper.ts'
import { exportToCsv, exportToJson, exportToSql, downloadBlob } from '@/utils/exportHelper.ts'
import { useDbSchemaStore } from '@/stores/dbSchema'

const { connId, schema, tableName, tabId, dbType, schemaPath } = defineProps({
  connId: String,
  schema: String,
  tableName: String,
  tabId: String,
  dbType: String,
  schemaPath: String,
})

const dbSchemaProxy = useDbSchemaStore()
const resolvedDbType = ref('')
const effectiveDbType = computed(() => dbType || dbSchemaProxy.getDbType(schema) || resolvedDbType.value || '')

async function resolveDbType() {
  if (dbType || dbSchemaProxy.getDbType(schema)) return
  try {
    const resp = await http.get('/listConn2', { params: { pageSize: 1000 } })
    const result = (resp.data && resp.data.data ? resp.data.data : resp.data) || {}
    const connList = result.data || []
    const conn = connList.find(c => String(c.id) === String(connId))
    if (conn && conn.dbType) {
      resolvedDbType.value = conn.dbType
    }
  } catch {}
}

const emit = defineEmits(['viewTableInfo', 'openDataBrowser', 'openTableManager'])

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
const columnFilterPopoverRef = useTemplateRef('columnFilterPopoverRef')
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
const editingOriginalValue = ref(null)  // original value before inline edit (preserves null vs '')
const changedRows = ref({})  // { [rowKey]: { [colName]: newValue, ... } }
const originalRows = ref({})  // { [rowKey]: { [colName]: value, ... } }
const savingInline = ref(false)
let editInputRef = null
// Guards against repeated setup on every re-render (inline ref functions are
// re-invoked on each render because the arrow function identity changes).
// Without this, el.select() runs on every keystroke/paste, selecting all text
// and breaking cursor positioning — especially after paste where el-input's
// own setCursor() never fires (paste is preventDefault'd).
let _editInputSetupDone = false

// Capture-phase paste handler for inline editing input
// Bypasses el-input's internal event handling by intercepting at capture phase
let _inputPasteHandler = null
function _createInputPasteHandler(nativeInput) {
  return function(e) {
    e.preventDefault()
    e.stopPropagation()
    e.stopImmediatePropagation()
    const text = e.clipboardData?.getData('text/plain')
    if (!text) return
    const start = nativeInput.selectionStart ?? editingValue.value.length
    const end = nativeInput.selectionEnd ?? start
    const val = editingValue.value
    const newVal = val.substring(0, start) + text + val.substring(end)
    nativeInput.value = newVal
    editingValue.value = newVal
    const newPos = start + text.length
    nativeInput.setSelectionRange(newPos, newPos)
  }
}

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
// Pending mousedown state: distinguishes click vs drag for range selection
const pendingStart = ref(null)  // { row, col } | null
const pendingMoved = ref(false)

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
  pendingStart.value = null
  pendingMoved.value = false
}

function onCellMouseDown(rowIdx, colName, e) {
  if (editingCell.value) return
  // Only respond to primary button
  if (e.button !== 0) return
  const colIdx = colNameToIndex(colName)
  if (colIdx < 0) return

  // Alt+drag: native text selection within a cell
  if (e.altKey) {
    clearRangeSelection()
    return
  }

  // Shift+click: extend range from anchor (no drag needed)
  if (e.shiftKey && selAnchor.value.row >= 0) {
    selEnd.value = { row: rowIdx, col: colIdx }
    return
  }

  // Plain mousedown: record pending start; do NOT preventDefault so native
  // text selection can begin. Range selection activates on first mousemove.
  pendingStart.value = { row: rowIdx, col: colIdx }
  pendingMoved.value = false
  selAnchor.value = { row: rowIdx, col: colIdx }
}

function onCellMouseMove(rowIdx, colName) {
  if (!pendingStart.value) return
  const colIdx = colNameToIndex(colName)
  if (colIdx < 0) return
  // Only activate range selection when moving to a DIFFERENT cell.
  // Same-cell movement = native text selection (don't clear it).
  if (rowIdx === pendingStart.value.row && colIdx === pendingStart.value.col) {
    return
  }
  // First move to another cell: activate range selection and clear native text selection
  if (!pendingMoved.value) {
    pendingMoved.value = true
    selStart.value = { ...pendingStart.value }
    selEnd.value = { row: rowIdx, col: colIdx }
    // Clear native text selection so it doesn't conflict with range highlight
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
    return
  }
  selEnd.value = { row: rowIdx, col: colIdx }
}

function onCellMouseEnter(rowIdx, colName) {
  // During drag-range selection, update end on enter
  if (pendingStart.value && pendingMoved.value) {
    const colIdx = colNameToIndex(colName)
    if (colIdx < 0) return
    selEnd.value = { row: rowIdx, col: colIdx }
  }
}

function onTableMouseUp() {
  // If user clicked without moving, treat as single-cell selection for Ctrl+C
  if (pendingStart.value && !pendingMoved.value) {
    selStart.value = { ...pendingStart.value }
    selEnd.value = { ...pendingStart.value }
  }
  pendingStart.value = null
  pendingMoved.value = false
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
      return val != null ? String(val) : '\\N'
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
  if (!text) return

  const lines = text.split(/\r?\n/)
  if (lines.length > 0 && lines[lines.length - 1] === '') {
    lines.pop()
  }
  const rows_data = lines.map(l => l.split('\t'))
  if (rows_data.length === 0) return

  const pasteRows = rows_data.length
  const pasteCols = Math.max(...rows_data.map(r => r.length))
  const availableCols = dataColumns.value.length - bounds.colMin

  const neededRows = bounds.rowMin + pasteRows
  while (rows.value.length < neededRows) {
    addBlankRowSilent()
  }

  for (let r = 0; r < pasteRows; r++) {
    const targetRow = rows.value[bounds.rowMin + r]
    if (!targetRow) continue
    const key = getRowKey(targetRow)
    if (!changedRows.value[key]) changedRows.value[key] = {}

    for (let c = 0; c < Math.min(pasteCols, availableCols); c++) {
      const colName = dataColumns.value[bounds.colMin + c]?.name
      if (!colName) continue
      // \N from clipboard represents NULL
      const rawVal = rows_data[r][c] !== undefined ? rows_data[r][c] : ''
      const val = rawVal === '\\N' ? null : rawVal

      if (isNewRow(targetRow) && pkColumns.value.includes(colName)) {
        targetRow[colName] = val
      }
      // Type-aware comparison: null !== ''
      const currentVal = targetRow[colName]
      if (currentVal !== val && !(currentVal === null && val === '') && !(currentVal === '' && val === null)) {
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
const importDialogRef = useTemplateRef('importDialogRef')
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
  document.removeEventListener('mousedown', onFilterPopoverMouseDown)
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

function isColumnFiltered(colName) {
  if (!filterExpr.value.trim()) return false
  const escapedColName = colName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const q = effectiveDbType.value === 'mysql' || effectiveDbType.value === 'mariadb' ? '`' : '"'
  const escapedQ = q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const pattern = new RegExp(`${escapedQ}${escapedColName}${escapedQ}|(?<![a-zA-Z0-9_])${escapedColName}(?![a-zA-Z0-9_])`, 'i')
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
  const sql = buildCountSQL(tableName, effectiveDbType.value, filterExpr.value.trim() || undefined)
  const params = new URLSearchParams()
  params.append('connId', connId)
  params.append('schema', schema)
  params.append('tableName', tableName)
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
  let orderBy = ''
  if (sortColumn.value && sortOrder.value) {
    const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
    orderBy = quoteId(sortColumn.value, effectiveDbType.value) + ' ' + dir
  }
  const sql = buildSelectSQL(tableName, effectiveDbType.value, {
    where: filterExpr.value.trim() || undefined,
    orderBy: orderBy || undefined,
    limit: pageSize.value,
    offset: offset,
  })
  const params = new URLSearchParams()
  params.append('connId', connId)
  params.append('schema', schema)
  params.append('tableName', tableName)
  params.append('sql', sql)
  const resp = await http.post('/execSQL', params)
  const data = resp.data.data

  if (data && data.columns) {
    dataColumns.value = data.columns
      .filter(col => col.name !== 'RN')
      .map((col) => ({
        name: col.name,
        comment: col.comment || '',
        type: col.type || '',
      }))
    if (data.keys && data.keys.length > 0) {
      pkColumns.value = data.keys
    } else {
      const colNames = dataColumns.value.map(c => c.name)
      const idCol = colNames.find(n => n.toLowerCase() === 'id')
      pkColumns.value = idCol ? [idCol] : colNames.slice(0, 1)
    }
  }

  const rawRows = data?.data ?? []
  rows.value = rawRows.map(row => {
    const filtered = { ...row }
    delete filtered.RN
    return filtered
  })
  changedRows.value = {}
  newRowUids.value = new Set()
  rows.value.forEach(row => {
    originalRows.value[getRowKey(row)] = { ...row }
  })
}

async function loadData() {
  if (!connId || !schema || !tableName) return
  await resolveDbType()
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
    if (_editInputSetupDone) return
    _editInputSetupDone = true
    el.focus?.()
    el.select?.()
    // Attach capture-phase paste handler directly on the native input
    // This fires BEFORE el-input's internal handlers and prevents default replace behavior
    const nativeInput = el.$el?.querySelector('input')
    if (nativeInput) {
      _inputPasteHandler = _createInputPasteHandler(nativeInput)
      nativeInput.addEventListener('paste', _inputPasteHandler, true)
    }
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
  const changed = changedRows.value[key]
  const currentVal = changed && changed[colName] !== undefined ? changed[colName] : row[colName]
  editingOriginalValue.value = currentVal
  editingValue.value = currentVal ?? ''
  _editInputSetupDone = false
  editingCell.value = { rowKey: key, colName }
}

function commitInlineEdit() {
  if (!editingCell.value) return
  // Clean up native input paste handler
  if (_inputPasteHandler && editInputRef) {
    const nativeInput = editInputRef.$el?.querySelector('input')
    if (nativeInput) nativeInput.removeEventListener('paste', _inputPasteHandler, true)
    _inputPasteHandler = null
  }
  const { rowKey, colName } = editingCell.value
  const newVal = editingValue.value
  const origVal = editingOriginalValue.value

  // Strict comparison: null !== '' so NULL->'' is a real change
  // Only treat as "no change" when values are strictly equal
  if (newVal === origVal || (newVal === '' && origVal === null) || (newVal === null && origVal === '')) {
    // No actual change (null and '' both represent "empty" in inline edit context)
    if (changedRows.value[rowKey]) {
      delete changedRows.value[rowKey][colName]
      if (Object.keys(changedRows.value[rowKey]).length === 0) {
        delete changedRows.value[rowKey]
      }
    }
  } else {
    // Value changed
    if (!changedRows.value[rowKey]) {
      changedRows.value[rowKey] = {}
    }
    // If original was null and user typed something, store the new value
    // If original was null and user cleared to '', preserve null (no change)
    if (origVal === null && newVal === '') {
      // User cleared a NULL field - treat as no change
      delete changedRows.value[rowKey][colName]
      if (Object.keys(changedRows.value[rowKey]).length === 0) {
        delete changedRows.value[rowKey]
      }
    } else {
      changedRows.value[rowKey][colName] = newVal
    }
  }

  editingCell.value = null
  editingValue.value = ''
  editingOriginalValue.value = null
}

function cancelInlineEdit() {
  // Clean up native input paste handler
  if (_inputPasteHandler && editInputRef) {
    const nativeInput = editInputRef.$el?.querySelector('input')
    if (nativeInput) nativeInput.removeEventListener('paste', _inputPasteHandler, true)
    _inputPasteHandler = null
  }
  editingCell.value = null
  editingValue.value = ''
}

function applyPasteGrid(grid, startRowIdx, startColIdx) {
  if (startRowIdx < 0 || startColIdx < 0) return

  const snapshot = {
    changedRows: JSON.parse(JSON.stringify(changedRows.value)),
    restoredCells: []
  }

  for (let ri = 0; ri < grid.length; ri++) {
    let targetRowIdx = startRowIdx + ri
    if (targetRowIdx >= rows.value.length) {
      const blank = { _rowUid: nextRowUid++, _autoExpanded: true }
      dataColumns.value.forEach(col => { blank[col.name] = '' })
      rows.value.push(blank)
      const key = getRowKey(blank)
      if (!originalRows.value[key]) {
        originalRows.value[key] = {}
      }
    }
    const targetRow = rows.value[targetRowIdx]
    const rowKey = getRowKey(targetRow)

    for (let ci = 0; ci < grid[ri].length; ci++) {
      const targetColIdx = startColIdx + ci
      if (targetColIdx >= dataColumns.value.length) break
      const colName = dataColumns.value[targetColIdx].name
      if (!isNewRow(targetRow) && pkColumns.value.includes(colName)) continue

      const newVal = grid[ri][ci]
      const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined

      const oldChanged = changedRows.value[rowKey]?.[colName]
      snapshot.restoredCells.push({
        rowKey,
        colName,
        oldVal: oldChanged !== undefined ? oldChanged : origVal
      })

      // Type-aware comparison: null !== '', treat null and '' as equivalent only for "no change"
      const newIsNull = newVal === null || newVal === undefined
      const origIsNull = origVal === null || origVal === undefined
      const isChanged = !(newVal === origVal || (newIsNull && origIsNull) || (newIsNull && origVal === '') || (newVal === '' && origIsNull))

      if (isChanged) {
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

function parsePasteGrid(text) {
  const lines = text.split(/\r?\n/)
  if (lines.length > 0 && lines[lines.length - 1] === '') {
    lines.pop()
  }
  return lines.map(line => line.split('\t'))
}

function handlePaste(event) {
  event.preventDefault()

  const text = event.clipboardData?.getData('text/plain')
  if (!text) return

  let startRowIdx = -1
  let startColIdx = -1
  let targetRowCount = -1
  let targetColCount = -1

  const bounds = selectionBounds.value
  if (bounds) {
    startRowIdx = bounds.rowMin
    startColIdx = bounds.colMin
    targetRowCount = bounds.rowMax - bounds.rowMin + 1
    targetColCount = bounds.colMax - bounds.colMin + 1
  } else if (activeCellIndex.value >= 0 && activeColName.value) {
    startRowIdx = activeCellIndex.value
    startColIdx = dataColumns.value.findIndex(c => c.name === activeColName.value)
  }

  const grid = parsePasteGrid(text)
  if (grid.length === 0) return

  // 当有范围选区（大于1x1）且粘贴板内容与选区不完全匹配时，以粘贴板内容重复贴入
  if (targetRowCount > 1 || targetColCount > 1) {
    const pasteRows = grid.length
    const pasteCols = Math.max(...grid.map(r => r.length))

    if (pasteRows !== targetRowCount || pasteCols !== targetColCount) {
      const tiledGrid = []
      for (let r = 0; r < targetRowCount; r++) {
        const row = []
        const srcRow = grid[r % pasteRows]
        for (let c = 0; c < targetColCount; c++) {
          row.push(srcRow[c % pasteCols] ?? '')
        }
        tiledGrid.push(row)
      }
      applyPasteGrid(tiledGrid, startRowIdx, startColIdx)
      return
    }
  }

  applyPasteGrid(grid, startRowIdx, startColIdx)
}

async function handleEditPaste() {
  const cell = editingCell.value
  if (!cell) return

  const startRowIdx = rows.value.findIndex(r => getRowKey(r) === cell.rowKey)
  const startColIdx = dataColumns.value.findIndex(c => c.name === cell.colName)

  let text = ''
  try {
    text = await navigator.clipboard.readText()
  } catch { return }
  if (!text) return

  const grid = parsePasteGrid(text)
  if (grid.length === 0) return

  cancelInlineEdit()
  applyPasteGrid(grid, startRowIdx, startColIdx)
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
  if (editingCell.value) return
  const bounds = selectionBounds.value
  if (!bounds) return

  if (event.ctrlKey && event.key === 'c') {
    // If user has native text selected, let the browser copy it
    const sel = window.getSelection()
    if (sel && sel.toString().length > 0) return
    event.preventDefault()
    copySelectedRange()
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

  try {
    const sqlStatements = []

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
          return val !== null && val !== undefined
        })
      if (insertCols.length === 0) continue

      const colList = insertCols.map(c => quoteId(c.name, effectiveDbType.value)).join(', ')
      const valList = insertCols.map(c => fmtVal(merged[c.name], effectiveDbType.value)).join(', ')

      sqlStatements.push('INSERT INTO ' + quoteId(tableName, effectiveDbType.value) + ' (' + colList + ') VALUES (' + valList + ')')
    }

    for (const rowKey of existingKeys) {
      const changed = changedRows.value[rowKey]
      const orig = originalRows.value[rowKey]
      if (!orig) continue

      const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(orig).slice(0, 1)
      const setClauses = Object.keys(changed)
        .map(k => quoteId(k, effectiveDbType.value) + ' = ' + fmtVal(changed[k], effectiveDbType.value))
        .join(', ')

      const allWhereCols = [
        ...pkCols,
        ...Object.keys(changed).filter(k => !pkCols.includes(k))
      ]
      const whereClauses = allWhereCols
        .map(k => buildWhereCondition(k, orig[k], effectiveDbType.value))
        .join(' AND ')

      sqlStatements.push('UPDATE ' + quoteId(tableName, effectiveDbType.value) + ' SET ' + setClauses + ' WHERE ' + whereClauses)
    }

    if (sqlStatements.length === 0) {
      ElMessage({ message: '没有需要保存的更改', type: 'warning' })
      return
    }

    const batchSql = sqlStatements.join('; ')
    const params = new URLSearchParams()
    params.append('connId', connId)
    params.append('schema', schema)
    params.append('sql', batchSql)

    const resp = await http.post('/execSQL', params)
    const respData = resp.data.data

    if (respData && respData.msg) {
      ElMessage({ message: '保存失败，请检查数据', type: 'error' })
    } else {
      ElMessage({ message: `成功保存 ${sqlStatements.length} 条记录`, type: 'success' })
    }
    await loadData()
  } catch (err) {
    console.error('[DataBrowser] 保存失败:', err)
    ElMessage({ message: '保存失败', type: 'error' })
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
      copied[col.name] = row[col.name] !== undefined ? row[col.name] : ''
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

function getRowValue(row, colName) {
  const key = getRowKey(row)
  const changed = changedRows.value[key]
  if (changed && changed[colName] !== undefined) {
    return changed[colName]
  }
  return row[colName]
}

function formatCellTitle(val) {
  if (val === null || val === undefined) return 'NULL'
  return String(val)
}

function openColumnFilter(col, event) {
  if (currentColumn.value?.name === col.name && columnFilterDialogVisible.value) {
    columnFilterDialogVisible.value = false
    return
  }
  currentColumn.value = col
  filterTriggerRef.value = event.currentTarget
  
  const savedCondition = columnFilterConditions.value[col.name]
  if (savedCondition) {
    columnFilterOperator.value = savedCondition.operator
    columnFilterValue.value = savedCondition.value
  } else {
    columnFilterOperator.value = '='
    columnFilterValue.value = ''
  }
  
  columnFilterDialogVisible.value = true
}

function onFilterPopoverMouseDown(e) {
  if (!columnFilterDialogVisible.value) return
  if (e.target.closest('.el-popper')) return
  if (filterTriggerRef.value && (filterTriggerRef.value === e.target || filterTriggerRef.value.contains(e.target))) return
  columnFilterDialogVisible.value = false
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
  
  const colName = quoteId(currentColumn.value.name, effectiveDbType.value)
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
    const formatted = values.map(v => fmtVal(v, effectiveDbType.value)).join(', ')
    return `${colName} ${op} (${formatted})`
  }
  
  if (op === 'LIKE' || op === 'NOT LIKE') {
    return `${colName} ${op} ${fmtVal(val, effectiveDbType.value)}`
  }
  
  return `${colName} ${op} ${fmtVal(val, effectiveDbType.value)}`
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
  const quotedColName = quoteId(colName, effectiveDbType.value)
  const conditions = filterExpr.value.split(/\s+AND\s+/i).filter(c => {
    const trimmed = c.trim()
    return !trimmed.startsWith(quotedColName) && 
           !trimmed.startsWith(colName) &&
           !trimmed.includes(quotedColName) &&
           !trimmed.includes(colName)
  })
  
  filterExpr.value = conditions.join(' AND ')
  
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
      exportToCsv(cols, rows, tableName)
    } else if (format === 'json') {
      exportToJson(rows, tableName)
    } else if (format === 'sql') {
      const sqlText = exportToSql(cols, rows, tableName, effectiveDbType.value)
      downloadBlob(sqlText, tableName + '.sql', 'text/plain')
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
  let orderBy = ''
  if (sortColumn.value && sortOrder.value) {
    const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
    orderBy = quoteId(sortColumn.value, effectiveDbType.value) + ' ' + dir
  }
  const sql = buildSelectSQL(tableName, effectiveDbType.value, {
    where: filterExpr.value.trim() || undefined,
    orderBy: orderBy || undefined,
  })
  const params = new URLSearchParams()
  params.append('connId', connId)
  params.append('schema', schema)
  params.append('tableName', tableName)
  params.append('sql', sql)
  params.append('maxLine', '100000')
  return await http.post('/execSQL', params)
}

async function exportToExcel() {
  let orderBy = ''
  if (sortColumn.value && sortOrder.value) {
    const dir = sortOrder.value === 'descending' ? 'DESC' : 'ASC'
    orderBy = quoteId(sortColumn.value, effectiveDbType.value) + ' ' + dir
  }
  const sql = buildSelectSQL(tableName, effectiveDbType.value, {
    where: filterExpr.value.trim() || undefined,
    orderBy: orderBy || undefined,
  })
  const params = new URLSearchParams()
  params.append('connId', connId)
  params.append('schema', schema)
  params.append('filename', tableName)
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
      a.download = tableName + '.xlsx'
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

  const setClauses = changedCols.map(key => quoteId(key, effectiveDbType.value) + ' = ' + fmtVal(current[key], effectiveDbType.value)).join(', ')

  const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(origin).slice(0, 1)
  const allWhereCols = [
    ...pkCols,
    ...changedCols.filter(k => !pkCols.includes(k))
  ]
  const whereClauses = allWhereCols.map(key => buildWhereCondition(key, origin[key], effectiveDbType.value)).join(' AND ')

  const sql = 'UPDATE ' + quoteId(tableName, effectiveDbType.value) + ' SET ' + setClauses + ' WHERE ' + whereClauses

  saving.value = true
  try {
    const params = new URLSearchParams()
    params.append('connId', connId)
    params.append('schema', schema)
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
  // Only exclude null/undefined, keep empty string as valid value
  const cols = Object.keys(row).filter(k => row[k] !== null && row[k] !== undefined)

  if (cols.length === 0) {
    ElMessage({ message: '请至少填写一个字段', type: 'warning' })
    return
  }

  const colList = cols.map(k => quoteId(k, effectiveDbType.value)).join(', ')
  const valList = cols.map(k => fmtVal(row[k], effectiveDbType.value)).join(', ')
  const sql = 'INSERT INTO ' + quoteId(tableName, effectiveDbType.value) + ' (' + colList + ') VALUES (' + valList + ')'

  inserting.value = true
  try {
    const params = new URLSearchParams()
    params.append('connId', connId)
    params.append('schema', schema)
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
  const whereClauses = pkCols.map(key => buildWhereCondition(key, row[key], effectiveDbType.value)).join(' AND ')
  const sql = 'DELETE FROM ' + quoteId(tableName, effectiveDbType.value) + ' WHERE ' + whereClauses

  try {
    const params = new URLSearchParams()
    params.append('connId', connId)
    params.append('schema', schema)
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
  param.append('connId', connId)
  param.append('schema', schema)
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
  const wasQuoted = []
  let current = ''
  let inQuotes = false
  let fieldQuoted = false
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
        fieldQuoted = true
      } else if (ch === ',') {
        wasQuoted.push(fieldQuoted)
        result.push(current)
        current = ''
        fieldQuoted = false
      } else {
        current += ch
      }
    }
  }
  wasQuoted.push(fieldQuoted)
  result.push(current)
  // Convert unquoted \N to null (MySQL convention), quoted \N stays as literal string
  return result.map((val, idx) => !wasQuoted[idx] && val === '\\N' ? null : val)
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
      const dataRows = json.map(obj => headers.map(h => obj[h] ?? null))
      
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
        const cols = Object.keys(row).filter(k => row[k] !== undefined)
        if (cols.length === 0) continue

        if (mode === 'insert') {
          const nonNullCols = cols.filter(k => row[k] !== null)
          if (nonNullCols.length === 0) continue
          const colList = nonNullCols.map(k => quoteId(k, effectiveDbType.value)).join(', ')
          const valList = nonNullCols.map(k => fmtVal(row[k], effectiveDbType.value)).join(', ')
          sqlStatements.push('INSERT INTO ' + quoteId(tableName, effectiveDbType.value) + ' (' + colList + ') VALUES (' + valList + ')')
        } else {
          const setClauses = cols.map(k => quoteId(k, effectiveDbType.value) + ' = ' + fmtVal(row[k], effectiveDbType.value)).join(', ')
          const pkCols = pkColumns.value.length > 0 ? pkColumns.value : cols.slice(0, 1)
          const whereClauses = pkCols.map(k => buildWhereCondition(k, row[k], effectiveDbType.value)).join(' AND ')
          if (!whereClauses) continue
          sqlStatements.push('UPDATE ' + quoteId(tableName, effectiveDbType.value) + ' SET ' + setClauses + ' WHERE ' + whereClauses)
        }
      }

      const batchSql = sqlStatements.join('; ')
      if (!batchSql) continue

      const params = new URLSearchParams()
      params.append('connId', connId)
      params.append('schema', schema)
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
  document.addEventListener('mousedown', onFilterPopoverMouseDown)
})

function openTableStructure() {
  emit('viewTableInfo', {
    connId: connId,
    schema: schema,
    tableName: tableName,
  })
}

watch(
  () => [connId, schema, tableName],
  () => {
    currentPage.value = 1
    loadData()
  }
)
</script>

<style>
.cell-range-selected {
  background-color: #68a6eb !important;
  outline: none !important;
  outline-offset: -1px;
}
</style>

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
  user-select: text;
}

.inline-cell > span {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: block;
  width: 100%;
  user-select: text;
}

.null-placeholder {
  color: var(--text-tertiary, #bbb);
  font-style: italic;
  font-size: 0.85em;
  user-select: none;
}

.empty-placeholder {
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

:deep(.row-changed td) {
  border-bottom: 2px solid #faad14;
}
</style>
