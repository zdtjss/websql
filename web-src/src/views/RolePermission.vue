<template>
  <div class="role-permission-page">
    <div class="page-content">
      <div class="role-permission-container">
        <div class="role-list-panel">
          <div class="panel-header">
            <h2>角色列表</h2>
            <el-button type="primary" size="small" @click="createRole" icon="Plus">新建角色</el-button>
          </div>
          <el-table :data="roles" style="width: 100%" highlight-current-row @current-change="handleRoleChange">
            <el-table-column prop="name" label="角色名称" />
            <el-table-column label="操作" width="80">
              <template #default="scope">
                <el-button type="danger" size="small" icon="Delete" @click.stop="deleteRole(scope.row)" />
              </template>
            </el-table-column>
          </el-table>
        </div>
        <div class="permission-config-panel">
          <div class="panel-header">
            <h2 v-if="currentRole">配置权限：{{ currentRole.name }}</h2>
            <h2 v-else>请选择角色</h2>
          </div>
          <div v-if="currentRole" class="permission-config-content">
            <div class="level-nav">
              <el-breadcrumb separator="/">
                <el-breadcrumb-item>
                  <el-button :type="currentLevel === 'conn' ? 'primary' : ''" size="small" @click="navigateToLevel('conn')">
                    <el-icon><Connection /></el-icon> 连接
                  </el-button>
                </el-breadcrumb-item>
                <el-breadcrumb-item v-if="selectedConnId">
                  <el-button :type="currentLevel === 'schema' ? 'primary' : ''" size="small" @click="navigateToLevel('schema')">
                    <el-icon><Folder /></el-icon> {{ selectedConnLabel }}
                  </el-button>
                </el-breadcrumb-item>
                <el-breadcrumb-item v-if="selectedSchema">
                  <el-button :type="currentLevel === 'table' ? 'primary' : ''" size="small" @click="navigateToLevel('table')">
                    <el-icon><Grid /></el-icon> {{ selectedSchemaLabel }}
                  </el-button>
                </el-breadcrumb-item>
                <el-breadcrumb-item v-if="selectedTable">
                  <el-button :type="currentLevel === 'column' ? 'primary' : ''" size="small" @click="navigateToLevel('column')">
                    <el-icon><Document /></el-icon> {{ selectedTableLabel }}
                  </el-button>
                </el-breadcrumb-item>
              </el-breadcrumb>
              <div class="action-buttons">
                <el-button type="primary" size="small" @click="savePermissions" :loading="saving">
                  <el-icon><Check /></el-icon> 保存
                </el-button>
                <el-button size="small" @click="cancelEdit"><el-icon><Close /></el-icon> 取消</el-button>
              </div>
            </div>
            <el-alert title="勾选复选框授予权限，点击节点名称进入下一级。上级权限包含下级（如勾选连接则拥有其下所有权限）。「子级已选」表示下级有权限配置但当前节点本身未直接授权。" type="info" :closable="false" style="margin-bottom: 12px; flex-shrink: 0;" />
            <div class="classic-view-toggle" v-if="currentLevel === 'conn'">
              <el-switch v-model="classicViewEnabled" active-text="允许使用经典视图" inactive-text="禁止使用经典视图" @change="onClassicViewChanged" />
            </div>
            <div class="permission-tree-wrapper">
              <div class="permission-tree-container">
                <el-tree ref="treeRef" :data="treeData" :props="treeProps" node-key="id" show-checkbox check-strictly :default-expand-all="true" @check="handleCheck" @node-click="handleNodeClick">
                  <template #default="{ node, data }">
                    <span class="tree-node">
                      <el-icon v-if="data.level === 'dir'"><Folder /></el-icon>
                      <el-icon v-else-if="data.level === 'conn'"><Connection /></el-icon>
                      <el-icon v-else-if="data.level === 'schema'"><Folder /></el-icon>
                      <el-icon v-else-if="data.level === 'table'"><Grid /></el-icon>
                      <el-icon v-else-if="data.level === 'column'"><Document /></el-icon>
                      <span class="node-label" :class="{ 'node-clickable': data.level !== 'column' && data.level !== 'dir' }">{{ node.label }}</span>
                      <el-tag v-if="isImplicitNode(data.id)" size="small" type="warning" class="implicit-tag">子级已选</el-tag>
                      <span v-if="data.data && data.data.comment" class="node-comment">{{ data.data.comment }}</span>
                    </span>
                  </template>
                </el-tree>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, nextTick } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage, ElMessageBox } from 'element-plus'

const roles = ref([])
const currentRole = ref(null)

const currentLevel = ref('conn')
const selectedConnId = ref('')
const selectedConnLabel = ref('')
const selectedSchema = ref('')
const selectedSchemaLabel = ref('')
const selectedTable = ref('')
const selectedTableLabel = ref('')

const treeRef = ref(null)
const treeData = ref([])
const saving = ref(false)
const treeProps = { children: 'children', label: 'label' }

// explicitKeys: 用户手动勾选的权限 key（只有这些会保存到后端）
const explicitKeys = reactive(new Set())
// implicitKeys: 因为下级有权限而需要在视觉上标记的父级 key（不保存到后端）
const implicitKeys = reactive(new Set())
// currentTreeNodeKeys: 当前树中所有节点的 key
const currentTreeNodeKeys = reactive(new Set())
// baselineCheckedKeys: 以API返回的checked状态为基准，保存时用于对比新增/删除
const baselineCheckedKeys = reactive(new Set())
// classicViewEnabled: 是否允许该角色使用经典视图
const classicViewEnabled = ref(false)
// classicViewDirty: 经典视图开关是否有变更
let classicViewDirty = false

let isProgrammatic = false

onMounted(() => {
  loadRoles()
})

function loadRoles() {
  http.get('/roleList').then(resp => {
    roles.value = resp.data.data || []
  })
}

function createRole() {
  ElMessageBox.prompt('请输入角色名称', '新建角色', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    inputPattern: /.+/,
    inputErrorMessage: '角色名称不能为空',
  }).then(({ value }) => {
    http.post('/saveRole', { id: '', name: value, addPowers: [], delPowers: [], viewClassic: 0 }).then(() => {
      loadRoles()
    })
  })
}

function handleRoleChange(role) {
  if (!role) return
  currentRole.value = role
  classicViewEnabled.value = !!(role.viewClassic)
  classicViewDirty = false
  initExplicitKeysFromRole(role)
  navigateToLevel('conn')
}

function deleteRole(role) {
  ElMessageBox.confirm(`确定要删除角色 "${role.name}" 吗？`, '确认删除', {
    confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning',
  }).then(() => {
    http.get('/delRole', { params: { id: role.id } }).then(() => {
      ElMessage.success('删除成功')
      if (currentRole.value?.id === role.id) currentRole.value = null
      loadRoles()
    })
  })
}

function initExplicitKeysFromRole(role) {
  explicitKeys.clear()
  implicitKeys.clear()
  baselineCheckedKeys.clear()
  const powers = role.powerList || []
  for (const p of powers) {
    explicitKeys.add(permToKey(p))
  }
}

function permToKey(p) {
  const parts = [p.connId]
  if (p.schemaName) parts.push(p.schemaName)
  if (p.tableName) parts.push(p.tableName)
  if (p.columnName) parts.push(p.columnName)
  return parts.join('::')
}

function navigateToLevel(level) {
  currentLevel.value = level
  if (level === 'conn') {
    selectedConnId.value = ''
    selectedConnLabel.value = ''
    selectedSchema.value = ''
    selectedSchemaLabel.value = ''
    selectedTable.value = ''
    selectedTableLabel.value = ''
  } else if (level === 'schema') {
    selectedSchema.value = ''
    selectedSchemaLabel.value = ''
    selectedTable.value = ''
    selectedTableLabel.value = ''
  } else if (level === 'table') {
    selectedTable.value = ''
    selectedTableLabel.value = ''
  }
  loadTree()
}

function loadTree() {
  const params = { level: currentLevel.value, roleId: currentRole.value?.id }
  if (selectedConnId.value) params.connId = selectedConnId.value
  if (selectedSchema.value) params.schema = selectedSchema.value
  if (selectedTable.value) params.table = selectedTable.value

  http.get('/permissionTree', { params }).then(resp => {
    treeData.value = resp.data.data || []
    currentTreeNodeKeys.clear()
    collectNodeKeys(treeData.value)
    captureBaseline(treeData.value)
    nextTick(() => syncTreeVisual())
  })
}

// 记录API返回的checked状态作为baseline
function captureBaseline(nodes) {
  for (const n of nodes) {
    if (n.checked && !n.id.startsWith('dir::')) {
      baselineCheckedKeys.add(n.id)
    }
    if (n.children && n.children.length > 0) {
      captureBaseline(n.children)
    }
  }
}

function collectNodeKeys(nodes) {
  for (const n of nodes) {
    currentTreeNodeKeys.add(n.id)
    if (n.children && n.children.length > 0) collectNodeKeys(n.children)
  }
}

function syncTreeVisual() {
  if (!treeRef.value) return
  isProgrammatic = true

  const checkedKeys = []
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (explicitKeys.has(key)) {
      checkedKeys.push(key)
    }
  }

  implicitKeys.clear()
  computeImplicitKeys()

  const allVisualKeys = [...checkedKeys]
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (implicitKeys.has(key) && !allVisualKeys.includes(key)) {
      allVisualKeys.push(key)
    }
  }

  for (const key of currentTreeNodeKeys) {
    if (!key.startsWith('dir::')) continue
    const dirNode = findNodeInTree(treeData.value, key)
    if (!dirNode || !dirNode.children || dirNode.children.length === 0) continue
    const connChildren = dirNode.children.filter(c => c.level === 'conn')
    if (connChildren.length === 0) continue
    const selectedCount = connChildren.filter(c => allVisualKeys.includes(c.id)).length
    if (selectedCount === connChildren.length) {
      allVisualKeys.push(key)
    } else if (selectedCount > 0) {
      implicitKeys.add(key)
    }
  }

  treeRef.value.setCheckedKeys(allVisualKeys)
  nextTick(() => { isProgrammatic = false })
}

function computeImplicitKeys() {
  implicitKeys.clear()
  for (const key of explicitKeys) {
    const ancestors = getAncestorKeys(key)
    for (const ancestor of ancestors) {
      if (!explicitKeys.has(ancestor) && currentTreeNodeKeys.has(ancestor)) {
        implicitKeys.add(ancestor)
      }
    }
  }
}

function getAncestorKeys(key) {
  if (key.startsWith('dir::')) return []
  const parts = key.split('::')
  const ancestors = []
  for (let i = parts.length - 1; i >= 1; i--) {
    ancestors.push(parts.slice(0, i).join('::'))
  }
  return ancestors
}

function findNodeInTree(nodes, id) {
  for (const n of nodes) {
    if (n.id === id) return n
    if (n.children) {
      const found = findNodeInTree(n.children, id)
      if (found) return found
    }
  }
  return null
}

function isImplicitNode(nodeId) {
  return implicitKeys.has(nodeId)
}

function onClassicViewChanged() {
  classicViewDirty = true
}

function handleCheck(nodeData, checkState) {
  if (isProgrammatic) return
  if (nodeData.id.startsWith('dir::')) {
    if (nodeData.children) {
      const isNowChecked = checkState.checkedKeys.includes(nodeData.id)
      for (const child of nodeData.children) {
        if (child.level === 'conn') {
          if (isNowChecked) {
            explicitKeys.add(child.id)
          } else {
            explicitKeys.delete(child.id)
            removeDescendantKeys(child.id)
          }
        }
      }
    }
    nextTick(() => syncTreeVisual())
    return
  }

  const isNowChecked = checkState.checkedKeys.includes(nodeData.id)
  if (isNowChecked) {
    explicitKeys.add(nodeData.id)
  } else {
    if (explicitKeys.has(nodeData.id)) {
      explicitKeys.delete(nodeData.id)
    } else {
      removeDescendantKeys(nodeData.id)
    }
  }
  nextTick(() => syncTreeVisual())
}

function removeDescendantKeys(parentKey) {
  const prefix = parentKey + '::'
  const toRemove = []
  for (const key of explicitKeys) {
    if (key.startsWith(prefix)) {
      toRemove.push(key)
    }
  }
  for (const key of toRemove) {
    explicitKeys.delete(key)
  }
}

function handleNodeClick(nodeData) {
  if (nodeData.level === 'dir') return
  if (nodeData.level === 'conn') {
    selectedConnId.value = nodeData.id
    selectedConnLabel.value = nodeData.label
    navigateToLevel('schema')
  } else if (nodeData.level === 'schema') {
    selectedSchema.value = nodeData.id
    selectedSchemaLabel.value = nodeData.label
    navigateToLevel('table')
  } else if (nodeData.level === 'table') {
    selectedTable.value = nodeData.id
    selectedTableLabel.value = nodeData.label
    navigateToLevel('column')
  }
}

function savePermissions() {
  if (!currentRole.value) return
  saving.value = true

  const currentPowers = []
  for (const key of explicitKeys) {
    if (key.startsWith('dir::')) continue
    const parts = key.split('::')
    const connId = parts[0]
    const schema = parts[1] || ''
    const table = parts[2] || ''
    const column = parts[3] || ''
    let level = 'conn'
    if (column) level = 'column'
    else if (table) level = 'table'
    else if (schema) level = 'schema'

    currentPowers.push({
      connId,
      connName: null,
      schemaName: schema || null,
      tableName: table || null,
      columnName: column || null,
      level,
    })
  }

  const schemaKeysWithTableChildren = new Set()
  for (const key of explicitKeys) {
    if (key.startsWith('dir::')) continue
    const parts = key.split('::')
    if (parts.length >= 3 && parts[2]) {
      schemaKeysWithTableChildren.add(parts[0] + '::' + parts[1])
    }
  }
  const filteredPowers = currentPowers.filter(p => {
    if (p.level === 'schema' && p.schemaName) {
      const schemaKey = p.connId + '::' + p.schemaName
      if (schemaKeysWithTableChildren.has(schemaKey)) {
        return false
      }
    }
    return true
  })

  const newKeySet = new Set(filteredPowers.map(permToKey))

  // 与baseline对比：baseline中不存在 → 新增；baseline中存在但新key中不存在 → 删除
  const addPowers = filteredPowers.filter(p => {
    const key = permToKey(p)
    return !baselineCheckedKeys.has(key)
  })

  const delPowers = []
  for (const id of baselineCheckedKeys) {
    if (!newKeySet.has(id)) {
      const parts = id.split('::')
      const connId = parts[0]
      const schema = parts[1] || null
      const table = parts[2] || null
      const column = parts[3] || null
      let level = 'conn'
      if (column) level = 'column'
      else if (table) level = 'table'
      else if (schema) level = 'schema'

      delPowers.push({
        id: '',
        roleId: '',
        connId,
        connName: null,
        schemaName: schema,
        tableName: table,
        columnName: column,
        level,
      })
    }
  }

  const roleData = {
    id: currentRole.value.id,
    name: currentRole.value.name,
    addPowers,
    delPowers,
    viewClassic: classicViewEnabled.value ? 1 : 0,
  }

  http.post('/saveRole', roleData).then(() => {
    saving.value = false
    classicViewDirty = false
    currentRole.value.powerList = filteredPowers
    currentRole.value.viewClassic = classicViewEnabled.value ? 1 : 0
    loadRoles()
    ElMessage.success('保存成功')
  }).catch(() => {
    saving.value = false
  })
}

function cancelEdit() {
  if (currentRole.value) {
    classicViewEnabled.value = !!(currentRole.value.viewClassic)
    initExplicitKeysFromRole(currentRole.value)
    loadTree()
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
  height: 100%;
}
.level-nav {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid #e4e7ed;
  flex-shrink: 0;
}
.action-buttons {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.classic-view-toggle {
  padding: 8px 0;
  flex-shrink: 0;
}
.permission-tree-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}
.permission-tree-container {
  flex: 1;
  overflow-y: auto;
  overflow-x: auto;
}
.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
}
.node-label {
  flex-shrink: 0;
}
.node-label.node-clickable {
  cursor: pointer;
  color: #409eff;
}
.node-label.node-clickable:hover {
  text-decoration: underline;
}
.node-comment {
  font-size: 12px;
  color: #909399;
  margin-left: 8px;
}
.implicit-tag {
  margin-left: 8px;
  font-size: 11px;
}
:deep(.el-tree) .is-checked .el-checkbox__inner {
  background-color: #409eff;
  border-color: #409eff;
}
:deep(.el-table) {
  font-size: 14px;
}
:deep(.el-table .el-button) {
  padding: 4px 8px;
}
:deep(.el-breadcrumb) {
  display: flex;
  align-items: center;
}
</style>
