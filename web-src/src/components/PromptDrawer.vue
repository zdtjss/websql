<template>
  <el-drawer
    :model-value="modelValue"
    @update:model-value="$emit('update:modelValue', $event)"
    title="常用提示词"
    direction="ltr"
    size="400px"
    :before-close="handleClose"
    class="prompt-drawer"
  >
    <div class="prompt-drawer-body">
      <div class="prompt-toolbar">
        <el-button primary text size="small" @click="handleAdd" icon="Plus">
          新增
        </el-button>
      </div>

      <div class="prompt-list">
        <div v-if="loading" style="text-align: center; padding: 20px;">
          <el-icon class="is-loading"><Loading /></el-icon>
        </div>
        <div v-else-if="prompts.length === 0" class="prompt-empty">
          暂无提示词
        </div>
        <div
          v-for="prompt in prompts"
          :key="prompt.id"
          class="prompt-item"
          @click="handleEdit(prompt)"
        >
          <div class="prompt-item-info">
            <div class="prompt-item-title">{{ prompt.title }}</div>
            <div v-if="prompt.isShared" class="prompt-item-shared">
              <el-icon size="12"><Share /></el-icon>
              {{ prompt.sharedByName || '他人分享' }}
            </div>
          </div>
          <div class="prompt-item-actions">
            <!-- <el-button v-if="!prompt.isShared" size="small" text type="primary" @click.stop="handleEdit(prompt)" title="编辑">
              <el-icon><Edit /></el-icon>
            </el-button> -->
            <el-button size="small" text type="success" @click.stop="handleSend(prompt)" title="发送给大模型">
              <el-icon><Promotion /></el-icon>
            </el-button>
            <el-button v-if="!prompt.isShared" size="small" text type="danger" @click.stop="handleDelete(prompt)" title="删除">
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>
      </div>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, watch } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Edit, Loading, Promotion, Share } from '@element-plus/icons-vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
})

const emit = defineEmits(['update:modelValue', 'add', 'edit', 'sendToAI'])

const prompts = ref([])
const loading = ref(false)

watch(() => props.modelValue, (val) => {
  if (val) {
    loadPrompts()
  }
})

async function loadPrompts() {
  loading.value = true
  try {
    const resp = await http.get('/promptList')
    prompts.value = (resp.data.data || []).map(p => ({
      ...p,
      isShared: p.createdBy !== p.currentUserId,
    }))
  } catch (e) {
    console.error('加载提示词列表失败:', e)
  } finally {
    loading.value = false
  }
}

function handleClose() {
  emit('update:modelValue', false)
}

function handleAdd() {
  emit('update:modelValue', false)
  emit('add')
}

function handleEdit(prompt) {
  emit('update:modelValue', false)
  emit('edit', prompt.id)
}

function handleItemClick(prompt) {
  emit('sendToAI', prompt.content)
}

function handleSend(prompt) {
  emit('sendToAI', prompt.content)
}

function handleDelete(prompt) {
  ElMessageBox.confirm(
    `确定要删除提示词 "${prompt.title}" 吗？`,
    '确认删除',
    { confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning' }
  ).then(async () => {
    try {
      await http.get('/delPrompt', { params: { id: prompt.id } })
      ElMessage.success('删除成功')
      loadPrompts()
    } catch (e) {
      ElMessage.error('删除失败')
    }
  }).catch(() => {})
}

defineExpose({ loadPrompts })
</script>

<style scoped>
.prompt-drawer {
  margin-bottom: 0px;
  padding: 0px;
  border-bottom: 1px solid #e4e7ed;
}

.prompt-drawer-body {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.prompt-toolbar {
  border-bottom: 1px solid #f0f0f0;
  flex-shrink: 0;
}

.prompt-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.prompt-empty {
  text-align: center;
  color: #909399;
  padding: 40px 0;
  font-size: 14px;
}

.prompt-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 5px;;
  border-radius: 5px;
  cursor: pointer;
  transition: all 0.2s;
  margin-bottom: 4px;
}

.el-button--small {
  padding: 5px;
}

.el-button+.el-button {
  margin-left: 3px;
}

.prompt-item:hover {
  background: #f5f7fa;
}

.prompt-item-info {
  flex: 1;
  min-width: 0;
}

.prompt-item-title {
  font-size: 14px;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
}

.prompt-item-shared {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #909399;
  margin-top: 2px;
}

.prompt-item-actions {
  display: flex;
  gap: 0;
  opacity: 0;
  transition: opacity 0.2s;
  flex-shrink: 0;
}

.prompt-item:hover .prompt-item-actions {
  opacity: 1;
}
</style>
