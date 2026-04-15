<template>
  <el-dialog
    :model-value="modelValue"
    @update:model-value="$emit('update:modelValue', $event)"
    :title="isEdit ? '编辑提示词' : '新增提示词'"
    width="1000px"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    @open="handleOpen"
    @closed="handleClosed"
    class="prompt-edit-dialog"
    :show-close="false"
  >
    <template #header="{ close }">
      <div class="dialog-header">
        <span class="dialog-title">{{ isEdit ? '编辑提示词' : '新增提示词' }}</span>
        <div class="dialog-header-actions">
          <el-button text @click="close" title="关闭">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
      </div>
    </template>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
      <el-form-item label="标题" prop="title">
        <el-input v-model="form.title" placeholder="请输入提示词标题" maxlength="100" show-word-limit />
      </el-form-item>
      <el-form-item label="内容" prop="content">
        <MdEditor
          ref="editorRef"
          v-model="form.content"
          style="height: 450px"
          :preview="true"
          :toolbarsExclude="['image', 'save', 'fullscreen', 'github', 'htmlPreview', 'strikeThrough']"
          language="zh-CN"
          placeholder="请输入提示词内容（支持 Markdown 格式）"
        />
      </el-form-item>
      <el-form-item label="分享给">
        <el-select
          v-model="form.sharedUserIds"
          multiple
          filterable
          remote
          :remote-method="searchUsers"
          :loading="userSearchLoading"
          placeholder="搜索并选择用户进行分享"
          class="share-select"
        >
          <el-option v-for="u in userOptions" :key="u.id" :label="u.name || u.loginName" :value="u.id" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <div class="dialog-footer">
        <el-button @click="$emit('update:modelValue', false)">取消</el-button>
        <el-button type="primary" @click="handleSendToAI" :disabled="!form.content.trim()">
          <el-icon><Promotion /></el-icon>
          发送
        </el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage } from 'element-plus'
import { Close, Promotion } from '@element-plus/icons-vue'
import { MdEditor } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  promptId: { type: String, default: '' },
})

const emit = defineEmits(['update:modelValue', 'saved', 'sendToAI'])

const isEdit = computed(() => !!props.promptId)
const formRef = ref(null)
const saving = ref(false)
const userSearchLoading = ref(false)
const userOptions = ref([])
const editorRef = ref(null)
const isPageFullscreen = ref(false)

const form = ref({
  id: '',
  title: '',
  content: '',
  sharedUserIds: [],
})

const rules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  content: [{ required: true, message: '请输入内容', trigger: 'blur' }],
}

function handleOpen() {
  if (props.promptId) {
    loadPromptDetail(props.promptId)
  } else {
    form.value = { id: '', title: '', content: '', sharedUserIds: [] }
    userOptions.value = []
  }
  nextTick(() => {
    if (editorRef.value) {
      editorRef.value.on('pageFullscreen', (status) => {
        isPageFullscreen.value = status
      })
    }
  })
}

function handleClosed() {
  isPageFullscreen.value = false
}

function handleEscKey(event) {
  if (event.key === 'Escape' && props.modelValue) {
    if (isPageFullscreen.value) {
      event.preventDefault()
      event.stopPropagation()
      editorRef.value?.togglePageFullscreen(false)
    } else {
      emit('update:modelValue', false)
    }
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleEscKey, true)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleEscKey, true)
})

async function searchUsers(query) {
  if (!query) return
  userSearchLoading.value = true
  try {
    const resp = await http.get('/findUserBase', { params: { loginName: query } })
    const users = resp.data.data || []
    userOptions.value = users.map(u => ({ id: u.id, name: u.name, loginName: u.loginName }))
    const existingIds = new Set(userOptions.value.map(u => u.id))
    for (const uid of form.value.sharedUserIds) {
      if (!existingIds.has(uid)) {
        userOptions.value.push({ id: uid, name: uid, loginName: '' })
      }
    }
  } catch (e) {
    console.error('搜索用户失败:', e)
  } finally {
    userSearchLoading.value = false
  }
}

async function loadPromptDetail(id) {
  try {
    const resp = await http.get('/promptDetail', { params: { id } })
    const data = resp.data.data
    if (data) {
      form.value = {
        id: data.id,
        title: data.title,
        content: data.content,
        sharedUserIds: data.sharedUserIds || [],
      }
      if (data.sharedUsers && data.sharedUsers.length > 0) {
        userOptions.value = data.sharedUsers.map(u => ({ id: u.id, name: u.name, loginName: u.loginName || '' }))
      }
    }
  } catch (e) {
    console.error('加载提示词详情失败:', e)
  }
}

async function handleSave() {
  if (!formRef.value) return
  await formRef.value.validate()

  saving.value = true
  try {
    await http.post('/savePrompt', form.value)
    ElMessage.success('保存成功')
    emit('saved')
    emit('update:modelValue', false)
  } catch (e) {
    console.error('保存提示词失败:', e)
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

function handleSendToAI() {
  if (!form.value.content.trim()) return
  emit('sendToAI', form.value.content)
}
</script>

<style scoped>
.prompt-edit-dialog :deep(.el-dialog__body) {
  padding: 16px 20px;
}

.dialog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.dialog-title {
  font-size: 18px;
  font-weight: 600;
  color: #303133;
}

.dialog-header-actions {
  display: flex;
  gap: 4px;
}

.share-select {
  width: 100%;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
