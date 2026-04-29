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
                      <el-tooltip v-if="data.level !== 'column' && data.level !== 'dir'" :content="treeVisibleKeys.has(data.id) ? '在树中可见' : '在树中隐藏'" placement="top">
                        <el-icon class="tree-vis-toggle" :class="{ 'tree-vis-on': treeVisibleKeys.has(data.id) }" @click.stop="toggleTreeVisible(data)">
                          <View v-if="treeVisibleKeys.has(data.id)" />
                          <Hide v-else />
                        </el-icon>
                      </el-tooltip>
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

// ─── 角色列表 ───
const roles = ref([])
const currentRole = ref(null)

// ─── 层级导航状态 ───
const currentLevel = ref('conn')
const selectedConnId = ref('')
const selectedConnLabel = ref('')
const selectedSchema = ref('')
const selectedSchemaLabel = ref('')
const selectedTable = ref('')
const selectedTableLabel = ref('')

// ─── 树相关 ───
const treeRef = ref(null)
const treeData = ref([])
const saving = ref(false)
const treeProps = { children: 'children', label: 'label' }

// ─── 核心权限状态 ───
// explicitKeys: 用户手动勾选的权限 key（只有这些会保存到后端）
// 格式: connId / connId::schema / connId::schema::table / connId::schema::table::column
const explicitKeys = reactive(new Set())

// implicitKeys: 因为下级有权限而需要在视觉上标记的父级 key（不保存到后端）
const implicitKeys = reactive(new Set())

// 当前树中所有节点的 key（用于增量保存时判断范围）
const currentTreeNodeKeys = reactive(new Set())

// treeVisibleKeys: 树可见性标记（仅 conn/schema/table 层级）
const treeVisibleKeys = reactive(new Set())

// 防止程序设置 checkbox 时触发 handleCheck
let isProgrammatic = false

// ─── 生命周期 ───
onMounted(() => {
  loadRoles()
})

// ─── 角色管理 ───
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
    http.post('/saveRole', { id: '', name: value, addPowers: [], delPowers: [] }).then(() => {
      loadRoles()
    })
  })
}

function handleRoleChange(role) {
  if (!role) return
  currentRole.value = role
  // 从 role.powerList 初始化 explicitKeys
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

// ─── 从角色的 powerList 初始化 explicitKeys ───
function initExplicitKeysFromRole(role) {
  explicitKeys.clear()
  implicitKeys.clear()
  treeVisibleKeys.clear()
  const powers = role.powerList || []
  for (const p of powers) {
    explicitKeys.add(permToKey(p))
    if (p.treeVisible) {
      treeVisibleKeys.add(permToKey(p))
    }
  }
}

function permToKey(p) {
  const parts = [p.connId]
  if (p.schemaName) parts.push(p.schemaName)
  if (p.tableName) parts.push(p.tableName)
  if (p.columnName) parts.push(p.columnName)
  return parts.join('::')
}

// ─── 层级导航 ───
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

// ─── 加载权限树 ───
function loadTree() {
  const params = { level: currentLevel.value, roleId: currentRole.value?.id }
  if (selectedConnId.value) params.connId = selectedConnId.value
  if (selectedSchema.value) params.schema = selectedSchema.value
  if (selectedTable.value) params.table = selectedTable.value

  http.get('/permissionTree', { params }).then(resp => {
    treeData.value = resp.data.data || []
    currentTreeNodeKeys.clear()
    collectNodeKeys(treeData.value)
    mergeCheckedFromApi(treeData.value)
    nextTick(() => syncTreeVisual())
  })
}

function mergeCheckedFromApi(nodes) {
  for (const n of nodes) {
    if (n.checked && !n.id.startsWith('dir::') && !explicitKeys.has(n.id)) {
      explicitKeys.add(n.id)
    }
    if (n.treeVisible && !treeVisibleKeys.has(n.id)) {
      treeVisibleKeys.add(n.id)
    }
    if (n.children && n.children.length > 0) {
      mergeCheckedFromApi(n.children)
    }
  }
}

function collectNodeKeys(nodes) {
  for (const n of nodes) {
    currentTreeNodeKeys.add(n.id)
    if (n.children && n.children.length > 0) collectNodeKeys(n.children)
  }
}

// ─── 同步树的视觉状态 ───
// 根据 explicitKeys 计算当前树中哪些节点应该被勾选（explicit）或标记（implicit）
function syncTreeVisual() {
  if (!treeRef.value) return
  isProgrammatic = true

  // 1. 计算当前树中应该勾选的节点（explicit 的）
  const checkedKeys = []
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (explicitKeys.has(key)) {
      checkedKeys.push(key)
    }
  }

  // 2. 计算 implicit 节点（当前树中的节点，自身不在 explicitKeys 中，但有下级在 explicitKeys 中）
  implicitKeys.clear()
  computeImplicitKeys()

  // 3. 对于 implicit 的节点，也需要勾选（视觉上显示为选中），但会有「子级已选」标签区分
  const allVisualKeys = [...checkedKeys]
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue // dir 节点不勾选
    if (implicitKeys.has(key) && !allVisualKeys.includes(key)) {
      allVisualKeys.push(key)
    }
  }

  // 4. dir 节点处理：
  //    - 所有子连接都被选中（explicit 或 implicit）→ 勾选 dir
  //    - 部分子连接被选中 → 不勾选 dir，但标记为 implicit（显示「子级已选」）
  //    - 无子连接被选中 → 不勾选 dir
  for (const key of currentTreeNodeKeys) {
    if (!key.startsWith('dir::')) continue
    const dirNode = findNodeInTree(treeData.value, key)
    if (!dirNode || !dirNode.children || dirNode.children.length === 0) continue
    const connChildren = dirNode.children.filter(c => c.level === 'conn')
    if (connChildren.length === 0) continue
    const selectedCount = connChildren.filter(c => allVisualKeys.includes(c.id)).length
    if (selectedCount === connChildren.length) {
      // 全选 → 勾选 dir
      allVisualKeys.push(key)
    } else if (selectedCount > 0) {
      // 部分选中 → 标记为 implicit（不勾选，只显示标签）
      implicitKeys.add(key)
    }
  }

  treeRef.value.setCheckedKeys(allVisualKeys)
  nextTick(() => { isProgrammatic = false })
}

// 计算 implicit keys：当前树中的节点，自身不在 explicitKeys，但其下级路径有 explicitKeys
function computeImplicitKeys() {
  implicitKeys.clear()
  // 遍历所有 explicitKeys，对每个 key 向上推导其所有祖先
  for (const key of explicitKeys) {
    const ancestors = getAncestorKeys(key)
    for (const ancestor of ancestors) {
      // 只有祖先自身不在 explicitKeys 中，才算 implicit
      if (!explicitKeys.has(ancestor) && currentTreeNodeKeys.has(ancestor)) {
        implicitKeys.add(ancestor)
      }
    }
  }
}

// 根据 key 格式推导所有祖先 key
// connId::schema::table::column -> [connId::schema::table, connId::schema, connId]
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

// ─── 判断节点是否为 implicit（子级已选但自身未直接授权）───
function isImplicitNode(nodeId) {
  return implicitKeys.has(nodeId)
}

function toggleTreeVisible(nodeData) {
  if (treeVisibleKeys.has(nodeData.id)) {
    treeVisibleKeys.delete(nodeData.id)
  } else {
    treeVisibleKeys.add(nodeData.id)
  }
}

// ─── 处理用户勾选 ───
function handleCheck(nodeData, checkState) {
  if (isProgrammatic) return
  // dir 节点不作为权限
  if (nodeData.id.startsWith('dir::')) {
    // 如果勾选了 dir，则勾选其下所有 conn 子节点
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
    // 取消勾选：如果节点本身在 explicitKeys 中，只移除自身
    // 如果节点是 implicit 的（自身不在 explicitKeys，但因子级而显示），则移除所有下级
    if (explicitKeys.has(nodeData.id)) {
      explicitKeys.delete(nodeData.id)
    } else {
      // implicit 节点被取消 → 移除所有下级权限
      removeDescendantKeys(nodeData.id)
    }
  }
  nextTick(() => syncTreeVisual())
}

// 移除某个 key 的所有下级 explicitKeys
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

// ─── 节点点击（进入下一级）───
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

// ─── 保存权限 ───
function savePermissions() {
  if (!currentRole.value) return
  saving.value = true

  // 从 explicitKeys 构建当前层级范围内的权限列表
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
      treeVisible: treeVisibleKeys.has(key) ? 1 : 0,
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

  const oldPowers = currentRole.value.powerList || []
  const oldKeySet = new Set(oldPowers.map(permToKey))
  const newKeySet = new Set(filteredPowers.map(permToKey))

  const storedKeys = new Set()

  const addPowers = filteredPowers.filter(p => {
    const key = permToKey(p)
    storedKeys.add(key)
    if (!oldKeySet.has(key)) return true
    // tree_visible 变更：需要删旧插新
    const oldP = oldPowers.find(op => permToKey(op) === key)
    if (oldP && (oldP.treeVisible ? 1 : 0) !== (p.treeVisible ? 1 : 0)) {
      return true
    }
    return false
  })
  const delPowers = []
  for (const p of oldPowers) {
    const key = permToKey(p)
    if (!newKeySet.has(key)) {
      delPowers.push(p)
    } else if (storedKeys.has(key)) {
      // key 在 add 中已处理（tree_visible 变更），需要删除旧的
      delPowers.push(p)
    }
  }

  const roleData = {
    id: currentRole.value.id,
    name: currentRole.value.name,
    addPowers,
    delPowers,
  }

  http.post('/saveRole', roleData).then(() => {
    saving.value = false
    // 更新本地角色的 powerList
    currentRole.value.powerList = filteredPowers
    loadRoles()
    ElMessage.success('保存成功')
  }).catch(() => {
    saving.value = false
  })
}

function cancelEdit() {
  if (currentRole.value) {
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
.tree-vis-toggle {
  margin-left: 8px;
  cursor: pointer;
  color: #909399;
  font-size: 16px;
}
.tree-vis-toggle:hover {
  color: #409eff;
}
.tree-vis-toggle.tree-vis-on {
  color: #409eff;
}
/* implicit 节点的 checkbox 用半选样式区分 */
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
