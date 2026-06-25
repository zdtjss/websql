<template>
  <el-dialog v-model="visible" width="1100px" :close-on-click-modal="false" :fullscreen="isFullscreen" :draggable="!isFullscreen" :show-close="false" aria-label="数据字典对话框" @opened="onDialogOpened">
    <template #header="{ close }">
      <div style="display:flex;justify-content:space-between;align-items:center">
        <span style="font-size:18px;font-weight:600">数据字典</span>
        <div style="display:flex;gap:4px">
          <!-- 图标按钮：仅图标无文字，需补充 aria-label（复用 title 的值） -->
          <el-button text @click="toggleFullscreen" :title="isFullscreen ? '还原' : '全屏'" :aria-label="isFullscreen ? '还原窗口大小' : '全屏显示'">
            <el-icon><FullScreen /></el-icon>
          </el-button>
          <el-button text @click="close" title="关闭" aria-label="关闭对话框" aria-keyshortcuts="Escape">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
      </div>
    </template>
    <el-row style="margin-bottom:15px">
      <el-col :span="12">
        <el-button type="primary" @click="generateDict" :loading="generating" :disabled="!selectedCount" aria-keyshortcuts="Alt+Enter">生成字典</el-button>
        <el-button @click="toggleSelectAll" :aria-label="selectAllTables ? '取消全选所有表' : '全选所有表'">{{selectAllTables ? '取消全选' : '全选'}}</el-button>
        <!-- 已选数量动态更新，aria-live 通知屏幕阅读器 -->
        <span style="margin-left:10px;color:#909399;font-size:13px" aria-live="polite">已选 {{selectedCount}} / {{tables.length}} 张表</span>
      </el-col>
      <el-col :span="12" style="text-align:right">
        <el-button type="success" @click="exportHTML" :disabled="!dictData" aria-label="导出数据字典为 HTML 文件">导出HTML</el-button>
        <el-button type="warning" @click="exportPDF" :disabled="!dictData" aria-label="导出数据字典为 PDF 文件">导出PDF</el-button>
      </el-col>
    </el-row>

    <el-row :gutter="15">
      <el-col :span="6">
        <el-card shadow="never" class="dict-table-card" :style="{maxHeight: contentMaxHeight}">
          <template #header><span style="font-weight:bold">表列表</span></template>
          <el-input ref="tableFilterInputRef" v-model="tableFilter" placeholder="搜索表..." size="small" clearable style="margin-bottom:10px" aria-label="过滤表名" />
          <!-- 表数量较多时使用虚拟滚动（FixedSizeList），避免一次性渲染大量 checkbox 导致卡顿 -->
          <div v-if="filteredTables.length >= VIRTUAL_THRESHOLD" class="dict-table-list">
            <el-auto-resizer>
              <template #default="{ height, width }">
                <FixedSizeList :data="filteredTables" :total="filteredTables.length" :item-size="TABLE_ITEM_SIZE" :height="height" :width="width" :cache="4">
                  <template #default="{ data, index, style }">
                    <el-checkbox :style="style" v-model="data[index].checked" :key="data[index].name"
                      class="dict-table-checkbox"
                      :aria-label="`选择表 ${data[index].name}${data[index].comment ? '，注释 ' + data[index].comment : ''}，${data[index].rows || 0} 行`">
                      {{ data[index].name }}
                      <span v-if="data[index].comment" style="font-size:11px;color:#606266"> - {{ data[index].comment }}</span>
                      <span style="font-size:11px;color:#909399">({{ data[index].rows || 0 }}行)</span>
                    </el-checkbox>
                  </template>
                </FixedSizeList>
              </template>
            </el-auto-resizer>
          </div>
          <!-- 表数量较少时直接渲染，避免虚拟滚动复杂度 -->
          <el-checkbox v-else v-for="t in filteredTables" :key="t.name" v-model="t.checked" style="display:block;margin:4px 0" :aria-label="`选择表 ${t.name}${t.comment ? '，注释 ' + t.comment : ''}，${t.rows || 0} 行`">
            {{ t.name }}
            <span v-if="t.comment" style="font-size:11px;color:#606266"> - {{ t.comment }}</span>
            <span style="font-size:11px;color:#909399">({{ t.rows || 0 }}行)</span>
          </el-checkbox>
        </el-card>
      </el-col>
      <el-col :span="18">
        <div v-if="!dictData" style="text-align:center;padding:60px;color:#909399" role="status">
          <el-icon :size="50" aria-hidden="true"><Document /></el-icon>
          <p style="margin-top:10px">选择表后点击"生成字典"</p>
        </div>
        <div v-else :style="{maxHeight: dictMaxHeight, overflow: 'auto', paddingRight: '10px'}">
          <div v-for="table in dictData.tables" :key="table.name" style="margin-bottom:25px" role="group" :aria-label="`表 ${table.name} 的数据字典`">
            <h3 style="color:#409EFF;border-bottom:2px solid #409EFF;padding-bottom:5px">
              {{ table.name }}
              <span style="font-size:13px;color:#909399;font-weight:normal;margin-left:8px">{{ table.comment }}</span>
            </h3>
            <p style="color:#909399;font-size:12px">引擎: {{ table.engine }} | 行数: {{ table.rows }}</p>

            <el-table :data="table.columns" stripe size="small" style="margin-bottom:8px" border :aria-label="`表 ${table.name} 字段列表`">
              <el-table-column prop="position" label="#" width="50" />
              <el-table-column prop="name" label="列名" width="140">
                <template #default="{row}">
                  <span>{{ row.name }}</span>
                  <el-tag v-if="row.primaryKey" type="warning" size="small" style="margin-left:4px">PK</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="type" label="类型" width="140">
                <template #default="{row}">
                  <code>{{ row.type }}</code>
                </template>
              </el-table-column>
              <el-table-column label="可空" width="60">
                <template #default="{row}">
                  <span :style="{color: row.nullable ? '#67c23a' : '#f56c6c'}">{{ row.nullable ? 'YES' : 'NO' }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="defaultValue" label="默认值" width="100" />
              <el-table-column prop="comment" label="注释" min-width="150" />
            </el-table>

            <div v-if="table.indexes && table.indexes.length" style="margin-top:5px">
              <strong style="font-size:12px;color:#909399">索引:</strong>
              <el-tag v-for="idx in table.indexes" :key="idx.name" size="small" :type="idx.unique ? 'success' : 'info'" style="margin:2px 4px">
                {{ idx.name }}({{ idx.columns.join(',') }})
              </el-tag>
            </div>
          </div>
        </div>
      </el-col>
    </el-row>
  </el-dialog>
</template>

<script setup>
import { nextTick, ref, computed, useTemplateRef } from 'vue'
import { ElMessage, FixedSizeList } from 'element-plus'
import { Document, FullScreen, Close } from '@element-plus/icons-vue'
import { getDatadictTables, generateDatadict, exportDatadictHtml, exportDatadictPdf } from '@/api/sql'

const visible = defineModel({ default: false })
const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const tables = ref([])
const dictData = ref(null)
const generating = ref(false)
const tableFilter = ref('')
const tableFilterInputRef = useTemplateRef('tableFilterInputRef')
const selectAllTables = ref(false)
const isFullscreen = ref(false)

// 虚拟滚动配置
// VIRTUAL_THRESHOLD：表数量达到该阈值时启用虚拟滚动，低于该值直接渲染避免复杂度
const VIRTUAL_THRESHOLD = 50
// TABLE_ITEM_SIZE：单个表 checkbox 项的高度（px），与 .dict-table-checkbox 样式匹配
const TABLE_ITEM_SIZE = 40

function toggleFullscreen() {
  isFullscreen.value = !isFullscreen.value
}

const filteredTables = computed(() => {
  if (!tableFilter.value) return tables.value
  const kw = tableFilter.value.toLowerCase()
  return tables.value.filter(t =>
    t.name.toLowerCase().includes(kw) ||
    (t.comment && t.comment.toLowerCase().includes(kw))
  )
})

const selectedCount = computed(() => tables.value.filter(t => t.checked).length)

const contentMaxHeight = computed(() => isFullscreen.value ? 'calc(100vh - 150px)' : '600px')
const dictMaxHeight = computed(() => isFullscreen.value ? 'calc(100vh - 160px)' : '560px')

function toggleSelectAll() {
  selectAllTables.value = !selectAllTables.value
  tables.value.forEach(t => t.checked = selectAllTables.value)
}

async function loadTables() {
  try {
    const res = await getDatadictTables(connId, schema)
    const result = res.data.data || res.data
    tables.value = (result.tables || []).map(t => ({ ...t, checked: false }))
  } catch (e) {
    ElMessage.error('加载表失败')
  }
}

// 对话框打开后加载表数据并聚焦到过滤输入框，便于键盘操作
function onDialogOpened() {
  loadTables()
  nextTick(() => {
    tableFilterInputRef.value?.focus?.()
  })
}

async function generateDict() {
  const selected = tables.value.filter(t => t.checked).map(t => t.name).join(',')
  if (!selected) { ElMessage.warning('请选择要生成字典的表'); return }
  generating.value = true
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('tables', selected)
    const res = await generateDatadict(formData)
    dictData.value = res.data.data || res.data
    ElMessage.success('字典生成成功')
  } catch (e) {
    ElMessage.error('生成失败')
  } finally {
    generating.value = false
  }
}

async function exportHTML() {
  const selected = tables.value.filter(t => t.checked).map(t => t.name).join(',')
  if (!selected && dictData.value) {
    ElMessage.info('使用当前字典数据导出')
  }
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('tables', selected)
    const res = await exportDatadictHtml(formData)
    const blob = new Blob([res.data], { type: 'text/html;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `datadict_${schema}.html`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('HTML导出成功')
  } catch (e) {
    ElMessage.error('导出失败')
  }
}

async function exportPDF() {
  const selected = tables.value.filter(t => t.checked).map(t => t.name).join(',')
  try {
    const formData = new FormData()
    formData.append('connId', connId)
    formData.append('schema', schema)
    formData.append('tables', selected)
    const res = await exportDatadictPdf(formData)
    const blob = new Blob([res.data], { type: 'text/html;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const win = window.open(url, '_blank')
    if (!win) {
      ElMessage.warning('弹窗被浏览器拦截，请允许弹窗后重试')
    }
    setTimeout(() => URL.revokeObjectURL(url), 60000)
  } catch (e) {
    ElMessage.error('导出失败')
  }
}
</script>

<style scoped>
/* 让 el-card 在 maxHeight 内使用 flex 列布局，使虚拟列表容器自动撑满剩余空间，
   避免精确计算高度，同时适配全屏/非全屏切换 */
.dict-table-card {
  display: flex;
  flex-direction: column;
}
.dict-table-card :deep(.el-card__body) {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  padding: 12px;
}
/* 虚拟滚动容器：撑满 card body 剩余空间（扣除搜索框高度） */
.dict-table-list {
  flex: 1;
  min-height: 0;
  width: 100%;
}
/* 虚拟滚动中的 checkbox 项：FixedSizeList 会通过 inline style 注入
   position/top/height/width，此处仅补充布局与内边距 */
.dict-table-checkbox {
  display: flex;
  align-items: center;
  height: 40px;
  padding: 0 4px;
  box-sizing: border-box;
}
</style>
