<template>
  <div class="table-manager classical-panel">
    <!-- Toolbar -->
    <div class="tm-toolbar">
      <el-button size="small" type="primary" @click="onNewTable" :icon="Plus">新建表</el-button>
      <el-button size="small" @click="loadTables" :icon="Refresh">刷新</el-button>
    </div>

    <!-- 新建表 Dialog -->
    <el-dialog v-model="newTableDialogVisible" title="新建表" width="850px" :close-on-click-modal="false">
    <el-form :model="newTableForm" label-width="80px" size="small">
      <el-row :gutter="12">
        <el-col :span="12">
          <el-form-item label="表名" required>
            <el-input v-model="newTableForm.tableName" placeholder="请输入表名" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="表注释">
            <el-input v-model="newTableForm.tableComment" placeholder="可选" />
          </el-form-item>
        </el-col>
      </el-row>

      <!-- Column rows -->
      <div style="margin-bottom: 8px; font-weight: 600; font-size: 13px;">字段列表</div>
      <el-table :data="newTableForm.columns" size="small" border style="width: 100%;">
        <el-table-column label="字段名" min-width="120" resizable>
          <template #default="scope">
            <el-input v-model="scope.row.colName" size="small" placeholder="字段名" />
          </template>
        </el-table-column>
        <el-table-column label="类型" min-width="200" resizable>
          <template #default="scope">
            <div style="display: flex; gap: 4px; align-items: center;">
              <el-select v-model="scope.row.colBaseType" size="small" style="flex: 1; min-width: 100px;" @change="onColTypeChange(scope.row)">
                <el-option v-for="t in colTypeOptions" :key="t.value" :label="t.label" :value="t.value" />
              </el-select>
              <el-input
                v-if="typeNeedsLength(scope.row.colBaseType)"
                v-model="scope.row.colLength"
                size="small"
                style="width: 72px;"
                :placeholder="typeLengthPlaceholder(scope.row.colBaseType)"
              />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="可空" width="60" align="center" resizable>
          <template #default="scope">
            <el-checkbox v-model="scope.row.nullable" />
          </template>
        </el-table-column>
        <el-table-column label="默认值" min-width="100" resizable>
          <template #default="scope">
            <el-input v-model="scope.row.defaultVal" size="small" placeholder="默认值" />
          </template>
        </el-table-column>
        <el-table-column label="注释" min-width="120" resizable>
          <template #default="scope">
            <el-input v-model="scope.row.comment" size="small" placeholder="注释" />
          </template>
        </el-table-column>
        <el-table-column label="" width="50" align="center" resizable>
          <template #default="scope">
            <el-button type="danger" size="small" link @click="removeColumn(scope.$index)">删</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div style="margin-top: 8px;">
        <el-button size="small" @click="addColumn">+ 添加字段</el-button>
      </div>
    </el-form>

    <template #footer>
      <el-button size="small" @click="newTableDialogVisible = false">取消</el-button>
      <el-button size="small" type="primary" :loading="newTableSubmitting" @click="submitNewTable">确定</el-button>
    </template>
  </el-dialog>

  <!-- 重命名表 Dialog -->
  <el-dialog v-model="renameDialogVisible" title="重命名表" width="500px" :close-on-click-modal="false">
    <el-form size="small" label-width="80px">
      <el-form-item label="原表名">
        <el-input :value="renameTarget" disabled />
      </el-form-item>
      <el-form-item label="新表名" required>
        <el-input v-model="renameNewName" placeholder="请输入新表名" @keyup.enter="submitRename" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button size="small" @click="renameDialogVisible = false">取消</el-button>
      <el-button size="small" type="primary" :loading="renameSubmitting" @click="submitRename">确定</el-button>
    </template>
  </el-dialog>

  <!-- Main content: left list + right editor -->
    <div class="tm-body">
      <!-- Left: table list -->
      <div class="tm-sidebar" :style="{ width: sidebarWidth + 'px' }">
        <div class="tm-search">
          <el-input
            v-model="searchKeyword"
            placeholder="搜索表名或注释..."
            clearable
            size="small"
            :prefix-icon="Search"
          />
        </div>
        <div class="tm-table-list">
          <el-table
            :data="filteredTables"
            highlight-current-row
            size="small"
            style="width: 100%;"
            height="100%"
            @row-click="onTableClick"
          >
            <el-table-column prop="name" label="表名" show-overflow-tooltip resizable />
            <el-table-column prop="comment" label="注释" show-overflow-tooltip resizable />
            <el-table-column label="" width="44" align="center" resizable>
              <template #default="scope">
                <el-dropdown trigger="hover" @command="(cmd) => onTableAction(cmd, scope.row)" @click.stop>
                  <el-icon style="cursor: pointer; color: #909399;" :size="16" title="操作"><MoreFilled /></el-icon>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="browse">
                        <el-icon><Document /></el-icon>浏览数据
                      </el-dropdown-item>
                      <el-dropdown-item command="export">
                        <el-icon><Download /></el-icon>数据导出
                      </el-dropdown-item>
                      <el-dropdown-item command="rename">
                        <el-icon><Edit /></el-icon>重命名
                      </el-dropdown-item>
                      <el-dropdown-item command="truncate" divided>
                        <el-icon><Delete /></el-icon>清空表
                      </el-dropdown-item>
                      <el-dropdown-item command="drop" style="color: #f56c6c;">
                        <el-icon><DeleteFilled /></el-icon>删除表
                      </el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>

      <!-- Resizable handle -->
      <div class="tm-resize-handle" @mousedown.prevent="onResizeStart"></div>

      <!-- Right: TableEditor -->
      <div class="tm-editor">
        <div v-if="!selectedTable" class="classical-empty">
          <div class="empty-icon">📋</div>
          <div>点击左侧表名查看表结构</div>
        </div>
        <template v-else>
          <div class="tm-editor-header">
            <span class="tm-table-title" @click="onBrowseCurrentTable">
              {{ selectedTable }}
              <span v-if="selectedTableComment" class="tm-table-comment">{{ selectedTableComment }}</span>
            </span>
          </div>
          <TableEditor
            :tableMeta="tableMeta"
            @tableDrop="onTableDrop"
          />
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import http from '@/utils/httpProxy.js'
import { Delete, DeleteFilled, Document, Download, Edit, MoreFilled, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import TableEditor from '@/components/data/TableEditor.vue'

const { connId, schema, dbType, tabId, schemaPath } = defineProps({
  connId: String,
  schema: String,
  dbType: String,
  tabId: String,
  schemaPath: String,
})

const emit = defineEmits(['openDataBrowser'])

const tableList = ref([])
const searchKeyword = ref('')
const selectedTable = ref(null)

const tableMeta = computed(() => {
  if (!selectedTable.value) return null
  return {
    connId: connId,
    schema: schema,
    tableName: selectedTable.value,
    dbType: dbType || '',
  }
})

const filteredTables = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase()
  if (!kw) return tableList.value
  return tableList.value.filter(
    (t) => t.name.toLowerCase().includes(kw) || (t.comment && t.comment.toLowerCase().includes(kw))
  )
})

const selectedTableComment = computed(() => {
  if (!selectedTable.value) return ''
  const t = tableList.value.find(t => t.name === selectedTable.value)
  return t ? t.comment : ''
})

// Sidebar drag resize
const sidebarWidth = ref(500)
let isResizing = false

function initSidebarWidth() {
  const el = document.querySelector('.tm-sidebar')
  if (el) {
    sidebarWidth.value = el.offsetWidth
  }
}

function onResizeStart(e) {
  isResizing = true
  initSidebarWidth()
  document.addEventListener('mousemove', onResizeMove)
  document.addEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
}

function onResizeMove(e) {
  if (!isResizing) return
  const parent = document.querySelector('.tm-body')
  if (!parent) return
  const parentRect = parent.getBoundingClientRect()
  let newWidth = e.clientX - parentRect.left
  newWidth = Math.max(600, Math.min(800, newWidth))
  sidebarWidth.value = newWidth
}

function onResizeEnd() {
  isResizing = false
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

function loadTables() {
  if (!connId || !schema) return
  http.get('/listTable', { params: { connId, schema } })
    .then((resp) => {
      tableList.value = resp.data || []
    })
    .catch((err) => {
      console.error(err)
    })
}

function onTableClick(row) {
  selectedTable.value = row.name
}

// ── 新建表 ──────────────────────────────────────────────
const colTypeOptions = [
  { label: 'VARCHAR', value: 'VARCHAR' },
  { label: 'CHAR', value: 'CHAR' },
  { label: 'INT', value: 'INT' },
  { label: 'BIGINT', value: 'BIGINT' },
  { label: 'TINYINT', value: 'TINYINT' },
  { label: 'SMALLINT', value: 'SMALLINT' },
  { label: 'FLOAT', value: 'FLOAT' },
  { label: 'DOUBLE', value: 'DOUBLE' },
  { label: 'DECIMAL', value: 'DECIMAL' },
  { label: 'TEXT', value: 'TEXT' },
  { label: 'LONGTEXT', value: 'LONGTEXT' },
  { label: 'DATETIME', value: 'DATETIME' },
  { label: 'DATE', value: 'DATE' },
  { label: 'TIMESTAMP', value: 'TIMESTAMP' },
  { label: 'BOOLEAN', value: 'BOOLEAN' },
  { label: 'BLOB', value: 'BLOB' },
]

// Types that accept a length/precision argument
const LENGTH_TYPES = new Set(['VARCHAR', 'CHAR', 'DECIMAL', 'FLOAT', 'DOUBLE'])

function typeNeedsLength(baseType) {
  return LENGTH_TYPES.has((baseType || '').toUpperCase())
}

function typeLengthPlaceholder(baseType) {
  const t = (baseType || '').toUpperCase()
  if (t === 'DECIMAL') return '10,2'
  if (t === 'VARCHAR') return '255'
  if (t === 'CHAR') return '32'
  return '长度'
}

function resolveColType(row) {
  const base = row.colBaseType || 'VARCHAR'
  if (typeNeedsLength(base) && row.colLength && row.colLength.trim()) {
    return `${base}(${row.colLength.trim()})`
  }
  return base
}

function onColTypeChange(row) {
  // Set sensible default length when switching type
  const t = (row.colBaseType || '').toUpperCase()
  if (t === 'VARCHAR') row.colLength = '255'
  else if (t === 'CHAR') row.colLength = '32'
  else if (t === 'DECIMAL') row.colLength = '10,2'
  else row.colLength = ''
}

const newTableDialogVisible = ref(false)
const newTableSubmitting = ref(false)

function makeEmptyColumn() {
  return { colName: '', colBaseType: 'VARCHAR', colLength: '255', nullable: true, defaultVal: '', comment: '' }
}

const newTableForm = ref({
  tableName: '',
  tableComment: '',
  columns: [makeEmptyColumn()],
})

function onNewTable() {
  newTableForm.value = { tableName: '', tableComment: '', columns: [makeEmptyColumn()] }
  newTableDialogVisible.value = true
}

function addColumn() {
  newTableForm.value.columns.push(makeEmptyColumn())
}

function removeColumn(index) {
  newTableForm.value.columns.splice(index, 1)
}

function buildCreateTableSQL() {
  const { tableName, tableComment, columns } = newTableForm.value
  const colDefs = columns
    .filter((c) => c.colName.trim())
    .map((c) => {
      const colType = resolveColType(c)
      let def = `  \`${c.colName}\` ${colType}`
      def += c.nullable ? ' NULL' : ' NOT NULL'
      if (c.defaultVal !== '') {
        const needsQuote = /^(VARCHAR|CHAR|TEXT|DATETIME)/i.test(colType)
        def += needsQuote ? ` DEFAULT '${c.defaultVal}'` : ` DEFAULT ${c.defaultVal}`
      }
      if (c.comment) def += ` COMMENT '${c.comment}'`
      return def
    })
  let sql = `CREATE TABLE \`${tableName}\` (\n${colDefs.join(',\n')}\n)`
  if (tableComment) sql += ` COMMENT='${tableComment}'`
  sql += ';'
  return sql
}

function submitNewTable() {
  const { tableName, columns } = newTableForm.value
  if (!tableName.trim()) {
    ElMessage({ message: '请输入表名', type: 'warning' })
    return
  }
  if (!columns.some((c) => c.colName.trim())) {
    ElMessage({ message: '请至少添加一个字段', type: 'warning' })
    return
  }
  const sql = buildCreateTableSQL()
  newTableSubmitting.value = true
  http.post('/execSQL', { connId, schema, sql })
    .then(() => {
      newTableDialogVisible.value = false
      loadTables()
      ElMessage({ message: '建表成功', type: 'success' })
    })
    .catch(() => {
      // error already shown by httpProxy interceptor; keep dialog open
    })
    .finally(() => {
      newTableSubmitting.value = false
    })
}

function onBrowseData(row) {
  emit('openDataBrowser', { connId, schema, tableName: row.name, dbType })
}

function onBrowseCurrentTable() {
  emit('openDataBrowser', { connId, schema, tableName: selectedTable.value, dbType })
}

function exportTable(row) {
  http.get(`/exportXlsx?connId=${connId}&schema=${schema}&table=${row.name}`, { responseType: 'blob' })
    .then((res) => {
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
      a.download = row.name + '.xlsx'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      window.URL.revokeObjectURL(url)
    })
    .catch(() => ElMessage({ message: '导出失败', type: 'error' }))
}

// ── 表级操作 ──────────────────────────────────────────────
const renameDialogVisible = ref(false)
const renameTarget = ref('')
const renameNewName = ref('')
const renameSubmitting = ref(false)

function onTableAction(command, row) {
  if (command === 'browse') {
    onBrowseData(row)
  } else if (command === 'export') {
    exportTable(row)
  } else if (command === 'rename') {
    renameTarget.value = row.name
    renameNewName.value = row.name
    renameDialogVisible.value = true
  } else if (command === 'truncate') {
    ElMessageBox.confirm(
      `确定要清空表 "${row.name}" 的所有数据吗？此操作不可恢复！`,
      '清空表',
      { confirmButtonText: '确定清空', cancelButtonText: '取消', type: 'warning' }
    ).then(() => {
      const sql = `TRUNCATE TABLE \`${row.name}\``
      http.post('/execSQL', { connId, schema, sql })
        .then(() => {
          ElMessage({ message: `表 "${row.name}" 已清空`, type: 'success' })
        })
    }).catch(() => {})
  } else if (command === 'drop') {
    ElMessageBox.confirm(
      `确定要删除表 "${row.name}" 吗？表结构和所有数据将被永久删除，无法恢复！`,
      '删除表',
      { confirmButtonText: '确定删除', cancelButtonText: '取消', type: 'error' }
    ).then(() => {
      const sql = `DROP TABLE \`${row.name}\``
      http.post('/execSQL', { connId, schema, sql })
        .then(() => {
          ElMessage({ message: `表 "${row.name}" 已删除`, type: 'success' })
          if (selectedTable.value === row.name) {
            selectedTable.value = null
          }
          loadTables()
        })
    }).catch(() => {})
  }
}

function submitRename() {
  const newName = renameNewName.value.trim()
  if (!newName) {
    ElMessage({ message: '请输入新表名', type: 'warning' })
    return
  }
  if (newName === renameTarget.value) {
    renameDialogVisible.value = false
    return
  }
  const sql = `RENAME TABLE \`${renameTarget.value}\` TO \`${newName}\``
  renameSubmitting.value = true
  http.post('/execSQL', { connId, schema, sql })
    .then(() => {
      ElMessage({ message: `表已重命名为 "${newName}"`, type: 'success' })
      if (selectedTable.value === renameTarget.value) {
        selectedTable.value = newName
      }
      renameDialogVisible.value = false
      loadTables()
    })
    .catch(() => {})
    .finally(() => {
      renameSubmitting.value = false
    })
}

function onTableDrop() {
  selectedTable.value = null
  loadTables()
}

onMounted(() => {
  loadTables()
  initSidebarWidth()
})

onBeforeUnmount(() => {
  document.removeEventListener('mousemove', onResizeMove)
  document.removeEventListener('mouseup', onResizeEnd)
})

watch(
  () => [connId, schema],
  () => {
    selectedTable.value = null
    loadTables()
  }
)
</script>


<style scoped>
.table-manager {
  height: calc(100vh - 60px);
  display: flex;
  flex-direction: column;
  background: #fff;
}

.tm-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: #fafbfc;
  border-bottom: 1px solid #ebeef5;
}

.tm-toolbar .el-button {
  border-radius: 6px;
  font-size: 13px;
}

.tm-body {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.tm-sidebar {
  border-right: 1px solid #ebeef5;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #fafbfc;
  flex-shrink: 0;
}

.tm-resize-handle {
  width: 5px;
  cursor: col-resize;
  background: transparent;
  flex-shrink: 0;
  transition: background 0.15s;
  position: relative;
  z-index: 10;
}

.tm-resize-handle:hover {
  background: var(--accent-color, #409eff);
  opacity: 0.4;
}

.tm-search {
  padding: 10px 12px;
  border-bottom: 1px solid #ebeef5;
}

.tm-table-list {
  flex: 1;
  overflow: auto;
}

.tm-editor {
  flex: 1;
  overflow: auto;
  padding: 8px 12px;
  display: flex;
  flex-direction: column;
}

.tm-editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 4px 8px 4px;
  border-bottom: 1px solid #ebeef5;
  margin-bottom: 8px;
}

.tm-table-title {
  font-size: 16px;
  font-weight: 600;
  color: #303133;
  cursor: pointer;
}

.tm-table-title:hover {
  color: var(--accent-color, #409eff);
}

.tm-table-comment {
  font-size: 13px;
  font-weight: 400;
  color: #909399;
  margin-left: 8px;
}

/* TableEditor 在 TableManager 内时撑满高度 */
.tm-editor :deep(.table-editor-tabs) {
  flex: 1;
  min-height: 0;
}

.classical-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #c0c4cc;
  font-size: 14px;
  flex-direction: column;
  gap: 12px;
}

.classical-empty .empty-icon {
  font-size: 48px;
  opacity: 0.4;
}
</style>
