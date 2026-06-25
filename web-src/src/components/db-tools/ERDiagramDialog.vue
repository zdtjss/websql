<template>
  <div class="er-diagram-wrapper" role="application" aria-label="ER 关系图">
    <div class="er-toolbar" role="toolbar" aria-label="ER 图工具栏">
      <el-checkbox v-model="showAllTables" @change="onShowAllTablesChange" aria-label="显示所有表，取消则仅显示有外键关联的表">显示所有表</el-checkbox>
      <!-- 搜索框：输入表名关键字高亮匹配节点，回车依次跳转 -->
      <el-input
        v-model="searchQuery"
        placeholder="搜索表名（回车跳转下一个）"
        size="small"
        clearable
        class="er-search-input"
        aria-label="搜索表名"
        @keydown.enter="focusNextMatch"
        @clear="onSearchClear"
      >
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <span v-if="searchQuery" class="er-toolbar-hint er-search-count">{{ matchedNodeIds.length }} 个匹配</span>
      <span class="er-toolbar-hint">💡 双击表名浏览数据 | Ctrl+滚轮缩放 | 拖动平移 | 点击 ▾ 折叠</span>
      <div style="flex:1;"></div>
      <!-- 全部折叠 / 全部展开 -->
      <el-button-group size="small">
        <el-button @click="collapseAll" aria-label="全部折叠表节点">全部折叠</el-button>
        <el-button @click="expandAll" aria-label="全部展开表节点">全部展开</el-button>
      </el-button-group>
      <!-- 重新布局：清除手动拖动位置并重新计算自动布局 -->
      <el-button size="small" @click="relayout" aria-label="重新计算自动布局">重新布局</el-button>
      <el-radio-group v-model="layoutType" size="small" @change="rebuildGraph" aria-label="布局方向">
        <el-radio-button value="TB">从上到下</el-radio-button>
        <el-radio-button value="LR">从左到右</el-radio-button>
      </el-radio-group>
      <!-- AI 分析表关系：实际数据库通常不建外键，关系由程序定义，
           此按钮调用 AI 根据表名/字段/注释推断关系并叠加到画布上。
           推断结果仅保存在内存中，不持久化、不写数据库。 -->
      <el-button size="small" type="primary" plain :loading="aiAnalyzing" @click="analyzeRelationsWithAI"
        title="调用 AI 根据表名、字段、注释推断表关系（仅当前会话有效，不持久化）"
        aria-label="AI 分析表关系">
        <el-icon style="margin-right:4px;"><MagicStick /></el-icon>AI 分析关系
      </el-button>
      <!-- 手动编辑关系：增删改连线，仅内存中，不持久化 -->
      <el-button size="small" @click="openRelationEditor" aria-label="手动编辑表关系">编辑关系</el-button>
      <!-- 导出：支持 PNG / SVG，包含白色背景 -->
      <el-dropdown trigger="click" @command="handleExport" aria-label="导出 ER 图">
        <el-button size="small">导出<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="png">导出 PNG</el-dropdown-item>
            <el-dropdown-item command="svg">导出 SVG</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-button size="small" @click="fitGraph" aria-label="适应画布">适应画布</el-button>
    </div>
    <!-- 画布加载态：aria-busy 通知屏幕阅读器 -->
    <div ref="containerRef" class="er-canvas" v-loading="loading" :aria-busy="loading" role="img" :aria-label="loading ? '正在加载 ER 图' : 'ER 关系图画布'"></div>
    <div v-if="!loading && allNodes.length === 0" class="er-empty" role="status">该数据库没有表</div>
    <!-- 底部统计信息：动态更新，aria-live 通知屏幕阅读器 -->
    <div class="er-footer" aria-live="polite">
      表: {{ filteredNodes.length }} | 关系: {{ filteredEdges.length }}
      <span v-if="collapsedNodes.size > 0"> | 已折叠: {{ collapsedNodes.size }}</span>
      <span v-if="manualPositions.size > 0"> | 已手动定位: {{ manualPositions.size }}</span>
    </div>
    <!-- 关系连线 hover 提示框 -->
    <div
      v-show="edgeTooltip.visible"
      class="er-edge-tooltip"
      :style="{ left: edgeTooltip.x + 'px', top: edgeTooltip.y + 'px' }"
      role="tooltip"
    >
      <div class="er-edge-tooltip-type">{{ edgeTooltip.relationType }}</div>
      <div class="er-edge-tooltip-line"><b>{{ edgeTooltip.source }}</b> → <b>{{ edgeTooltip.target }}</b></div>
      <div class="er-edge-tooltip-cols">字段映射: {{ edgeTooltip.columns }}</div>
      <div v-if="edgeTooltip.constraintName" class="er-edge-tooltip-fk">外键: {{ edgeTooltip.constraintName }}</div>
    </div>

    <!-- 手动编辑关系对话框：仅修改内存中的 allEdges，关闭即生效，不持久化 -->
    <el-dialog v-model="relationEditorVisible" title="编辑表关系" width="780px" :draggable="true" :close-on-click-modal="false"
      append-to-body>
      <div class="relation-editor-tip">
        提示：在此处增删的关系仅用于当前 ER 图显示，不会写入数据库也不会保存。刷新页面后将恢复为数据库物理外键。
      </div>
      <el-table :data="editableRelations" size="small" max-height="420" border>
        <el-table-column label="源表" width="160">
          <template #default="{ row, $index }">
            <el-select v-model="row.source" size="small" filterable @change="onRelationRowChange($index)">
              <el-option v-for="n in allNodes" :key="n.id" :label="n.label" :value="n.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="源字段" width="140">
          <template #default="{ row, $index }">
            <el-select v-model="row.sourceCol" size="small" filterable :disabled="!row.source">
              <el-option v-for="c in getColumnsOf(row.source)" :key="c" :label="c" :value="c" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="目标表" width="160">
          <template #default="{ row, $index }">
            <el-select v-model="row.target" size="small" filterable @change="onRelationRowChange($index)">
              <el-option v-for="n in allNodes" :key="n.id" :label="n.label" :value="n.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="目标字段" width="140">
          <template #default="{ row }">
            <el-select v-model="row.targetCol" size="small" filterable :disabled="!row.target">
              <el-option v-for="c in getColumnsOf(row.target)" :key="c" :label="c" :value="c" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-select v-model="row.relationType" size="small">
              <el-option label="1:1" value="1:1" />
              <el-option label="1:N" value="1:N" />
              <el-option label="N:M" value="N:M" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.origin === 'ai' ? 'warning' : (row.origin === 'manual' ? 'success' : 'info')">
              {{ row.origin === 'ai' ? 'AI' : (row.origin === 'manual' ? '手动' : 'FK') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="70" fixed="right">
          <template #default="{ $index }">
            <el-button size="small" type="danger" link @click="removeRelation($index)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div style="margin-top:10px;display:flex;justify-content:space-between;align-items:center;">
        <el-button size="small" type="primary" plain @click="addRelation">
          <el-icon style="margin-right:4px;"><Plus /></el-icon>新增关系
        </el-button>
        <span class="relation-editor-count">共 {{ editableRelations.length }} 条关系</span>
      </div>
      <template #footer>
        <el-button @click="relationEditorVisible = false">取消</el-button>
        <el-button type="primary" @click="applyRelationEditor">应用并关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { DagreLayout } from '@antv/layout'
import { Graph, Shape, Export } from '@antv/x6'
import { Search, ArrowDown, MagicStick, Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { execSQL } from '@/api/sql'
import { analyzeERRelations } from '@/api/er'
import { useDbSchemaStore } from '@/stores/dbSchema'
import { isValidIdentifier } from '@/utils/identifierValidator'
import { handleError } from '@/utils/errorHandler'
const dbSchemaProxy = useDbSchemaStore()

// 节点尺寸常量
const NODE_WIDTH = 300
const HEADER_HEIGHT = 30
const COMMENT_HEIGHT = 20
const COL_ROW_HEIGHT = 22
const BOTTOM_PADDING = 4
// 表数量超过该阈值时启用异步/虚拟渲染，提升大数据量下的性能
const VIRTUAL_THRESHOLD = 50

// 计算表节点高度（展开状态下）
function getNodeHeight(n) {
  let h = HEADER_HEIGHT
  if (n.comment) h += COMMENT_HEIGHT
  if (n.columns && n.columns.length > 0) h += n.columns.length * COL_ROW_HEIGHT
  h += BOTTOM_PADDING
  return h
}

// 计算表节点高度（考虑折叠状态）
function getNodeHeightByState(n, collapsed) {
  if (collapsed) return HEADER_HEIGHT + BOTTOM_PADDING
  return getNodeHeight(n)
}

// 创建 ER 表节点的 HTML 内容（支持折叠态：折叠后仅显示表名）
function createErTableHtml(cell) {
  const nodeData = cell.getData() || {}
  const isCollapsed = !!nodeData.collapsed
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
  const c = {
    headerBg: isDark ? '#313147' : '#409eff',
    headerText: '#ffffff',
    nodeBg: isDark ? '#1e1e2e' : '#ffffff',
    nodeBorder: isDark ? '#45475a' : '#d9d9d9',
    commentColor: isDark ? '#a6adc8' : '#606266',
    colNameColor: isDark ? '#cdd6f4' : '#303133',
    colTypeColor: isDark ? '#a6adc8' : '#909399',
    colCommentColor: isDark ? '#6c7086' : '#b0b0b0',
    borderColor: isDark ? '#313244' : '#ebeef5',
    pkColor: '#e6a23c',
  }

  const el = document.createElement('div')
  el.setAttribute('data-node-id', nodeData.id || cell.id)
  el.style.cssText = `background:${c.nodeBg};border:2px solid ${c.nodeBorder};border-radius:6px;overflow:hidden;cursor:default;box-shadow:0 1px 4px rgba(0,0,0,0.1);width:100%;height:100%;display:flex;flex-direction:column;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',sans-serif;transition:opacity 0.15s ease,border-color 0.15s ease;`

  // 表标题栏：包含折叠按钮、表名、类型标识
  const header = document.createElement('div')
  header.className = 'er-table-header'
  header.setAttribute('data-table-id', nodeData.id || cell.id)
  header.style.cssText = `background:${c.headerBg};padding:6px 10px;font-size:12px;font-weight:600;color:${c.headerText};cursor:pointer;user-select:none;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;display:flex;align-items:center;gap:6px;`

  // 折叠/展开切换按钮（▾ 展开 / ▶ 折叠）
  const toggle = document.createElement('span')
  toggle.className = 'er-collapse-toggle'
  toggle.setAttribute('role', 'button')
  toggle.setAttribute('aria-label', isCollapsed ? '展开表字段' : '折叠表字段')
  toggle.style.cssText = `cursor:pointer;display:inline-block;width:14px;text-align:center;flex-shrink:0;font-size:11px;opacity:0.9;`
  toggle.textContent = isCollapsed ? '▶' : '▾'
  header.appendChild(toggle)

  const title = document.createElement('span')
  title.style.cssText = `flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;`
  title.textContent = nodeData.label || cell.id
  header.appendChild(title)

  // 表类型标识（VIEW 用不同颜色）
  if (nodeData.type === 'VIEW') {
    const typeTag = document.createElement('span')
    typeTag.style.cssText = `font-size:9px;background:rgba(255,255,255,0.25);padding:1px 5px;border-radius:3px;font-weight:400;flex-shrink:0;`
    typeTag.textContent = 'VIEW'
    header.appendChild(typeTag)
  }
  el.appendChild(header)

  // 折叠状态下不渲染注释与字段列表
  if (!isCollapsed) {
    if (nodeData.comment) {
      const comment = document.createElement('div')
      comment.style.cssText = `padding:3px 10px;font-size:10px;color:${c.commentColor};border-bottom:1px solid ${c.borderColor};line-height:1.4;`
      comment.textContent = nodeData.comment
      el.appendChild(comment)
    }

    if (nodeData.columns && nodeData.columns.length > 0) {
      nodeData.columns.forEach(col => {
        const row = document.createElement('div')
        row.style.cssText = `display:flex;padding:2px 10px;font-size:10px;border-bottom:1px solid ${c.borderColor};align-items:center;line-height:1.5;user-select:text;cursor:text;position:relative;`
        row.setAttribute('title', `字段: ${col.name}\n类型: ${col.type}${col.comment ? '\n注释: ' + col.comment : ''}`)

        const nameSpan = document.createElement('span')
        // 主键字段用金色钥匙图标标记
        const pkMark = col.primaryKey ? '🔑 ' : ''
        nameSpan.style.cssText = `flex:2;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:${col.primaryKey ? c.pkColor : c.colNameColor};font-weight:${col.primaryKey ? 600 : 500};user-select:text;`
        nameSpan.textContent = pkMark + col.name
        nameSpan.setAttribute('title', col.name)
        row.appendChild(nameSpan)

        const typeSpan = document.createElement('span')
        typeSpan.style.cssText = `flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:${c.colTypeColor};font-family:Consolas,Monaco,monospace;font-size:9px;user-select:text;`
        typeSpan.textContent = col.type
        typeSpan.setAttribute('title', col.type)
        row.appendChild(typeSpan)

        const commentSpan = document.createElement('span')
        commentSpan.style.cssText = `flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:${c.colCommentColor};font-size:9px;user-select:text;`
        commentSpan.textContent = col.comment || ''
        commentSpan.setAttribute('title', col.comment || '')
        row.appendChild(commentSpan)

        el.appendChild(row)
      })
    }
  }

  return el
}

Shape.HTML.register({
  shape: 'er-table',
  width: NODE_WIDTH,
  height: 60,
  effect: ['data'],
  html: createErTableHtml,
})

const { tabId, connId, schema, dbType, tableName } = defineProps({
  tabId: String,
  connId: String,
  schema: String,
  dbType: String,
  tableName: String,
})

const emit = defineEmits(['openDataBrowser'])

const loading = ref(false)
const containerRef = useTemplateRef('containerRef')
const layoutType = ref('TB')
const showAllTables = ref(true)

// 搜索状态
const searchQuery = ref('')
const currentMatchIndex = ref(0)

// 折叠状态（组件存活期间持久化）
const collapsedNodes = ref(new Set())

// 手动拖动后的节点位置（在显式重新布局前保持）
const manualPositions = ref(new Map())

// 关系连线 hover 提示框状态
const edgeTooltip = ref({ visible: false, x: 0, y: 0, relationType: '', source: '', target: '', columns: '', constraintName: '' })

let graph = null
const allNodes = ref([])
const allEdges = ref([])
let maxZIndex = 1
let mousedownHandler = null
let lastHeaderClick = { tableId: null, time: 0 }

// 主题色配置（响应深色模式切换）
const themeColors = ref(getThemeColors())

function getThemeColors() {
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
  return {
    nodeBorder: isDark ? '#45475a' : '#d9d9d9',
    edgeColor: isDark ? '#585b70' : '#8c8c8c',
    hoverBorder: isDark ? '#89b4fa' : '#1890ff',
    gridColor: isDark ? '#2a2a3c' : '#e0e0e0',
    labelColor: isDark ? '#a6adc8' : '#999999',
    // 关系类型徽标颜色：1:1 绿色 / 1:N 蓝色 / N:M 橙色
    rel11: '#67c23a',
    rel1N: '#409eff',
    relNM: '#e6a23c',
  }
}

const filteredNodes = computed(() => {
  let nodes
  if (showAllTables.value) {
    nodes = allNodes.value
  } else {
    const connectedIds = new Set()
    allEdges.value.forEach(e => { connectedIds.add(e.source); connectedIds.add(e.target) })
    nodes = allNodes.value.filter(n => connectedIds.has(n.id))
  }
  return [...nodes].sort((a, b) => a.label.localeCompare(b.label))
})

const filteredEdges = computed(() => {
  if (showAllTables.value) return allEdges.value
  const connectedIds = new Set()
  allEdges.value.forEach(e => { connectedIds.add(e.source); connectedIds.add(e.target) })
  return allEdges.value.filter(e => connectedIds.has(e.source) && connectedIds.has(e.target))
})

// 搜索匹配的节点 ID 列表（按表名字母顺序）
const matchedNodeIds = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return []
  return filteredNodes.value
    .filter(n => n.label.toLowerCase().includes(q))
    .map(n => n.id)
})

async function execQuery(sql, maxLine) {
  const resp = await execSQL({ connId, schema, sql, maxLine: String(maxLine || 5000) })
  return resp.data.data?.data || []
}

async function loadData() {
  loading.value = true
  allNodes.value = []
  allEdges.value = []
  try {
    const dbTypeVal = (dbType || dbSchemaProxy.getDbType(schema) || '').toLowerCase()
    if (dbTypeVal === 'mysql') {
      await loadMysqlData(schema)
    } else if (dbTypeVal === 'sqlite') {
      await loadSqliteData()
    }
    // 加载完成后推断关系类型（1:1 / 1:N / N:M）
    inferAllRelationTypes()
    // 物理外键标记为 origin=fk，便于在编辑器中与 AI 推断/手动新增的关系区分
    allEdges.value.forEach(e => { e.origin = 'fk' })
  } catch (err) {
    handleError(err, '加载ER图数据')
  } finally {
    loading.value = false
    await nextTick()
    await rebuildGraph()
  }
}

async function loadMysqlData(schema) {
  if (!isValidIdentifier(schema)) {
    console.error('[ERDiagram] 非法的 schema 名:', schema)
    return
  }
  // 增加 COLUMN_KEY 字段以判断主键(PRI)/唯一键(UNI)，用于推断关系类型
  const [tableRows, fkRows, columnRows] = await Promise.all([
    execQuery(`SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT, TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = '${schema}' AND TABLE_TYPE IN ('BASE TABLE', 'VIEW')`),
    execQuery(`SELECT TABLE_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = '${schema}' AND REFERENCED_TABLE_NAME IS NOT NULL`),
    execQuery(`SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, COLUMN_COMMENT, COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '${schema}' ORDER BY TABLE_NAME, ORDINAL_POSITION`, 50000),
  ])

  const colMap = {}
  const tableNamesFromColumns = new Set()
  columnRows.forEach(r => {
    const tableName = r.TABLE_NAME || r.table_name
    tableNamesFromColumns.add(tableName)
    if (!colMap[tableName]) colMap[tableName] = []
    const colKey = r.COLUMN_KEY || r.column_key || ''
    colMap[tableName].push({
      name: r.COLUMN_NAME || r.column_name,
      type: r.COLUMN_TYPE || r.column_type,
      comment: r.COLUMN_COMMENT || r.column_comment || '',
      primaryKey: colKey === 'PRI',
      unique: colKey === 'UNI',
    })
  })

  if (tableRows.length > 0) {
    allNodes.value = tableRows.map(r => ({
      id: r.TABLE_NAME || r.table_name,
      label: r.TABLE_NAME || r.table_name,
      type: (r.TABLE_TYPE || r.table_type) === 'VIEW' ? 'VIEW' : 'TABLE',
      comment: r.TABLE_COMMENT || r.table_comment || '',
      rows: r.TABLE_ROWS || r.table_rows || '',
      columns: colMap[r.TABLE_NAME || r.table_name] || [],
    }))
  } else {
    const sortedNames = [...tableNamesFromColumns].sort()
    allNodes.value = sortedNames.map(name => ({
      id: name,
      label: name,
      type: 'TABLE',
      comment: '',
      rows: '',
      columns: colMap[name] || [],
    }))
  }

  // 保留外键列对信息（用于关系类型推断与 hover 提示）
  const fkMap = {}
  fkRows.forEach(r => {
    const src = r.TABLE_NAME || r.table_name
    const dst = r.REFERENCED_TABLE_NAME || r.referenced_table_name
    const constraint = r.CONSTRAINT_NAME || r.constraint_name || ''
    const srcCol = r.COLUMN_NAME || r.column_name
    const dstCol = r.REFERENCED_COLUMN_NAME || r.referenced_column_name
    const key = src + '→' + dst
    if (!fkMap[key]) fkMap[key] = { source: src, target: dst, columnPairs: [], constraintNames: [] }
    fkMap[key].columnPairs.push({ sourceCol: srcCol, targetCol: dstCol })
    if (constraint && !fkMap[key].constraintNames.includes(constraint)) {
      fkMap[key].constraintNames.push(constraint)
    }
  })
  allEdges.value = Object.values(fkMap).map(e => ({
    source: e.source,
    target: e.target,
    columnPairs: e.columnPairs,
    columns: e.columnPairs.map(cp => cp.sourceCol + '=' + cp.targetCol).join(', '),
    constraintName: e.constraintNames.join(', '),
  }))
}

async function loadSqliteData() {
  const tableRows = await execQuery("SELECT name AS TABLE_NAME, type AS TABLE_TYPE FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY type, name")

  allNodes.value = tableRows.map(r => ({
    id: r.TABLE_NAME || r.name,
    label: r.TABLE_NAME || r.name,
    type: (r.TABLE_TYPE || r.type) === 'view' ? 'VIEW' : 'TABLE',
    comment: '',
    rows: '',
    columns: [],
  }))

  const tablePromises = allNodes.value
    .filter(n => n.type === 'TABLE')
    .map(async n => {
      try {
        if (!isValidIdentifier(n.id)) {
          return { tableName: n.id, columns: [], fks: [] }
        }
        // PRAGMA table_info 返回 pk 字段（1 表示主键列）
        const [cols, fks] = await Promise.all([
          execQuery(`PRAGMA table_info('${n.id}')`),
          execQuery(`PRAGMA foreign_key_list('${n.id}')`),
        ])
        return {
          tableName: n.id,
          columns: cols.map(c => ({ name: c.name, type: c.type, comment: '', primaryKey: c.pk === 1, unique: false })),
          fks: fks.map(fk => ({
            source: n.id,
            target: fk.table,
            columnPairs: [{ sourceCol: fk.from || '', targetCol: fk.to || '' }],
            columns: (fk.from || '') + '=' + (fk.to || ''),
          })),
        }
      } catch (err) { handleError(err, '加载SQLite表结构'); return { tableName: n.id, columns: [], fks: [] } }
    })

  const results = await Promise.all(tablePromises)
  const colMap = {}
  const fkList = []
  results.forEach(r => {
    colMap[r.tableName] = r.columns
    fkList.push(...r.fks)
  })
  allNodes.value.forEach(n => {
    if (colMap[n.id]) n.columns = colMap[n.id]
  })

  const fkMap = {}
  fkList.forEach(e => {
    const key = e.source + '→' + e.target
    if (!fkMap[key]) fkMap[key] = { source: e.source, target: e.target, columnPairs: [] }
    fkMap[key].columnPairs.push(...e.columnPairs)
  })
  allEdges.value = Object.values(fkMap).map(e => ({
    source: e.source,
    target: e.target,
    columnPairs: e.columnPairs,
    columns: e.columnPairs.map(cp => cp.sourceCol + '=' + cp.targetCol).join(', '),
    constraintName: '',
  }))
}

// 判断表是否为多对多中间表：至少 2 个外键且所有主键列均为外键列
function isMiddleTable(node, edges) {
  const outEdges = edges.filter(e => e.source === node.id)
  if (outEdges.length < 2) return false
  const fkSourceCols = new Set()
  outEdges.forEach(e => (e.columnPairs || []).forEach(cp => fkSourceCols.add(cp.sourceCol)))
  const pkCols = (node.columns || []).filter(c => c.primaryKey).map(c => c.name)
  if (pkCols.length === 0) return false
  return pkCols.every(pk => fkSourceCols.has(pk))
}

// 推断单条关系的关系类型：中间表→N:M，目标列为唯一/主键→1:1，否则→1:N
function inferRelationType(edge, nodes, edges) {
  const sourceNode = nodes.find(n => n.id === edge.source)
  if (sourceNode && isMiddleTable(sourceNode, edges)) {
    return 'N:M'
  }
  const targetNode = nodes.find(n => n.id === edge.target)
  if (targetNode && edge.columnPairs && edge.columnPairs.length > 0) {
    const allPkOrUnique = edge.columnPairs.every(cp => {
      const col = targetNode.columns.find(c => c.name === cp.targetCol)
      return col && (col.primaryKey || col.unique)
    })
    if (allPkOrUnique) return '1:1'
  }
  return '1:N'
}

// 为所有边推断关系类型
// 注意：AI 推断（origin=ai）与手动新增（origin=manual）的关系类型由来源给定，
// 不参与自动推断，避免被覆盖。仅物理外键（origin=fk）走推断逻辑。
function inferAllRelationTypes() {
  allEdges.value.forEach(e => {
    if (e.origin === 'ai' || e.origin === 'manual') return
    e.relationType = inferRelationType(e, allNodes.value, allEdges.value)
  })
}

function destroyGraph() {
  if (mousedownHandler && containerRef.value) {
    containerRef.value.removeEventListener('mousedown', mousedownHandler, true)
    mousedownHandler = null
  }
  if (graph) {
    graph.dispose()
    graph = null
  }
}

async function rebuildGraph() {
  destroyGraph()
  if (!containerRef.value) return

  const nodes = filteredNodes.value
  const edges = filteredEdges.value
  if (nodes.length === 0) return

  // 刷新主题色（支持深色模式切换后重建）
  themeColors.value = getThemeColors()
  const colors = themeColors.value

  const useAsync = nodes.length > VIRTUAL_THRESHOLD
  graph = new Graph({
    container: containerRef.value,
    width: containerRef.value.clientWidth,
    height: containerRef.value.clientHeight,
    background: { color: 'transparent' },
    grid: { visible: true, size: 10, type: 'dot', args: { color: colors.gridColor } },
    panning: { enabled: true },
    mousewheel: {
      enabled: true,
      modifiers: ['ctrl'],
      factor: 1.1,
    },
    selecting: { enabled: false },
    connecting: { enabled: false },
    interacting: { nodeMovable: true },
    // 表数量较多时启用异步与虚拟渲染，避免一次性渲染卡顿
    async: useAsync,
    virtual: useAsync,
  })

  // 注册导出插件（X6 v3 需显式注册后才能使用 toPNG/toSVG）
  graph.use(new Export())

  mousedownHandler = (e) => {
    // 折叠按钮点击：优先处理，阻止冒泡以避免触发标题栏的双击逻辑
    const toggle = e.target.closest('.er-collapse-toggle')
    if (toggle) {
      e.stopPropagation()
      e.preventDefault()
      const nodeEl = toggle.closest('[data-node-id]')
      const nodeId = nodeEl?.getAttribute('data-node-id')
      if (nodeId) {
        toggleCollapse(nodeId)
      }
      return
    }
    const header = e.target.closest('.er-table-header')
    if (header) {
      const nodeEl = header.closest('[data-node-id]')
      const nodeId = nodeEl?.getAttribute('data-node-id')
      if (nodeId) {
        const node = graph.getCellById(nodeId)
        if (node) {
          maxZIndex++
          node.setZIndex(maxZIndex)
        }
      }
      const tableId = header.getAttribute('data-table-id')
      const now = Date.now()
      if (tableId && lastHeaderClick.tableId === tableId && (now - lastHeaderClick.time) < 400) {
        lastHeaderClick = { tableId: null, time: 0 }
        emit('openDataBrowser', { connId, schema, tableName: tableId, dbType })
        return
      }
      lastHeaderClick = { tableId, time: now }
    } else {
      lastHeaderClick = { tableId: null, time: 0 }
      if (e.target.closest('[data-node-id]')) {
        graph.disablePanning()
        const onUp = () => {
          graph.enablePanning()
          document.removeEventListener('mouseup', onUp)
        }
        document.addEventListener('mouseup', onUp)
      }
    }
  }
  containerRef.value.addEventListener('mousedown', mousedownHandler, true)

  maxZIndex = 1

  // 添加节点（应用折叠状态与持久化的手动位置）
  nodes.forEach(n => {
    const isCollapsed = collapsedNodes.value.has(n.id)
    const nodeHeight = getNodeHeightByState(n, isCollapsed)
    const nodeConfig = {
      id: n.id,
      shape: 'er-table',
      width: NODE_WIDTH,
      height: nodeHeight,
      data: { ...n, collapsed: isCollapsed },
      zIndex: 1,
    }
    // 若存在持久化的手动位置，恢复之
    const manualPos = manualPositions.value.get(n.id)
    if (manualPos) {
      nodeConfig.x = manualPos.x
      nodeConfig.y = manualPos.y
    }
    graph.addNode(nodeConfig)
  })

  // 添加边：连线中点显示关系类型徽标，源端圆点 + 目标端箭头
  edges.forEach(e => {
    const relType = e.relationType || '1:N'
    const relColor = relType === '1:1' ? colors.rel11 : relType === 'N:M' ? colors.relNM : colors.rel1N
    graph.addEdge({
      source: { cell: e.source },
      target: { cell: e.target },
      zIndex: 0,
      attrs: {
        line: {
          stroke: colors.edgeColor,
          strokeWidth: 1.5,
          // 目标端箭头表示引用方向
          targetMarker: { name: 'classic', width: 8, height: 6 },
          // 源端圆点表示关系起点
          sourceMarker: { name: 'circle', r: 3 },
        },
      },
      labels: [{
        attrs: {
          text: { text: relType, fontSize: 10, fill: '#ffffff', fontWeight: 600 },
          rect: { fill: relColor, stroke: '#ffffff', 'stroke-width': 1, rx: 4, ry: 4 },
        },
        position: { distance: 0.5 },
      }],
      data: { ...e },
    })
  })

  // 仅在没有手动位置时执行自动布局
  if (manualPositions.value.size === 0) {
    await applyLayout()
    graph.positionContent('center', { padding: 40 })
  } else if (manualPositions.value.size < nodes.length) {
    // 部分节点有手动位置：对剩余节点布局，避免覆盖已定位节点
    await applyLayout(true)
    graph.positionContent('center', { padding: 40 })
  }

  graph.on('node:click', ({ node }) => {
    maxZIndex++
    node.setZIndex(maxZIndex)
  })

  // 节点拖动结束后记录位置，以便在折叠/展开等操作后保持
  graph.on('node:moved', ({ node }) => {
    const pos = node.getPosition()
    manualPositions.value.set(node.id, { x: pos.x, y: pos.y })
  })

  graph.on('node:mouseenter', ({ node }) => {
    const el = containerRef.value?.querySelector(`[data-node-id="${node.id}"]`)
    if (el && !searchQuery.value) el.style.borderColor = colors.hoverBorder
  })

  graph.on('node:mouseleave', ({ node }) => {
    const el = containerRef.value?.querySelector(`[data-node-id="${node.id}"]`)
    if (el && !searchQuery.value) el.style.borderColor = colors.nodeBorder
  })

  // 边 hover：高亮连线并显示关系详情提示框
  graph.on('edge:mouseenter', ({ edge, e }) => {
    edge.attr('line/strokeWidth', 3)
    edge.attr('line/stroke', colors.hoverBorder)
    const data = edge.getData() || {}
    const rect = containerRef.value?.getBoundingClientRect()
    if (rect) {
      edgeTooltip.value = {
        visible: true,
        x: e.clientX - rect.left + 12,
        y: e.clientY - rect.top + 12,
        relationType: data.relationType || '1:N',
        source: data.source,
        target: data.target,
        columns: data.columns || '',
        constraintName: data.constraintName || '',
      }
    }
  })

  graph.on('edge:mouseleave', ({ edge }) => {
    edge.attr('line/strokeWidth', 1.5)
    edge.attr('line/stroke', colors.edgeColor)
    edgeTooltip.value.visible = false
  })

  // 异步渲染完成后应用搜索高亮
  if (useAsync) {
    graph.on('render:done', () => {
      applySearchHighlight()
    })
  } else {
    await nextTick()
    applySearchHighlight()
  }
}

// 执行 dagre 自动布局；preserveManual 为 true 时保留已手动定位的节点
async function applyLayout(preserveManual = false) {
  if (!graph) return
  const nodes = filteredNodes.value
  const edges = filteredEdges.value
  try {
    const dagreLayout = new DagreLayout({
      type: 'dagre',
      rankdir: layoutType.value,
      // 适当增加间距以减少节点重叠
      nodesep: 60,
      ranksep: 100,
      nodeSize: (n) => [NODE_WIDTH, getNodeHeightByState(n, collapsedNodes.value.has(n.id))],
    })
    await dagreLayout.execute({
      nodes: nodes.map(n => ({ id: n.id })),
      edges: edges.map(e => ({ source: e.source, target: e.target })),
    })
    dagreLayout.forEachNode((layoutNode) => {
      // 保留手动定位的节点位置
      if (preserveManual && manualPositions.value.has(layoutNode.id)) return
      const node = graph.getCellById(layoutNode.id)
      if (node) {
        const n = nodes.find(x => x.id === layoutNode.id) || {}
        const size = [NODE_WIDTH, getNodeHeightByState(n, collapsedNodes.value.has(layoutNode.id))]
        const x = layoutNode.x - size[0] / 2
        const y = layoutNode.y - size[1] / 2
        node.setPosition(x, y, { silent: true })
      }
    })
  } catch (err) {
    handleError(err, 'ER图布局计算')
    // 布局失败时使用网格排列兜底
    const cols = Math.ceil(Math.sqrt(nodes.length))
    nodes.forEach((n, idx) => {
      if (preserveManual && manualPositions.value.has(n.id)) return
      const row = Math.floor(idx / cols)
      const col = idx % cols
      const node = graph.getCellById(n.id)
      if (node) {
        const nodeHeight = getNodeHeightByState(n, collapsedNodes.value.has(n.id))
        node.setPosition({ x: col * (NODE_WIDTH + 60) + 40, y: row * (nodeHeight + 60) + 40 }, { silent: true })
      }
    })
  }
}

// 重新布局：清除手动位置并重新计算
async function relayout() {
  if (!graph) return
  manualPositions.value.clear()
  await applyLayout()
  if (graph) graph.positionContent('center', { padding: 40 })
}

function fitGraph() {
  if (graph) {
    graph.zoomToFit({ padding: 40, maxScale: 2 })
  }
}

// ========== 搜索功能 ==========
// 应用搜索高亮：匹配节点高亮边框，非匹配节点降低透明度
function applySearchHighlight() {
  if (!graph || !containerRef.value) return
  const q = searchQuery.value.trim().toLowerCase()
  const matched = new Set(matchedNodeIds.value)
  const colors = themeColors.value
  // 有搜索词但无匹配时，不改变任何节点样式（避免全部变暗造成困惑）
  const hasMatch = matched.size > 0
  filteredNodes.value.forEach(n => {
    const el = containerRef.value.querySelector(`[data-node-id="${n.id}"]`)
    if (!el) return
    if (!q || !hasMatch) {
      el.style.opacity = '1'
      el.style.borderColor = colors.nodeBorder
      el.style.borderWidth = '2px'
    } else if (matched.has(n.id)) {
      el.style.opacity = '1'
      el.style.borderColor = colors.hoverBorder
      el.style.borderWidth = '3px'
    } else {
      el.style.opacity = '0.25'
      el.style.borderColor = colors.nodeBorder
      el.style.borderWidth = '2px'
    }
  })
}

// 回车依次跳转到下一个匹配节点
function focusNextMatch() {
  const matched = matchedNodeIds.value
  if (matched.length === 0) return
  if (currentMatchIndex.value >= matched.length) {
    currentMatchIndex.value = 0
  }
  const nodeId = matched[currentMatchIndex.value]
  const node = graph?.getCellById(nodeId)
  if (node) {
    graph.centerCell(node)
    maxZIndex++
    node.setZIndex(maxZIndex)
  }
  currentMatchIndex.value = (currentMatchIndex.value + 1) % matched.length
}

function onSearchClear() {
  currentMatchIndex.value = 0
  applySearchHighlight()
}

// 监听搜索关键字变化，重置匹配索引并应用高亮
watch(searchQuery, () => {
  currentMatchIndex.value = 0
  nextTick(() => applySearchHighlight())
})

// ========== 折叠/展开功能 ==========
// 切换单个节点的折叠状态（更新数据触发 HTML 重渲染 + 调整节点高度）
function toggleCollapse(nodeId) {
  if (collapsedNodes.value.has(nodeId)) {
    collapsedNodes.value.delete(nodeId)
  } else {
    collapsedNodes.value.add(nodeId)
  }
  const node = graph?.getCellById(nodeId)
  if (!node) return
  const isCollapsed = collapsedNodes.value.has(nodeId)
  const data = node.getData()
  node.setData({ ...data, collapsed: isCollapsed })
  const nodeData = filteredNodes.value.find(n => n.id === nodeId) || data
  const height = getNodeHeightByState(nodeData, isCollapsed)
  node.resize(NODE_WIDTH, height)
  // HTML 重渲染会重置内联样式，需在下一帧重新应用搜索高亮
  nextTick(() => applySearchHighlight())
}

// 全部折叠
function collapseAll() {
  filteredNodes.value.forEach(n => collapsedNodes.value.add(n.id))
  applyCollapseStateToAll()
}

// 全部展开
function expandAll() {
  collapsedNodes.value.clear()
  applyCollapseStateToAll()
}

// 将当前折叠状态批量应用到画布节点
function applyCollapseStateToAll() {
  filteredNodes.value.forEach(n => {
    const node = graph?.getCellById(n.id)
    if (!node) return
    const isCollapsed = collapsedNodes.value.has(n.id)
    const data = node.getData()
    node.setData({ ...data, collapsed: isCollapsed })
    node.resize(NODE_WIDTH, getNodeHeightByState(n, isCollapsed))
  })
  // HTML 重渲染会重置内联样式，需在下一帧重新应用搜索高亮
  nextTick(() => applySearchHighlight())
}

// ========== 导出功能 ==========
// 生成时间戳字符串用于文件名
function getTimestamp() {
  const d = new Date()
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`
}

function handleExport(command) {
  if (command === 'png') exportPNG()
  else if (command === 'svg') exportSVG()
}

// 导出 PNG：使用 X6 Export 插件的 toPNG，带白色背景
function exportPNG() {
  if (!graph) return
  graph.toPNG((dataUri) => {
    const link = document.createElement('a')
    link.download = `er_diagram_${schema}_${getTimestamp()}.png`
    link.href = dataUri
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }, {
    padding: 20,
    backgroundColor: '#ffffff',
  })
}

// 导出 SVG：通过 beforeSerialize 注入白色背景矩形
function exportSVG() {
  if (!graph) return
  graph.toSVG((svg) => {
    const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.download = `er_diagram_${schema}_${getTimestamp()}.svg`
    link.href = url
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }, {
    copyStyles: true,
    beforeSerialize: (svg) => {
      // SVG 无 backgroundColor 选项，手动插入白色背景矩形
      const bbox = graph.getContentBBox()
      const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
      rect.setAttribute('x', bbox.x)
      rect.setAttribute('y', bbox.y)
      rect.setAttribute('width', bbox.width)
      rect.setAttribute('height', bbox.height)
      rect.setAttribute('fill', '#ffffff')
      svg.insertBefore(rect, svg.firstChild)
      return svg
    },
  })
}

// 切换"显示所有表"时清除手动位置（图结构变化，旧位置不再适用）
function onShowAllTablesChange() {
  manualPositions.value.clear()
  rebuildGraph()
}

// ========== AI 分析表关系 ==========
// 实际数据库通常不建物理外键（关系由程序定义），ER 图加载到的物理外键常常为空。
// 此功能把表元数据发给后端 AI，由 AI 根据命名/注释推断表关系，
// 推断结果仅保存在内存 allEdges 中（origin='ai'），不持久化、不写数据库。
const aiAnalyzing = ref(false)

async function analyzeRelationsWithAI() {
  if (allNodes.value.length === 0) {
    ElMessage.warning('当前没有表，无法分析关系')
    return
  }
  if (allNodes.value.length > 30) {
    // 与后端 maxTables=30 对齐，避免发送过大数据
    try {
      await ElMessageBox.confirm(
        `当前共 ${allNodes.value.length} 张表，AI 分析将仅取前 30 张（按当前排序）。是否继续？`,
        '提示',
        { type: 'warning' }
      )
    } catch {
      return  // 用户取消
    }
  }

  aiAnalyzing.value = true
  try {
    // 取前 30 张表（与后端限制一致）
    const tablesForAI = allNodes.value.slice(0, 30).map(n => ({
      name: n.id,
      comment: n.comment || '',
      columns: (n.columns || []).map(c => ({
        name: c.name,
        type: c.type,
        comment: c.comment || '',
        primaryKey: !!c.primaryKey,
        unique: !!c.unique,
      })),
    }))
    // 已有的物理外键/AI 关系一并传给 AI，避免重复推断
    const existingRelations = allEdges.value
      .filter(e => e.origin !== 'manual')
      .flatMap(e => (e.columnPairs || []).map(cp => ({
        source: e.source,
        sourceCol: cp.sourceCol,
        target: e.target,
        targetCol: cp.targetCol,
        relationType: e.relationType || '1:N',
        confidence: 'high',
        reason: '已存在的外键关系',
      })))

    const resp = await analyzeERRelations({
      connId,
      schema,
      dbType: dbType || dbSchemaProxy.getDbType(schema) || '',
      tables: tablesForAI,
      existingRelations,
    })
    const inferred = resp.data?.data?.relations || []
    if (inferred.length === 0) {
      ElMessage.info('AI 未推断出表关系，可尝试手动编辑')
      return
    }

    // 合并 AI 推断结果：先去掉之前的 AI 关系（origin=ai），再追加新结果
    // 物理外键（origin=fk）和手动新增（origin=manual）保留
    allEdges.value = allEdges.value.filter(e => e.origin !== 'ai')
    inferred.forEach(r => {
      allEdges.value.push({
        source: r.source,
        target: r.target,
        columnPairs: [{ sourceCol: r.sourceCol || '', targetCol: r.targetCol || '' }],
        columns: (r.sourceCol || '') + '=' + (r.targetCol || ''),
        constraintName: '',
        relationType: r.relationType || '1:N',
        origin: 'ai',
        // confidence/reason 仅在编辑器中显示，不影响画布渲染
        confidence: r.confidence || 'medium',
        reason: r.reason || '',
      })
    })
    // 重新推断关系类型（仅对物理外键生效，AI/manual 的 type 已由来源给定）
    inferAllRelationTypes()
    manualPositions.value.clear()
    await rebuildGraph()
    ElMessage.success(`AI 推断出 ${inferred.length} 条关系`)
  } catch (err) {
    handleError(err, 'AI 分析表关系')
  } finally {
    aiAnalyzing.value = false
  }
}

// ========== 手动编辑关系 ==========
// 提供 UI 增删改连线，仅修改内存 allEdges，关闭对话框后应用，不持久化。
const relationEditorVisible = ref(false)
// 编辑器使用独立副本，"应用"时才同步回 allEdges
const editableRelations = ref([])

function openRelationEditor() {
  // 打开时深拷贝当前 allEdges，避免取消时污染
  editableRelations.value = allEdges.value.map(e => ({
    source: e.source,
    sourceCol: e.columnPairs?.[0]?.sourceCol || '',
    target: e.target,
    targetCol: e.columnPairs?.[0]?.targetCol || '',
    relationType: e.relationType || '1:N',
    origin: e.origin || 'fk',
    confidence: e.confidence || '',
    reason: e.reason || '',
  }))
  relationEditorVisible.value = true
}

function getColumnsOf(tableId) {
  if (!tableId) return []
  const node = allNodes.value.find(n => n.id === tableId)
  return (node?.columns || []).map(c => c.name)
}

function onRelationRowChange(idx) {
  // 表名变更后清空字段，避免出现不存在的字段引用
  const row = editableRelations.value[idx]
  if (!row) return
  row.sourceCol = ''
  row.targetCol = ''
}

function addRelation() {
  editableRelations.value.push({
    source: '',
    sourceCol: '',
    target: '',
    targetCol: '',
    relationType: '1:N',
    origin: 'manual',
    confidence: '',
    reason: '',
  })
}

function removeRelation(idx) {
  editableRelations.value.splice(idx, 1)
}

async function applyRelationEditor() {
  // 过滤掉不完整的关系（必须有源表、目标表、字段）
  const valid = editableRelations.value.filter(r =>
    r.source && r.target && r.sourceCol && r.targetCol && r.source !== r.target
  )
  if (valid.length < editableRelations.value.length) {
    ElMessage.warning('已自动忽略 ' + (editableRelations.value.length - valid.length) + ' 条不完整关系')
  }
  // 保留 FK 与 manual，AI 推断的关系若编辑器未删除则保留原样
  // 此处简化策略：以编辑器内容为准重建 allEdges
  // 但需要保留 origin=fk 的物理外键（不能被编辑器删除，否则刷新后又出现）
  const fkEdges = allEdges.value.filter(e => e.origin === 'fk')
  const newEdges = valid.map(r => ({
    source: r.source,
    target: r.target,
    columnPairs: [{ sourceCol: r.sourceCol, targetCol: r.targetCol }],
    columns: r.sourceCol + '=' + r.targetCol,
    constraintName: '',
    relationType: r.relationType || '1:N',
    origin: r.origin || 'manual',
    confidence: r.confidence || '',
    reason: r.reason || '',
  }))
  allEdges.value = [...fkEdges, ...newEdges.filter(e => e.origin !== 'fk')]
  inferAllRelationTypes()
  manualPositions.value.clear()
  relationEditorVisible.value = false
  await rebuildGraph()
  ElMessage.success('关系已应用')
}

onMounted(() => {
  loadData()
})

// 监听主题变化，刷新主题色并重建图形以应用新颜色
watch(() => document.documentElement.getAttribute('data-theme'), () => {
  themeColors.value = getThemeColors()
  if (allNodes.value.length > 0) {
    nextTick(() => rebuildGraph())
  }
})

onBeforeUnmount(() => {
  destroyGraph()
})
</script>

<style scoped>
.er-diagram-wrapper {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  position: relative;
}
.er-toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
}
.er-toolbar-hint {
  font-size: 12px;
  color: var(--text-tertiary);
}
.er-search-input {
  width: 200px;
}
.er-search-count {
  color: var(--db-accent, #409eff);
  font-weight: 600;
}
.er-canvas {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--border-primary);
  border-radius: 6px;
  overflow: hidden;
  margin: 8px 12px;
}
.er-empty {
  text-align: center;
  padding: 40px;
  color: var(--text-tertiary);
}
.er-footer {
  padding: 4px 12px 8px;
  font-size: 12px;
  color: var(--text-tertiary);
}
/* 关系连线 hover 提示框 */
.er-edge-tooltip {
  position: absolute;
  z-index: 1000;
  background: var(--db-card-bg, #ffffff);
  border: 1px solid var(--db-card-border, #e4e7ed);
  border-radius: 6px;
  box-shadow: var(--db-card-shadow, 0 2px 8px rgba(0, 0, 0, 0.08));
  padding: 8px 12px;
  font-size: 12px;
  color: var(--db-text-primary, #303133);
  pointer-events: none;
  max-width: 320px;
  word-break: break-all;
}
.er-edge-tooltip-type {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 3px;
  background: var(--db-accent, #409eff);
  color: #ffffff;
  font-weight: 600;
  font-size: 11px;
  margin-bottom: 4px;
}
.er-edge-tooltip-line {
  margin: 2px 0;
}
.er-edge-tooltip-cols {
  color: var(--db-text-secondary, #606266);
  font-family: Consolas, Monaco, monospace;
  font-size: 11px;
}
.er-edge-tooltip-fk {
  color: var(--db-text-tertiary, #6c6e72);
  font-size: 11px;
  margin-top: 2px;
}
/* 关系编辑器 */
.relation-editor-tip {
  margin-bottom: 12px;
  padding: 8px 12px;
  background: var(--bg-tertiary, #f5f7fa);
  border-left: 3px solid var(--accent-color, #409eff);
  border-radius: 4px;
  font-size: 12px;
  color: var(--text-secondary, #606266);
  line-height: 1.5;
}
.relation-editor-count {
  font-size: 12px;
  color: var(--text-tertiary, #909399);
}
:deep(.el-radio-button__inner) {
  font-size: 12px;
  padding: 4px 12px;
}
:deep(.el-input__wrapper) {
  font-size: 12px;
}
</style>
