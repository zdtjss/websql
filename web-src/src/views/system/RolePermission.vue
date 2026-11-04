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
            <el-table-column label="角色名称" resizable>
              <template #default="scope">
                <span>{{ scope.row.name }}</span>
                <el-tag v-if="scope.row.isBuiltin" size="small" type="info" style="margin-left: 6px;">内置</el-tag>
                <el-tag v-if="scope.row.isAdmin" size="small" type="warning" style="margin-left: 6px;">管理员</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120" resizable>
              <template #default="scope">
                <el-button
                  type="primary"
                  link
                  size="small"
                  :icon="Edit"
                  title="重命名角色"
                  aria-label="重命名角色"
                  :disabled="scope.row.isBuiltin === 1"
                  @click.stop="renameRole(scope.row)"
                />
                <el-button
                  type="danger"
                  link
                  size="small"
                  :icon="Delete"
                  title="删除角色"
                  aria-label="删除角色"
                  :disabled="scope.row.isBuiltin === 1"
                  @click.stop="deleteRole(scope.row)"
                />
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
                <el-button type="primary" size="small" @click="savePermissions" :loading="editor.state.saving">
                  <el-icon><Check /></el-icon> 保存
                </el-button>
                <el-button size="small" @click="cancelEdit"><el-icon><Close /></el-icon> 取消</el-button>
              </div>
            </div>
            <el-alert title="勾选复选框授予权限，点击节点名称进入下一级。上级权限包含下级（如勾选连接则拥有其下所有权限）。「子级已选」表示下级有权限配置但当前节点本身未直接授权。" type="info" :closable="false" style="margin-bottom: 12px; flex-shrink: 0;" />
            <el-alert
              v-if="currentLevel === 'column'"
              title="列级授权仅约束 AI 对话：经典视图与 SQL 编辑器按表级授权放行，读取该表时会返回全部列。"
              type="warning"
              :closable="false"
              show-icon
              style="margin-bottom: 12px; flex-shrink: 0;"
            />
            <div class="classic-view-toggle" v-if="currentLevel === 'conn'">
              <el-switch v-model="classicViewEnabled" active-text="允许使用经典视图" inactive-text="禁止使用经典视图" />
              <el-switch v-model="modifyDataEnabled" style="margin-left: 20px;" active-text="允许修改数据" inactive-text="禁止修改数据" />
              <el-tooltip v-if="showAISwitch" content="仅约束 AI 对话的数据写操作；需同时开启「允许修改数据」才生效" placement="top">
                <el-switch
                  v-model="aiModifyEnabled"
                  style="margin-left: 20px;"
                  :disabled="!modifyDataEnabled"
                  active-text="允许 AI 对话修改数据"
                  inactive-text="禁止 AI 对话修改数据"
                />
              </el-tooltip>
              <span v-if="showAISwitch && !modifyDataEnabled" class="switch-hint">需先允许修改数据</span>
              <el-tag
                v-if="showAISwitch && aiModifyEnabled && !modifyDataEnabled"
                size="small"
                type="warning"
                style="margin-left: 8px;"
              >存量配置：开（当前不生效）</el-tag>
            </div>
            <div v-if="editor.state.conflict" class="conflict-banner">
              <el-alert
                title="权限已被其他管理员修改，当前编辑基于旧快照。请查看最新配置后重新编辑并保存。"
                type="warning"
                :closable="false"
                show-icon
              />
            </div>
            <div class="permission-tree-wrapper">
              <RolePermissionTree
                ref="treeComp"
                :data="treeData"
                :checked-keys="treeCheckedKeys"
                :implicit-keys="implicitKeys"
                :tree-key="treeKey"
                @check="handleCheck"
                @node-click="handleNodeClick"
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, useTemplateRef } from 'vue'
import { Delete, Edit, Plus, Close, Check, Connection, Folder, Grid, Document } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getPermissionTree,
  getRoleCapability,
  getRoleList,
  getUsersByRole,
  saveRole,
  delRole,
  type Role,
  type RoleCapability,
} from '@/api/system'
import { unwrap } from '@/utils/apiResult'
import RolePermissionTree from './rolePermission/RolePermissionTree.vue'
import { useRolePermissionEditor, type EditorRole } from './rolePermission/useRolePermissionEditor'
import { computeImplicitKeys } from './rolePermission/permissionPolicy'

type Level = 'conn' | 'schema' | 'table' | 'column'

interface TreeNode {
  id: string
  label: string
  level: 'dir' | 'conn' | 'schema' | 'table' | 'column'
  children?: TreeNode[]
  data?: { comment?: string; schemaName?: string; tableName?: string }
}

const roles = ref<Role[]>([])
const currentRole = ref<Role | null>(null)
const capability = ref<RoleCapability>({ capabilityVersion: 1, aiPolicyEnforce: false })
const editor = useRolePermissionEditor()

const currentLevel = ref<Level>('conn')
const selectedConnId = ref('')
const selectedConnLabel = ref('')
const selectedSchema = ref('')
const selectedSchemaLabel = ref('')
const selectedTable = ref('')
const selectedTableLabel = ref('')

const treeComp = useTemplateRef('treeComp')
const treeData = ref<TreeNode[]>([])
const treeKey = ref(0)
const treeCheckedKeys = ref<string[]>([])
const implicitKeys = ref<Set<string>>(new Set())
const currentTreeNodeKeys = ref<Set<string>>(new Set())

const classicViewEnabled = ref(false)
const modifyDataEnabled = ref(false)
const aiModifyEnabled = ref(false)

/** AI 开关仅在能力版本 2 且策略已生效时展示。 */
const showAISwitch = ref(false)

onMounted(() => {
  loadCapability()
  loadRoles()
})

function loadCapability() {
  getRoleCapability()
    .then((resp) => {
      capability.value = resp.data.data || { capabilityVersion: 1, aiPolicyEnforce: false }
      showAISwitch.value =
        capability.value.capabilityVersion >= 2 && capability.value.aiPolicyEnforce === true
    })
    .catch(() => {
      showAISwitch.value = false
    })
}

function asEditorRole(role: Role): EditorRole {
  return {
    id: role.id,
    name: role.name,
    viewClassic: role.viewClassic ?? 0,
    allowModify: role.allowModify ?? 0,
    aiAllowModify: role.aiAllowModify ?? 0,
    permissionDigest: role.permissionDigest,
    powerList: role.powerList,
  }
}

function loadRoles() {
  getRoleList().then((resp) => {
    roles.value = resp.data.data || []
    if (currentRole.value) {
      const fresh = roles.value.find((r) => r.id === currentRole.value?.id)
      if (fresh) {
        currentRole.value = fresh
        editor.load(asEditorRole(fresh))
        syncSwitches(fresh)
      } else {
        currentRole.value = null
      }
    }
  })
}

function syncSwitches(role: Role) {
  classicViewEnabled.value = !!role.viewClassic
  modifyDataEnabled.value = !!role.allowModify
  aiModifyEnabled.value = !!role.aiAllowModify
}

function createRole() {
  ElMessageBox.prompt('请输入角色名称', '新建角色', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    inputPattern: /.+/,
    inputErrorMessage: '角色名称不能为空',
  }).then(async ({ value }) => {
    try {
      unwrap(await saveRole({
        id: '',
        name: value,
        addPowers: [],
        delPowers: [],
        viewClassic: 0,
        allowModify: 1,
        // AIPolicyEnforce 生效后由服务端默认写入 0（AI 只读）；兼容期不提交该字段。
        aiAllowModify: showAISwitch.value ? 0 : undefined,
      }))
      loadRoles()
    } catch (e: any) {
      ElMessage.error(e?.message || '新建角色失败')
    }
  })
}

async function renameRole(row: unknown) {
  const role = row as Role
  if (!role?.id || role.isBuiltin === 1) return
  let value = ''
  try {
    const result = await ElMessageBox.prompt('请输入新的角色名称', '重命名角色', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      inputValue: role.name,
      inputPattern: /\S+/,
      inputErrorMessage: '角色名称不能为空',
    })
    value = String(result.value || '').trim()
  } catch {
    return
  }
  if (!value || value === role.name) return
  try {
    unwrap(await saveRole({
      id: role.id,
      name: value,
      addPowers: [],
      delPowers: [],
      viewClassic: role.viewClassic ?? 0,
      allowModify: role.allowModify ?? 0,
      // 仅在 AI 开关开放时提交该字段，避免旧协议把存量配置重置
      aiAllowModify: showAISwitch.value ? (role.aiAllowModify ?? 0) : undefined,
      expectedDigest: role.permissionDigest,
    }))
    ElMessage.success('重命名成功')
    loadRoles()
  } catch (e: any) {
    if (e?.response?.status === 409) {
      ElMessage.warning('角色权限已被其他管理员修改，请刷新后重试')
      loadRoles()
      return
    }
    ElMessage.error(e?.message || '重命名失败')
  }
}

// el-table 的 current-change 事件类型为 DefaultRow，这里在函数内收窄为 Role。
function handleRoleChange(row: unknown) {
  const role = row as Role | null
  if (!role?.id) return
  currentRole.value = role
  syncSwitches(role)
  editor.load(asEditorRole(role))
  navigateToLevel('conn')
}

async function deleteRole(row: unknown) {
  const role = row as Role
  if (!role?.id) return
  // 删除前统计已绑定用户：删除角色会级联清理 t_user_role，成员会瞬间失去权限。
  let boundCount = 0
  try {
    const users = unwrap(await getUsersByRole(role.id))
    boundCount = Array.isArray(users) ? users.length : 0
  } catch {
    boundCount = 0
  }
  const extra = boundCount > 0 ? `\n该角色已绑定 ${boundCount} 个用户，删除后这些用户将失去对应权限。` : ''
  ElMessageBox.confirm(`确定要删除角色 "${role.name}" 吗？${extra}`, '确认删除', {
    confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning',
  }).then(async () => {
    try {
      unwrap(await delRole(role.id))
      ElMessage.success('删除成功')
      if (currentRole.value?.id === role.id) currentRole.value = null
      loadRoles()
    } catch (e: any) {
      ElMessage.error(e?.message || '删除失败')
    }
  })
}

function resetSelection(level: Level) {
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
}

function navigateToLevel(level: Level) {
  currentLevel.value = level
  resetSelection(level)
  loadTree()
}

function loadTree() {
  const params: { level: string; roleId?: string; connId?: string; schema?: string; table?: string } = {
    level: currentLevel.value,
    roleId: currentRole.value?.id,
  }
  if (selectedConnId.value) params.connId = selectedConnId.value
  if (selectedSchema.value) params.schema = selectedSchema.value
  if (selectedTable.value) params.table = selectedTable.value

  getPermissionTree(params).then((resp) => {
    treeData.value = (resp.data.data || []) as unknown as TreeNode[]
    const keys = new Set<string>()
    collectNodeKeys(treeData.value, keys)
    currentTreeNodeKeys.value = keys
    refreshTreeState(true)
  })
}

function collectNodeKeys(nodes: TreeNode[], into: Set<string>) {
  for (const node of nodes) {
    into.add(node.id)
    if (node.children?.length) collectNodeKeys(node.children, into)
  }
}

/**
 * 重新计算展示态。
 * rebuild=true 时递增 treeKey 强制重建（加载新树）；否则直接同步勾选，避免丢失展开状态。
 */
function refreshTreeState(rebuild: boolean) {
  const workingKeys = editor.state.working.keys()
  implicitKeys.value = computeImplicitKeys(workingKeys, currentTreeNodeKeys.value)
  treeCheckedKeys.value = editor.checkedKeysFor(currentTreeNodeKeys.value)
  if (rebuild) {
    treeKey.value++
    return
  }
  nextTick(() => {
    treeComp.value?.setCheckedKeys(treeCheckedKeys.value)
  })
}

function handleCheck(node: TreeNode, checked: boolean) {
  if (node.level === 'dir') {
    const connChildren = (node.children || []).filter((child) => child.level === 'conn')
    editor.toggleMany(connChildren.map((child) => child.id), checked)
  } else {
    editor.toggle(node.id, checked)
  }
  refreshTreeState(false)
}

function handleNodeClick(node: TreeNode) {
  if (node.level === 'dir') return
  if (node.level === 'conn') {
    selectedConnId.value = node.id
    selectedConnLabel.value = node.label
    navigateToLevel('schema')
  } else if (node.level === 'schema') {
    selectedSchema.value = node.data?.schemaName || node.id
    selectedSchemaLabel.value = node.label
    navigateToLevel('table')
  } else if (node.level === 'table') {
    selectedTable.value = node.data?.tableName || node.id
    selectedTableLabel.value = node.label
    navigateToLevel('column')
  }
}

function savePermissions() {
  if (!currentRole.value) return
  const role = currentRole.value
  const payload = editor.buildPayload(asEditorRole(role), {
    viewClassic: classicViewEnabled.value ? 1 : 0,
    allowModify: modifyDataEnabled.value ? 1 : 0,
    aiAllowModify: showAISwitch.value ? (aiModifyEnabled.value ? 1 : 0) : undefined,
  })
  if (!payload) {
    ElMessage.info('没有需要保存的变更')
    return
  }

  editor.state.saving = true
  saveRole(payload)
    .then((resp) => {
      // 后端参数错误为 HTTP 200 + code 400，必须校验业务 code，
      // 否则会把失败当成功并留下过期 digest。
      const data = unwrap(resp) as { permissionDigest?: string }
      editor.markSaved(data?.permissionDigest)
      ElMessage.success('保存成功')
      loadRoles()
      loadTree()
    })
    .catch((err) => {
      // 409：并发编辑冲突。只提示并重新加载，禁止自动覆盖重试。
      if (err?.response?.status === 409) {
        editor.markConflict()
        ElMessage.warning('角色权限已被其他管理员修改，已标记冲突，请确认最新配置后重新保存')
        loadRoles()
        return
      }
      editor.state.saving = false
      const msg = err?.message || err?.response?.data?.msg || '保存失败'
      ElMessage.error(msg)
    })
}

function cancelEdit() {
  if (!currentRole.value) return
  syncSwitches(currentRole.value)
  editor.reset()
  loadTree()
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
  display: flex;
  align-items: center;
}
.switch-hint {
  margin-left: 8px;
  font-size: 12px;
  color: var(--text-tertiary);
}
.conflict-banner {
  margin-bottom: 12px;
  flex-shrink: 0;
}
.permission-tree-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
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
