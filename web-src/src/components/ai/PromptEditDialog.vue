<template>
  <el-dialog
    v-model="dialogVisible"
    :title="dialogTitle"
    width="1000px"
    :draggable="true"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    @open="handleOpen"
    class="prompt-edit-dialog"
    :show-close="false"
  >
    <template #header="{ close }">
      <div class="dialog-header">
        <span class="dialog-title">{{ dialogTitle }}</span>
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
        <div class="vditor-wrapper">
          <!-- 编辑器异步加载期间先用纯文本输入框兜底，内容立即可见、可编辑 -->
          <div v-if="!vditorReady" class="vditor-fallback">
            <el-input
              v-model="form.content"
              type="textarea"
              resize="none"
              class="vditor-fallback-input"
              placeholder="请输入提示词内容（支持 Markdown 格式）"
            />
            <div class="vditor-fallback-tip">
              <el-icon class="is-loading"><Loading /></el-icon>
              <span>正在加载 Markdown 编辑器，可直接先输入内容…</span>
            </div>
          </div>
          <!-- 注意：动态 class 必须加在外层容器上。Vditor 会给挂载元素追加自己的
               `vditor` class，若对该元素做 :class 绑定，Vue 更新时会整体覆盖
               className，导致 vditor 样式（flex 布局）丢失、内容溢出弹窗 -->
          <div class="vditor-holder" :class="{ 'vditor-holder--hidden': !vditorReady }">
            <div ref="vditorContainerRef" class="vditor-container"></div>
          </div>
        </div>
      </el-form-item>
      <el-form-item v-if="!form.roleId && isRemote" label="分享给">
        <el-select
          v-model="form.sharedUserIds"
          multiple
          filterable
          remote
          :multiple-limit="MAX_SHARED_USERS"
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
import { findUserBase } from '@/api/system'
import { ElMessage } from 'element-plus'
import { Close, Promotion, Loading } from '@element-plus/icons-vue'
import { loadVditorModule, ensureVditorCss, preloadVditor, preloadVditorWhenIdle, getVditorLutePath, getVditorI18n } from '@/utils/vditorLoader'

const dialogVisible = defineModel({ default: false })

const { promptId, roleId, initialTitle, initialContent, initialConnSchemas, initialTables } = defineProps({
  promptId: { type: String, default: '' },
  roleId: { type: String, default: '' },
  // 新增场景的预填内容：用于「从对话提取提示词」后直接进入编辑确认
  initialTitle: { type: String, default: '' },
  initialContent: { type: String, default: '' },
  // 提取场景：会话中实际使用的连接/Schema，打开时预填「连接/Schema」
  initialConnSchemas: { type: Array, default: () => [] },
  // 提取场景：会话中实际使用的表（表标签），打开时预填「相关表」
  initialTables: { type: Array, default: () => [] },
})

const emit = defineEmits(['saved', 'sendToAI'])

const isRemote = sessionStorage.getItem('isRemote') === 'true'
const isEdit = computed(() => !!promptId)
const dialogTitle = computed(() => {
  if (isEdit.value) return '编辑提示词'
  return initialTitle || initialContent ? '保存提取的提示词' : '新增提示词'
})
const formRef = useTemplateRef('formRef')
const saving = ref(false)
const userSearchLoading = ref(false)
// 与后端 prompt_policy.go 的 promptMaxSharedUsers 保持一致，后端仍是最终校验方。
const MAX_SHARED_USERS = 50
// 用户选项用 id -> option 映射维护：搜索结果只替换“非已选”部分，
// 已选用户的标签不随后续搜索变化，避免退化成原始 ID。
const userOptionMap = ref(new Map())
const userOptions = computed(() => Array.from(userOptionMap.value.values()))
const vditorContainerRef = useTemplateRef('vditorContainerRef')
const vditorInstance = shallowRef(null)
const vditorReady = ref(false)
const vditorReadyPromise = shallowRef(null)

// 打开页面空闲时就开始预加载编辑器模块，避免首次打开提示词弹窗时干等
preloadVditorWhenIdle()

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
    await waitForVditorReady()
    vditorReady.value = true
    vditorInstance.value.setValue(form.value.content || '')
    return
  }

  try {
    ensureVditorCss()
    const Vditor = await loadVditorModule()

    if (!vditorContainerRef.value) return

    vditorReadyPromise.value = new Promise((resolve) => {
      vditorInstance.value = new Vditor(vditorContainerRef.value, {
        _lutePath: getVditorLutePath(),
        height: 500,
        width: '100%',
        mode: 'ir',
        preview: {
          mode: 'editor',
          // 关闭代码块高亮：遇到代码块时 Vditor 会额外从 CDN 拉取 1MB+ 的 highlight.js
          hljs: { enable: false },
        },
        placeholder: '请输入提示词内容（支持 Markdown 格式）',
        lang: 'zh_CN',
        i18n: getVditorI18n() || undefined,
        toolbar: [
          'headings', 'bold', 'italic', 'strike', '|',
          'line', 'quote', 'list', 'ordered-list', 'check', '|',
          'code', 'inline-code', 'table', 'link', 'undo', 'redo', '|', 'fullscreen',
        ],
        cache: { enable: false },
        after: () => {
          // Lute 默认开启「标题 ID」扩展，会把标题末尾的 `{...}` 当成 heading-id 语法，
          // 导致 `## {{业务域一}}` 被解析成空标题 + 残余 `}`。提示词模板普遍使用
          // {{占位符}}，关闭该扩展以保证标题里的占位符原样显示。
          // 注意：Lute 实例挂在 Vditor 内部对象 `vditor` 上，直接访问实例的 `lute`
          // 取不到（optional chaining 会静默跳过，看似调用成功实则未生效）。
          vditorInstance.value?.vditor?.lute?.SetHeadingID(false)
          vditorReady.value = true
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
  } catch (e) {
    // 编辑器加载失败时保留纯文本输入框，内容仍可正常编辑与保存
    console.error('Markdown 编辑器加载失败，已降级为纯文本输入', e)
  }
}

async function waitForVditorReady() {
  if (vditorReadyPromise.value) {
    await vditorReadyPromise.value
  }
}

// 提示词正文本身就是 Markdown。从对话提取或从别处粘贴的内容，常被整段包在
// ```markdown 围栏里，Vditor 会把围栏内容按代码块渲染（列表、表格都不再排版），
// 这里把这类围栏解包成普通 Markdown，其它语言的代码块保持原样。
const MARKDOWN_FENCE_OPEN_RE = /^(\s*)(`{3,}|~{3,})\s*(?:markdown|md)\s*$/i

function unwrapMarkdownFences(markdown) {
  if (!markdown || !markdown.includes('```')) return markdown
  const lines = markdown.split(/\r?\n/)
  const out = []
  let i = 0
  while (i < lines.length) {
    const open = lines[i].match(MARKDOWN_FENCE_OPEN_RE)
    if (!open) {
      out.push(lines[i])
      i++
      continue
    }
    const marker = open[2]
    const closeRe = new RegExp(`^\\s*${marker[0]}{${marker.length},}\\s*$`)
    let end = i + 1
    while (end < lines.length && !closeRe.test(lines[end])) end++
    // 没有闭合围栏时保持原样，避免误删正文
    if (end >= lines.length) {
      out.push(lines[i])
      i++
      continue
    }
    const indent = open[1]
    for (const line of lines.slice(i + 1, end)) {
      out.push(indent && line.startsWith(indent) ? line.slice(indent.length) : line)
    }
    i = end + 1
  }
  return out.join('\n')
}

function handleOpen() {
  preloadVditor()
  loadConnList()
  // 每次打开统一重置分享选项，避免切换不同提示词时残留上一次的用户列表。
  userOptionMap.value = new Map()
  if (promptId) {
    loadPromptDetail(promptId)
  } else {
    const schemaValues = (initialConnSchemas || [])
      .filter(cs => cs && cs.connId && cs.schema)
      .map(cs => cs.connId + '::' + cs.schema)
    const tableValues = (initialTables || []).filter(t => typeof t === 'string' && t)
    form.value = {
      id: '',
      title: initialTitle || '',
      content: unwrapMarkdownFences(initialContent || ''),
      roleId: roleId || '',
      connSchemas: [],
      tables: [],
      sharedUserIds: [],
    }
    selectedConnSchema.value = schemaValues
    selectedTables.value = []
    tableList.value = []
    if (schemaValues.length > 0) {
      // 表选项依赖连接/Schema，加载完成后再按可用表回填，避免出现无效标签
      loadTableListForSchemas().then(() => {
        if (tableValues.length === 0) return
        const available = new Set(tableList.value.map(t => t.label || t.name))
        selectedTables.value = tableValues.filter(t => available.has(t))
      })
    }
    if (vditorInstance.value && vditorReady.value) {
      vditorInstance.value.setValue(form.value.content)
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
    if (vditorInstance.value && vditorReady.value) {
      vditorInstance.value.setValue('')
    }
  }
})

onUnmounted(() => {
  clearTimeout(searchTimer)
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
        content: unwrapMarkdownFences(data.content || ''),
        roleId: data.roleId || roleId || '',
        connSchemas: data.connSchemas || [],
        tables: data.tables || [],
        sharedUserIds: data.sharedUserIds || [],
      }
      selectedConnSchema.value = (data.connSchemas || []).map(cs => cs.connId + '::' + cs.schema)
      applySharedUsers(data.sharedUserIds || [], data.sharedUsers || [])
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
      await waitForVditorReady()
      if (vditorInstance.value) {
        vditorInstance.value.setValue(form.value.content)
      }
    }
  } catch (e) {
    console.error('加载提示词详情失败:', e)
  }
}

// 详情返回的分享名单：以 sharedUserIds 为已选保底，再用 sharedUsers 补全标签。
function applySharedUsers(sharedUserIds, sharedUsers) {
  form.value.sharedUserIds = sharedUserIds || []
  const next = new Map()
  for (const id of form.value.sharedUserIds) {
    next.set(id, { id, name: id, loginName: '' })
  }
  for (const u of sharedUsers || []) {
    if (!u?.id) continue
    next.set(u.id, { id: u.id, name: u.name || u.loginName || u.id, loginName: u.loginName || '' })
  }
  userOptionMap.value = next
}

// 搜索结果只替换非已选项，已选项保留原有姓名标签。
function replaceOptionsWithSelection(users) {
  const next = new Map()
  for (const id of form.value.sharedUserIds) {
    const old = userOptionMap.value.get(id)
    if (old) next.set(id, old)
  }
  for (const u of users) {
    if (!u?.id) continue
    next.set(u.id, { id: u.id, name: u.name || u.loginName || u.id, loginName: u.loginName || '' })
  }
  for (const id of form.value.sharedUserIds) {
    if (!next.has(id)) next.set(id, { id, name: id, loginName: '' })
  }
  userOptionMap.value = next
}

// 清空搜索时只保留已选用户，已选项缺失标签时用 ID 保底显示。
function pruneUnselectedOptions() {
  const next = new Map()
  for (const id of form.value.sharedUserIds) {
    next.set(id, userOptionMap.value.get(id) || { id, name: id, loginName: '' })
  }
  userOptionMap.value = next
}

let searchTimer = null
let searchSeq = 0

function searchUsers(query) {
  const keyword = (query || '').trim()
  clearTimeout(searchTimer)
  if (!keyword) {
    // 让在途请求的响应失效，同时清空非已选结果。
    searchSeq++
    userSearchLoading.value = false
    pruneUnselectedOptions()
    return
  }
  const seq = ++searchSeq
  userSearchLoading.value = true
  searchTimer = setTimeout(async () => {
    try {
      const resp = await findUserBase(keyword)
      if (seq !== searchSeq) return
      replaceOptionsWithSelection(resp.data.data || [])
    } catch (e) {
      if (seq === searchSeq) ElMessage.error('搜索用户失败')
    } finally {
      if (seq === searchSeq) userSearchLoading.value = false
    }
  }, 300)
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
    // 403/400 的文案已由请求拦截器统一提示（如“无权修改该提示词”）
    if (!e?.response?.data?.msg) ElMessage.error('保存失败')
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
  position: relative;
  width: 100%;
  min-height: 500px;
}

.vditor-fallback {
  width: 100%;
}

.vditor-fallback-input :deep(.el-textarea__inner) {
  height: 500px;
  font-family: Consolas, Monaco, "Courier New", monospace;
  line-height: 1.6;
}

.vditor-fallback-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.vditor-holder {
  width: 100%;
}

/* 加载期间编辑器先在后台初始化，避免撑出空白区域 */
.vditor-holder--hidden {
  position: absolute;
  inset: 0;
  visibility: hidden;
  pointer-events: none;
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
