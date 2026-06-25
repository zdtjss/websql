<template>
  <el-dialog
    v-model="visible"
    :title="'数据库对象 - ' + schema"
    width="1060px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
    aria-label="数据库对象对话框"
    @open="onDialogOpen"
  >
    <el-tabs v-if="visibleTabs.length > 0" v-model="activeTab" type="card" @tab-change="onTabChange">
      <el-tab-pane
        v-for="tab in visibleTabs"
        :key="tab.name"
        :label="tab.label"
        :name="tab.name"
      >
        <!-- 搜索过滤：按名称过滤当前类型的对象 -->
        <div style="margin-bottom: 8px;">
          <el-input
            v-model="searchKeyword[tab.name]"
            placeholder="按名称搜索"
            clearable
            size="small"
            style="width: 240px;"
            :aria-label="`搜索${tab.label}`"
          />
        </div>
        <el-table
          :data="pagedData(tab.name)"
          style="width: 100%"
          v-loading="loading"
          :aria-busy="loading"
          max-height="440"
          :aria-label="`${tab.label}列表`"
        >
          <el-table-column
            v-for="col in columnsOf(tab.name)"
            :key="col.prop"
            :prop="col.prop"
            :label="col.label"
            :width="col.width"
            :min-width="col.minWidth"
            show-overflow-tooltip
            resizable
          >
            <template #default="scope">
              <el-tag v-if="col.render === 'tag' && scope.row[col.prop]" size="small">{{ scope.row[col.prop] }}</el-tag>
              <span v-else>{{ scope.row[col.prop] }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100" resizable fixed="right">
            <template #default="scope">
              <el-button size="small" link type="primary" @click="viewObjectDetail(scope.row, tab.name)" :aria-label="`查看 ${scope.row.name} 的定义`">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
        <!-- 分页：对象数量超过单页（PAGE_SIZE）时显示，避免一次性渲染大量行导致卡顿 -->
        <div v-if="filteredData(tab.name).length > PAGE_SIZE" class="obj-pagination">
          <el-pagination
            :current-page="getCurrentPage(tab.name)"
            :page-size="PAGE_SIZE"
            :total="filteredData(tab.name).length"
            :pager-count="7"
            layout="prev, pager, next, total"
            small
            background
            @current-change="(p) => onPageChange(tab.name, p)"
          />
        </div>
        <el-empty v-if="!loading && filteredData(tab.name).length === 0" :description="`没有${tab.label}`" />
      </el-tab-pane>
    </el-tabs>
    <el-empty v-else description="未识别到数据库类型，无法展示对象" />

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="detailVisible"
    :title="detailTitle"
    width="800px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
    aria-label="对象定义详情对话框"
    @opened="onDetailOpened"
  >
    <!-- 图标按钮：仅图标无文字，需补充 aria-label -->
    <el-icon style="position:absolute;right:18px;cursor:pointer;z-index:9999;" size="16" role="button" tabindex="0"
      aria-label="复制对象定义到剪贴板" title="复制"
      @click="copyDetail" @keyup.enter="copyDetail">
      <CopyDocument />
    </el-icon>
    <div style="max-height: 500px;overflow-y:auto;">
      <pre v-loading="detailLoading"><code class="language-sql" v-html="highlightedCode"></code></pre>
    </div>
    <template #footer>
      <el-button @click="detailVisible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { CopyDocument } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { highlightSql } from '@/utils/lazyDeps'
import { listDbObjects, getObjectDDL } from '@/api/sql'
import { useDbSchemaStore } from '@/stores/dbSchema'
import { isValidIdentifier } from '@/utils/identifierValidator'

const dbSchemaProxy = useDbSchemaStore()

const visible = defineModel({ default: false })
const { connId, schema } = defineProps({
  connId: String,
  schema: String,
})

// 根据 schema 获取数据库类型（mysql/mariadb/oracle/sqlite）
const dbType = computed(() => (dbSchemaProxy.getDbType(schema) || '').toLowerCase())

// 对象类型 Tab 配置：每种类型仅在指定数据库类型下显示。
// SQLite 不支持存储过程/函数/事件；Oracle 不支持事件；表/视图/触发器四种数据库均支持。
const TAB_CONFIG = [
  { name: 'table', label: '表', types: ['mysql', 'mariadb', 'oracle', 'sqlite'] },
  { name: 'view', label: '视图', types: ['mysql', 'mariadb', 'oracle', 'sqlite'] },
  { name: 'procedure', label: '存储过程', types: ['mysql', 'mariadb', 'oracle'] },
  { name: 'function', label: '函数', types: ['mysql', 'mariadb', 'oracle'] },
  { name: 'trigger', label: '触发器', types: ['mysql', 'mariadb', 'oracle', 'sqlite'] },
  { name: 'event', label: '事件', types: ['mysql', 'mariadb'] },
]

// 当前 dbType 下可见的 Tab 列表
const visibleTabs = computed(() => TAB_CONFIG.filter(t => t.types.includes(dbType.value)))

// 当前激活的 Tab
const activeTab = ref('')
const loading = ref(false)
// 各类型对象的数据缓存：{ table: [...], view: [...], ... }（已归一化）
const dataMap = reactive({})
// 各类型是否已加载标记，避免重复请求
const loadedMap = reactive({})
// 各类型的搜索关键字
const searchKeyword = reactive({})
// 记录已加载的 schema，schema 变化时清空缓存，避免不同连接/库的数据串扰
const loadedSchema = ref('')

// 各对象类型的表格列配置（基于归一化后的字段）
const COLUMN_CONFIG = {
  table: [
    { prop: 'name', label: '表名', width: 220 },
    { prop: 'type', label: '类型', width: 120, render: 'tag' },
    { prop: 'comment', label: '注释', minWidth: 160 },
  ],
  view: [
    { prop: 'name', label: '视图名', width: 220 },
    { prop: 'definition', label: '定义', minWidth: 260 },
    { prop: 'updatable', label: '可更新', width: 100 },
  ],
  procedure: [
    { prop: 'name', label: '过程名', width: 280 },
  ],
  function: [
    { prop: 'name', label: '函数名', width: 280 },
  ],
  trigger: [
    { prop: 'name', label: '触发器名', width: 200 },
    { prop: 'tableName', label: '所在表', width: 180 },
    { prop: 'timing', label: '时机', width: 100, render: 'tag' },
    { prop: 'event', label: '事件', width: 100, render: 'tag' },
  ],
  event: [
    { prop: 'name', label: '事件名', width: 220 },
    { prop: 'type', label: '类型', width: 100, render: 'tag' },
    { prop: 'status', label: '状态', width: 100, render: 'tag' },
  ],
}

// 分页配置：对象数量较多时使用客户端分页，避免一次性渲染大量表格行导致卡顿
// 选用分页而非 el-table-v2 的原因：当前表格使用了 min-width 自适应、show-overflow-tooltip、
// resizable、fixed right 等特性，el-table-v2 对这些特性支持有限，改造成本高且易损失功能
const PAGE_SIZE = 50
// 各对象类型的当前页码：{ table: 1, view: 1, ... }
const currentPage = reactive({})

function columnsOf(objType) {
  return COLUMN_CONFIG[objType] || [{ prop: 'name', label: '名称', width: 280 }]
}

// 从后端原始行数据中按候选字段名提取值（大小写不敏感），兼容不同数据库的列名差异
function pickField(row, keys) {
  for (const k of keys) {
    if (row[k] != null && row[k] !== '') return row[k]
  }
  const lowerKeys = keys.map(k => k.toLowerCase())
  for (const rk of Object.keys(row)) {
    if (lowerKeys.includes(rk.toLowerCase()) && row[rk] != null && row[rk] !== '') return row[rk]
  }
  return ''
}

// 将后端返回的原始行归一化为统一的字段结构，便于前端表格统一渲染
function normalizeRow(row, objType) {
  switch (objType) {
    case 'table':
      return {
        name: pickField(row, ['TABLE_NAME']),
        type: pickField(row, ['TABLE_TYPE']),
        comment: pickField(row, ['table_comment', 'TABLE_COMMENT']),
      }
    case 'view':
      return {
        name: pickField(row, ['VIEW_NAME', 'TABLE_NAME']),
        definition: pickField(row, ['VIEW_DEFINITION']),
        updatable: pickField(row, ['IS_UPDATABLE']),
      }
    case 'procedure':
    case 'function':
      return { name: pickField(row, ['ROUTINE_NAME', 'OBJECT_NAME']) }
    case 'trigger':
      return {
        name: pickField(row, ['TRIGGER_NAME']),
        tableName: pickField(row, ['EVENT_OBJECT_TABLE', 'TABLE_NAME']),
        timing: pickField(row, ['ACTION_TIMING', 'TRIGGER_TYPE']),
        event: pickField(row, ['EVENT_MANIPULATION', 'TRIGGERING_EVENT']),
      }
    case 'event':
      return {
        name: pickField(row, ['EVENT_NAME']),
        type: pickField(row, ['EVENT_TYPE']),
        status: pickField(row, ['STATUS']),
      }
    default:
      return { name: pickField(row, ['NAME']) }
  }
}

// 对话框打开时初始化激活 Tab 并加载首个 Tab 数据
function onDialogOpen() {
  // schema 变化时清空缓存，避免不同连接/库的数据串扰
  if (loadedSchema.value !== schema) {
    Object.keys(dataMap).forEach(k => delete dataMap[k])
    Object.keys(loadedMap).forEach(k => delete loadedMap[k])
    Object.keys(searchKeyword).forEach(k => delete searchKeyword[k])
    // 同步清空分页页码
    Object.keys(currentPage).forEach(k => delete currentPage[k])
    loadedSchema.value = schema
  }
  if (visibleTabs.value.length > 0) {
    activeTab.value = visibleTabs.value[0].name
    loadObjects(activeTab.value)
  }
}

function onTabChange(name) {
  loadObjects(name)
}

// 加载指定类型的对象列表
function loadObjects(objType) {
  if (!objType) return
  if (loadedMap[objType]) return
  if (!isValidIdentifier(schema)) {
    ElMessage.error('非法的 schema 名')
    return
  }
  loading.value = true
  listDbObjects({ connId, schema, type: objType })
    .then(resp => {
      const rawList = resp.data?.data || []
      dataMap[objType] = rawList.map(r => normalizeRow(r, objType))
      loadedMap[objType] = true
    })
    .catch(() => {
      dataMap[objType] = []
      loadedMap[objType] = true
    })
    .finally(() => { loading.value = false })
}

// 按搜索关键字过滤后的列表
function filteredData(objType) {
  const list = dataMap[objType] || []
  const kw = (searchKeyword[objType] || '').trim().toLowerCase()
  if (!kw) return list
  return list.filter(item => (item.name || '').toLowerCase().includes(kw))
}

// 获取指定类型的当前页码（未初始化时默认为 1）
function getCurrentPage(objType) {
  return currentPage[objType] || 1
}

// 分页切换：更新当前类型的页码
function onPageChange(objType, page) {
  currentPage[objType] = page
}

// 按当前页码切片后的数据，供 el-table 渲染
function pagedData(objType) {
  const list = filteredData(objType)
  const page = getCurrentPage(objType)
  const totalPages = Math.ceil(list.length / PAGE_SIZE) || 1
  // 当前页超出总页数时回退到第 1 页（搜索过滤后数据变少的情况）
  const safePage = page > totalPages ? 1 : page
  if (safePage !== page) {
    currentPage[objType] = safePage
  }
  const start = (safePage - 1) * PAGE_SIZE
  return list.slice(start, start + PAGE_SIZE)
}

// 搜索关键字变化时重置所有页码到第 1 页，避免过滤后停留在空页
watch(searchKeyword, () => {
  Object.keys(currentPage).forEach(k => { currentPage[k] = 1 })
}, { deep: true })

// ===== 对象 DDL 详情 =====
const detailVisible = ref(false)
const detailTitle = ref('')
const detailCode = ref('')
const highlightedCode = ref('')
const detailLoading = ref(false)

watch(detailCode, async (val) => {
  if (!val) { highlightedCode.value = ''; return }
  highlightedCode.value = await highlightSql(val)
}, { immediate: true })

// 查看对象 DDL：调用后端接口获取，兼容不同数据库的 DDL 提取方式
function viewObjectDetail(row, objType) {
  detailTitle.value = row.name
  detailCode.value = ''
  highlightedCode.value = ''
  detailVisible.value = true
  if (!isValidIdentifier(row.name)) {
    detailCode.value = '-- 非法的对象名，无法获取定义'
    return
  }
  detailLoading.value = true
  getObjectDDL({ connId, schema, type: objType, name: row.name })
    .then(resp => {
      detailCode.value = resp.data?.data || '-- 没有可用的定义'
    })
    .catch(() => {
      detailCode.value = '-- 获取定义失败'
    })
    .finally(() => { detailLoading.value = false })
}

// 对话框打开后将焦点移到复制按钮，便于键盘操作
function onDetailOpened() {
  nextTick(() => {
    document.querySelector('.classical-dialog [role="button"][aria-label^="复制"]')?.focus()
  })
}

function copyDetail() {
  navigator.clipboard.writeText(detailCode.value).then(() => {
    ElMessage({ message: '已复制到剪贴板', type: 'success' })
  })
}
</script>

<style scoped>
/* 分页工具栏：右对齐，与表格顶部搜索框形成视觉对称 */
.obj-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 8px;
}
</style>
