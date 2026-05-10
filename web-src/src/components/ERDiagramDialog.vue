<template>
  <el-dialog
    v-model="visible"
    :title="'ER 关系图 - ' + schema"
    width="90%"
    max-width="1600px"
    :draggable="true"
    destroy-on-close
    fullscreen
    class="classical-dialog"
    @opened="loadData"
  >
    <div style="display:flex;gap:8px;margin-bottom:10px;align-items:center;">
      <el-checkbox v-model="showAllTables" @change="rebuildGraph">显示所有表</el-checkbox>
      <span style="font-size:12px;color:var(--text-tertiary);">仅显示有关联的表</span>
      <div style="flex:1;"></div>
      <el-radio-group v-model="layoutType" size="small" @change="rebuildGraph">
        <el-radio-button value="TB">从上到下</el-radio-button>
        <el-radio-button value="LR">从左到右</el-radio-button>
      </el-radio-group>
      <el-button size="small" @click="fitGraph">适应画布</el-button>
      <el-select v-model="zoomLevel" size="small" style="width:80px;" @change="onZoomChange">
        <el-option label="50%" :value="0.5" />
        <el-option label="75%" :value="0.75" />
        <el-option label="100%" :value="1" />
        <el-option label="125%" :value="1.25" />
        <el-option label="150%" :value="1.5" />
      </el-select>
    </div>
    <div ref="containerRef" style="width:100%;height:65vh;min-height:400px;border:1px solid var(--border-primary);border-radius:6px;overflow:hidden;" v-loading="loading"></div>
    <el-empty v-if="!loading && allNodes.length === 0" description="该数据库没有表或外键关系" />

    <template #footer>
      <span style="font-size:12px;color:var(--text-tertiary);margin-right:16px;">
        表: {{ filteredNodes.length }} | 关系: {{ filteredEdges.length }}
      </span>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { DagreLayout } from '@antv/layout'
import { Graph } from '@antv/x6'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import http from '../js/utils/httpProxy.js'
import { dbSchemaProxy } from '../stores/sql'

const props = defineProps({
  modelValue: Boolean,
  connId: String,
  schema: String,
})

const emit = defineEmits(['update:modelValue', 'tableClick'])

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const loading = ref(false)
const containerRef = ref(null)
const layoutType = ref('TB')
const showAllTables = ref(false)
const zoomLevel = ref(1)

let graph = null
const allNodes = ref([])
const allEdges = ref([])

const filteredNodes = computed(() => {
  if (showAllTables.value) return allNodes.value
  const connectedIds = new Set()
  allEdges.value.forEach(e => { connectedIds.add(e.source); connectedIds.add(e.target) })
  return allNodes.value.filter(n => connectedIds.has(n.id))
})

const filteredEdges = computed(() => {
  if (showAllTables.value) return allEdges.value
  const connectedIds = new Set()
  allEdges.value.forEach(e => { connectedIds.add(e.source); connectedIds.add(e.target) })
  return allEdges.value.filter(e => connectedIds.has(e.source) && connectedIds.has(e.target))
})

async function execQuery(sql) {
  const params = new URLSearchParams()
  params.append('connId', props.connId)
  params.append('schema', props.schema)
  params.append('sql', sql)
  params.append('maxLine', '5000')
  const resp = await http.post('/execSQL', params)
  return resp.data.data?.data || []
}

async function loadData() {
  loading.value = true
  allNodes.value = []
  allEdges.value = []
  try {
    const dbType = (dbSchemaProxy.getDbType(props.schema) || '').toLowerCase()
    const schema = props.schema
    if (dbType === 'mysql') {
      await loadMysqlData(schema)
    } else if (dbType === 'sqlite') {
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
  const [tableRows, fkRows] = await Promise.all([
    execQuery(`SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT, TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = '${schema}' AND TABLE_TYPE IN ('BASE TABLE', 'VIEW')`),
    execQuery(`SELECT TABLE_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = '${schema}' AND REFERENCED_TABLE_NAME IS NOT NULL`),
  ])

  allNodes.value = tableRows.map(r => ({
    id: r.TABLE_NAME || r.table_name,
    label: r.TABLE_NAME || r.table_name,
    type: (r.TABLE_TYPE || r.table_type) === 'VIEW' ? 'VIEW' : 'TABLE',
    comment: r.TABLE_COMMENT || r.table_comment || '',
    rows: r.TABLE_ROWS || r.table_rows || '',
  }))

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
  }))

  const fkPromises = allNodes.value
    .filter(n => n.type === 'TABLE')
    .map(async n => {
      try {
        const fks = await execQuery(`PRAGMA foreign_key_list('${n.id}')`)
        return fks.map(fk => ({
          source: n.id,
          target: fk.table,
          columns: (fk.from || '') + '=' + (fk.to || ''),
        }))
      } catch { return [] }
    })
  const results = await Promise.all(fkPromises)
  const fkMap = {}
  results.flat().forEach(e => {
    const key = e.source + '→' + e.target
    if (!fkMap[key]) fkMap[key] = { source: e.source, target: e.target, columns: [] }
    fkMap[key].columns.push(e.columns)
  })
  allEdges.value = Object.values(fkMap).map(e => ({ ...e, columns: e.columns.join(', ') }))
}

function destroyGraph() {
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

  graph = new Graph({
    container: containerRef.value,
    width: containerRef.value.clientWidth,
    height: containerRef.value.clientHeight,
    background: { color: 'transparent' },
    grid: { visible: true, size: 10, type: 'dot', args: { color: '#e0e0e0' } },
    panning: { enabled: true, modifiers: ['shift', 'ctrl'] },
    mousewheel: { enabled: true, modifiers: ['alt'] },
    selecting: { enabled: true, rubberband: true },
    connecting: { enabled: false },
  })

  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
  const headerBg = isDark ? '#313147' : '#409eff'
  const headerText = '#ffffff'
  const nodeBg = isDark ? '#1e1e2e' : '#ffffff'
  const nodeBorder = isDark ? '#45475a' : '#d9d9d9'
  const edgeColor = isDark ? '#585b70' : '#8c8c8c'

  nodes.forEach(n => {
    const bodyHtml = n.comment ? `<div style="padding:6px 12px;font-size:11px;color:${isDark?'#a6adc8':'#606266'};text-align:center;">${n.comment}</div>` : ''

    graph.addNode({
      id: n.id,
      shape: 'html',
      width: Math.max(140, n.label.length * 9 + 60),
      height: n.comment ? 64 : 44,
      html: `<div style="background:${nodeBg};border:1px solid ${nodeBorder};border-radius:6px;overflow:hidden;cursor:pointer;box-shadow:0 1px 4px rgba(0,0,0,0.1);width:100%;height:100%;display:flex;flex-direction:column;"><div style="background:${headerBg};padding:6px 12px;text-align:center;font-size:13px;font-weight:600;color:${headerText};">${n.label}</div>${bodyHtml}</div>`,
    })
  })

  edges.forEach(e => {
    graph.addEdge({
      source: { cell: e.source, port: 'top' },
      target: { cell: e.target, port: 'bottom' },
      attrs: {
        line: {
          stroke: edgeColor,
          strokeWidth: 1.5,
          targetMarker: { name: 'block', width: 6, height: 4 },
        },
      },
      labels: [{
        attrs: {
          text: { text: e.columns || '', fontSize: 9, fill: isDark ? '#a6adc8' : '#999999' },
          rect: { fill: 'transparent', stroke: 'none' },
        },
        position: { distance: 0.5 },
      }],
    })
  })

  // Apply dagre layout
  try {
    const dagreLayout = new DagreLayout({
      type: 'dagre',
      rankdir: layoutType.value,
      nodesep: 60,
      ranksep: 80,
    })
    const model = dagreLayout.layout({
      nodes: nodes.map(n => ({ id: n.id, width: Math.max(140, n.label.length * 9 + 60), height: n.comment ? 64 : 44 })),
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
    // Fallback to simple grid
    const cols = Math.ceil(Math.sqrt(nodes.length))
    nodes.forEach((n, idx) => {
      const row = Math.floor(idx / cols)
      const col = idx % cols
      const node = graph.getCellById(n.id)
      if (node) {
        node.setPosition({ x: col * 240 + 40, y: row * 120 + 40 }, { silent: true })
      }
    })
  }

  graph.positionContent('center', { padding: 40 })

  graph.on('node:click', ({ node }) => {
    emit('tableClick', node.id)
  })

  graph.on('node:mouseenter', ({ node }) => {
    node.setAttrs({
      body: { stroke: isDark ? '#89b4fa' : '#1890ff', strokeWidth: 2 },
    })
  })
  graph.on('node:mouseleave', ({ node }) => {
    node.setAttrs({
      body: { stroke: nodeBorder, strokeWidth: 1 },
    })
  })

  zoomLevel.value = 1
}

function fitGraph() {
  if (graph) {
    graph.zoomToFit({ padding: 40, maxScale: 1.5 })
    zoomLevel.value = Math.round(graph.zoom() * 100) / 100
  }
}

function onZoomChange(val) {
  if (graph) {
    graph.zoomTo(val)
  }
}

watch(visible, (v) => {
  if (!v) {
    destroyGraph()
  }
})

watch(() => document.documentElement.getAttribute('data-theme'), () => {
  if (visible.value && allNodes.value.length > 0) {
    nextTick(() => rebuildGraph())
  }
})

onBeforeUnmount(() => {
  destroyGraph()
})
</script>

<style scoped>
:deep(.el-radio-button__inner) {
  font-size: 12px;
  padding: 4px 12px;
}
</style>