<template>
  <div class="role-permission-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-left">
        <el-button @click="goBack" icon="ArrowLeft" circle />
        <h1 class="page-title">角色权限管理</h1>
      </div>
    </div>

    <!-- 主内容区 -->
    <div class="page-content">
      <div class="role-permission-container">
        <!-- 左侧：角色列表 -->
        <div class="role-list-panel">
          <div class="panel-header">
            <h2>角色列表</h2>
            <el-button type="primary" size="small" @click="createRole" icon="Plus">
              新建角色
            </el-button>
          </div>
          
          <el-table 
            :data="roles" 
            style="width: 100%" 
            highlight-current-row
            @current-change="handleRoleChange"
          >
            <el-table-column prop="name" label="角色名称" />
            <el-table-column label="操作" width="80">
              <template #default="scope">
                <el-button 
                  type="danger" 
                  size="small" 
                  icon="Delete"
                  @click="deleteRole(scope.row)"
                />
              </template>
            </el-table-column>
          </el-table>
        </div>

        <!-- 中间：权限配置 -->
        <div class="permission-config-panel">
          <div class="panel-header">
            <h2 v-if="currentRole">配置权限：{{ currentRole.name }}</h2>
            <h2 v-else>请选择角色</h2>
          </div>
          
          <div v-if="currentRole" class="permission-config-content">
            <!-- 层级导航 -->
            <div class="level-nav">
              <el-button 
                :type="currentLevel === 'conn' ? 'primary' : ''" 
                size="small"
                @click="navigateToLevel('conn')"
              >
                <el-icon><Connection /></el-icon>
                连接
              </el-button>
              <el-button 
                :type="currentLevel === 'schema' ? 'primary' : ''" 
                size="small"
                :disabled="!selectedConnId"
                @click="navigateToLevel('schema')"
              >
                <el-icon><Folder /></el-icon>
                Schema
              </el-button>
              <el-button 
                :type="currentLevel === 'table' ? 'primary' : ''" 
                size="small"
                :disabled="!selectedSchema"
                @click="navigateToLevel('table')"
              >
                <el-icon><Grid /></el-icon>
                表
              </el-button>
              <el-button 
                :type="currentLevel === 'column' ? 'primary' : ''" 
                size="small"
                :disabled="!selectedTable"
                @click="navigateToLevel('column')"
              >
                <el-icon><Document /></el-icon>
                字段
              </el-button>
            </div>

            <!-- 权限树 -->
            <div class="permission-tree-container">
              <el-alert
                v-if="currentLevel === 'conn'"
                title="提示"
                type="info"
                :closable="false"
                style="margin-bottom: 12px;"
              >
                <el-icon><InfoFilled /></el-icon>
                点击节点可进入下一级，勾选复选框授予权限
              </el-alert>
              
              <el-tree
                ref="permissionTree"
                :data="permissionTreeData"
                :props="treeProps"
                node-key="id"
                show-checkbox
                :default-expand-all="false"
                @check-change="handleCheckChange"
                @node-click="handleNodeClick"
              >
                <template #default="{ node, data }">
                  <span class="tree-node">
                    <el-icon v-if="data.level === 'conn'"><Connection /></el-icon>
                    <el-icon v-else-if="data.level === 'schema'"><Folder /></el-icon>
                    <el-icon v-else-if="data.level === 'table'"><Grid /></el-icon>
                    <el-icon v-else-if="data.level === 'column'"><Document /></el-icon>
                    <span class="node-label">{{ node.label }}</span>
                    <span v-if="data.data?.comment" class="node-comment">
                      {{ data.data.comment }}
                    </span>
                  </span>
                </template>
              </el-tree>
            </div>

            <!-- 操作按钮 -->
            <div class="action-buttons">
              <el-button type="primary" @click="savePermissions" :loading="saving">
                <el-icon><Check /></el-icon>
                保存权限配置
              </el-button>
              <el-button @click="cancelEdit">
                <el-icon><Close /></el-icon>
                取消
              </el-button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import http from '@/js/utils/httpProxy'
import { ElMessage, ElMessageBox } from 'element-plus'

const router = useRouter()

// 数据
const roles = ref([])
const currentRole = ref(null)
const currentLevel = ref('conn')
const permissionTreeData = ref([])
const saving = ref(false)
const permissionTree = ref(null)

// 当前选中状态
const selectedConnId = ref('')
const selectedSchema = ref('')
const selectedTable = ref('')

// 维护所有选中的 keys（包括不在当前树中的父级节点）
const allCheckedKeys = ref([])

// 树配置
const treeProps = {
  children: 'children',
  label: 'label',
  level: 'level'
}

// 生命周期
onMounted(() => {
  loadRoles()
})

// 方法
function goBack() {
  router.push('/')
}

function loadRoles() {
  http.get('/roleList').then(resp => {
    roles.value = resp.data.data || []
  })
}

// 将权限对象转换为可比较的字符串 key
function permToKey(p) {
  const parts = [p.connId]
  if (p.schemaName) parts.push(p.schemaName)
  if (p.tableName) parts.push(p.tableName)
  if (p.columnName) parts.push(p.columnName)
  return parts.join('::')
}

function createRole() {
  ElMessageBox.prompt('请输入角色名称', '新建角色', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    inputPattern: /.+/,
    inputErrorMessage: '角色名称不能为空',
  }).then(({ value }) => {
    const newRole = {
      id: '',
      name: value,
      powerList: []
    }
    saveRoleData(newRole).then(() => {
      loadRoles()
    })
  })
}

function handleRoleChange(role) {
  if (role) {
    currentRole.value = role
    navigateToLevel('conn')
  }
}

function deleteRole(role) {
  ElMessageBox.confirm(
    `确定要删除角色 "${role.name}" 吗？`,
    '确认删除',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    }
  ).then(() => {
    http.get('/delRole', { params: { id: role.id } }).then(() => {
      ElMessage.success('删除成功')
      if (currentRole.value?.id === role.id) {
        currentRole.value = null
      }
      loadRoles()
    })
  })
}

function navigateToLevel(level) {
  currentLevel.value = level
  
  // 重置下级选择
  if (level === 'conn') {
    selectedConnId.value = ''
    selectedSchema.value = ''
    selectedTable.value = ''
  } else if (level === 'schema') {
    selectedSchema.value = ''
    selectedTable.value = ''
  } else if (level === 'table') {
    selectedTable.value = ''
  }
  
  // 清空当前树的选中状态，但保留 allCheckedKeys
  if (permissionTree.value) {
    permissionTree.value.setCheckedKeys([])
  }
  
  loadPermissionTree()
}

function loadPermissionTree() {
  const params = { 
    level: currentLevel.value,
    roleId: currentRole.value?.id
  }
  
  if (selectedConnId.value) params.connId = selectedConnId.value
  if (selectedSchema.value) params.schema = selectedSchema.value
  if (selectedTable.value) params.table = selectedTable.value
  
  http.get('/permissionTree', { params }).then(resp => {
    permissionTreeData.value = resp.data.data || []
    nextTick(() => {
      syncTreeCheckedState()
    })
  })
}

function handleNodeClick(node) {
  if (node.level === 'conn') {
    selectedConnId.value = node.id
    navigateToLevel('schema')
  } else if (node.level === 'schema') {
    selectedSchema.value = node.id
    navigateToLevel('table')
  } else if (node.level === 'table') {
    selectedTable.value = node.id
    navigateToLevel('column')
  }
}

// 处理节点勾选状态变化
function handleCheckChange(nodeData, checked) {
  console.log('handleCheckChange 被调用:', {
    nodeId: nodeData.id,
    checked: checked,
    nodeLevel: nodeData.level
  })
  
  if (checked) {
    // 选中时，自动选中所有父级节点（向上传递）
    const currentKey = nodeData.id
    const parentKeys = getParentKeys(currentKey)
    
    console.log('父级节点 keys:', parentKeys)
    
    // 获取当前树中已选中的 keys
    const currentCheckedKeys = permissionTree.value.getCheckedKeys() || []
    console.log('当前树中选中的 keys:', currentCheckedKeys)
    
    // 合并当前选中的 key 和所有父级 key
    const allKeys = new Set([...currentCheckedKeys, ...parentKeys])
    console.log('合并后的 keys:', allKeys)
    
    // 更新全局选中 keys
    allCheckedKeys.value = Array.from(allKeys)
    console.log('allCheckedKeys:', allCheckedKeys.value)
    
    // 使用 setCheckedKeys 批量设置（只设置当前树中存在的 keys）
    permissionTree.value.setCheckedKeys(currentCheckedKeys)
  } else {
    // 取消选中时，从全局选中 keys 中移除
    const currentKey = nodeData.id
    allCheckedKeys.value = allCheckedKeys.value.filter(key => key !== currentKey)
  }
}

// 获取当前节点的所有父级节点 key
// 节点 ID 格式：connId::schema::table::column
function getParentKeys(key) {
  const parts = key.split('::')
  const parents = []
  
  // 逐级向上构建父级节点的 key
  // 例如：conn1::schema1::table1::col1 的父级有：
  // - conn1::schema1::table1 (table 级)
  // - conn1::schema1 (schema 级)
  // - conn1 (conn 级)
  for (let i = parts.length - 1; i > 0; i--) {
    parents.push(parts.slice(0, i).join('::'))
  }
  
  return parents
}

// 同步树的勾选状态
function syncTreeCheckedState() {
  const treeRef = permissionTree.value
  if (!treeRef) return
  
  // 从后端返回的节点数据中提取所有已勾选的节点 key
  const extractCheckedKeys = (nodes) => {
    const keys = []
    const traverse = (nodeList) => {
      for (const node of nodeList) {
        if (node.checked) {
          keys.push(node.id)
        }
        if (node.children && node.children.length > 0) {
          traverse(node.children)
        }
      }
    }
    traverse(nodes)
    return keys
  }
  
  const checkedKeys = extractCheckedKeys(permissionTreeData.value)
  
  // 同步到全局选中 keys
  allCheckedKeys.value = [...new Set([...allCheckedKeys.value, ...checkedKeys])]
  
  // 设置树的选中状态
  if (checkedKeys.length > 0) {
    treeRef.setCheckedKeys(checkedKeys)
  }
}

function getConnNameById(connId) {
  // 在权限树数据中查找连接名称
  const findConn = (nodes) => {
    for (const node of nodes) {
      if (node.id === connId) return node.label
      if (node.children) {
        const found = findConn(node.children)
        if (found) return found
      }
    }
    return null
  }
  return findConn(permissionTreeData.value) || ''
}

function findConnById(connId) {
  const findInTree = (nodes) => {
    for (const node of nodes) {
      if (node.id === connId) return node
      if (node.children) {
        const found = findInTree(node.children)
        if (found) return found
      }
    }
    return null
  }
  return findInTree(permissionTreeData.value)
}

function savePermissions() {
  if (!currentRole.value) return
  
  saving.value = true
  
  // 使用全局选中的 keys（包括父级节点）
  const checkedKeys = allCheckedKeys.value || []
  
  // 调试信息：查看选中的 keys
  console.log('选中的 keys:', checkedKeys)
  
  // 从勾选的节点构建权限列表
  const newPowers = []
  for (const key of checkedKeys) {
    const parts = key.split('::')
    const connId = parts[0]
    const schema = parts[1] || ''
    const table = parts[2] || ''
    const column = parts[3] || ''
    
    let level = 'conn'
    if (column) level = 'column'
    else if (table) level = 'table'
    else if (schema) level = 'schema'
    
    const connName = getConnNameById(connId)
    
    newPowers.push({
      connId,
      connName,
      schemaName: schema || null,
      tableName: table || null,
      columnName: column || null,
      level
    })
  }
  
  // 调试信息：查看构建的权限列表
  console.log('构建的权限列表:', newPowers)
  
  // 计算增量：新选中的作为 addPowers，原选中本次被取消的作为 delPowers
  const oldPowers = currentRole.value.powerList || []
  const oldKeys = new Set(oldPowers.map(permToKey))
  const newKeys = new Set(newPowers.map(permToKey))
  
  const addPowers = newPowers.filter(p => !oldKeys.has(permToKey(p)))
  const delPowers = oldPowers.filter(p => !newKeys.has(permToKey(p)))
  
  // 调试信息：查看增量
  console.log('addPowers:', addPowers)
  console.log('delPowers:', delPowers)
  
  const roleData = {
    id: currentRole.value.id,
    name: currentRole.value.name,
    addPowers,
    delPowers
  }
  
  saveRoleData(roleData).then(() => {
    saving.value = false
    currentRole.value.powerList = newPowers
    loadRoles()
    ElMessage.success('保存成功')
  }).catch(() => {
    saving.value = false
  })
}

function saveRoleData(roleData) {
  return http.post('/saveRole', roleData)
}

function cancelEdit() {
  // 取消编辑，重新加载角色权限
  if (currentRole.value) {
    loadPermissionTree()
  }
}
</script>

<style scoped>
.role-permission-page {
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

.role-permission-container {
  display: grid;
  grid-template-columns: 300px 1fr;
  gap: 20px;
  height: 100%;
}

.role-list-panel,
.permission-config-panel {
  background: #fff;
  border-radius: 8px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.panel-header {
  padding: 16px 20px;
  border-bottom: 1px solid #e4e7ed;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.panel-header h2 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
  color: #303133;
}

.permission-config-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 20px;
  overflow: hidden;
}

.level-nav {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid #e4e7ed;
}

.permission-tree-container {
  flex: 1;
  overflow-y: auto;
  margin-bottom: 20px;
}

.action-buttons {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  padding-top: 16px;
  border-top: 1px solid #e4e7ed;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
}

.node-label {
  flex: 1;
}

.node-comment {
  font-size: 12px;
  color: #909399;
  margin-left: 8px;
}

:deep(.el-table) {
  font-size: 14px;
}

:deep(.el-table .el-button) {
  padding: 4px 8px;
}
</style>
