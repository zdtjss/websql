<template>
  <el-dialog
    :model-value="modelValue"
    @update:model-value="$emit('update:modelValue', $event)"
    :title="isEdit ? '编辑提示词' : '新增提示词'"
    width="1000px"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    @open="handleOpen"
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
    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="标题" prop="title">
        <el-input v-model="form.title" placeholder="请输入提示词标题" maxlength="100" show-word-limit />
      </el-form-item>
      <el-form-item label="连接/Schema">
        <div v-if="connLoading" class="conn-schema-loading">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>加载连接中...</span>
        </div>
        <el-tree-select v-else
          v-model="selectedConnSchema"
          :data="connTree"
          :props="{ label: 'label', value: 'value', children: 'children', disabled: 'disabled' }"
          placeholder="选择数据库连接和Schema（可选）"
          class="modern-tree-select"
          filterable
          :check-on-click-node="true"
          clearable
          :teleported="false"
        />
      </el-form-item>
      <el-form-item label="内容" prop="content">
        <div ref="vditorContainerRef" class="vditor-container" v-loading="vditorLoading" element-loading-text="稍等片刻...">
        </div>
      </el-form-item>
      <el-form-item v-if="!roleId" label="分享给">
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
        <el-button v-if="!roleId" type="primary" @click="handleSendToAI" :disabled="!form.content.trim()">
          <el-icon><Promotion /></el-icon>
          发送
        </el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, onUnmounted, nextTick, watch, shallowRef } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage } from 'element-plus'
import { Close, Promotion, Loading } from '@element-plus/icons-vue'
import { loadVditorModule, ensureVditorCss, preloadVditor } from '@/utils/vditorLoader'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  promptId: { type: String, default: '' },
  roleId: { type: String, default: '' },
})

const emit = defineEmits(['update:modelValue', 'saved', 'sendToAI'])

const isEdit = computed(() => !!props.promptId)
const formRef = ref(null)
const saving = ref(false)
const userSearchLoading = ref(false)
const userOptions = ref([])
const vditorContainerRef = ref(null)
const vditorInstance = shallowRef(null)
const vditorLoading = ref(false)
const vditorReadyPromise = shallowRef(null)

const form = ref({
  id: '',
  title: '',
  content: '',
  roleId: '',
  connId: '',
  schema: '',
  sharedUserIds: [],
})

const rules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  content: [{ required: true, message: '请输入内容', trigger: 'blur' }],
}

const connLoading = ref(false)
const connSchemaList = ref([])
const connTree = ref([])
const selectedConnSchema = ref('')

function buildSchemaTree(rawList) {
  const dirMap = new Map()
  const noDir = []

  for (const item of rawList) {
    const schemas = item.schemas || []
    if (item.available === false) {
      const node = {
        label: item.name,
        value: item.connId + '::',
        connId: item.connId,
        schemaName: '',
        disabled: true,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
      continue
    }
    if (schemas.length <= 1) {
      const singleSchema = schemas.length === 1 ? schemas[0].name : (item.dbSchema || 'default')
      const node = {
        label: item.name,
        value: item.connId + '::' + singleSchema,
        connId: item.connId,
        schemaName: singleSchema,
        disabled: false,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(node)
      } else {
        noDir.push(node)
      }
    } else {
      const schemaChildren = schemas.map(s => ({
        label: s.name,
        value: item.connId + '::' + s.name,
        connId: item.connId,
        schemaName: s.name,
        disabled: false,
      }))
      const connNode = {
        label: item.name,
        value: '__conn__' + item.connId,
        disabled: true,
        children: schemaChildren,
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(connNode)
      } else {
        noDir.push(connNode)
      }
    }
  }

  const tree = []
  for (const [dirName, children] of dirMap) {
    tree.push({ label: dirName, value: '__dir__' + dirName, disabled: true, children })
  }
  tree.push(...noDir)
  return tree
}

async function loadConnList() {
  connLoading.value = true
  try {
    const auth = sessionStorage.getItem('authentication') || ''
    const apiBase = import.meta.env.VITE_API_URL || ''
    const resp = await fetch(apiBase + '/listUserConnSchemasStream', {
      headers: { 'Authorization': auth }
    })
    if (!resp.ok) {
      throw new Error('HTTP ' + resp.status)
    }
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    const rawList = []

    while (true) {
      const { done, value } = await reader.read()
      if (value && value.length > 0) {
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop()
        for (const line of lines) {
          const trimmed = line.trim()
          if (!trimmed) continue
          try {
            const data = JSON.parse(trimmed)
            if (data.connId) {
              rawList.push(data)
              connTree.value = buildSchemaTree(rawList)
            }
          } catch (e) {
            console.warn('parse error for line', trimmed, e)
          }
        }
      }
      if (done) break
    }
    connSchemaList.value = rawList
    if (rawList.length === 0) {
      throw new Error('no connections')
    }
  } catch (e) {
    console.error('加载连接列表失败，降级使用旧接口', e)
    try {
      const resp = await http.get('/listUserConn')
      const data = resp.data.data || []
      const converted = data.map(conn => ({
        connId: conn.id,
        name: conn.name,
        dirName: conn.dirName,
        dbType: conn.dbType,
        available: conn.available !== false,
        schemas: [{ name: conn.dbSchema || 'default' }],
      }))
      connSchemaList.value = converted
      connTree.value = buildSchemaTree(converted)
    } catch (e2) {
      console.error('加载连接列表完全失败', e2)
    }
  } finally {
    connLoading.value = false
  }
}

async function initVditor() {
  if (!vditorContainerRef.value) return

  // 如果已有实例且容器未变，直接复用
  if (vditorInstance.value) {
    vditorInstance.value.setValue(form.value.content || '')
    return
  }

  vditorLoading.value = true

  // 并行加载 CSS 和 JS 模块
  ensureVditorCss()
  const Vditor = await loadVditorModule()

  // 加载完成后容器可能已被销毁（用户快速关闭）
  if (!vditorContainerRef.value) {
    vditorLoading.value = false
    return
  }

  vditorReadyPromise.value = new Promise((resolve) => {
    vditorInstance.value = new Vditor(vditorContainerRef.value, {
      height: 500,
      width: '100%',
      mode: 'ir',
      preview: { mode: 'editor' },
      placeholder: '请输入提示词内容（支持 Markdown 格式）',
      lang: 'zh_CN',
      toolbar: [
        'headings', 'bold', 'italic', 'strike', '|',
        'line', 'quote', 'list', 'ordered-list', 'check', '|',
        'code', 'inline-code', 'table', 'link', 'undo', 'redo', '|', 'fullscreen',
      ],
      cache: { enable: false },
      // 关闭不需要的重型子模块，避免运行时再去加载 WASM/大 JS
      math: false,
      graph: false,
      codeHighlight: false,
      after: () => {
        vditorLoading.value = false
        resolve()
        if (form.value.content) {
          vditorInstance.value.setValue(form.value.content)
        }
      },
      input: (value) => {
        form.value.content = value
      },
    })
  })
}

async function waitForVditorReady() {
  if (vditorReadyPromise.value) {
    await vditorReadyPromise.value
  }
}

function handleOpen() {
  preloadVditor()
  loadConnList()
  if (props.promptId) {
    loadPromptDetail(props.promptId)
  } else {
    form.value = { id: '', title: '', content: '', roleId: props.roleId || '', connId: '', schema: '', sharedUserIds: [] }
    selectedConnSchema.value = ''
    userOptions.value = []
    if (vditorInstance.value) {
      vditorInstance.value.setValue('')
    }
  }
  nextTick(() => {
    initVditor()
  })
}

watch(selectedConnSchema, (val) => {
  if (!val) {
    form.value.connId = ''
    form.value.schema = ''
    return
  }
  const idx = val.indexOf('::')
  if (idx !== -1) {
    form.value.connId = val.substring(0, idx)
    form.value.schema = val.substring(idx + 2)
  }
})

watch(() => props.modelValue, (visible) => {
  if (!visible) {
    if (vditorInstance.value) {
      vditorInstance.value.setValue('')
    }
  }
})

onUnmounted(() => {
  if (vditorInstance.value) {
    vditorInstance.value.destroy()
    vditorInstance.value = null
  }
})

async function loadPromptDetail(id) {
  try {
    const resp = await http.get('/promptDetail', { params: { id } })
    const data = resp.data.data
    if (data) {
      form.value = {
        id: data.id,
        title: data.title,
        content: data.content,
        roleId: data.roleId || props.roleId || '',
        connId: data.connId || '',
        schema: data.schema || '',
        sharedUserIds: data.sharedUserIds || [],
      }
      if (data.connId && data.schema) {
        selectedConnSchema.value = data.connId + '::' + data.schema
      } else {
        selectedConnSchema.value = ''
      }
      if (data.sharedUsers && data.sharedUsers.length > 0) {
        userOptions.value = data.sharedUsers.map(u => ({ id: u.id, name: u.name, loginName: u.loginName || '' }))
      }
      if (vditorInstance.value) {
        vditorInstance.value.setValue(form.value.content)
      } else {
        await waitForVditorReady()
        if (vditorInstance.value) {
          vditorInstance.value.setValue(form.value.content)
        }
      }
    }
  } catch (e) {
    console.error('加载提示词详情失败:', e)
  }
}

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
  emit('sendToAI', form.value.content, {
    connId: form.value.connId,
    schema: form.value.schema,
  })
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

.modern-tree-select {
  width: 100%;
}

.conn-schema-loading {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  padding: 8px 0;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.vditor-container {
  width: 100%;
}

.vditor-container :deep(.vditor) {
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
}

.vditor-container :deep(.vditor-toolbar) {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-start;
  background-color: var(--el-fill-color-light) !important;
  border-bottom-color: var(--el-border-color-light) !important;
}

.vditor-container :deep(.vditor-toolbar__item) {
  flex-shrink: 0;
}

.vditor-container :deep(.vditor-reset) {
  background-color: var(--el-bg-color) !important;
  color: var(--el-text-color-primary) !important;
}
</style>
