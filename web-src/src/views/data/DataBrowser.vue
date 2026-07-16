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
        <el-button size="small" @click="dataQuery.loadData" :loading="dataQuery.loading.value" :icon="Refresh">刷新</el-button>
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
        <DataExportDialog :exporting="dataExport.exporting.value" @export="onExport" />
      </div>
      <div class="toolbar-right" v-if="dataQuery.filterExpr.value">
        <span class="toolbar-filter-hint">
          <el-icon :size="12"><Filter /></el-icon>
          数据已过滤
        </span>
      </div>
    </div>

    <!-- Filter bar + column filter popover -->
    <DataFilterPanel
      ref="filterPanelRef"
      :data-columns="dataQuery.dataColumns.value"
      :effective-db-type="dataQuery.effectiveDbType.value"
      :filter-expr="dataQuery.filterExpr.value"
      @filter-applied="onFilterApplied"
      @filter-cleared="onFilterCleared"
      @all-filters-cleared="dataQuery.clearAllFilters"
    />

    <!-- Data table + inline edit status bar + context menu -->
    <DataTableView
      :rows="dataQuery.rows.value"
      :data-columns="dataQuery.dataColumns.value"
      :pk-columns="dataQuery.pkColumns.value"
      :effective-db-type="dataQuery.effectiveDbType.value"
      :conn-id="connId"
      :schema="schema"
      :table-name="tableName"
      :row-index-offset="dataQuery.rowIndexOffset.value"
      :sort-column="dataQuery.sortColumn.value"
      :sort-order="dataQuery.sortOrder.value"
      :is-date-column="dataQuery.isDateColumn"
      :get-row-key="dataQuery.getRowKey"
      :is-column-filtered="filterPanelRef?.isColumnFiltered || (() => false)"
      :handle-sort="dataQuery.handleSort"
      :load-data="dataQuery.loadData"
      @open-column-filter="onOpenColumnFilter"
      @edit-row="openEditDialog"
    />

    <!-- Pagination -->
    <DataPagination
      :current-page="dataQuery.currentPage.value"
      :page-size="dataQuery.pageSize.value"
      :total="dataQuery.total.value"
      @update:current-page="dataQuery.currentPage.value = $event"
      @update:page-size="dataQuery.pageSize.value = $event"
      @page-change="dataQuery.onPageChange"
      @size-change="dataQuery.onSizeChange"
    />
  </div>

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
          v-for="col in dataQuery.dataColumns.value"
          :key="col.name"
          :label="col.name"
          :title="col.comment"
        >
          <div style="display: flex; align-items: flex-start; gap: 6px; width: 100%;">
            <el-date-picker
              v-if="dataQuery.isDateColumn(col.name)"
              v-model="editRowData[col.name]"
              type="datetime"
              value-format="YYYY-MM-DDTHH:mm:ss"
              :placeholder="editRowData[col.name] === null ? 'NULL' : ''"
              style="flex: 1;"
            />
            <el-input
              v-else
              v-model="editRowData[col.name]"
              type="textarea"
              autosize
              :placeholder="editRowData[col.name] === null ? 'NULL' : ''"
              style="flex: 1;"
            />
            <el-button
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
          v-for="col in dataQuery.dataColumns.value"
          :key="col.name"
          :label="col.name"
          :title="col.comment"
        >
          <el-date-picker
            v-if="dataQuery.isDateColumn(col.name)"
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
    :on-import-success="dataQuery.loadData"
    ref="importDialogRef"
    @confirm-import-data="handleCsvJsonImport"
  />
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { ArrowDown, Filter, Grid, InfoFilled, Refresh, Timer } from '@element-plus/icons-vue'
import { ElLoading, ElMessage } from 'element-plus'
import * as XLSX from 'xlsx'
import ImportPreviewDialog from '@/components/data/ImportPreviewDialog.vue'
import http from '@/api/index'
import { buildWhereCondition, fmtVal, quoteId } from '@/utils/sqlHelper.ts'
import { useDataQuery } from './composables/useDataQuery'
import { useDataExport } from './composables/useDataExport'
import type { ExportFormat } from './composables/useDataExport'
import type { DataColumn } from './composables/useDataQuery'
import DataTableView from './components/DataTableView.vue'
import DataFilterPanel from './components/DataFilterPanel.vue'
import DataPagination from './components/DataPagination.vue'
import DataExportDialog from './components/DataExportDialog.vue'

const { connId, schema, tableName, tabId, dbType, schemaPath } = defineProps<{
  connId: string
  schema: string
  tableName: string
  tabId?: string
  dbType?: string
  schemaPath?: string
}>()

const emit = defineEmits(['viewTableInfo', 'openDataBrowser', 'openTableManager'])

// ===== Data query + export composables =====
const dataQuery = useDataQuery({
  connId: () => connId,
  schema: () => schema,
  tableName: () => tableName,
  dbType: () => dbType,
  schemaPath: () => schemaPath,
})

const dataExport = useDataExport({
  connId: () => connId,
  schema: () => schema,
  tableName: () => tableName,
  effectiveDbType: () => dataQuery.effectiveDbType.value,
  filterExpr: () => dataQuery.filterExpr.value,
  sortColumn: () => dataQuery.sortColumn.value,
  sortOrder: () => dataQuery.sortOrder.value,
  dataColumns: () => dataQuery.dataColumns.value,
  fetchFullData: dataQuery.fetchFullData,
})

function onExport(format: ExportFormat) {
  dataExport.handleExportCommand(format, dataQuery.loading)
}

// ===== Filter panel wiring =====
const filterPanelRef = useTemplateRef<InstanceType<typeof DataFilterPanel>>('filterPanelRef')

function onOpenColumnFilter(col: DataColumn, triggerEl: HTMLElement) {
  filterPanelRef.value?.openColumnFilter(col, triggerEl)
}

function onFilterApplied(payload: { colName: string; condition: string }) {
  // First remove any existing condition for the same column (to support "modify filter")
  if (payload.colName && dataQuery.filterExpr.value.trim()) {
    const effectiveDbType = dataQuery.effectiveDbType.value
    const quotedColName = quoteId(payload.colName, effectiveDbType)
    const conditions = dataQuery.filterExpr.value
      .split(/\s+AND\s+/i)
      .filter((c) => {
        const trimmed = c.trim()
        return (
          !trimmed.startsWith(quotedColName) &&
          !trimmed.startsWith(payload.colName) &&
          !trimmed.includes(quotedColName) &&
          !trimmed.includes(payload.colName)
        )
      })
    dataQuery.filterExpr.value = conditions.join(' AND ')
  }

  // Append the new condition to the existing WHERE clause
  if (dataQuery.filterExpr.value.trim()) {
    dataQuery.filterExpr.value = dataQuery.filterExpr.value.trim() + ' AND ' + payload.condition
  } else {
    dataQuery.filterExpr.value = payload.condition
  }
  dataQuery.currentPage.value = 1
  dataQuery.loadData()
}

function onFilterCleared(colName: string) {
  // Remove conditions referencing this column from filterExpr
  const effectiveDbType = dataQuery.effectiveDbType.value
  const quotedColName = quoteId(colName, effectiveDbType)
  const conditions = dataQuery.filterExpr.value
    .split(/\s+AND\s+/i)
    .filter((c) => {
      const trimmed = c.trim()
      return (
        !trimmed.startsWith(quotedColName) &&
        !trimmed.startsWith(colName) &&
        !trimmed.includes(quotedColName) &&
        !trimmed.includes(colName)
      )
    })
  dataQuery.filterExpr.value = conditions.join(' AND ')
  dataQuery.currentPage.value = 1
  dataQuery.loadData()
}

// ===== Auto-refresh =====
const autoRefreshInterval = ref(0)
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

function handleAutoRefresh(seconds: string) {
  const sec = parseInt(seconds)
  autoRefreshInterval.value = sec
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
  if (sec > 0) {
    autoRefreshTimer = setInterval(() => {
      if (!dataQuery.loading.value) {
        dataQuery.loadData()
      }
    }, sec * 1000)
  }
}

onBeforeUnmount(() => {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer)
})

// ===== Edit dialog =====
const editDialogVisible = ref(false)
const editRowData = ref<Record<string, any>>({})
const originRowData = ref<Record<string, any>>({})
const saving = ref(false)

function openEditDialog(row: Record<string, any>) {
  editRowData.value = { ...row }
  originRowData.value = { ...row }
  editDialogVisible.value = true
}

async function saveData() {
  const origin = originRowData.value
  const current = editRowData.value
  const changedCols = Object.keys(origin).filter((key) => origin[key] !== current[key])
  if (changedCols.length === 0) {
    ElMessage({ message: '数据未修改', type: 'warning' })
    return
  }
  const dbType = dataQuery.effectiveDbType.value
  const setClauses = changedCols
    .map((key) => quoteId(key, dbType) + ' = ' + fmtVal(current[key], dbType))
    .join(', ')
  const pkCols = dataQuery.pkColumns.value.length > 0 ? dataQuery.pkColumns.value : Object.keys(origin).slice(0, 1)
  const allWhereCols = [...pkCols, ...changedCols.filter((k) => !pkCols.includes(k))]
  const whereClauses = allWhereCols.map((key) => buildWhereCondition(key, origin[key], dbType)).join(' AND ')
  const sql = 'UPDATE ' + quoteId(tableName, dbType) + ' SET ' + setClauses + ' WHERE ' + whereClauses

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
      await dataQuery.loadData()
    }
  } catch (err) {
    console.error('[DataBrowser] 保存失败:', err)
    ElMessage({ message: '保存失败', type: 'error' })
  } finally {
    saving.value = false
  }
}

// ===== Insert dialog =====
const insertDialogVisible = ref(false)
const insertRowData = ref<Record<string, any>>({})
const inserting = ref(false)

async function insertData() {
  const row = insertRowData.value
  const dbType = dataQuery.effectiveDbType.value
  const cols = Object.keys(row).filter((k) => row[k] !== null && row[k] !== undefined)
  if (cols.length === 0) {
    ElMessage({ message: '请至少填写一个字段', type: 'warning' })
    return
  }
  const colList = cols.map((k) => quoteId(k, dbType)).join(', ')
  const valList = cols.map((k) => fmtVal(row[k], dbType)).join(', ')
  const sql = 'INSERT INTO ' + quoteId(tableName, dbType) + ' (' + colList + ') VALUES (' + valList + ')'

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
      await dataQuery.loadData()
    }
  } catch (err) {
    console.error('[DataBrowser] 新增失败:', err)
    ElMessage({ message: '新增失败', type: 'error' })
  } finally {
    inserting.value = false
  }
}

// ===== Import =====
const fileList = ref<any[]>([])
const importPreviewVisible = ref(false)
const dbColumns = ref<string[]>([])
const importDialogRef = useTemplateRef<InstanceType<typeof ImportPreviewDialog>>('importDialogRef')
const importMode = ref('insert')
const importFormat = ref('xlsx')

const importAccept = computed(() => {
  switch (importFormat.value) {
    case 'csv': return '.csv'
    case 'json': return '.json'
    default: return '.xlsx,.xls'
  }
})

function handleImportCommand(command: string) {
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
  const fileInput = document.querySelector('.data-browser input[type="file"]') as HTMLInputElement | null
  if (fileInput) fileInput.click()
}

async function handleFileSelect(options: any) {
  const file = options.file
  if (importFormat.value === 'csv') {
    handleCsvFile(file)
  } else if (importFormat.value === 'json') {
    handleJsonFile(file)
  } else {
    handleExcelFile(file)
  }
}

function normalizeHeaders(headers: any[]): string[] {
  let unnamedIdx = 0
  return (headers || []).map((h) => {
    if (h == null || String(h).trim() === '') {
      unnamedIdx++
      return `未命名_${unnamedIdx}`
    }
    return String(h)
  })
}

function setImportDialogData(file: File, headers: string[], dataRows: any[][]) {
  if (dataQuery.dataColumns.value && dataQuery.dataColumns.value.length > 0) {
    dbColumns.value = dataQuery.dataColumns.value.map((col) => col.name)
  }
  importDialogRef.value?.setFileData(file, headers, dataRows)
  importDialogRef.value?.initMapping()
  importDialogRef.value?.previewData()
  if (importDialogRef.value?.setImportMode) {
    importDialogRef.value.setImportMode(importMode.value)
  }
  importPreviewVisible.value = true
}

function handleExcelFile(file: File) {
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const data = new Uint8Array(e.target!.result as ArrayBuffer)
      const workbook = XLSX.read(data, { type: 'array', raw: false, dateNF: 'yyyy-mm-dd HH:mm:ss' })
      const firstSheetName = workbook.SheetNames[0]
      const worksheet = workbook.Sheets[firstSheetName]
      const jsonData = XLSX.utils.sheet_to_json(worksheet, { header: 1, defval: null })
      if (jsonData.length === 0) {
        ElMessage({ message: 'Excel 文件为空', type: 'warning' })
        return
      }
      const headers = normalizeHeaders(jsonData[0] as any[] || [])
      const dataRows = jsonData.slice(1) as any[][]
      setImportDialogData(file, headers, dataRows)
    } catch (err) {
      console.error('[DataBrowser] 读取 Excel 文件失败:', err)
      ElMessage({ message: '读取 Excel 文件失败，请检查文件格式', type: 'error' })
    }
  }
  reader.readAsArrayBuffer(file)
}

function handleCsvFile(file: File) {
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const text = e.target!.result as string
      const lines = text.split(/\r?\n/).filter((line) => line.trim())
      if (lines.length === 0) {
        ElMessage({ message: 'CSV 文件为空', type: 'warning' })
        return
      }
      const headers = normalizeHeaders(parseCsvLine(lines[0]))
      const dataRows = lines.slice(1).map(parseCsvLine)
      setImportDialogData(file, headers, dataRows)
    } catch (err) {
      console.error('[DataBrowser] 读取 CSV 文件失败:', err)
      ElMessage({ message: '读取 CSV 文件失败，请检查文件格式', type: 'error' })
    }
  }
  reader.readAsText(file)
}

function parseCsvLine(line: string): string[] {
  const result: string[] = []
  const wasQuoted: boolean[] = []
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
  // Convert unquoted \N to null (MySQL convention)
  return result.map((val, idx) => (!wasQuoted[idx] && val === '\\N' ? null as any : val))
}

function handleJsonFile(file: File) {
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const json = JSON.parse(e.target!.result as string)
      if (!Array.isArray(json) || json.length === 0) {
        ElMessage({ message: 'JSON 文件应为非空数组', type: 'warning' })
        return
      }
      const rawHeaders = Object.keys(json[0])
      const headers = normalizeHeaders(rawHeaders)
      const dataRows = json.map((obj: Record<string, any>) => rawHeaders.map((h) => obj[h] ?? null))
      setImportDialogData(file, headers, dataRows)
    } catch (err) {
      console.error('[DataBrowser] 读取 JSON 文件失败:', err)
      ElMessage({ message: '读取 JSON 文件失败，请检查文件格式', type: 'error' })
    }
  }
  reader.readAsText(file)
}

async function handleCsvJsonImport({ data, mode }: { data: Record<string, any>[]; mode: string }) {
  if (!data || data.length === 0) {
    ElMessage({ message: '没有可导入的数据', type: 'warning' })
    return
  }
  const loading = ElLoading.service({
    fullscreen: false,
    text: `正在${mode === 'insert' ? '新增' : '更新'}导入 ${data.length} 条数据...`,
  })
  let successCount = 0
  let errorCount = 0
  const dbType = dataQuery.effectiveDbType.value

  try {
    const batchSize = 50
    for (let i = 0; i < data.length; i += batchSize) {
      const batch = data.slice(i, i + batchSize)
      const sqlStatements: string[] = []
      for (const row of batch) {
        const cols = Object.keys(row).filter((k) => row[k] !== undefined)
        if (cols.length === 0) continue
        if (mode === 'insert') {
          const nonNullCols = cols.filter((k) => row[k] !== null)
          if (nonNullCols.length === 0) continue
          const colList = nonNullCols.map((k) => quoteId(k, dbType)).join(', ')
          const valList = nonNullCols.map((k) => fmtVal(row[k], dbType)).join(', ')
          sqlStatements.push('INSERT INTO ' + quoteId(tableName, dbType) + ' (' + colList + ') VALUES (' + valList + ')')
        } else {
          const setClauses = cols.map((k) => quoteId(k, dbType) + ' = ' + fmtVal(row[k], dbType)).join(', ')
          const pkCols = dataQuery.pkColumns.value.length > 0 ? dataQuery.pkColumns.value : cols.slice(0, 1)
          const whereClauses = pkCols.map((k) => buildWhereCondition(k, row[k], dbType)).join(' AND ')
          if (!whereClauses) continue
          sqlStatements.push('UPDATE ' + quoteId(tableName, dbType) + ' SET ' + setClauses + ' WHERE ' + whereClauses)
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
    await dataQuery.loadData()
  }
}

// ===== Table structure =====
function openTableStructure() {
  emit('viewTableInfo', { connId, schema, tableName })
}

// ===== Lifecycle =====
onMounted(() => {
  dataQuery.loadData()
})
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

.toolbar-filter-hint {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #409eff;
  background: #ecf5ff;
  padding: 2px 10px;
  border-radius: 10px;
  user-select: none;
}

[data-theme="dark"] .toolbar-filter-hint {
  color: #63b3ed;
  background: #1a2c40;
}
</style>
