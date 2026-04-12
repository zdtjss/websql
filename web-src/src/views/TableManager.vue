<template>
  <div class="table-manager" style="height: calc(100vh - 60px); display: flex; flex-direction: column;">
    <!-- Toolbar -->
    <div class="toolbar" style="padding: 6px 10px; border-bottom: 1px solid #e4e7ed; display: flex; align-items: center; gap: 8px;">
      <el-button size="small" type="primary" @click="onNewTable">新建表</el-button>
      <el-button size="small" @click="loadTables">刷新</el-button>
    </div>

    <!-- 新建表 Dialog -->
    <el-dialog v-model="newTableDialogVisible" title="新建表" width="800px" :close-on-click-modal="false">
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
        <el-table-column label="字段名" min-width="120">
          <template #default="scope">
            <el-input v-model="scope.row.colName" size="small" placeholder="字段名" />
          </template>
        </el-table-column>
        <el-table-column label="类型" min-width="200">
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
        <el-table-column label="可空" width="60" align="center">
          <template #default="scope">
            <el-checkbox v-model="scope.row.nullable" />
          </template>
        </el-table-column>
        <el-table-column label="默认值" min-width="100">
          <template #default="scope">
            <el-input v-model="scope.row.defaultVal" size="small" placeholder="默认值" />
          </template>
        </el-table-column>
        <el-table-column label="注释" min-width="120">
          <template #default="scope">
            <el-input v-model="scope.row.comment" size="small" placeholder="注释" />
          </template>
        </el-table-column>
        <el-table-column label="" width="50" align="center">
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
  <el-dialog v-model="renameDialogVisible" title="重命名表" width="400px" :close-on-click-modal="false">
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
    <div style="flex: 1; display: flex; overflow: hidden;">
      <!-- Left: table list -->
      <div style="width: 30%; border-right: 1px solid #e4e7ed; display: flex; flex-direction: column; overflow: hidden;">
        <div style="padding: 8px;">
          <el-input
            v-model="searchKeyword"
            placeholder="搜索表名"
            clearable
            size="small"
          />
        </div>
        <div style="flex: 1; overflow: auto;">
          <el-table
            :data="filteredTables"
            highlight-current-row
            size="small"
            style="width: 100%;"
            height="100%"
            @row-click="onTableClick"
          >
            <el-table-column prop="name" label="表名" show-overflow-tooltip />
            <el-table-column prop="comment" label="注释" show-overflow-tooltip />
            <el-table-column label="操作" width="60" align="center">
              <template #default="scope">
                <el-dropdown trigger="click" @command="(cmd) => onTableAction(cmd, scope.row)" @click.stop>
                  <el-icon style="cursor: pointer;" :size="16" title="操作"><MoreFilled /></el-icon>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="browse">浏览数据</el-dropdown-item>
                      <el-dropdown-item command="export">数据导出</el-dropdown-item>
                      <el-dropdown-item command="rename">重命名</el-dropdown-item>
                      <el-dropdown-item command="truncate" divided>清空表</el-dropdown-item>
                      <el-dropdown-item command="drop" style="color: #f56c6c;">删除表</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>

      <!-- Right: TableEditor -->
      <div style="width: 70%; overflow: auto; padding: 8px;">
        <div v-if="!selectedTable" style="display: flex; align-items: center; justify-content: center; height: 100%; color: #909399;">
          点击左侧表名查看表结构
        </div>
        <TableEditor
          v-else
          :tableMeta="tableMeta"
          @tableDrop="onTableDrop"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import http from '@/js/utils/httpProxy.js'
import { ElMessageBox } from 'element-plus'
import { computed, onMounted, ref, watch } from 'vue'
import TableEditor from './comonents/TableEditor.vue'

const props = defineProps({
  connId: String,
  schema: String,
  dbType: String,
})

const emit = defineEmits(['openDataBrowser'])

const tableList = ref([])
const searchKeyword = ref('')
const selectedTable = ref(null)

const tableMeta = computed(() => {
  if (!selectedTable.value) return null
  return {
    connId: props.connId,
    schema: props.schema,
    tableName: selectedTable.value,
    dbType: props.dbType || '',
  }
})

const filteredTables = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase()
  if (!kw) return tableList.value
  return tableList.value.filter(
    (t) => t.name.toLowerCase().includes(kw) || (t.comment && t.comment.toLowerCase().includes(kw))
  )
})

function loadTables() {
  if (!props.connId || !props.schema) return
  http.get('/listTable', { params: { connId: props.connId, schema: props.schema } })
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
  http.post('/execSQL', { connId: props.connId, schema: props.schema, sql })
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
  emit('openDataBrowser', { connId: props.connId, schema: props.schema, tableName: row.name })
}

function exportTable(row) {
  http.get(`/exportXlsx?connId=${props.connId}&schema=${props.schema}&table=${row.name}`, { responseType: 'blob' })
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
      http.post('/execSQL', { connId: props.connId, schema: props.schema, sql })
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
      http.post('/execSQL', { connId: props.connId, schema: props.schema, sql })
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
  http.post('/execSQL', { connId: props.connId, schema: props.schema, sql })
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
})

watch(
  () => [props.connId, props.schema],
  () => {
    selectedTable.value = null
    loadTables()
  }
)
</script>
