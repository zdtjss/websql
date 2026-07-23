<template>
  <el-drawer
    v-model="visible"
    title="SQL 收藏夹"
    :size="snippetDrawerWidth + 'px'"
    @close="visible = false"
  >
    <div v-if="visible" class="drawer-drag-handle"
        :style="{ right: snippetDrawerWidth + 'px' }"
        @mousedown="onSnippetDrawerDragStart"></div>

    <!-- 顶部操作栏 -->
    <div class="snippet-toolbar">
      <el-button size="small" type="primary" @click="showAddSnippet(true)">+ 收藏当前 SQL</el-button>
      <el-button size="small" @click="showAddSnippet(false)" :icon="Plus">新建</el-button>
      <el-upload
        :show-file-list="false"
        accept=".json"
        :http-request="handleImportFile"
      >
        <el-button size="small" :icon="Upload">导入</el-button>
      </el-upload>
      <el-button size="small" :icon="Download" @click="handleExport">导出</el-button>
      <el-button
        v-if="hasLocalSnippets"
        size="small"
        type="warning"
        @click="migrateFromLocal"
      >从本地迁移</el-button>
      <el-button size="small" text :icon="Refresh" @click="reloadAll" :loading="loading">刷新</el-button>
    </div>

    <!-- 搜索与标签过滤 -->
    <div class="snippet-filter">
      <el-input
        v-model="searchText"
        placeholder="搜索标题、描述、SQL 内容..."
        clearable
        size="small"
        :prefix-icon="Search"
        @input="onSearchInput"
        style="flex: 1;"
      />
      <el-select
        v-model="filterTag"
        placeholder="标签过滤"
        clearable
        size="small"
        style="width: 140px;"
        @change="loadList"
      >
        <el-option v-for="t in allTags" :key="t" :label="t" :value="t" />
      </el-select>
    </div>

    <!-- 主体：左侧分类树 + 右侧列表 -->
    <div class="snippet-body">
      <!-- 左侧分类 -->
      <div class="snippet-cates">
        <div class="snippet-cates-title">分类</div>
        <el-scrollbar>
          <div
            class="snippet-cate-item"
            :class="{ active: activeCategory === 'all' }"
            @click="selectCategory('all')"
          >
            <span>全部</span>
            <el-badge :value="totalCount" :max="999" type="info" />
          </div>
          <div
            v-for="cate in categories"
            :key="cate.name"
            class="snippet-cate-item"
            :class="{ active: activeCategory === cate.name }"
            @click="selectCategory(cate.name)"
          >
            <el-icon><Folder /></el-icon>
            <span class="cate-name" :title="cate.name">{{ cate.name }}</span>
            <el-badge :value="cate.count" :max="999" type="info" />
          </div>
        </el-scrollbar>
      </div>

      <!-- 右侧列表 -->
      <div class="snippet-list-wrap">
        <el-scrollbar>
          <div v-if="loading" class="snippet-empty">加载中...</div>
          <div v-else-if="filteredSnippets.length === 0" class="snippet-empty">
            暂无收藏的 SQL 片段
          </div>
          <div
            v-for="snippet in filteredSnippets"
            :key="snippet.id"
            class="snippet-card"
            @click="applySnippet(snippet)"
          >
            <div class="snippet-header">
              <span class="snippet-name">{{ snippet.title || '未命名' }}</span>
              <div class="snippet-ops" @click.stop>
                <el-icon class="snippet-op" :size="14" title="复制 SQL" @click="copySql(snippet)">
                  <CopyDocument />
                </el-icon>
                <el-icon class="snippet-op" :size="14" title="编辑" @click="showEditSnippet(snippet)">
                  <Edit />
                </el-icon>
                <el-icon class="snippet-op" :size="14" title="删除" @click="deleteSnippet(snippet)">
                  <Delete />
                </el-icon>
              </div>
            </div>
            <div v-if="snippet.description" class="snippet-desc">{{ snippet.description }}</div>
            <pre class="snippet-body-text">{{ snippet.sqlContent }}</pre>
            <div class="snippet-meta">
              <el-tag v-if="snippet.category" size="small" type="info">{{ snippet.category }}</el-tag>
              <el-tag
                v-for="t in splitTags(snippet.tags)"
                :key="t"
                size="small"
                effect="plain"
              >{{ t }}</el-tag>
              <span v-if="snippet.dbType" class="meta-text">{{ snippet.dbType }}</span>
              <span class="meta-time">{{ snippet.updatedAt || snippet.createdAt || '' }}</span>
            </div>
          </div>
        </el-scrollbar>
      </div>
    </div>

    <!-- 新增/编辑对话框 -->
    <el-dialog
      v-model="editDialogVisible"
      :title="editingId ? '编辑收藏' : '添加收藏'"
      width="750px"
      :append-to-body="true"
      destroy-on-close
    >
      <el-form ref="formRef" :model="formData" label-width="100px" size="default" :rules="rules">
        <el-form-item label="标题" prop="title">
          <el-input v-model="formData.title" placeholder="必填，简明描述用途" maxlength="255" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input v-model="formData.description" type="textarea" :rows="2" placeholder="选填，补充说明" />
        </el-form-item>
        <el-form-item label="SQL" prop="sqlContent">
          <el-input
            v-model="formData.sqlContent"
            type="textarea"
            :rows="8"
            placeholder="必填，SQL 语句"
            class="sql-textarea"
          />
        </el-form-item>
        <el-form-item label="分类" prop="category">
          <el-select
            v-model="formData.category"
            placeholder="选择或输入新分类"
            filterable
            allow-create
            clearable
            default-first-option
            style="width: 100%;"
          >
            <el-option
              v-for="cate in categoryOptions"
              :key="cate"
              :label="cate"
              :value="cate"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="标签" prop="tagsInput">
          <div class="tag-editor">
            <el-tag
              v-for="(t, idx) in formData.tagList"
              :key="t"
              closable
              size="small"
              @close="removeTag(idx)"
              style="margin-right: 4px; margin-bottom: 4px;"
            >{{ t }}</el-tag>
            <el-input
              v-if="tagInputVisible"
              ref="tagInputRef"
              v-model="tagInputValue"
              size="small"
              style="width: 120px;"
              placeholder="回车添加"
              @keyup.enter="addTag"
              @blur="addTag"
            />
            <el-button v-else size="small" @click="showTagInput">+ 标签</el-button>
          </div>
        </el-form-item>
        <el-form-item label="数据库类型" prop="dbType">
          <el-select v-model="formData.dbType" placeholder="选填" clearable style="width: 100%;">
            <el-option label="MySQL" value="mysql" />
            <el-option label="MariaDB" value="mariadb" />
            <el-option label="SQLite" value="sqlite" />
            <el-option label="Oracle" value="oracle" />
          </el-select>
        </el-form-item>
        <el-form-item label="关联连接">
          <el-input :model-value="currentConnName || connId" disabled placeholder="来自当前标签页" />
        </el-form-item>
        <el-form-item label="Schema">
          <el-input :model-value="schema" disabled placeholder="来自当前标签页" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button size="default" @click="editDialogVisible = false">取消</el-button>
        <el-button size="default" type="primary" :loading="saving" @click="saveSnippet">保存</el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { Delete, Edit, Download, Upload, Plus, Search, Refresh, Folder, CopyDocument } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { computed, nextTick, onMounted, ref, useTemplateRef, watch } from 'vue'
import {
  listSnippets,
  listSnippetCategories,
  listSnippetTags,
  saveSnippet as apiSaveSnippet,
  deleteSnippet as apiDeleteSnippet,
  importSnippets,
  exportSnippetsToFile,
  type SqlSnippet,
  type SnippetCategoryStat,
  type SnippetExportItem
} from '@/api/snippet'
import copyToClipboard from '@/utils/copy-to-clipboard'

// 旧版 localStorage 存储键，用于数据迁移
const LEGACY_STORAGE_KEY = 'websql_snippets'

const visible = defineModel({ default: false })
const props = defineProps({
  currentSql: { type: String, default: '' },
  connId:     { type: String, default: '' },
  schema:     { type: String, default: '' },
  dbType:     { type: String, default: '' },
  schemaPath: { type: String, default: '' },
})

// 从 schemaPath 中提取连接名称（格式：connName/schema 或仅 schema）
const currentConnName = computed(() => {
  if (!props.schemaPath) return ''
  const idx = props.schemaPath.lastIndexOf('/')
  return idx > 0 ? props.schemaPath.slice(0, idx) : ''
})
const emit = defineEmits(['apply'])

// 列表与过滤状态
const snippets = ref<SqlSnippet[]>([])
const categories = ref<SnippetCategoryStat[]>([])
const allTags = ref<string[]>([])
const searchText = ref('')
const filterTag = ref('')
const activeCategory = ref('all')
const loading = ref(false)
const saving = ref(false)

// 是否存在旧的本地收藏数据（用于显示迁移按钮）
const hasLocalSnippets = ref(false)

// 抽屉宽度与拖拽
const snippetDrawerWidth = ref(560)
const isDraggingSnippet = ref(false)

// 编辑对话框状态
const editDialogVisible = ref(false)
const editingId = ref('')
const formRef = useTemplateRef<FormInstance>('formRef')
const tagInputRef = useTemplateRef<any>('tagInputRef')
const tagInputVisible = ref(false)
const tagInputValue = ref('')

const formData = ref({
  title: '',
  description: '',
  sqlContent: '',
  category: '',
  tagList: [] as string[],
  dbType: '',
  connId: '',
  schemaName: '',
})

const rules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  sqlContent: [{ required: true, message: '请输入 SQL 语句', trigger: 'blur' }],
}

// 分类下拉选项（去重），用于编辑表单的 allow-create 选择
const categoryOptions = computed(() => {
  const set = new Set<string>()
  categories.value.forEach(c => set.add(c.name))
  return Array.from(set)
})

// 总条数
const totalCount = computed(() =>
  categories.value.reduce((sum, c) => sum + c.count, 0)
)

// 前端二次过滤（关键字在标题/描述/SQL 中匹配；后端已支持，这里兜底处理本地缓存场景）
const filteredSnippets = computed(() => {
  const kw = searchText.value.trim().toLowerCase()
  if (!kw) return snippets.value
  return snippets.value.filter(s =>
    (s.title || '').toLowerCase().includes(kw) ||
    (s.description || '').toLowerCase().includes(kw) ||
    (s.sqlContent || '').toLowerCase().includes(kw)
  )
})

// 标签字符串拆分为数组（去除空白与空项）
function splitTags(tags?: string): string[] {
  if (!tags) return []
  return tags.split(',').map(t => t.trim()).filter(Boolean)
}

// 搜索防抖
let searchTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => loadList(), 250)
}

// 加载列表（带过滤参数）
async function loadList() {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (searchText.value.trim()) params.keyword = searchText.value.trim()
    if (activeCategory.value && activeCategory.value !== 'all') {
      // 未分类使用后端约定的哨兵值，区分"不按分类过滤"与"分类为空"
      params.category = activeCategory.value === '未分类' ? '__uncategorized__' : activeCategory.value
    }
    if (filterTag.value) params.tag = filterTag.value
    const resp = await listSnippets(params)
    const result = resp.data?.data
    snippets.value = result?.items || []
  } catch (e) {
    // 错误已由全局拦截器提示
  } finally {
    loading.value = false
  }
}

// 加载分类与标签
async function loadMeta() {
  try {
    const [cateResp, tagResp] = await Promise.all([listSnippetCategories(), listSnippetTags()])
    categories.value = cateResp.data?.data || []
    allTags.value = tagResp.data?.data || []
  } catch (e) {
    // 忽略
  }
}

// 重新加载全部
async function reloadAll() {
  await Promise.all([loadList(), loadMeta()])
}

// 选择分类
function selectCategory(name: string) {
  activeCategory.value = name
  loadList()
}

// 显示新增对话框（useCurrentSql=true 时预填当前编辑器 SQL，false 时新建空白）
function showAddSnippet(useCurrentSql: boolean = true) {
  editingId.value = ''
  formData.value = {
    title: '',
    description: '',
    sqlContent: useCurrentSql ? (props.currentSql || '') : '',
    category: '',
    tagList: [],
    dbType: props.dbType || '',
    connId: props.connId || '',
    schemaName: props.schema || '',
  }
  tagInputVisible.value = false
  tagInputValue.value = ''
  editDialogVisible.value = true
}

// 显示编辑对话框
function showEditSnippet(snippet: SqlSnippet) {
  editingId.value = snippet.id || ''
  formData.value = {
    title: snippet.title || '',
    description: snippet.description || '',
    sqlContent: snippet.sqlContent || '',
    category: snippet.category || '',
    tagList: splitTags(snippet.tags),
    dbType: snippet.dbType || '',
    connId: snippet.connId || '',
    schemaName: snippet.schemaName || '',
  }
  tagInputVisible.value = false
  tagInputValue.value = ''
  editDialogVisible.value = true
}

// 保存（新增/更新）
async function saveSnippet() {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    saving.value = true
    try {
      await apiSaveSnippet({
        id: editingId.value || undefined,
        title: formData.value.title.trim(),
        description: formData.value.description.trim(),
        sqlContent: formData.value.sqlContent.trim(),
        category: formData.value.category?.trim() || '',
        tags: formData.value.tagList.join(','),
        dbType: formData.value.dbType || '',
        connId: props.connId?.trim() || '',
        schemaName: props.schema?.trim() || '',
      })
      ElMessage.success(editingId.value ? '更新成功' : '收藏成功')
      editDialogVisible.value = false
      await reloadAll()
    } catch (e) {
      // 错误已由全局拦截器提示
    } finally {
      saving.value = false
    }
  })
}

// 删除
async function deleteSnippet(snippet: SqlSnippet) {
  try {
    await ElMessageBox.confirm(
      `确定删除收藏「${snippet.title}」吗？`,
      '删除确认',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  if (!snippet.id) return
  try {
    await apiDeleteSnippet(snippet.id)
    ElMessage.success('删除成功')
    await reloadAll()
  } catch (e) {
    // 错误已由全局拦截器提示
  }
}

// 复制 SQL 到剪贴板
function copySql(snippet: SqlSnippet) {
  copyToClipboard(
    snippet.sqlContent || '',
    () => ElMessage.success('已复制 SQL 到剪贴板'),
    () => ElMessage.error('复制失败')
  )
}

// 应用到编辑器
function applySnippet(snippet: SqlSnippet) {
  emit('apply', snippet.sqlContent || '')
  visible.value = false
}

// 标签输入
function showTagInput() {
  tagInputVisible.value = true
  nextTick(() => tagInputRef.value?.focus?.())
}
function addTag() {
  const val = tagInputValue.value.trim()
  if (val && !formData.value.tagList.includes(val)) {
    formData.value.tagList.push(val)
  }
  tagInputValue.value = ''
  tagInputVisible.value = false
}
function removeTag(idx: number) {
  formData.value.tagList.splice(idx, 1)
}

// 导出全部为 JSON 文件
async function handleExport() {
  try {
    await exportSnippetsToFile()
    ElMessage.success('已导出')
  } catch (e) {
    // 错误已由全局拦截器提示
  }
}

// 导入 JSON 文件
async function handleImportFile(option: { file: File }) {
  const file = option.file
  if (!file) return
  try {
    const text = await file.text()
    const parsed = JSON.parse(text)
    // 兼容两种格式：导出根结构 {items} 或直接数组
    const items: SnippetExportItem[] = Array.isArray(parsed)
      ? parsed
      : (parsed?.items || [])
    if (!items.length) {
      ElMessage.warning('文件中没有可导入的数据')
      return
    }
    const resp = await importSnippets(items)
    const count = resp.data?.data?.count || 0
    ElMessage.success(`成功导入 ${count} 条`)
    await reloadAll()
  } catch (e) {
    ElMessage.error('导入失败：JSON 解析错误')
  }
}

// 从旧版 localStorage 迁移数据到后端
async function migrateFromLocal() {
  let legacy: Array<{ name?: string; sql?: string }> = []
  try {
    legacy = JSON.parse(localStorage.getItem(LEGACY_STORAGE_KEY) || '[]')
  } catch {
    legacy = []
  }
  if (!legacy.length) {
    ElMessage.info('本地没有可迁移的数据')
    hasLocalSnippets.value = false
    return
  }
  try {
    await ElMessageBox.confirm(
      `检测到本地有 ${legacy.length} 条旧收藏，将导入到后端并清除本地存储。是否继续？`,
      '本地迁移',
      { type: 'warning', confirmButtonText: '迁移', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const items: SnippetExportItem[] = legacy.map(it => ({
      title: it.name || '未命名',
      sqlContent: it.sql || '',
      category: '本地迁移',
    }))
    const resp = await importSnippets(items)
    const count = resp.data?.data?.count || 0
    ElMessage.success(`迁移完成，导入 ${count} 条`)
    localStorage.removeItem(LEGACY_STORAGE_KEY)
    hasLocalSnippets.value = false
    await reloadAll()
  } catch (e) {
    // 错误已由全局拦截器提示
  }
}

// 检查本地是否存在旧数据
function checkLocalSnippets() {
  try {
    const legacy = JSON.parse(localStorage.getItem(LEGACY_STORAGE_KEY) || '[]')
    hasLocalSnippets.value = Array.isArray(legacy) && legacy.length > 0
  } catch {
    hasLocalSnippets.value = false
  }
}

// 抽屉拖拽调整宽度
function onSnippetDrawerDragStart(e: MouseEvent) {
  isDraggingSnippet.value = true
  document.addEventListener('mousemove', onSnippetDrawerDragMove)
  document.addEventListener('mouseup', onSnippetDrawerDragEnd)
  e.preventDefault()
}
function onSnippetDrawerDragMove(e: MouseEvent) {
  if (!isDraggingSnippet.value) return
  const newWidth = window.innerWidth - e.clientX
  if (newWidth >= 360 && newWidth <= 1200) {
    snippetDrawerWidth.value = newWidth
  }
}
function onSnippetDrawerDragEnd() {
  isDraggingSnippet.value = false
  document.removeEventListener('mousemove', onSnippetDrawerDragMove)
  document.removeEventListener('mouseup', onSnippetDrawerDragEnd)
}

// 抽屉打开时加载数据
watch(visible, (val) => {
  if (val) {
    checkLocalSnippets()
    reloadAll()
  }
})

onMounted(() => {
  checkLocalSnippets()
})
</script>

<style scoped>
.snippet-toolbar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.snippet-filter {
  display: flex;
  gap: 8px;
  margin: 12px 0;
}

.snippet-body {
  display: flex;
  gap: 8px;
  height: calc(100vh - 200px);
}

.snippet-cates {
  width: 160px;
  flex-shrink: 0;
  border-right: 1px solid var(--el-border-color-light, #ebeef5);
  padding-right: 8px;
}

.snippet-cates-title {
  font-size: 12px;
  color: #909399;
  margin-bottom: 8px;
  padding-left: 4px;
}

.snippet-cate-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  color: #606266;
  transition: background 0.15s;
}

.snippet-cate-item:hover {
  background: #f5f7fa;
}

.snippet-cate-item.active {
  background: #ecf5ff;
  color: #409eff;
  font-weight: 600;
}

.cate-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.snippet-list-wrap {
  flex: 1;
  overflow: hidden;
}

.snippet-empty {
  text-align: center;
  color: #c0c4cc;
  padding: 40px 0;
  font-size: 14px;
}

.snippet-card {
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
  cursor: pointer;
  transition: all 0.15s;
}

.snippet-card:hover {
  border-color: #409eff;
  background: #ecf5ff;
}

.snippet-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.snippet-name {
  font-size: 13px;
  font-weight: 600;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.snippet-ops {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.snippet-op {
  color: #909399;
  cursor: pointer;
}

.snippet-op:hover {
  color: #409eff;
}

.snippet-op:last-child:hover {
  color: #f56c6c;
}

.snippet-desc {
  font-size: 12px;
  color: #909399;
  margin-bottom: 6px;
  white-space: pre-wrap;
}

.snippet-body-text {
  background: #f5f7fa;
  border-radius: 4px;
  padding: 8px;
  font-size: 12px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 100px;
  overflow: hidden;
  color: #606266;
  margin: 0;
}

.snippet-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  margin-top: 8px;
}

.meta-text {
  font-size: 11px;
  color: #909399;
}

.meta-time {
  font-size: 11px;
  color: #c0c4cc;
  margin-left: auto;
}

.tag-editor {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.drawer-drag-handle {
  position: fixed;
  top: 0;
  bottom: 0;
  width: 6px;
  cursor: ew-resize;
  z-index: 3000;
  background: transparent;
  transition: background 0.2s;
}

.drawer-drag-handle:hover {
  background: rgba(64, 158, 255, 0.3);
}

:deep(.sql-textarea textarea) {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12px;
}
</style>
