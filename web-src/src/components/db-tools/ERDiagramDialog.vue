<template>
  <div class="er-diagram-wrapper">
    <div class="er-toolbar">
      <el-checkbox v-model="showAllTables" @change="rebuildGraph">显示所有表</el-checkbox>
      <span class="er-toolbar-hint">取消仅显示有外键关联的表</span>
      <span class="er-toolbar-hint" style="margin-left:8px;">💡 双击表名浏览数据 | Ctrl+滚轮缩放 | 拖动平移</span>
      <div style="flex:1;"></div>
      <el-radio-group v-model="layoutType" size="small" @change="rebuildGraph">
        <el-radio-button value="TB">从上到下</el-radio-button>
        <el-radio-button value="LR">从左到右</el-radio-button>
      </el-radio-group>
      <el-button size="small" @click="fitGraph">适应画布</el-button>
    </div>
    <div ref="containerRef" class="er-canvas" v-loading="loading"></div>
    <div v-if="!loading && allNodes.length === 0" class="er-empty">该数据库没有表</div>
    <div class="er-footer">表: {{ filteredNodes.length }} | 关系: {{ filteredEdges.length }}</div>
  </div>
</template>

<script setup>
import { DagreLayout } from '@antv/layout'
import { Graph, Shape } from '@antv/x6'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import http from '@/utils/httpProxy.js'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()

const NODE_WIDTH = 300
const HEADER_HEIGHT = 30
const COMMENT_HEIGHT = 20
const COL_ROW_HEIGHT = 22
const BOTTOM_PADDING = 4

function getNodeHeight(n) {
  let h = HEADER_HEIGHT
  if (n.comment) h += COMMENT_HEIGHT
  if (n.columns && n.columns.length > 0) h += n.columns.length * COL_ROW_HEIGHT
  h += BOTTOM_PADDING
  return h
}

function createErTableHtml(cell) {
  const nodeData = cell.getData() || {}
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
  }

  const el = document.createElement('div')
  el.setAttribute('data-node-id', nodeData.id || cell.id)
  el.style.cssText = `background:${c.nodeBg};border:2px solid ${c.nodeBorder};border-radius:6px;overflow:hidden;cursor:default;box-shadow:0 1px 4px rgba(0,0,0,0.1);width:100%;height:100%;display:flex;flex-direction:column;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',sans-serif;transition:border-color 0.15s ease;`

  const header = document.createElement('div')
  header.className = 'er-table-header'
  header.setAttribute('data-table-id', nodeData.id || cell.id)
  header.style.cssText = `background:${c.headerBg};padding:6px 10px;font-size:12px;font-weight:600;color:${c.headerText};cursor:pointer;user-select:none;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;`
  header.textContent = nodeData.label || cell.id
  el.appendChild(header)

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
      nameSpan.style.cssText = `flex:2;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:${c.colNameColor};font-weight:500;user-select:text;`
      nameSpan.textContent = col.name
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

let graph = null
const allNodes = ref([])
const allEdges = ref([])
let maxZIndex = 1
let mousedownHandler = null
let lastHeaderClick = { tableId: null, time: 0 }

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

async function execQuery(sql, maxLine) {
  const params = new URLSearchParams()
  params.append('connId', connId)
  params.append('schema', schema)
  params.append('sql', sql)
  params.append('maxLine', String(maxLine || 5000))
  const resp = await http.post('/execSQL', params)
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
  } catch (err) {
    console.error('[ERDiagram] load data error:', err)
  } finally {
    loading.value = false
    await nextTick()
    rebuildGraph()
  }
}

async function loadMysqlData(schema) {
  const [tableRows, fkRows, columnRows] = await Promise.all([
    execQuery(`SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT, TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = '${schema}' AND TABLE_TYPE IN ('BASE TABLE', 'VIEW')`),
    execQuery(`SELECT TABLE_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = '${schema}' AND REFERENCED_TABLE_NAME IS NOT NULL`),
    execQuery(`SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '${schema}' ORDER BY TABLE_NAME, ORDINAL_POSITION`, 50000),
  ])

  const colMap = {}
  const tableNamesFromColumns = new Set()
  columnRows.forEach(r => {
    const tableName = r.TABLE_NAME || r.table_name
    tableNamesFromColumns.add(tableName)
    if (!colMap[tableName]) colMap[tableName] = []
    colMap[tableName].push({
      name: r.COLUMN_NAME || r.column_name,
      type: r.COLUMN_TYPE || r.column_type,
      comment: r.COLUMN_COMMENT || r.column_comment || '',
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

  const fkMap = {}
  fkRows.forEach(r => {
    const src = r.TABLE_NAME || r.table_name
    const dst = r.REFERENCED_TABLE_NAME || r.referenced_table_name
    const key = src + '→' + dst
    if (!fkMap[key]) fkMap[key] = { source: src, target: dst, columns: [] }
    fkMap[key].columns.push((r.COLUMN_NAME || r.column_name) + '=' + (r.REFERENCED_COLUMN_NAME || r.referenced_column_name))
  })
  allEdges.value = Object.values(fkMap).map(e => ({
    ...e,
    columns: e.columns.join(', '),
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
        const [cols, fks] = await Promise.all([
          execQuery(`PRAGMA table_info('${n.id}')`),
          execQuery(`PRAGMA foreign_key_list('${n.id}')`),
        ])
        return {
          tableName: n.id,
          columns: cols.map(c => ({ name: c.name, type: c.type, comment: '' })),
          fks: fks.map(fk => ({
            source: n.id,
            target: fk.table,
            columns: (fk.from || '') + '=' + (fk.to || ''),
          })),
        }
      } catch { return { tableName: n.id, columns: [], fks: [] } }
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
    if (!fkMap[key]) fkMap[key] = { source: e.source, target: e.target, columns: [] }
    fkMap[key].columns.push(e.columns)
  })
  allEdges.value = Object.values(fkMap).map(e => ({ ...e, columns: e.columns.join(', ') }))
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

function rebuildGraph() {
  destroyGraph()
  if (!containerRef.value) return

  const nodes = filteredNodes.value
  const edges = filteredEdges.value
  if (nodes.length === 0) return

  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
  const colors = {
    nodeBorder: isDark ? '#45475a' : '#d9d9d9',
    edgeColor: isDark ? '#585b70' : '#8c8c8c',
    hoverBorder: isDark ? '#89b4fa' : '#1890ff',
    gridColor: isDark ? '#2a2a3c' : '#e0e0e0',
    labelColor: isDark ? '#a6adc8' : '#999999',
  }

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
  })

  mousedownHandler = (e) => {
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

  nodes.forEach(n => {
    const nodeHeight = getNodeHeight(n)
    graph.addNode({
      id: n.id,
      shape: 'er-table',
      width: NODE_WIDTH,
      height: nodeHeight,
      data: { ...n },
      zIndex: 1,
    })
  })

  edges.forEach(e => {
    graph.addEdge({
      source: { cell: e.source },
      target: { cell: e.target },
      zIndex: 0,
      attrs: {
        line: {
          stroke: colors.edgeColor,
          strokeWidth: 1.5,
          targetMarker: { name: 'block', width: 6, height: 4 },
        },
      },
      labels: [{
        attrs: {
          text: { text: e.columns || '', fontSize: 9, fill: colors.labelColor },
          rect: { fill: 'transparent', stroke: 'none' },
        },
        position: { distance: 0.5 },
      }],
    })
  })

  try {
    const dagreLayout = new DagreLayout({
      type: 'dagre',
      rankdir: layoutType.value,
      nodesep: 80,
      ranksep: 120,
    })
    const model = dagreLayout.layout({
      nodes: nodes.map(n => ({ id: n.id, width: NODE_WIDTH, height: getNodeHeight(n) })),
      edges: edges.map(e => ({ source: e.source, target: e.target })),
    })
    if (model.nodes) {
      model.nodes.forEach(n => {
        const node = graph.getCellById(n.id)
        if (node) {
          node.setPosition({ x: n.x, y: n.y }, { silent: true })
        }
      })
    }
  } catch {
    const cols = Math.ceil(Math.sqrt(nodes.length))
    nodes.forEach((n, idx) => {
      const row = Math.floor(idx / cols)
      const col = idx % cols
      const node = graph.getCellById(n.id)
      if (node) {
        node.setPosition({ x: col * (NODE_WIDTH + 60) + 40, y: row * 300 + 40 }, { silent: true })
      }
    })
  }

  graph.positionContent('center', { padding: 40 })

  graph.on('node:click', ({ node }) => {
    maxZIndex++
    node.setZIndex(maxZIndex)
  })

  graph.on('node:mouseenter', ({ node }) => {
    const el = containerRef.value?.querySelector(`[data-node-id="${node.id}"]`)
    if (el) el.style.borderColor = colors.hoverBorder
  })

  graph.on('node:mouseleave', ({ node }) => {
    const el = containerRef.value?.querySelector(`[data-node-id="${node.id}"]`)
    if (el) el.style.borderColor = colors.nodeBorder
  })
}

function fitGraph() {
  if (graph) {
    graph.zoomToFit({ padding: 40, maxScale: 2 })
  }
}

onMounted(() => {
  loadData()
})

watch(() => document.documentElement.getAttribute('data-theme'), () => {
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
:deep(.el-radio-button__inner) {
  font-size: 12px;
  padding: 4px 12px;
}
</style>
