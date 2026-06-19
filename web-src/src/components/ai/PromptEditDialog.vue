<template>
  <el-dialog
    v-model="dialogVisible"
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
          placeholder="选择数据库连接和Schema（可选，支持多选）"
          class="modern-tree-select"
          filterable
          multiple
          :check-on-click-node="true"
          clearable
          collapse-tags
          collapse-tags-tooltip
          max-collapse-tags="3"
          :teleported="false"
          @change="handleConnSchemaChange"
        />
      </el-form-item>
      <el-form-item label="相关表">
        <div v-if="tablesLoading" class="conn-schema-loading">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>加载表列表...</span>
        </div>
        <el-select v-else v-model="selectedTables" multiple filterable placeholder="请先选择连接/Schema，再选择表" class="modern-select" :disabled="selectedConnSchema.length === 0">
          <template #tag="{ data, deleteTag, selectDisabled }">
            <el-tag
              v-for="item in data.slice(0, 2)"
              :key="item.value"
              :closable="!selectDisabled && !item.isDisabled"
              @close="deleteTag($event, item)"
              size="small"
              disable-transitions
            >
              <el-tooltip :content="getTableComment(item.currentLabel)" :disabled="!getTableComment(item.currentLabel)" placement="top">
                <span>{{ item.currentLabel }}</span>
              </el-tooltip>
            </el-tag>
            <el-tooltip v-if="data.length > 2" placement="bottom">
              <template #content>
                <div v-for="item in data.slice(2)" :key="'c-' + item.value" style="line-height: 2;">
                  {{ item.currentLabel }}<span v-if="getTableComment(item.currentLabel)" style="color: var(--el-text-color-secondary); margin-left: 6px;">{{ getTableComment(item.currentLabel) }}</span>
                </div>
              </template>
              <el-tag size="small" type="info" disable-transitions>+ {{ data.length - 2 }}</el-tag>
            </el-tooltip>
          </template>
          <el-option v-for="table in tableList" :key="table.name + (table.schema || '')"
            :label="table.label || table.name"
            :value="table.label || table.name">
            <div class="table-option-content">
              <span class="table-option-name">{{ table.name }}</span>
              <span v-if="table.comment" class="table-option-comment">{{ table.comment }}</span>
              <span v-if="table.schema && selectedConnSchema.length > 1" class="table-option-schema">{{ table.schema }}</span>
            </div>
          </el-option>
        </el-select>
      </el-form-item>
      <el-form-item label="内容" prop="content">
        <div class="vditor-wrapper" v-loading="vditorLoading" element-loading-text="稍等片刻...">
          <div ref="vditorContainerRef" class="vditor-container"></div>
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
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button v-if="!roleId" type="primary" @click="handleSendToAI" :disabled="!form.content.trim()">
          <el-icon><Promotion /></el-icon>
          发送到AI模型
        </el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, onUnmounted, nextTick, watch, shallowRef, useTemplateRef } from 'vue'
import { listTableNamesBySchemas } from '@/api/conn'
import { getPromptDetail, savePrompt } from '@/api/ai'
import { findUserByLoginName } from '@/api/system'
import { ElMessage } from 'element-plus'
import { Close, Promotion, Loading } from '@element-plus/icons-vue'
import { loadVditorModule, ensureVditorCss, preloadVditor } from '@/utils/vditorLoader'

const dialogVisible = defineModel({ default: false })

const { promptId, roleId } = defineProps({
  promptId: { type: String, default: '' },
  roleId: { type: String, default: '' },
})

const emit = defineEmits(['saved', 'sendToAI'])

const isEdit = computed(() => !!promptId)
const formRef = useTemplateRef('formRef')
const saving = ref(false)
const userSearchLoading = ref(false)
const userOptions = ref([])
const vditorContainerRef = useTemplateRef('vditorContainerRef')
const vditorInstance = shallowRef(null)
const vditorLoading = ref(false)
const vditorReadyPromise = shallowRef(null)

const form = ref({
  id: '',
  title: '',
  content: '',
  roleId: '',
  connSchemas: [],
  tables: [],
  sharedUserIds: [],
})

const rules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  content: [{ required: true, message: '请输入内容', trigger: 'blur' }],
}

const connLoading = ref(false)
const connSchemaList = ref([])
const connTree = ref([])
const selectedConnSchema = ref([])
const tablesLoading = ref(false)
const tableList = ref([])
const selectedTables = ref([])

function parseSchemaValue(value) {
  const idx = value.indexOf('::')
  if (idx === -1) return null
  return { connId: value.substring(0, idx), schema: value.substring(idx + 2) }
}

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
      const singleSchema = schemas.length === 1 ? schemas[0].name : (item.dbSchema || '')
      const schemaChild = {
        label: singleSchema,
        value: item.connId + '::' + singleSchema,
        connId: item.connId,
        schemaName: singleSchema,
        disabled: false,
      }
      const connNode = {
        label: item.name,
        value: '__conn__' + item.connId,
        disabled: true,
        children: [schemaChild],
      }
      const dir = item.dirName
      if (dir) {
        if (!dirMap.has(dir)) dirMap.set(dir, [])
        dirMap.get(dir).push(connNode)
      } else {
        noDir.push(connNode)
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
          if (!line.startsWith('data:')) continue
          const data = line.slice(5).trim()
          if (!data) continue
          if (data === '"ok"' || data === '"empty"') continue
          try {
            const item = JSON.parse(data)
            if (item.connId) {
              rawList.push(item)
              connTree.value = buildSchemaTree(rawList)
            }
          } catch (_) {}
        }
      }
      if (done) break
    }
    connSchemaList.value = rawList
    if (rawList.length === 0) {
      throw new Error('no connections')
    }
  } catch (e) {
    if (e.response && e.response.status === 401) return
    console.error('加载连接列表失败', e)
  } finally {
    connLoading.value = false
  }
}

async function loadTableListForSchemas() {
  tablesLoading.value = true
  if (selectedConnSchema.value.length === 0) {
    tableList.value = []
    tablesLoading.value = false
    return
  }
  try {
    const schemaRefs = selectedConnSchema.value
      .map(v => parseSchemaValue(v))
      .filter(Boolean)

    if (schemaRefs.length === 0) {
      tableList.value = []
      tablesLoading.value = false
      return
    }

    const resp = await listTableNamesBySchemas(schemaRefs)
    const tables = resp.data.data || []
    const allTables = tables.map(t => {
      const hasSchema = t.schema && selectedConnSchema.value.length > 1
      return {
        name: t.name,
        comment: t.comment || '',
        schema: t.schema || '',
        label: hasSchema ? t.schema + '.' + t.name : t.name,
      }
    })
    tableList.value = allTables

    if (selectedTables.value.length > 0) {
      const newValues = allTables.map(t => t.label || t.name)
      selectedTables.value = selectedTables.value.filter(name => newValues.includes(name))
    }
  } catch (e) {
    tableList.value = []
  } finally {
    tablesLoading.value = false
  }
}

function handleConnSchemaChange() {
  loadTableListForSchemas()
}

function getTableComment(value) {
  const found = tableList.value.find(t => (t.label || t.name) === value)
  return found?.comment || ''
}

async function initVditor() {
  if (!vditorContainerRef.value) return

  if (vditorInstance.value) {
    vditorInstance.value.setValue(form.value.content || '')
    return
  }

  vditorLoading.value = true

  ensureVditorCss()
  const Vditor = await loadVditorModule()

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
  if (promptId) {
    loadPromptDetail(promptId)
  } else {
    form.value = { id: '', title: '', content: '', roleId: roleId || '', connSchemas: [], tables: [], sharedUserIds: [] }
    selectedConnSchema.value = []
    selectedTables.value = []
    tableList.value = []
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
  form.value.connSchemas = val.map(v => {
    const idx = v.indexOf('::')
    if (idx !== -1) {
      return { connId: v.substring(0, idx), schema: v.substring(idx + 2) }
    }
    return null
  }).filter(Boolean)
})

watch(selectedTables, (val) => {
  form.value.tables = val.map(name => {
    const found = tableList.value.find(t => (t.label || t.name) === name)
    return { name, comment: found?.comment || '' }
  })
})

watch(dialogVisible, (visible) => {
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
    const resp = await getPromptDetail(id)
    const data = resp.data.data
    if (data) {
      form.value = {
        id: data.id,
        title: data.title,
        content: data.content,
        roleId: data.roleId || roleId || '',
        connSchemas: data.connSchemas || [],
        tables: data.tables || [],
        sharedUserIds: data.sharedUserIds || [],
      }
      selectedConnSchema.value = (data.connSchemas || []).map(cs => cs.connId + '::' + cs.schema)
      if (data.sharedUsers && data.sharedUsers.length > 0) {
        userOptions.value = data.sharedUsers.map(u => ({ id: u.id, name: u.name, loginName: u.loginName || '' }))
      }
      if (data.connSchemas && data.connSchemas.length > 0) {
        await loadTableListForSchemas()
        if (data.tables && data.tables.length > 0) {
          const availableNames = tableList.value.map(t => t.label || t.name)
          const tableNames = data.tables.map(t => typeof t === 'string' ? t : t.name)
          selectedTables.value = tableNames.filter(name => availableNames.includes(name))
        } else {
          selectedTables.value = []
        }
      } else {
        selectedTables.value = []
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
    const resp = await findUserByLoginName(query)
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
    await savePrompt(form.value)
    ElMessage.success('保存成功')
    emit('saved')
    dialogVisible.value = false
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
    connSchemas: form.value.connSchemas,
    tables: form.value.tables,
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

.modern-select {
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

.vditor-wrapper {
  min-height: 500px;
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

.table-option-content {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.table-option-name {
  font-weight: 500;
}

.table-option-comment {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 200px;
}

.table-option-schema {
  color: var(--el-color-primary);
  font-size: 11px;
  background: var(--el-color-primary-light-9);
  padding: 1px 6px;
  border-radius: 3px;
  flex-shrink: 0;
}
</style>
