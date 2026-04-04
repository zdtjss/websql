<template>
  <div class="system-management-page">
    <!-- 顶部导航栏 -->
    <div class="page-header">
      <div class="header-left">
        <el-button @click="goBack" icon="ArrowLeft" circle>
          <el-icon><ArrowLeft /></el-icon>
        </el-button>
        <h1 class="page-title">系统管理</h1>
      </div>
    </div>

    <!-- 主内容区 -->
    <div class="page-content">
      <el-tabs v-model="activeTab" type="border-card" class="system-tabs">
        <!-- 角色管理 -->
        <el-tab-pane label="角色管理" name="role">
          <div class="role-management">
            <div class="table-toolbar">
              <el-button type="primary" @click="addRole">
                <el-icon><Plus /></el-icon>
                添加角色
              </el-button>
            </div>
            
            <el-table :data="roleList" style="width: 100%" highlight-current-row>
              <el-table-column type="index" width="50" />
              <el-table-column prop="name" label="角色名" width="200">
                <template #default="scope">
                  <el-input 
                    v-if="scope.row.editing" 
                    v-model="scope.row.name" 
                    placeholder="请输入角色名"
                    @keyup.enter="saveRole(scope.row)"
                  />
                  <span v-else>{{ scope.row.name }}</span>
                </template>
              </el-table-column>
              <el-table-column label="权限配置" min-width="500">
                <template #default="scope">
                  <div v-if="!scope.row.editing" class="permission-display">
                    <el-tag 
                      v-for="perm in getPermissionSummary(scope.row)" 
                      :key="perm.id" 
                      size="small" 
                      :type="getPermissionType(perm.level)"
                      class="permission-tag"
                    >
                      <el-icon v-if="perm.level === 'conn'"><Connection /></el-icon>
                      <el-icon v-else-if="perm.level === 'schema'"><Folder /></el-icon>
                      <el-icon v-else-if="perm.level === 'table'"><Grid /></el-icon>
                      <el-icon v-else-if="perm.level === 'column'"><Document /></el-icon>
                      {{ formatPermissionPath(perm) }}
                    </el-tag>
                    <el-tag v-if="!scope.row.powerList || scope.row.powerList.length === 0" size="small" type="info">
                      暂无权限
                    </el-tag>
                  </div>
                  <div v-else class="permission-editor">
                    <div class="permission-editor-header">
                      <el-breadcrumb separator="/">
                        <el-breadcrumb-item 
                          :class="{active: permissionNavLevel === 'conn'}" 
                          @click="navigatePermissionTree('conn')"
                        >
                          连接
                        </el-breadcrumb-item>
                        <el-breadcrumb-item 
                          v-if="permissionNavLevel !== 'conn'"
                          :class="{active: permissionNavLevel === 'schema'}" 
                          @click="navigatePermissionTree('schema')"
                        >
                          Schema
                        </el-breadcrumb-item>
                        <el-breadcrumb-item 
                          v-if="['table', 'column'].includes(permissionNavLevel)"
                          :class="{active: permissionNavLevel === 'table'}" 
                          @click="navigatePermissionTree('table')"
                        >
                          表
                        </el-breadcrumb-item>
                        <el-breadcrumb-item 
                          v-if="permissionNavLevel === 'column'"
                          :class="{active: permissionNavLevel === 'column'}" 
                        >
                          字段
                        </el-breadcrumb-item>
                      </el-breadcrumb>
                    </div>
                    <el-alert
                      v-if="permissionNavLevel === 'conn'"
                      title="权限选择提示"
                      type="info"
                      :closable="false"
                      style="margin-bottom: 12px; font-size: 13px;"
                    >
                      <el-icon><InfoFilled /></el-icon>
                      点击连接节点可进入下一级权限配置（Schema → 表 → 字段），支持跨层级选择权限
                    </el-alert>
                    <el-tree
                      ref="permissionTree"
                      :data="permissionTreeData"
                      :props="{ children: 'children', label: 'label', level: 'level' }"
                      node-key="id"
                      show-checkbox
                      :default-expand-all="permissionNavLevel !== 'column'"
                      :default-checked-keys="scope.row.checkedPermissions"
                      @check="handlePermissionCheck"
                      @node-click="handlePermissionNodeClick"
                      lazy
                      :load="loadPermissionNode"
                    />
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="220" fixed="right">
                <template #default="scope">
                  <el-button 
                    v-if="!scope.row.editing" 
                    type="primary" 
                    size="small"
                    @click="startEditRole(scope.row)"
                  >
                    <el-icon><Edit /></el-icon>
                    编辑
                  </el-button>
                  <el-button 
                    v-else 
                    type="success" 
                    size="small"
                    @click="saveRole(scope.row)"
                  >
                    <el-icon><Check /></el-icon>
                    保存
                  </el-button>
                  <el-button 
                    v-if="scope.row.editing" 
                    type="warning" 
                    size="small"
                    @click="cancelEditRole(scope.row)"
                  >
                    <el-icon><Close /></el-icon>
                    取消
                  </el-button>
                  <el-popconfirm 
                    title="确定要删除这个角色吗？"
                    @confirm="delRole(scope.row)"
                  >
                    <template #reference>
                      <el-button type="danger" size="small">
                        <el-icon><Delete /></el-icon>
                        删除
                      </el-button>
                    </template>
                  </el-popconfirm>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- 用户管理 -->
        <el-tab-pane label="用户管理" name="user">
          <div class="user-management">
            <div class="table-toolbar">
              <el-form :inline="true" :model="userQuery">
                <el-form-item label="姓名">
                  <el-input v-model="userQuery.name" placeholder="请输入姓名" clearable />
                </el-form-item>
                <el-form-item label="登录名">
                  <el-input v-model="userQuery.loginName" placeholder="请输入登录名" clearable />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="findUser">查询</el-button>
                  <el-button type="primary" @click="addUser">添加用户</el-button>
                </el-form-item>
              </el-form>
            </div>
            
            <el-table :data="userList" style="width: 100%">
              <el-table-column type="index" width="50" />
              <el-table-column prop="name" label="姓名" width="150">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.name" />
                  <span v-else>{{ scope.row.name }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="loginName" label="登录名" width="150">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.loginName" />
                  <span v-else>{{ scope.row.loginName }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="pwd" label="密码" width="150">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.pwd" type="password" placeholder="不修改请留空" />
                  <span v-else>******</span>
                </template>
              </el-table-column>
              <el-table-column label="角色" width="200">
                <template #default="scope">
                  <span v-if="!scope.row.editing">{{ scope.row.roleName.join(",") }}</span>
                  <el-select 
                    v-else
                    v-model="scope.row.roleId" 
                    multiple 
                    filterable 
                    collapse-tags 
                    collapse-tags-tooltip 
                    placeholder="请选择角色"
                  >
                    <el-option v-for="item in roleList" :key="item.id" :label="item.name" :value="item.id" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="150" fixed="right">
                <template #default="scope">
                  <el-button 
                    v-if="!scope.row.editing" 
                    type="primary" 
                    size="small"
                    @click="scope.row.editing = true"
                  >
                    编辑
                  </el-button>
                  <el-button 
                    v-else 
                    type="success" 
                    size="small"
                    @click="saveUser(scope.row)"
                  >
                    保存
                  </el-button>
                  <el-popconfirm title="确定要删除这个用户吗？" @confirm="delUser(scope.row)">
                    <template #reference>
                      <el-button type="danger" size="small">删除</el-button>
                    </template>
                  </el-popconfirm>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- 连接管理 -->
        <el-tab-pane label="连接管理" name="conn">
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
                  />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="listConnCfg">查询</el-button>
                  <el-button type="primary" @click="addConn">添加连接</el-button>
                </el-form-item>
              </el-form>
            </div>
            
            <el-table :data="connList" style="width: 100%">
              <el-table-column type="index" width="50" />
              <el-table-column prop="name" label="连接名称" width="150">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.name" />
                  <span v-else>{{ scope.row.name }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="dbType" label="数据库类型" width="120">
                <template #default="scope">
                  <span v-if="!scope.row.editing">{{ getDbTypeLabel(scope.row.dbType) }}</span>
                  <el-select v-else v-model="scope.row.dbType" placeholder="请选择">
                    <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column prop="parentId" label="所属层级" width="150">
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
              <el-table-column prop="user" label="用户名" width="120">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.user" />
                  <span v-else>{{ scope.row.user }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="pwd" label="密码" width="120">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.pwd" type="password" />
                  <span v-else>******</span>
                </template>
              </el-table-column>
              <el-table-column prop="url" label="连接信息" min-width="200">
                <template #default="scope">
                  <el-input v-if="scope.row.editing" v-model="scope.row.url" type="textarea" :rows="2" />
                  <span v-else>{{ scope.row.url }}</span>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="200" fixed="right">
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
          </div>
        </el-tab-pane>

        <!-- 目录管理 -->
        <el-tab-pane label="目录管理" name="dir">
          <div class="dir-management">
            <el-alert 
              title="目录管理提示"
              type="info"
              :closable="false"
              style="margin-bottom: 16px;"
            >
              <p>💡 双击节点名称可编辑，编辑后点击"保存目录结构"生效</p>
            </el-alert>
            <el-tree 
              ref="dirTree"
              :data="conCfgTreeData" 
              draggable 
              default-expand-all 
              :expand-on-click-node="false"
            >
              <template #default="{ node, data }">
                <div class="tree-node-content">
                  <el-input 
                    v-model="data.label" 
                    style="width: 300px; margin-right: 16px;" 
                    placeholder="目录名称"
                  />
                  <el-button type="primary" size="small" @click="appendTreeNode(data)">
                    <el-icon><Plus /></el-icon>
                    添加子节点
                  </el-button>
                  <el-popconfirm title="确定要删除这个目录吗？" @confirm="removeDir(node, data)">
                    <template #reference>
                      <el-button type="danger" size="small" style="margin-left: 8px;">
                        <el-icon><Delete /></el-icon>
                        删除
                      </el-button>
                    </template>
                  </el-popconfirm>
                </div>
              </template>
            </el-tree>
            <div class="tree-actions">
              <el-button type="primary" @click="saveTree">
                <el-icon><Check /></el-icon>
                保存目录结构
              </el-button>
            </div>
          </div>
        </el-tab-pane>

        <!-- 系统配置 -->
        <el-tab-pane label="系统配置" name="system">
          <div class="system-config">
            <el-divider content-position="left">
              <el-icon><Monitor /></el-icon>
              AI 服务配置
            </el-divider>
            <el-form label-width="120px" :model="systemConfig">
              <el-form-item label="AI 提供商">
                <el-radio-group v-model="systemConfig.aiProvider">
                  <el-radio value="ollama">Ollama</el-radio>
                  <el-radio value="openai">OpenAI</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item label="Base URL">
                <el-input v-model="systemConfig.aiBaseUrl" placeholder="http://localhost:11434" />
              </el-form-item>
              <el-form-item label="Model">
                <el-input v-model="systemConfig.aiModel" placeholder="e.g. qwen2.5:14b" />
              </el-form-item>
              <el-form-item label="API Key">
                <el-input v-model="systemConfig.aiApiKey" type="password" show-password placeholder="sk-..." />
              </el-form-item>
              <el-form-item label="Temperature">
                <el-slider v-model="systemConfig.aiTemperature" :min="0" :max="2" :step="0.1" show-input />
              </el-form-item>
              <el-form-item label="Max Tokens">
                <el-input-number v-model="systemConfig.aiMaxTokens" :min="0" :max="128000" :step="1024" placeholder="0=不限" />
              </el-form-item>
              <el-form-item label="思考模式">
                <el-switch v-model="systemConfig.aiEnableThinking" />
                <span style="margin-left: 10px; font-size: 12px; color: #909399;">启用后模型会输出思考过程</span>
              </el-form-item>
              <el-form-item>
                <el-button type="primary" @click="testAiConfig" :loading="aiTesting">
                  <el-icon><Connection /></el-icon>
                  测试 AI 配置
                </el-button>
              </el-form-item>
            </el-form>

            <el-divider content-position="left">
              <el-icon><Link /></el-icon>
              外部用户认证
            </el-divider>
            <el-form label-width="120px" :model="systemConfig">
              <el-form-item label="认证接口 URL">
                <el-input v-model="systemConfig.outterUser" placeholder="http://localhost:8081/api/login" />
              </el-form-item>
              <el-form-item>
                <el-button type="primary" @click="testOutterUser" :loading="testingOutterUser">
                  测试接口
                </el-button>
              </el-form-item>
            </el-form>

            <el-divider content-position="left">
              <el-icon><Lock /></el-icon>
              IP 访问控制
            </el-divider>
            <el-form label-width="120px" :model="systemConfig">
              <el-form-item label="允许的 IP 列表">
                <el-input 
                  v-model="systemConfig.allowedIP" 
                  type="textarea"
                  :rows="4"
                  placeholder="请输入 IP 地址，每行一个" 
                />
                <div style="font-size: 12px; color: #909399; margin-top: 4px;">
                  💡 每行一个 IP 地址，例如：127.0.0.1 或 192.168.1.100
                </div>
              </el-form-item>
            </el-form>

            <div class="config-actions">
              <el-button type="primary" @click="saveAllConfig" :loading="savingAll" size="large">
                <el-icon><Check /></el-icon>
                保存所有配置
              </el-button>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import http from '@/js/utils/httpProxy'

const router = useRouter()
const activeTab = ref('role')

// 数据
const roleList = ref([])
const userList = ref([])
const connList = ref([])
const conCfgTreeData = ref([])
const permissionTreeData = ref([])
const roleConnIdList = ref([])
const powerListChecked = ref([])
const permissionNavLevel = ref('conn')
const currentConnId = ref('')
const currentSchema = ref('')
const currentTable = ref('')

// 查询条件
const userQuery = ref({ name: "", loginName: "" })
const connQuery = ref({ name: "", parentId: "" })

// 数据库类型
const dbTypeList = ref([
  { label: "MySQL", value: "mysql" }, 
  { label: "Oracle", value: "oracle" },
  { label: "SQLite", value: "sqlite" },
  { label: "MariaDB", value: "mariadb" }
])

// 系统配置
const systemConfig = ref({ 
  aiProvider: 'ollama', 
  aiBaseUrl: '', 
  aiModel: '',
  aiApiKey: '',
  aiTemperature: 0.7,
  aiMaxTokens: 0,
  aiEnableThinking: false,
  outterUser: '', 
  allowedIP: '127.0.0.1\n::1' 
})

const aiTesting = ref(false)
const testingOutterUser = ref(false)
const savingAll = ref(false)

// 生命周期
onMounted(() => {
  loadTabData('role')
})

// 方法
function goBack() {
  router.push('/')
}

function loadTabData(tabName) {
  if (tabName === 'role') {
    loadRoles()
  } else if (tabName === 'user') {
    // 用户数据在查询时加载
  } else if (tabName === 'conn') {
    listDirTree()
    listConnCfg()
  } else if (tabName === 'dir') {
    listDirTree()
  } else if (tabName === 'system') {
    loadSystemConfig()
  }
}

function getDbTypeLabel(dbType) {
  const item = dbTypeList.value.find(t => t.value === dbType)
  return item ? item.label : dbType
}

function formatPermissionPath(perm) {
  const parts = []
  if (perm.connName) parts.push(perm.connName)
  if (perm.schemaName) parts.push(perm.schemaName)
  if (perm.tableName) parts.push(perm.tableName)
  if (perm.columnName) parts.push(perm.columnName)
  return parts.join(' > ')
}

function getPermissionType(level) {
  const typeMap = {
    'conn': 'primary',
    'schema': 'success',
    'table': 'warning',
    'column': 'danger'
  }
  return typeMap[level] || 'info'
}

function getPermissionSummary(row) {
  if (!row.powerList || row.powerList.length === 0) {
    return []
  }
  return row.powerList.slice(0, 5) // 只显示前 5 个
}

// 角色管理
function loadRoles() {
  http.get("/roleList").then((resp) => {
    roleList.value = resp.data.data.map(e => {
      const row = Object.assign({ editing: false, checkedPermissions: [] }, e)
      if (row.powerList) {
        row.checkedPermissions = row.powerList.map(p => p.connId).filter(id => id)
      }
      return row
    })
  })
}

function addRole() {
  roleList.value.push({ 
    editing: true, 
    name: '', 
    powerList: [], 
    checkedPermissions: [] 
  })
}

function startEditRole(row) {
  row.editing = true
  row.checkedPermissions = []
  row.powerList = row.powerList || []
  row.powerList.forEach(p => {
    const key = `${p.connId}:${p.schemaName || ''}:${p.tableName || ''}:${p.columnName || ''}`
    if (!row.checkedPermissions.includes(key)) {
      row.checkedPermissions.push(key)
    }
  })
  permissionNavLevel.value = 'conn'
  currentConnId.value = ''
  currentSchema.value = ''
  currentTable.value = ''
  loadPermissionTree()
}

function cancelEditRole(row) {
  row.editing = false
  loadRoles()
}

function handlePermissionCheck(checkedKeys) {
  powerListChecked.value = checkedKeys.checkedKeys
}

function loadPermissionTree() {
  const params = { level: permissionNavLevel.value }
  if (currentConnId.value) params.connId = currentConnId.value
  if (currentSchema.value) params.schema = currentSchema.value
  if (currentTable.value) params.table = currentTable.value
  
  http.get("/permissionTree", { params }).then((resp) => {
    permissionTreeData.value = resp.data.data || []
  })
}

function navigatePermissionTree(level) {
  permissionNavLevel.value = level
  if (level === 'conn') {
    currentConnId.value = ''
    currentSchema.value = ''
    currentTable.value = ''
  } else if (level === 'schema' && currentConnId.value) {
    currentSchema.value = ''
    currentTable.value = ''
  } else if (level === 'table' && currentConnId.value && currentSchema.value) {
    currentTable.value = ''
  }
  loadPermissionTree()
}

function handlePermissionNodeClick(node) {
  // 点击节点时，根据层级自动导航到下一级
  if (node.level === 'conn') {
    currentConnId.value = node.id
    permissionNavLevel.value = 'schema'
    loadPermissionTree()
  } else if (node.level === 'schema') {
    currentSchema.value = node.id
    permissionNavLevel.value = 'table'
    loadPermissionTree()
  } else if (node.level === 'table') {
    currentTable.value = node.id
    permissionNavLevel.value = 'column'
    loadPermissionTree()
  }
}

function loadPermissionNode(node, resolve) {
  const level = node.level || 'conn'
  const params = { level }
  
  if (level === 'schema') {
    params.connId = node.id
  } else if (level === 'table') {
    params.connId = node.parentId || currentConnId.value
    params.schema = node.id
  } else if (level === 'column') {
    params.connId = node.parentId || currentConnId.value
    params.schema = node.data?.parentId || currentSchema.value
    params.table = node.id
  }
  
  http.get("/permissionTree", { params }).then((resp) => {
    resolve(resp.data.data || [])
  }).catch(() => {
    resolve([])
  })
}

function saveRole(row) {
  const param = Object.assign({}, row)
  
  // 将选中的权限转换为 PowerDetail 格式
  const powerList = []
  if (powerListChecked.value.length > 0) {
    powerListChecked.value.forEach(key => {
      const parts = key.split(':')
      const connId = parts[0]
      const schemaName = parts[1] || null
      const tableName = parts[2] || null
      const columnName = parts[3] || null
      
      // 根据权限的层级确定 level
      let level = 'conn'
      if (columnName) level = 'column'
      else if (tableName) level = 'table'
      else if (schemaName) level = 'schema'
      
      // 获取连接名称
      const conn = findConnById(connId)
      const connName = conn ? conn.name : ''
      
      powerList.push({
        connId,
        connName,
        schemaName,
        tableName,
        columnName,
        level
      })
    })
  }
  
  param.powerList = powerList
  param.connIdList = [] // 不再使用 connIdList
  
  http.post("/saveRole", param).then(() => {
    ElMessage.success("保存成功")
    loadRoles()
  })
}

function findConnById(connId) {
  // 从目录树中查找连接
  const findConn = (nodes) => {
    for (const node of nodes) {
      if (node.id === connId) return node
      if (node.children) {
        const found = findConn(node.children)
        if (found) return found
      }
    }
    return null
  }
  return findConn(conCfgTreeData.value)
}

function delRole(row) {
  if (row.id) {
    http.get("/delRole", { params: { id: row.id } }).then(() => {
      ElMessage.success("删除成功")
      loadRoles()
    })
  } else {
    roleList.value = roleList.value.filter(item => item != row)
  }
}

// 用户管理
function findUser() {
  if (!userQuery.value.name && !userQuery.value.loginName) {
    ElMessage.warning("请指定查询条件")
    return
  }
  http.get("/findUser", { params: userQuery.value }).then((resp) => {
    userList.value = resp.data.data.map(e => Object.assign({ editing: false }, e))
  })
}

function addUser() {
  userList.value.push({ 
    roleId: [], 
    roleName: [], 
    loginName: "", 
    name: "", 
    pwd: "", 
    editing: true 
  })
}

function saveUser(row) {
  http.post("/saveUser", row).then(() => {
    ElMessage.success("保存成功")
    row.roleName = row.roleId.map((val) => {
      const role = roleList.value.find(item => item.id === val)
      return role ? role.name : ''
    }).filter(n => n)
    row.editing = false
  })
}

function delUser(row) {
  if (row.id) {
    http.get("/delUser", { params: { id: row.id } }).then(() => {
      ElMessage.success("删除成功")
      findUser()
    })
  } else {
    userList.value = userList.value.filter(item => item != row)
  }
}

// 连接管理
function listDirTree() {
  http.get("/listDirTree").then((resp) => {
    conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
  })
}

function listConnCfg() {
  const param = new URLSearchParams()
  param.append("name", connQuery.value.name)
  param.append("parentId", connQuery.value.parentId || '')
  http.get("/listConn2", { params: param }).then((resp) => {
    connList.value = resp.data.data.map(e => Object.assign({ editing: false }, e))
  })
}

function addConn() {
  connList.value.unshift({ dbType: "mysql", editing: true })
}

function startEditConn(row) {
  row.editing = true
}

function saveConnCfg(row) {
  http.post("/saveConn", row).then(() => {
    ElMessage.success("保存成功")
    row.editing = false
  })
}

function testDbConn(row) {
  row.testing = true
  http.post("/testDbConn", row)
    .then((resp) => {
      if (resp.data.code === 200) {
        ElMessage.success("数据库连接成功")
      } else {
        ElMessage.error("数据库连接失败：" + resp.data.msg)
      }
    })
    .catch(() => {
      ElMessage.error("数据库连接失败：无法连接到数据库")
    })
    .finally(() => {
      row.testing = false
    })
}

function delConnCfg(row) {
  if (row.id) {
    http.get("/delConn", { params: { id: row.id } }).then(() => {
      ElMessage.success("删除成功")
      listConnCfg()
    })
  } else {
    connList.value = connList.value.filter(item => item != row)
  }
}

// 目录管理
function appendTreeNode(data) {
  const newChild = { label: "", value: "", id: "", children: [] }
  if (!data.children) {
    data.children = []
  }
  data.children.push(newChild)
}

function removeDir(node, data) {
  const removeNode = (list, targetNode) => {
    const index = list.findIndex(item => item === targetNode)
    if (index !== -1) {
      list.splice(index, 1)
      return true
    }
    for (const item of list) {
      if (item.children && removeNode(item.children, targetNode)) {
        return true
      }
    }
    return false
  }
  
  removeNode(conCfgTreeData.value, data)
  
  if (data.id) {
    http.get("/delTreeNode", { params: { id: data.id } }).then((resp) => {
      conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
    })
  }
}

function saveTree() {
  http.post("/saveTree", conCfgTreeData.value).then(() => {
    ElMessage.success("保存成功")
  })
}

// 系统配置
function loadSystemConfig() {
  http.get("/system/config/all/get").then((resp) => {
    if (resp.data && resp.data.data) {
      const data = resp.data.data
      systemConfig.value.aiProvider = data.aiProvider || 'ollama'
      systemConfig.value.aiBaseUrl = data.aiBaseUrl || ''
      systemConfig.value.aiModel = data.aiModel || ''
      systemConfig.value.aiApiKey = data.aiApiKey || ''
      systemConfig.value.aiTemperature = parseFloat(data.aiTemperature) || 0.7
      systemConfig.value.aiMaxTokens = parseInt(data.aiMaxTokens) || 0
      systemConfig.value.aiEnableThinking = data.aiEnableThinking === 'true'
      systemConfig.value.outterUser = data.outterUser || ''
      
      if (data.allowedIP && Array.isArray(data.allowedIP)) {
        systemConfig.value.allowedIP = data.allowedIP.join('\n')
      }
    }
  })
}

function saveAllConfig() {
  savingAll.value = true
  const ips = systemConfig.value.allowedIP.split('\n').map(ip => ip.trim()).filter(ip => ip !== '')
  
  http.post("/system/config/all/save", {
    aiProvider: systemConfig.value.aiProvider,
    aiBaseUrl: systemConfig.value.aiBaseUrl,
    aiModel: systemConfig.value.aiModel,
    aiApiKey: systemConfig.value.aiApiKey,
    aiTemperature: String(systemConfig.value.aiTemperature),
    aiMaxTokens: String(systemConfig.value.aiMaxTokens),
    aiEnableThinking: String(systemConfig.value.aiEnableThinking),
    outterUser: systemConfig.value.outterUser,
    allowedIP: ips
  }).then(() => {
    ElMessage.success("保存成功")
  }).finally(() => {
    savingAll.value = false
  })
}

function testAiConfig() {
  aiTesting.value = true
  http.post("/ai/config/test", {
    provider: systemConfig.value.aiProvider,
    baseUrl: systemConfig.value.aiBaseUrl,
    model: systemConfig.value.aiModel,
    apiKey: systemConfig.value.aiApiKey
  }).then(() => {
    ElMessage.success("连接成功")
  }).finally(() => {
    aiTesting.value = false
  })
}

function testOutterUser() {
  testingOutterUser.value = true
  http.post("/system/config/outterUser/test", { url: systemConfig.value.outterUser })
    .then((resp) => {
      if (resp.data.code === 200) {
        ElMessage.success("测试成功：" + JSON.stringify(resp.data.data))
      } else {
        ElMessage.error("测试失败：" + resp.data.msg)
      }
    })
    .catch(() => {
      ElMessage.error("测试失败：接口无响应")
    })
    .finally(() => {
      testingOutterUser.value = false
    })
}
</script>

<style scoped>
.system-management-page {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: #f5f7fa;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 24px;
  background: #fff;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  z-index: 100;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.page-title {
  font-size: 24px;
  font-weight: 600;
  color: #303133;
  margin: 0;
}

.page-content {
  flex: 1;
  padding: 20px;
  overflow: hidden;
}

.system-tabs {
  height: 100%;
  background: #fff;
  border-radius: 4px;
}

.tab-content {
  padding: 20px;
  height: calc(100vh - 180px);
  overflow-y: auto;
}

.table-toolbar {
  margin-bottom: 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.permission-display {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.permission-tag {
  max-width: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 4px;
}

.permission-editor {
  max-height: 500px;
  overflow-y: auto;
}

.permission-editor-header {
  margin-bottom: 16px;
  padding: 8px 12px;
  background: #f5f7fa;
  border-radius: 4px;
}

.permission-editor-header :deep(.el-breadcrumb-item) {
  cursor: pointer;
  transition: all 0.3s;
}

.permission-editor-header :deep(.el-breadcrumb-item.active) {
  font-weight: bold;
  color: #409eff;
}

.permission-editor-header :deep(.el-breadcrumb-item:not(:last-child)):hover {
  color: #66b1ff;
}

.tree-node-content {
  display: flex;
  align-items: center;
  padding: 8px 0;
}

.tree-actions {
  margin-top: 20px;
  text-align: right;
  padding: 20px;
  border-top: 1px solid #e4e7ed;
}

.system-config {
  max-width: 800px;
}

.config-actions {
  margin-top: 20px;
  text-align: center;
  padding-top: 20px;
  border-top: 1px solid #e4e7ed;
}

/* 表格样式优化 */
:deep(.el-table) {
  font-size: 14px;
}

:deep(.el-table th) {
  background-color: #f5f7fa;
  color: #606266;
  font-weight: 600;
}

/* 表格操作列按钮间距 */
:deep(.el-table-column--fixed) .el-button + .el-button {
  margin-left: 8px;
}

/* 全屏样式 */
:fullscreen {
  background: #fff;
}
</style>
