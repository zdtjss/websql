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
    
    <el-table :data="connList" style="width: 100%">
      <el-table-column type="index" width="50" resizable />
      <el-table-column prop="name" label="连接名称" width="180" resizable>
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.name" />
          <span v-else>{{ scope.row.name }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="dbType" label="数据库类型" width="120" resizable>
        <template #default="scope">
          <span v-if="!scope.row.editing">{{ getDbTypeLabel(scope.row.dbType) }}</span>
          <el-select v-else v-model="scope.row.dbType" placeholder="请选择">
            <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column prop="parentId" label="所属层级" width="150" resizable>
        <template #default="scope">
          <span v-if="!scope.row.editing">{{ scope.row.parentName || '未指定' }}</span>
          <el-tree-select 
            v-else
            v-model="scope.row.parentId" 
            :data="conCfgTreeData" 
            clearable 
            value-key="id" 
            placeholder="未指定" 
          />
        </template>
      </el-table-column>
      <el-table-column prop="user" label="用户名" width="180" resizable>
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.user" />
          <span v-else>{{ scope.row.user }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="pwd" label="密码" width="120" resizable>
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.pwd" type="password" />
          <span v-else>******</span>
        </template>
      </el-table-column>
      <el-table-column prop="url" label="连接信息" min-width="200" resizable>
        <template #default="scope">
          <el-input v-if="scope.row.editing" v-model="scope.row.url" type="textarea" :rows="2" />
          <span v-else>{{ scope.row.url }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="200" fixed="right" resizable>
        <template #default="scope">
          <el-button 
            v-if="!scope.row.editing" 
            type="success" 
            size="small"
            @click="startEditConn(scope.row)"
          >
            编辑
          </el-button>
          <el-button 
            v-if="scope.row.editing" 
            type="success" 
            size="small"
            @click="testDbConn(scope.row)"
            :loading="scope.row.testing"
          >
            测试
          </el-button>
          <el-button 
            v-if="scope.row.editing" 
            type="primary" 
            size="small"
            @click="saveConnCfg(scope.row)"
          >
            保存
          </el-button>
          <el-button 
            v-if="scope.row.editing" 
            type="warning" 
            size="small"
            @click="scope.row.editing = false"
          >
            取消
          </el-button>
          <el-popconfirm title="确定要删除这个连接吗？" @confirm="delConnCfg(scope.row)">
            <template #reference>
              <el-button type="danger" size="small">删除</el-button>
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

const emit = defineEmits(['conn-saved', 'conn-deleted'])

const connList = ref([])
const conCfgTreeData = ref([])
const connQuery = ref({ name: "", parentId: "" })
const pagination = ref({ page: 1, pageSize: 20, total: 0 })

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
    connList.value = (result.data || []).map(e => Object.assign({ editing: false }, e))
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
  connList.value.unshift({ dbType: "mysql", editing: true })
}

const startEditConn = (row) => {
  row.editing = true
}

const saveConnCfg = (row) => {
  http.post("/saveConn", row).then(() => {
    ElMessage.success("保存成功")
    row.editing = false
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

:deep(.el-table) {
  font-size: 14px;
}

:deep(.el-table th) {
  background-color: #f5f7fa;
  color: #606266;
  font-weight: 600;
}

:deep(.el-table-column--fixed) .el-button + .el-button {
  margin-left: 8px;
}
</style>
