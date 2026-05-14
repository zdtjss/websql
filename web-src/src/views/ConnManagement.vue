<template>
  <div class="conn-management">
    <div class="table-toolbar">
      <el-form :inline="true" :model="connQuery">
        <el-form-item label="连接名称">
          <el-input v-model="connQuery.name" placeholder="请输入连接名称" clearable />
        </el-form-item>
        <el-form-item label="所属层级">
          <el-tree-select
            v-model="connQuery.parentId"
            :data="conCfgTreeData"
            clearable
            value-key="id"
            placeholder="请选择"
            style="width: 180px"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="listConnCfg">查询</el-button>
          <el-button type="primary" @click="addConn">添加连接</el-button>
        </el-form-item>
      </el-form>
    </div>

    <el-table :data="connList" style="width: 100%" @cell-dblclick="onCellDblClick">
      <el-table-column type="index" width="50" resizable />
      <el-table-column prop="name" label="连接名称" width="180" resizable>
        <template #default="scope">
          <el-input v-if="isCellEditing(scope.row, 'name')" v-model="scope.row.name"
            v-focus size="small" @blur="exitCellEditing(scope.row, 'name')" @keyup.enter="exitCellEditing(scope.row, 'name')" />
          <span v-else class="cell-text">{{ scope.row.name }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="dbType" label="数据库类型" width="120" resizable>
        <template #default="scope">
          <el-select v-if="isCellEditing(scope.row, 'dbType')" v-model="scope.row.dbType"
            v-focus size="small" @change="exitCellEditing(scope.row, 'dbType')">
            <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
          <span v-else class="cell-text">{{ getDbTypeLabel(scope.row.dbType) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="parentId" label="所属层级" width="150" resizable>
        <template #default="scope">
          <el-tree-select v-if="isCellEditing(scope.row, 'parentId')"
            v-model="scope.row.parentId"
            :data="conCfgTreeData"
            clearable
            value-key="id"
            placeholder="未指定"
            size="small"
            @change="onParentIdChange(scope.row)"
          />
          <span v-else class="cell-text">{{ scope.row.parentName || '未指定' }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="user" label="用户名" width="180" resizable>
        <template #default="scope">
          <el-input v-if="isCellEditing(scope.row, 'user')" v-model="scope.row.user"
            v-focus size="small" @blur="exitCellEditing(scope.row, 'user')" @keyup.enter="exitCellEditing(scope.row, 'user')" />
          <span v-else class="cell-text">{{ scope.row.user }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="pwd" label="密码" width="120" resizable>
        <template #default="scope">
          <el-input v-if="isCellEditing(scope.row, 'pwd')" v-model="scope.row.pwd" type="password"
            v-focus size="small" @blur="exitCellEditing(scope.row, 'pwd')" @keyup.enter="exitCellEditing(scope.row, 'pwd')" />
          <span v-else class="cell-text">******</span>
        </template>
      </el-table-column>
      <el-table-column prop="url" label="连接信息" min-width="200" resizable>
        <template #default="scope">
          <el-input v-if="isCellEditing(scope.row, 'url')" v-model="scope.row.url" type="textarea" :rows="2"
            size="small" @blur="exitCellEditing(scope.row, 'url')" />
          <span v-else class="cell-text">{{ scope.row.url }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100" fixed="right" resizable>
        <template #default="scope">
          <el-tooltip v-if="isRowEditing(scope.row)" content="保存" placement="top">
            <el-icon class="action-icon" @click="saveConnCfg(scope.row)"><CircleCheck /></el-icon>
          </el-tooltip>
          <el-tooltip content="测试连接" placement="top">
            <el-icon class="action-icon" :class="{ 'is-loading': scope.row.testing }" @click="testDbConn(scope.row)"><Connection /></el-icon>
          </el-tooltip>
          <el-popconfirm title="确定要删除这个连接吗？" @confirm="delConnCfg(scope.row)">
            <template #reference>
              <el-tooltip content="删除" placement="top">
                <el-icon class="action-icon action-icon--danger"><Delete /></el-icon>
              </el-tooltip>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :page-sizes="[10, 20, 50, 100]"
      :total="pagination.total"
      layout="total, sizes, prev, pager, next, jumper"
      style="margin-top: 16px; justify-content: center;"
      @size-change="handleSizeChange"
      @current-change="handleCurrentChange"
    />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage } from 'element-plus'
import { CircleCheck, Connection, Delete } from '@element-plus/icons-vue'

const emit = defineEmits(['conn-saved', 'conn-deleted'])

const connList = ref([])
const conCfgTreeData = ref([])
const connQuery = ref({ name: "", parentId: "" })
const pagination = ref({ page: 1, pageSize: 20, total: 0 })
const editingCell = ref(null)

const EDITABLE_FIELDS = new Set(['name', 'dbType', 'parentId', 'user', 'pwd', 'url'])

const vFocus = {
  mounted(el) {
    const input = el.querySelector('input') || el.querySelector('textarea')
    if (input) input.focus()
  }
}

const dbTypeList = ref([
  { label: "MySQL", value: "mysql" },
  { label: "Oracle", value: "oracle" },
  { label: "SQLite", value: "sqlite" },
  { label: "MariaDB", value: "mariadb" }
])

const getDbTypeLabel = (dbType) => {
  const item = dbTypeList.value.find(t => t.value === dbType)
  return item ? item.label : dbType
}

function isCellEditing(row, field) {
  if (!row.id) return true
  return editingCell.value?.row === row && editingCell.value?.field === field
}

function isRowEditing(row) {
  if (!row.id) return true
  return editingCell.value?.row === row
}

function onCellDblClick(row, column) {
  if (!row.id) return
  const field = column.property
  if (!EDITABLE_FIELDS.has(field)) return
  editingCell.value = { row, field }
}

function exitCellEditing(row, field) {
  if (editingCell.value?.row === row && editingCell.value?.field === field) {
    editingCell.value = null
  }
}

function onParentIdChange(row) {
  const pid = row.parentId
  if (pid) {
    row.parentName = findLabelInTree(conCfgTreeData.value, pid)
  } else {
    row.parentName = null
  }
  exitCellEditing(row, 'parentId')
}

function findLabelInTree(nodes, id) {
  if (!nodes) return null
  for (const node of nodes) {
    if (node.id === id || node.value === id) return node.label
    if (node.children) {
      const found = findLabelInTree(node.children, id)
      if (found) return found
    }
  }
  return null
}

const listDirTree = () => {
  http.get("/listDirTree").then((resp) => {
    conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
  })
}

const listConnCfg = () => {
  pagination.value.page = 1
  const param = new URLSearchParams()
  param.append("name", connQuery.value.name)
  param.append("parentId", connQuery.value.parentId || '')
  param.append("page", pagination.value.page)
  param.append("pageSize", pagination.value.pageSize)
  http.get("/listConn2", { params: param }).then((resp) => {
    const result = resp.data.data || resp.data
    connList.value = result.data || []
    pagination.value.total = result.total || 0
  })
}

const handleSizeChange = (size) => {
  pagination.value.pageSize = size
  pagination.value.page = 1
  listConnCfg()
}

const handleCurrentChange = (page) => {
  pagination.value.page = page
  listConnCfg()
}

const addConn = () => {
  connList.value.unshift({ dbType: "mysql" })
}

const saveConnCfg = (row) => {
  http.post("/saveConn", row).then((resp) => {
    ElMessage.success("保存成功")
    editingCell.value = null
    const saved = resp.data
    if (saved && saved.id) {
      const idx = connList.value.indexOf(row)
      if (idx !== -1) {
        connList.value[idx] = saved
      }
    }
    emit('conn-saved', row)
  })
}

const testDbConn = (row) => {
  row.testing = true
  http.post("/testDbConn", row)
    .then((resp) => {
      if (resp.data.code === 200) {
        ElMessage.success("数据库连接成功")
      } else {
        console.error('[ConnManagement] 数据库连接测试失败 - msg:', resp.data.msg)
        ElMessage.error("数据库连接失败，请检查配置")
      }
    })
    .catch((err) => {
      console.error('[ConnManagement] 数据库连接测试异常:', err)
      ElMessage.error("数据库连接失败：无法连接到数据库")
    })
    .finally(() => {
      row.testing = false
    })
}

const delConnCfg = (row) => {
  if (row.id) {
    http.get("/delConn", { params: { id: row.id } }).then(() => {
      ElMessage.success("删除成功")
      pagination.value.page = 1
      listConnCfg()
      emit('conn-deleted', row)
    })
  } else {
    connList.value = connList.value.filter(item => item != row)
    emit('conn-deleted', row)
  }
}

listDirTree()
listConnCfg()
</script>

<style scoped>
.conn-management {
  padding: 20px;
  max-height: calc(100vh - 150px);
  overflow-y: auto;
}

.table-toolbar {
  margin-bottom: 16px;
}

.cell-text {
  cursor: default;
}

:deep(.el-table) {
  font-size: 14px;
}

:deep(.el-table th) {
  background-color: #f5f7fa;
  color: #606266;
  font-weight: 600;
}

.action-icon {
  font-size: 18px;
  cursor: pointer;
  color: #909399;
  margin-right: 8px;
  vertical-align: middle;
  transition: color 0.2s;
}

.action-icon:hover {
  color: #409eff;
}

.action-icon--danger:hover {
  color: #f56c6c;
}

.action-icon.is-loading {
  animation: rotating 1.5s linear infinite;
}

@keyframes rotating {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
