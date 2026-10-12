<template>
  <el-drawer
    v-model="visible"
    :size="snippetDrawerWidth + 'px'"
    class="snippet-drawer"
    :show-close="false"
    @close="visible = false"
  >
    <template #header>
      <div class="snippet-drawer-header">
        <div class="snippet-drawer-title">
          <el-icon class="snippet-drawer-title-icon"><Collection /></el-icon>
          <span>SQL 收藏夹</span>
          <el-tag class="snippet-count-tag" size="small" round effect="plain" type="primary">
            {{ totalCount }}
          </el-tag>
        </div>
        <el-icon class="snippet-drawer-close" title="关闭" @click="visible = false"><Close /></el-icon>
      </div>
    </template>

    <div v-if="visible" class="drawer-drag-handle"
        :style="{ right: snippetDrawerWidth + 'px' }"
        @mousedown="onSnippetDrawerDragStart"></div>

    <!-- 顶部操作栏 -->
    <div class="snippet-toolbar">
      <el-button type="primary" size="small" :icon="Star" @click="showAddSnippet(true)">收藏当前 SQL</el-button>
      <div class="snippet-toolbar-group">
        <el-button size="small" :icon="Plus" @click="showAddSnippet(false)">新建</el-button>
        <el-upload
          class="snippet-upload"
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
          plain
          :icon="RefreshLeft"
          @click="migrateFromLocal"
        >从本地迁移</el-button>
        <el-button size="small" :icon="Refresh" circle title="刷新" @click="reloadAll" :loading="loading" />
      </div>
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
      />
      <el-select
        v-model="activeCategory"
        placeholder="全部分类"
        size="small"
        class="filter-cate"
        @change="loadList"
      >
        <el-option label="全部" value="all" />
        <el-option
          v-for="cate in categories"
          :key="cate.name"
          :label="`${cate.name}（${cate.count}）`"
          :value="cate.name"
        />
      </el-select>
      <el-select
        v-model="filterTag"
        placeholder="全部标签"
        clearable
        size="small"
        class="filter-tag"
        @change="loadList"
      >
        <el-option v-for="t in allTags" :key="t" :label="t" :value="t" />
      </el-select>
    </div>

    <!-- 主体：收藏列表 -->
    <div class="snippet-body">
      <div class="snippet-list-wrap">
        <el-scrollbar>
          <div v-if="loading" class="snippet-loading">
            <el-icon class="is-loading"><Loading /></el-icon>
            加载中...
          </div>
          <el-empty
            v-else-if="filteredSnippets.length === 0"
            description="暂无收藏的 SQL 片段"
            :image-size="72"
          />
          <div
            v-for="snippet in filteredSnippets"
            :key="snippet.id"
            class="snippet-card"
            @click="applySnippet(snippet)"
          >
            <div class="snippet-header">
              <span class="snippet-name">{{ snippet.title || '未命名' }}</span>
              <div class="snippet-ops" @click.stop>
                <el-icon class="snippet-op" title="复制 SQL" @click="copySql(snippet)">
                  <CopyDocument />
                </el-icon>
                <el-icon class="snippet-op" title="编辑" @click="showEditSnippet(snippet)">
                  <Edit />
                </el-icon>
                <el-icon class="snippet-op danger" title="删除" @click="deleteSnippet(snippet)">
                  <Delete />
                </el-icon>
              </div>
            </div>
            <div v-if="snippet.description" class="snippet-desc">{{ snippet.description }}</div>
            <pre class="snippet-body-text">{{ snippet.sqlContent }}</pre>
            <div class="snippet-meta">
              <el-tag v-if="snippet.category" size="small" type="info" effect="plain">
                {{ snippet.category }}
              </el-tag>
              <el-tag
                v-for="t in splitTags(snippet.tags)"
                :key="t"
                size="small"
                effect="plain"
                class="meta-tag"
              >{{ t }}</el-tag>
              <el-tag
                v-if="snippet.dbType"
                size="small"
                effect="light"
                class="meta-db"
                :type="dbTypeTagType(snippet.dbType)"
              >{{ dbTypeLabel(snippet.dbType) }}</el-tag>
              <span class="meta-time">{{ formatTime(snippet.updatedAt || snippet.createdAt) }}</span>
            </div>
          </div>
        </el-scrollbar>
      </div>
    </div>

    <!-- 新增/编辑对话框 -->
    <el-dialog
      v-model="editDialogVisible"
      :title="editingId ? '编辑收藏' : '添加收藏'"
      width="760px"
      :draggable="true"
      class="snippet-edit-dialog"
      :append-to-body="true"
      destroy-on-close
    >
      <el-form ref="formRef" :model="formData" label-width="90px" size="default" :rules="rules">
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
            :rows="9"
            placeholder="必填，SQL 语句"
            class="sql-textarea"
          />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="12">
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
          </el-col>
          <el-col :span="12">
            <el-form-item label="数据库类型" prop="dbType">
              <el-select v-model="formData.dbType" placeholder="选填" clearable style="width: 100%;">
                <el-option
                  v-for="opt in dbTypeOptions"
                  :key="opt.value"
                  :label="opt.label"
                  :value="opt.value"
                />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="标签" prop="tagsInput">
          <div class="tag-editor">
            <el-tag
              v-for="(t, idx) in formData.tagList"
              :key="t"
              closable
              size="small"
              @close="removeTag(idx)"
            >{{ t }}</el-tag>
            <el-input
              v-if="tagInputVisible"
              ref="tagInputRef"
              v-model="tagInputValue"
              size="small"
              class="tag-input"
              placeholder="回车添加"
              @keyup.enter="addTag"
              @blur="addTag"
            />
            <el-button v-else size="small" :icon="Plus" @click="showTagInput">标签</el-button>
          </div>
        </el-form-item>
        <el-form-item label="来源">
          <div class="source-info">
            <el-icon class="source-icon"><Connection /></el-icon>
            <span class="source-conn">{{ currentConnName || connId || '当前连接' }}</span>
            <span class="source-divider">/</span>
            <span class="source-schema">{{ schema || '默认 Schema' }}</span>
          </div>
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
import {
  Delete, Edit, Download, Upload, Plus, Search, Refresh, RefreshLeft, Star,
  CopyDocument, Collection, Close, Connection, Loading
} from '@element-plus/icons-vue'
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

// 数据库类型选项：value 为存储值，label 为展示值（与编辑对话框下拉框一致）
const dbTypeOptions = [
  { label: 'MySQL', value: 'mysql' },
  { label: 'MariaDB', value: 'mariadb' },
  { label: 'SQLite', value: 'sqlite' },
  { label: 'Oracle', value: 'oracle' },
]

// 数据库类型存储值转展示名称（如 mysql → MySQL）
function dbTypeLabel(type?: string): string {
  if (!type) return ''
  const opt = dbTypeOptions.find(o => o.value === type.toLowerCase())
  return opt ? opt.label : type
}

// 数据库类型对应的标签配色
function dbTypeTagType(type?: string): 'primary' | 'success' | 'info' | 'warning' | 'danger' {
  switch ((type || '').toLowerCase()) {
    case 'mysql': return 'success'
    case 'mariadb': return 'primary'
    case 'sqlite': return 'warning'
    case 'oracle': return 'danger'
    default: return 'info'
  }
}

// 时间展示为 YYYY-MM-DD HH:mm。
// 后端以北京时间（UTC+8）墙钟值落库，但 SQLite 驱动会把无时区的 DATETIME 列解析为 UTC 的 time.Time，
// 序列化后带上 'Z'（如 "2026-10-08T18:05:59Z"）——该 'Z' 并非真实 UTC，直接按 UTC 转换会整体偏 +8 小时。
// 因此无数字时区偏移（含末尾 Z）的串一律原样展示其墙钟时分；
// 仅当带显式偏移（如 +08:00 / -05:00）时，才真正转换到 Asia/Shanghai。
function formatTime(val?: string): string {
  if (!val) return ''
  const naive = val.match(/^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})(?::(\d{2}))?(?:\.\d+)?Z?$/)
  if (naive) {
    return `${naive[1]}-${naive[2]}-${naive[3]} ${naive[4]}:${naive[5]}`
  }
  const d = new Date(val)
  if (isNaN(d.getTime())) return val
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(d)
  const get = (type: string) => parts.find(p => p.type === type)?.value || ''
  return `${get('year')}-${get('month')}-${get('day')} ${get('hour')}:${get('minute')}`
}

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
/* ============ 抽屉头部 ============ */
.snippet-drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.snippet-drawer-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.snippet-drawer-title-icon {
  font-size: 18px;
  color: var(--el-color-primary);
}

.snippet-count-tag {
  margin-left: 2px;
}

.snippet-drawer-close {
  font-size: 18px;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  transition: color 0.15s;
}

.snippet-drawer-close:hover {
  color: var(--el-color-primary);
}

/* ============ 顶部操作栏 ============ */
.snippet-toolbar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.snippet-toolbar-group {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.snippet-upload {
  line-height: 1;
}

/* ============ 搜索与过滤 ============ */
.snippet-filter {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin: 12px 0;
}

.snippet-filter .el-input {
  flex: 1 1 160px;
}

.snippet-filter :deep(.filter-cate) {
  width: 150px;
  flex-shrink: 0;
}

.snippet-filter :deep(.filter-tag) {
  width: 120px;
  flex-shrink: 0;
}

/* ============ 收藏列表 ============ */
.snippet-body {
  height: calc(100vh - 210px);
}

.snippet-list-wrap {
  height: 100%;
  overflow: hidden;
}

.snippet-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 40px 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.snippet-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 10px;
  background: var(--el-bg-color);
  cursor: pointer;
  transition: border-color 0.18s, box-shadow 0.18s;
}

.snippet-card:hover {
  border-color: var(--el-color-primary-light-5);
  box-shadow: 0 4px 12px rgba(64, 158, 255, 0.12);
}

.snippet-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.snippet-name {
  flex: 1;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.snippet-ops {
  display: flex;
  gap: 2px;
  flex-shrink: 0;
}

.snippet-op {
  padding: 4px;
  border-radius: 4px;
  font-size: 14px;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}

.snippet-op:hover {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.snippet-op.danger:hover {
  color: var(--el-color-danger);
  background: var(--el-color-danger-light-9);
}

.snippet-desc {
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
  white-space: pre-wrap;
}

.snippet-body-text {
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-left: 3px solid var(--el-color-primary-light-5);
  border-radius: 6px;
  padding: 8px 10px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 96px;
  overflow: hidden;
  color: var(--el-text-color-regular);
  margin: 0;
}

.snippet-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  margin-top: 10px;
}

.meta-db {
  font-weight: 600;
}

.meta-time {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  margin-left: auto;
}

/* ============ 编辑对话框 ============ */
.snippet-edit-dialog :deep(.el-dialog__body) {
  padding-top: 10px;
}

.snippet-edit-dialog :deep(.sql-textarea .el-textarea__inner) {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12.5px;
  line-height: 1.6;
}

.tag-editor {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  width: 100%;
}

.tag-editor :deep(.el-tag) {
  margin: 0;
}

.tag-input {
  width: 120px;
}

.source-info {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 6px 10px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  font-size: 12.5px;
  color: var(--el-text-color-regular);
}

.source-icon {
  color: var(--el-color-primary);
}

.source-conn {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.source-divider {
  color: var(--el-text-color-placeholder);
}

/* ============ 抽屉宽度拖拽手柄 ============ */
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
</style>
