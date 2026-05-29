<template>
  <div class="role-permission-page">
    <div class="page-content">
      <div class="role-permission-container">
        <div class="role-list-panel">
          <div class="panel-header">
            <h2>角色列表</h2>
            <el-button type="primary" size="small" @click="createRole" :icon="Plus">新建角色</el-button>
          </div>
          <el-table :data="roles" style="width: 100%" highlight-current-row @current-change="handleRoleChange">
            <el-table-column prop="name" label="角色名称" resizable />
            <el-table-column label="操作" width="80" resizable>
              <template #default="scope">
                <el-button type="danger" link size="small" :icon="Delete" @click.stop="deleteRole(scope.row)" />
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
            <div class="classic-view-toggle" v-if="currentLevel === 'conn'">
              <el-switch v-model="modifyDataEnabled" active-text="允许修改数据" inactive-text="禁止修改数据" @change="onModifyDataChanged" />
            </div>
            <div class="permission-tree-wrapper">
              <div class="permission-tree-container">
                <el-tree ref="treeRef" :key="treeKey" :data="treeData" :props="treeProps" node-key="id" show-checkbox check-strictly :default-expand-all="true" :default-checked-keys="treeCheckedKeys" :check-on-click-leaf="false" @check="handleCheck" @node-click="handleNodeClick">
                  <template #default="{ node, data }">
                    <span class="tree-node">
                      <el-icon v-if="data.level === 'dir'"><Folder /></el-icon>
                      <el-icon v-else-if="data.level === 'conn'"><Connection /></el-icon>
                      <el-icon v-else-if="data.level === 'schema'"><Folder /></el-icon>
                      <el-icon v-else-if="data.level === 'table'"><Grid /></el-icon>
                      <el-icon v-else-if="data.level === 'column'"><Document /></el-icon>
                      <span class="node-label" :class="{ 'node-clickable': data.level !== 'column' && data.level !== 'dir' }">{{ node.label }}</span>
                      <el-tag v-if="isImplicitNode(getNodeFullKey(data))" size="small" type="warning" class="implicit-tag">子级已选</el-tag>
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
import { ref, reactive, onMounted, nextTick, useTemplateRef } from 'vue'
import { Delete, Plus } from '@element-plus/icons-vue'
import http from '@/utils/httpProxy'
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

const treeRef = useTemplateRef('treeRef')
const treeData = ref([])
const treeKey = ref(0) // 递增 key，强制 el-tree 重建以确保 default-checked-keys 生效
const treeCheckedKeys = ref([]) // 计算好的勾选 key 列表，传给 default-checked-keys
const saving = ref(false)
const treeProps = { children: 'children', label: 'label' }

// explicitKeys: 用户手动勾选的权限 key（只有这些会保存到后端）
const explicitKeys = reactive(new Set())
// implicitKeys: 因为下级有权限而需要在视觉上标记的父级 key（不保存到后端）
const implicitKeys = reactive(new Set())
// currentTreeNodeKeys: 当前树中所有节点的 key
const currentTreeNodeKeys = reactive(new Set())
// baselineCheckedKeys: 保存时用于对比新增/删除的基准（只来自 role.powerList）
const baselineCheckedKeys = reactive(new Set())
// serverCheckedKeys: API 返回的 checked=true 状态（包含向上传播的权限），用于显示
const serverCheckedKeys = reactive(new Set())
// uncheckedKeys: 用户显式取消勾选的 key（防止被 serverCheckedKeys 重新勾选）
const uncheckedKeys = reactive(new Set())
// classicViewEnabled: 是否允许该角色使用经典视图
const classicViewEnabled = ref(false)
// classicViewDirty: 经典视图开关是否有变更
let classicViewDirty = false
// modifyDataEnabled: 是否允许该角色修改数据
const modifyDataEnabled = ref(true)
// modifyDataDirty: 修改数据开关是否有变更
let modifyDataDirty = false

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
    http.post('/saveRole', { id: '', name: value, addPowers: [], delPowers: [], viewClassic: 0, allowModify: 1 }).then(() => {
      loadRoles()
    })
  })
}

function handleRoleChange(role) {
  if (!role) return
  currentRole.value = role
  classicViewEnabled.value = !!(role.viewClassic)
  classicViewDirty = false
  modifyDataEnabled.value = !!(role.allowModify)
  modifyDataDirty = false
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
  uncheckedKeys.clear()
  const powers = role.powerList || []
  for (const p of powers) {
    const key = permToKey(p)
    explicitKeys.add(key)
    baselineCheckedKeys.add(key)
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
    captureServerChecked(treeData.value)
    // 计算应该勾选的 keys，然后通过递增 treeKey 强制 el-tree 重建
    // 重建时 default-checked-keys 会在树初始化阶段直接生效，不存在时序问题
    computeAndApplyCheckedKeys()
  })
}

function computeAndApplyCheckedKeys() {
  // 使用 explicitKeys 和 serverCheckedKeys 的并集来决定显示状态
  // 但要排除用户显式取消勾选的 key
  const combinedKeys = new Set()
  for (const key of explicitKeys) {
    combinedKeys.add(key)
  }
  for (const key of serverCheckedKeys) {
    if (!uncheckedKeys.has(key)) {
      combinedKeys.add(key)
    }
  }

  const checkedKeys = []
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (combinedKeys.has(key)) {
      checkedKeys.push(key)
    }
  }

  implicitKeys.clear()
  computeImplicitKeysFromCombined(combinedKeys)

  const allVisualKeys = [...checkedKeys]
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (implicitKeys.has(key) && !allVisualKeys.includes(key)) {
      allVisualKeys.push(key)
    }
  }

  // 处理 dir 节点的全选状态
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

  console.log('[computeAndApplyCheckedKeys] allVisualKeys:', allVisualKeys)

  // 设置 default-checked-keys 并递增 key 强制重建树
  treeCheckedKeys.value = allVisualKeys
  treeKey.value++
}

function getNodeFullKey(n) {
  return n.id
}

function captureServerChecked(nodes) {
  serverCheckedKeys.clear()
  _collectServerChecked(nodes)
}

function _collectServerChecked(nodes) {
  for (const n of nodes) {
    if (n.checked && !n.id.startsWith('dir::')) {
      serverCheckedKeys.add(getNodeFullKey(n))
    }
    if (n.children && n.children.length > 0) {
      _collectServerChecked(n.children)
    }
  }
}

function collectNodeKeys(nodes) {
  for (const n of nodes) {
    currentTreeNodeKeys.add(getNodeFullKey(n))
    if (n.children && n.children.length > 0) collectNodeKeys(n.children)
  }
}

function syncTreeVisual() {
  if (!treeRef.value) return
  isProgrammatic = true

  // 使用 explicitKeys 和 serverCheckedKeys 的并集来决定显示状态
  // 但要排除用户显式取消勾选的 key
  const combinedKeys = new Set()
  for (const key of explicitKeys) {
    combinedKeys.add(key)
  }
  for (const key of serverCheckedKeys) {
    if (!uncheckedKeys.has(key)) {
      combinedKeys.add(key)
    }
  }

  const checkedKeys = []
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (combinedKeys.has(key)) {
      checkedKeys.push(key)
    }
  }

  implicitKeys.clear()
  computeImplicitKeysFromCombined(combinedKeys)

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

  // 用户交互后同步视觉状态，此时树节点已存在，setCheckedKeys 可靠
  treeRef.value.setCheckedKeys(allVisualKeys)
  nextTick(() => { isProgrammatic = false })
}

function computeImplicitKeysFromCombined(combinedKeys) {
  implicitKeys.clear()
  for (const key of combinedKeys) {
    const ancestors = getAncestorKeys(key)
    for (const ancestor of ancestors) {
      if (!combinedKeys.has(ancestor) && currentTreeNodeKeys.has(ancestor)) {
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

function findNodeInTree(nodes, key) {
  for (const n of nodes) {
    if (getNodeFullKey(n) === key) return n
    if (n.children) {
      const found = findNodeInTree(n.children, key)
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

function onModifyDataChanged() {
  modifyDataDirty = true
}

function handleCheck(nodeData, checkState) {
  if (isProgrammatic) { console.log('[handleCheck] blocked by isProgrammatic'); return }
  const nodeKey = getNodeFullKey(nodeData)
  console.log('[handleCheck] nodeKey:', nodeKey, 'level:', nodeData.level, 'currentLevel:', currentLevel.value, 'isChecked:', checkState.checkedKeys.includes(nodeData.id))
  console.log('[handleCheck] explicitKeys:', [...explicitKeys], 'uncheckedKeys:', [...uncheckedKeys], 'serverCheckedKeys:', [...serverCheckedKeys])
  if (nodeData.id.startsWith('dir::')) {
    if (nodeData.children) {
      const isNowChecked = checkState.checkedKeys.includes(nodeData.id)
      for (const child of nodeData.children) {
        if (child.level === 'conn') {
          const childKey = getNodeFullKey(child)
          const shouldBeChecked = explicitKeys.has(childKey) || (serverCheckedKeys.has(childKey) && !uncheckedKeys.has(childKey))
          if (isNowChecked !== shouldBeChecked) {
            if (isNowChecked) {
              explicitKeys.add(childKey)
              uncheckedKeys.delete(childKey)
            } else {
              explicitKeys.delete(childKey)
              uncheckedKeys.add(childKey)
              const toUncheck = []
              for (const key of explicitKeys) {
                if (isDescendantOf(key, childKey)) {
                  toUncheck.push(key)
                }
              }
              for (const key of serverCheckedKeys) {
                if (!uncheckedKeys.has(key) && isDescendantOf(key, childKey)) {
                  toUncheck.push(key)
                }
              }
              for (const key of toUncheck) {
                explicitKeys.delete(key)
                uncheckedKeys.add(key)
              }
            }
          }
        }
      }
    }
    nextTick(() => syncTreeVisual())
    return
  }

  const isNowChecked = checkState.checkedKeys.includes(nodeData.id)
  const shouldBeChecked = explicitKeys.has(nodeKey) || (serverCheckedKeys.has(nodeKey) && !uncheckedKeys.has(nodeKey))

  if (isNowChecked === shouldBeChecked) return

  if (isNowChecked) {
    explicitKeys.add(nodeKey)
    uncheckedKeys.delete(nodeKey)
  } else {
    explicitKeys.delete(nodeKey)
    uncheckedKeys.add(nodeKey)
    const toUncheck = []
    for (const key of explicitKeys) {
      if (isDescendantOf(key, nodeKey)) {
        toUncheck.push(key)
      }
    }
    for (const key of serverCheckedKeys) {
      if (!uncheckedKeys.has(key) && isDescendantOf(key, nodeKey)) {
        toUncheck.push(key)
      }
    }
    for (const key of toUncheck) {
      explicitKeys.delete(key)
      uncheckedKeys.add(key)
    }
  }
  nextTick(() => syncTreeVisual())
}

function handleNodeClick(nodeData) {
  if (nodeData.level === 'dir') return
  if (nodeData.level === 'conn') {
    selectedConnId.value = nodeData.id
    selectedConnLabel.value = nodeData.label
    navigateToLevel('schema')
  } else if (nodeData.level === 'schema') {
    selectedSchema.value = nodeData.data?.schemaName || nodeData.id
    selectedSchemaLabel.value = nodeData.label
    navigateToLevel('table')
  } else if (nodeData.level === 'table') {
    selectedTable.value = nodeData.data?.tableName || nodeData.id
    selectedTableLabel.value = nodeData.label
    navigateToLevel('column')
  }
}

function getKeyLevel(key) {
  if (key.startsWith('dir::')) return 'dir'
  const parts = key.split('::')
  if (parts.length >= 4 && parts[3]) return 'column'
  if (parts.length >= 3 && parts[2]) return 'table'
  if (parts.length >= 2 && parts[1]) return 'schema'
  return 'conn'
}

function isDescendantOf(childKey, parentKey) {
  if (!childKey.startsWith(parentKey + '::')) return false
  return childKey.split('::').length > parentKey.split('::').length
}

function savePermissions() {
  if (!currentRole.value) return
  saving.value = true

  // 直接从 el-tree 获取当前勾选状态，避免依赖 handleCheck 的异步时序问题
  const treeChecked = treeRef.value
    ? new Set(treeRef.value.getCheckedKeys().filter(k => !k.startsWith('dir::')))
    : new Set()

  // 只同步当前层级的树状态到 explicitKeys/uncheckedKeys，确保 cascade 逻辑正确
  for (const key of currentTreeNodeKeys) {
    if (key.startsWith('dir::')) continue
    if (getKeyLevel(key) !== currentLevel.value) continue
    if (treeChecked.has(key)) {
      explicitKeys.add(key)
      uncheckedKeys.delete(key)
    } else {
      explicitKeys.delete(key)
      uncheckedKeys.add(key)
      const descendantsToUncheck = []
      for (const childKey of explicitKeys) {
        if (isDescendantOf(childKey, key)) {
          descendantsToUncheck.push(childKey)
        }
      }
      for (const childKey of descendantsToUncheck) {
        explicitKeys.delete(childKey)
        uncheckedKeys.add(childKey)
      }
    }
  }

  console.log('[savePermissions] START - currentLevel:', currentLevel.value)
  console.log('[savePermissions] explicitKeys:', [...explicitKeys])
  console.log('[savePermissions] baselineCheckedKeys:', [...baselineCheckedKeys])
  console.log('[savePermissions] uncheckedKeys:', [...uncheckedKeys])
  console.log('[savePermissions] serverCheckedKeys:', [...serverCheckedKeys])

  // 根据当前编辑的层级，筛选出该层级的权限用于对比
  // 其他层级的权限保持不变，不发送 add/del 请求
  const currentExplicitKeys = new Set()
  for (const key of explicitKeys) {
    if (getKeyLevel(key) === currentLevel.value) {
      currentExplicitKeys.add(key)
    }
  }

  const currentBaselineKeys = new Set()
  for (const key of baselineCheckedKeys) {
    if (getKeyLevel(key) === currentLevel.value) {
      currentBaselineKeys.add(key)
    }
  }

  const currentPowers = []
  for (const key of currentExplicitKeys) {
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

  // 从全量 explicitKeys 计算有 table/column 子级的 schema
  // 这样才能正确判断 schema 权限是否需要过滤
  const schemaKeysWithTableChildren = new Set()
  for (const key of explicitKeys) {
    if (key.startsWith('dir::')) continue
    const parts = key.split('::')
    if (parts.length >= 3 && parts[2]) {
      schemaKeysWithTableChildren.add(parts[0] + '::' + parts[1])
    }
  }

  function shouldKeepPower(p) {
    if (p.level === 'schema' && p.schemaName) {
      const schemaKey = p.connId + '::' + p.schemaName
      if (schemaKeysWithTableChildren.has(schemaKey)) {
        return false
      }
    }
    return true
  }

  const filteredPowers = currentPowers.filter(shouldKeepPower)
  const newKeySet = new Set(filteredPowers.map(permToKey))

  // 对 baseline 也应用同样的过滤，确保对比对称
  // 注意：需要基于全量 explicitKeys 来判断 baseline 中的 schema 是否应该被过滤
  const baselineSchemaKeysWithTableChildren = new Set()
  for (const key of baselineCheckedKeys) {
    if (key.startsWith('dir::')) continue
    const parts = key.split('::')
    if (parts.length >= 3 && parts[2]) {
      baselineSchemaKeysWithTableChildren.add(parts[0] + '::' + parts[1])
    }
  }

  function shouldKeepBaseline(p) {
    if (p.level === 'schema' && p.schemaName) {
      const schemaKey = p.connId + '::' + p.schemaName
      if (baselineSchemaKeysWithTableChildren.has(schemaKey)) {
        let hasActiveChildren = false
        for (const key of explicitKeys) {
          if (isDescendantOf(key, schemaKey)) {
            hasActiveChildren = true
            break
          }
        }
        if (hasActiveChildren) {
          return false
        }
      }
    }
    return true
  }

  const filteredBaseline = new Set()
  for (const id of currentBaselineKeys) {
    const parts = id.split('::')
    const connId = parts[0]
    const schema = parts[1] || null
    const table = parts[2] || null
    const column = parts[3] || null
    let level = 'conn'
    if (column) level = 'column'
    else if (table) level = 'table'
    else if (schema) level = 'schema'
    if (shouldKeepBaseline({ connId, schemaName: schema, tableName: table, columnName: column, level })) {
      filteredBaseline.add(id)
    }
  }

  const roleId = currentRole.value.id

  const addPowers = filteredPowers.filter(p => {
    const key = permToKey(p)
    return !filteredBaseline.has(key)
  }).map(p => ({ ...p, id: '', roleId }))

  const delPowers = []
  for (const id of filteredBaseline) {
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
        roleId,
        connId,
        connName: null,
        schemaName: schema,
        tableName: table,
        columnName: column,
        level,
      })
    }
  }

  const deletedCurrentLevelKeys = new Set()
  for (const key of currentBaselineKeys) {
    if (!currentExplicitKeys.has(key)) {
      deletedCurrentLevelKeys.add(key)
    }
  }
  for (const key of uncheckedKeys) {
    if (getKeyLevel(key) === currentLevel.value) {
      deletedCurrentLevelKeys.add(key)
    }
  }
  console.log('[savePermissions] deletedCurrentLevelKeys:', [...deletedCurrentLevelKeys])
  console.log('[savePermissions] delPowers before cascade:', JSON.stringify(delPowers))
  for (const key of baselineCheckedKeys) {
    if (key.startsWith('dir::')) continue
    if (explicitKeys.has(key)) continue
    const keyLevel = getKeyLevel(key)
    if (keyLevel === currentLevel.value) continue
    let isCascaded = false
    for (const deletedKey of deletedCurrentLevelKeys) {
      if (isDescendantOf(key, deletedKey)) {
        isCascaded = true
        break
      }
    }
    if (!isCascaded) continue
    const parts = key.split('::')
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
      roleId,
      connId,
      connName: null,
      schemaName: schema,
      tableName: table,
      columnName: column,
      level,
    })
  }

  const roleData = {
    id: currentRole.value.id,
    name: currentRole.value.name,
    addPowers,
    delPowers,
    viewClassic: classicViewEnabled.value ? 1 : 0,
    allowModify: modifyDataEnabled.value ? 1 : 0,
  }

  console.log('[savePermissions] FINAL roleData:', JSON.stringify(roleData))

  http.post('/saveRole', roleData).then(() => {
    saving.value = false
    classicViewDirty = false
    modifyDataDirty = false
    // 更新 explicitKeys：移除当前层级的旧权限，加入当前层级过滤后的权限
    for (const key of currentBaselineKeys) {
      explicitKeys.delete(key)
      uncheckedKeys.delete(key)
    }
    for (const key of newKeySet) {
      explicitKeys.add(key)
      uncheckedKeys.delete(key)
    }
    // 清理级联删除的下级权限
    for (const key of baselineCheckedKeys) {
      if (key.startsWith('dir::')) continue
      let isCascaded = false
      for (const deletedKey of deletedCurrentLevelKeys) {
        if (isDescendantOf(key, deletedKey)) {
          isCascaded = true
          break
        }
      }
      if (isCascaded) {
        explicitKeys.delete(key)
        uncheckedKeys.delete(key)
      }
    }
    // 清理 uncheckedKeys 中当前层级的已处理 key
    for (const key of deletedCurrentLevelKeys) {
      uncheckedKeys.delete(key)
    }
    // 更新 baselineCheckedKeys：同步全量状态
    baselineCheckedKeys.clear()
    for (const key of explicitKeys) {
      baselineCheckedKeys.add(key)
    }
    currentRole.value.powerList = []
    for (const key of explicitKeys) {
      if (key.startsWith('dir::')) continue
      const parts = key.split('::')
      const connId = parts[0]
      const schema = parts[1] || null
      const table = parts[2] || null
      const column = parts[3] || null
      let level = 'conn'
      if (column) level = 'column'
      else if (table) level = 'table'
      else if (schema) level = 'schema'
      currentRole.value.powerList.push({
        id: '',
        roleId: currentRole.value.id,
        connId,
        connName: null,
        schemaName: schema,
        tableName: table,
        columnName: column,
        level,
      })
    }
    currentRole.value.viewClassic = classicViewEnabled.value ? 1 : 0
    currentRole.value.allowModify = modifyDataEnabled.value ? 1 : 0
    loadRoles()
    navigateToLevel(currentLevel.value)
    ElMessage.success('保存成功')
  }).catch(() => {
    saving.value = false
  })
}

function cancelEdit() {
  if (currentRole.value) {
    classicViewEnabled.value = !!(currentRole.value.viewClassic)
    modifyDataEnabled.value = !!(currentRole.value.allowModify)
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
  background: var(--bg-tertiary);
}
.page-content {
  flex: 1;
  padding: 10px;
  overflow: hidden;
}
.role-permission-container {
  display: grid;
  grid-template-columns: 300px 1fr;
  gap: 10px;
  height: 100%;
}
.role-list-panel,
.permission-config-panel {
  background: var(--bg-primary);
  border-radius: 8px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.panel-header {
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-primary);
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.panel-header h2 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
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
  border-bottom: 1px solid var(--border-primary);
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
  color: var(--accent-color);
}
.node-label.node-clickable:hover {
  text-decoration: underline;
}
.node-comment {
  font-size: 12px;
  color: var(--text-tertiary);
  margin-left: 8px;
}
.implicit-tag {
  margin-left: 8px;
  font-size: 11px;
}
:deep(.el-tree) .is-checked .el-checkbox__inner {
  background-color: var(--accent-color);
  border-color: var(--accent-color);
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
