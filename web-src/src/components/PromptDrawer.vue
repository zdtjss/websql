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
      <el-tabs v-model="activeTab" class="prompt-tabs">
        <el-tab-pane label="我的" name="mine">
          <div class="prompt-toolbar">
            <el-button text size="small" @click="handleAdd">
              <el-icon><Plus /></el-icon>
            </el-button>
          </div>
          <div class="prompt-list">
            <div v-if="loading" style="text-align: center; padding: 20px;">
              <el-icon class="is-loading"><Loading /></el-icon>
            </div>
            <div v-else-if="myPrompts.length === 0" class="prompt-empty">
              暂无提示词
            </div>
            <div
              v-for="prompt in myPrompts"
              :key="prompt.id"
              class="prompt-item"
              @click="handleEdit(prompt)"
            >
              <div class="prompt-item-info">
                <div class="prompt-item-title">{{ prompt.title }}</div>
                <div v-if="prompt.isShared" class="prompt-item-sub">
                  <el-icon size="12"><Share /></el-icon>
                  {{ prompt.sharedByName || '他人分享' }}
                </div>
              </div>
              <div class="prompt-item-actions">
                <el-button size="small" text type="success" @click.stop="handleSend(prompt)" title="发送给大模型">
                  <el-icon><Promotion /></el-icon>
                </el-button>
                <el-button v-if="!prompt.isShared" size="small" text type="danger" @click.stop="handleDelete(prompt)" title="删除">
                  <el-icon><Delete /></el-icon>
                </el-button>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="系统" name="system">
          <div class="prompt-list">
            <div v-if="loading" style="text-align: center; padding: 10px;">
              <el-icon class="is-loading"><Loading /></el-icon>
            </div>
            <div v-else-if="systemPrompts.length === 0" class="prompt-empty">
              暂无系统提示词
            </div>
            <div
              v-for="prompt in systemPrompts"
              :key="prompt.id"
              class="prompt-item"
              @click="handleViewDetail(prompt)"
            >
              <div class="prompt-item-info">
                <div class="prompt-item-title">{{ prompt.title }}</div>
              </div>
              <div class="prompt-item-actions">
                <el-button size="small" text type="success" @click.stop="handleSend(prompt)" title="发送给大模型">
                  <el-icon><Promotion /></el-icon>
                </el-button>
              </div>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog
      v-model="detailVisible"
      :title="detailPrompt?.title || '提示词详情'"
      width="600px"
      append-to-body
      class="prompt-detail-dialog"
    >
      <div v-if="detailPrompt" class="prompt-detail-content markdown-body" v-html="md.render(detailPrompt.content)"></div>
      <template #footer>
        <el-button @click="detailVisible = false">关闭</el-button>
        <el-button type="primary" @click="handleSendFromDetail">
          <el-icon><Promotion /></el-icon>
          发送给大模型
        </el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Loading, Plus, Promotion, Share } from '@element-plus/icons-vue'
import MarkdownIt from 'markdown-it'

const md = new MarkdownIt({ html: true, breaks: true, linkify: true, typographer: false })

const props = defineProps({
  modelValue: { type: Boolean, default: false },
})

const emit = defineEmits(['update:modelValue', 'add', 'edit', 'sendToAI'])

const prompts = ref([])
const loading = ref(false)
const activeTab = ref('mine')
const detailVisible = ref(false)
const detailPrompt = ref(null)

const myPrompts = computed(() =>
  prompts.value.filter(p => !p.isRolePrompt)
)

const systemPrompts = computed(() =>
  prompts.value.filter(p => p.isRolePrompt)
)

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
      isShared: p.createdBy !== p.currentUserId && !p.isRolePrompt,
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
  if (prompt.isShared) return
  emit('update:modelValue', false)
  emit('edit', prompt.id)
}

function handleViewDetail(prompt) {
  detailPrompt.value = prompt
  detailVisible.value = true
}

function handleSend(prompt) {
  emit('sendToAI', prompt.content)
}

function handleSendFromDetail() {
  if (detailPrompt.value) {
    emit('sendToAI', detailPrompt.value.content)
    detailVisible.value = false
  }
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

.prompt-tabs {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.el-tabs__item {
  padding: 0px 5px;
}

.prompt-tabs :deep(.el-tabs__content) {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.prompt-tabs :deep(.el-tab-pane) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.prompt-toolbar {
  flex-shrink: 0;
}

.prompt-list {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px 8px;
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
  padding: 8px 5px;
  border-radius: 5px;
  cursor: pointer;
  transition: all 0.2s;
  margin-bottom: 2px;
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

.prompt-item-sub {
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

.prompt-detail-content {
  max-height: 60vh;
  overflow-y: auto;
}

.prompt-detail-content.markdown-body {
  font-size: 14px;
  line-height: 1.6;
  color: #303133;
}

.prompt-detail-content.markdown-body :deep(h1),
.prompt-detail-content.markdown-body :deep(h2),
.prompt-detail-content.markdown-body :deep(h3),
.prompt-detail-content.markdown-body :deep(h4),
.prompt-detail-content.markdown-body :deep(h5),
.prompt-detail-content.markdown-body :deep(h6) {
  margin-top: 0.8em;
  margin-bottom: 0.5em;
  font-weight: 600;
}

.prompt-detail-content.markdown-body :deep(p) {
  margin: 0.4em 0;
}

.prompt-detail-content.markdown-body :deep(pre) {
  background: #f6f8fa;
  border-radius: 6px;
  padding: 12px;
  overflow-x: auto;
  font-size: 13px;
  line-height: 1.5;
}

.prompt-detail-content.markdown-body :deep(code) {
  background: #f0f2f5;
  border-radius: 3px;
  padding: 2px 4px;
  font-size: 13px;
  font-family: 'Consolas', 'Monaco', monospace;
}

.prompt-detail-content.markdown-body :deep(pre code) {
  background: none;
  padding: 0;
}

.prompt-detail-content.markdown-body :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 8px 0;
}

.prompt-detail-content.markdown-body :deep(th),
.prompt-detail-content.markdown-body :deep(td) {
  border: 1px solid #e4e7ed;
  padding: 6px 10px;
  text-align: left;
}

.prompt-detail-content.markdown-body :deep(th) {
  background: #f5f7fa;
  font-weight: 600;
}

.prompt-detail-content.markdown-body :deep(ul),
.prompt-detail-content.markdown-body :deep(ol) {
  padding-left: 24px;
  margin: 0.4em 0;
}

.prompt-detail-content.markdown-body :deep(blockquote) {
  border-left: 3px solid #dcdfe6;
  padding-left: 12px;
  color: #909399;
  margin: 0.4em 0;
}

.prompt-detail-content.markdown-body :deep(a) {
  color: #409eff;
  text-decoration: none;
}

.prompt-detail-content.markdown-body :deep(a:hover) {
  text-decoration: underline;
}
</style>
