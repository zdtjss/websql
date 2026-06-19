<template>
  <div class="prompt-management-page">
    <div class="prompt-management-container">
      <div class="role-list-panel">
        <div class="panel-header">
          <h2>角色列表</h2>
        </div>
        <div class="role-search">
          <el-input
            v-model="roleSearchKey"
            placeholder="搜索角色"
            clearable
            :prefix-icon="Search"
            size="default"
          />
        </div>
        <div class="role-list-content">
          <div v-if="loadingRoles" style="text-align: center; padding: 20px;">
            <el-icon class="is-loading"><Loading /></el-icon>
          </div>
          <div v-else-if="filteredRoles.length === 0" class="empty-tip">暂无角色</div>
          <div
            v-for="role in filteredRoles"
            :key="role.id"
            :class="['role-item', { active: currentRole?.id === role.id }]"
            @click="handleRoleSelect(role)"
          >
            <el-icon><User /></el-icon>
            <span class="role-name">{{ role.name }}</span>
          </div>
        </div>
      </div>

      <div class="prompt-list-panel">
        <div class="panel-header">
          <h2 v-if="currentRole">{{ currentRole.name }} - 提示词列表</h2>
          <h2 v-else>请选择角色</h2>
          <el-button v-if="currentRole" type="primary" size="small" @click="handleAdd" :icon="Plus">
            新增提示词
          </el-button>
        </div>

        <div v-if="currentRole" class="prompt-list-content">
          <div v-if="loadingPrompts" style="text-align: center; padding: 20px;">
            <el-icon class="is-loading"><Loading /></el-icon>
          </div>
          <div v-else-if="prompts.length === 0" class="empty-tip">
            暂无提示词，点击上方"新增提示词"按钮添加
          </div>
          <el-table v-else :data="prompts" style="width: 100%" stripe>
            <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip resizable />
            <el-table-column label="创建者" width="120" resizable>
              <template #default="{ row }">
                {{ row.sharedByName || '-' }}
              </template>
            </el-table-column>
            <el-table-column label="更新时间" width="180" resizable>
              <template #default="{ row }">
                {{ row.updatedAt || row.createdAt || '-' }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120" fixed="right" resizable>
              <template #default="{ row }">
                <el-button type="primary" size="small" text @click="handleEdit(row)" title="编辑">
                  <el-icon><Edit /></el-icon>
                </el-button>
                <el-button type="danger" size="small" text @click="handleDelete(row)" title="删除">
                  <el-icon><Delete /></el-icon>
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-else class="prompt-empty-state">
          <el-icon size="48" color="#c0c4cc"><Document /></el-icon>
          <p>请在左侧选择一个角色来管理其提示词</p>
        </div>
      </div>
    </div>

    <PromptEditDialog
      v-model="dialogVisible"
      :prompt-id="dialogPromptId"
      :role-id="dialogRoleId"
      @saved="handlePromptSaved"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { getRoleBaseList, getPromptListByRole, delPrompt } from '@/api/ai'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Document, Edit, Loading, Plus, Search, User } from '@element-plus/icons-vue'
import PromptEditDialog from '@/components/ai/PromptEditDialog.vue'

const roles = ref([])
const currentRole = ref(null)
const prompts = ref([])
const loadingRoles = ref(false)
const loadingPrompts = ref(false)
const roleSearchKey = ref('')

const dialogVisible = ref(false)
const dialogPromptId = ref('')
const dialogRoleId = ref('')

const filteredRoles = computed(() => {
  if (!roleSearchKey.value) return roles.value
  const key = roleSearchKey.value.toLowerCase()
  return roles.value.filter(r => r.name.toLowerCase().includes(key))
})

onMounted(() => {
  loadRoles()
})

async function loadRoles() {
  loadingRoles.value = true
  try {
    const resp = await getRoleBaseList()
    roles.value = resp.data.data || []
  } catch (e) {
    console.error('加载角色列表失败:', e)
  } finally {
    loadingRoles.value = false
  }
}

function handleRoleSelect(role) {
  currentRole.value = role
  loadPromptsByRole(role.id)
}

async function loadPromptsByRole(roleId) {
  loadingPrompts.value = true
  try {
    const resp = await getPromptListByRole(roleId)
    prompts.value = resp.data.data || []
  } catch (e) {
    console.error('加载提示词列表失败:', e)
  } finally {
    loadingPrompts.value = false
  }
}

function handleAdd() {
  dialogRoleId.value = currentRole.value?.id || ''
  dialogPromptId.value = ''
  dialogVisible.value = true
}

function handleEdit(prompt) {
  dialogPromptId.value = prompt.id
  dialogRoleId.value = currentRole.value?.id || ''
  dialogVisible.value = true
}

function handleDelete(prompt) {
  ElMessageBox.confirm(
    `确定要删除提示词 "${prompt.title}" 吗？`,
    '确认删除',
    { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' }
  ).then(async () => {
    try {
      await delPrompt(prompt.id)
      ElMessage.success('删除成功')
      loadPromptsByRole(currentRole.value.id)
    } catch (e) {
      ElMessage.error('删除失败')
    }
  }).catch(() => {})
}

function handlePromptSaved() {
  if (currentRole.value) {
    loadPromptsByRole(currentRole.value.id)
  }
}
</script>

<style scoped>
.prompt-management-page {
  height: 100%;
}

.prompt-management-container {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 16px;
  height: 100%;
}

.role-list-panel,
.prompt-list-panel {
  background: var(--bg-primary);
  border-radius: 8px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.06);
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
  flex-shrink: 0;
}

.panel-header h2 {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
}

.role-search {
  padding: 12px 16px;
  flex-shrink: 0;
}

.role-list-content {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
}

.role-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
  color: var(--text-secondary);
  font-size: 14px;
}

.role-item:hover {
  background: var(--bg-hover);
  color: var(--accent-color);
}

.role-item.active {
  background: var(--bg-inline-bar);
  color: var(--accent-color);
  font-weight: 500;
}

.role-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.prompt-list-content {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
}

.prompt-empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--text-tertiary);
  gap: 12px;
}

.prompt-empty-state p {
  font-size: 14px;
  margin: 0;
}

.empty-tip {
  text-align: center;
  color: var(--text-tertiary);
  padding: 40px 0;
  font-size: 14px;
}

:deep(.el-table) {
  font-size: 14px;
}

:deep(.el-table .el-button) {
  padding: 4px 8px;
}
</style>
