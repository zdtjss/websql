<template>
  <div class="er-diagram-wrapper" role="application" aria-label="ER 关系图">
    <div class="er-toolbar" role="toolbar" aria-label="ER 图工具栏">
      <el-checkbox v-model="showAllTables" aria-label="显示所有表，取消则仅显示有外键关联的表" @change="onShowAllTablesChange">
        显示所有表
      </el-checkbox>
      <el-input
        v-model="searchQuery"
        placeholder="搜索表名/字段/注释（回车跳转）"
        size="small"
        clearable
        class="er-search-input"
        aria-label="搜索表名、字段或注释"
        @keydown.enter="focusNextMatch"
        @clear="onSearchClear"
      >
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <span v-if="searchQuery" class="er-toolbar-hint er-search-count">{{ matchedNodeIds.length }} 个匹配</span>
      <span class="er-toolbar-hint">双击表名浏览数据 | 点击连线选中 | 双击连线切换类型 | Delete 删除</span>
      <div class="er-toolbar-spacer"></div>
      <el-checkbox v-model="focusNeighbors" class="er-toolbar-check" aria-label="悬停表时聚焦其邻居">聚焦邻居</el-checkbox>
      <el-radio-group v-model="layoutType" size="small" aria-label="布局方向" @change="onLayoutChange">
        <el-radio-button value="TB">从上到下</el-radio-button>
        <el-radio-button value="LR">从左到右</el-radio-button>
      </el-radio-group>
      <el-button
        size="small"
        type="primary"
        plain
        :loading="aiAnalyzing"
        title="调用 AI 根据表名、字段、注释推断表关系（仅当前会话有效，不持久化）"
        aria-label="AI 分析表关系"
        @click="analyzeRelationsWithAI"
      >
        <el-icon style="margin-right: 4px"><MagicStick /></el-icon>AI 分析关系
      </el-button>
      <el-button size="small" title="刷新元数据（保留手动/AI 关系与手动布局）" aria-label="刷新元数据" @click="refreshSchema">
        <el-icon><Refresh /></el-icon>
      </el-button>
      <el-dropdown trigger="click" aria-label="导出 ER 图" @command="handleExport">
        <el-button size="small">导出<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="png">导出 PNG</el-dropdown-item>
            <el-dropdown-item command="svg">导出 SVG</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-button size="small" aria-label="适应画布" @click="fitGraph">适应画布</el-button>
      <el-button size="small" :plain="true" :type="showPanel ? 'primary' : undefined" @click="showPanel = !showPanel">关系面板</el-button>
    </div>

    <div class="er-legend-row">
      <LegendBar :items="legendItems" @toggle="toggleLegend" />
    </div>

    <div class="er-body">
      <div class="er-canvas-wrap">
        <div
          ref="containerRef"
          class="er-canvas"
          v-loading="loading"
          :aria-busy="loading"
          role="img"
          :aria-label="loading ? '正在加载 ER 图' : 'ER 关系图画布'"
        ></div>
        <div v-if="emptyState" class="er-empty-overlay" role="status">
          <div class="er-empty-text">{{ emptyState.message }}</div>
          <el-button v-if="emptyState.action === 'showAll'" size="small" type="primary" @click="enableShowAll">显示所有表</el-button>
          <el-button v-if="emptyState.action === 'refresh'" size="small" @click="refreshSchema">重新加载</el-button>
        </div>
      </div>
      <RelationPanel
        v-if="showPanel"
        :rows="relationRows"
        :selected-key="selectedRelationKey"
        @select="selectRelationFromPanel"
        @delete="deleteRelation"
        @cycle="cycleRelation"
      />
    </div>

    <div class="er-footer" aria-live="polite">
      表: {{ filteredNodes.length }} / {{ allNodes.length }} | 关系: {{ displayedEdges.length }} / {{ filteredEdges.length }}
      <span v-if="hiddenEdgeCount > 0"> | 已按图例隐藏: {{ hiddenEdgeCount }}</span>
      <span v-if="collapsedNodes.size > 0"> | 已折叠: {{ collapsedNodes.size }}</span>
      <span v-if="manualPositions.size > 0"> | 已手动定位: {{ manualPositions.size }}</span>
    </div>

    <div
      v-show="edgeTooltip.visible"
      class="er-edge-tooltip"
      :style="{ left: edgeTooltip.x + 'px', top: edgeTooltip.y + 'px' }"
      role="tooltip"
    >
      <div class="er-edge-tooltip-type">{{ edgeTooltip.relationType }}</div>
      <div class="er-edge-tooltip-line"><b>{{ edgeTooltip.source }}</b> → <b>{{ edgeTooltip.target }}</b></div>
      <div class="er-edge-tooltip-cols">字段映射: {{ edgeTooltip.sourceCol || '?' }} = {{ edgeTooltip.targetCol || '?' }}</div>
      <div v-if="edgeTooltip.constraintName" class="er-edge-tooltip-fk">外键: {{ edgeTooltip.constraintName }}</div>
      <div v-if="edgeTooltip.origin === 'ai'" class="er-edge-tooltip-fk">
        AI 推断（{{ edgeTooltip.confidence || 'medium' }}）<span v-if="edgeTooltip.reason">：{{ edgeTooltip.reason }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Export, Graph, Keyboard, Selection, Shape } from '@antv/x6'
import { ArrowDown, MagicStick, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { analyzeERRelations } from '@/api/er'
import { useDbSchemaStore } from '@/stores/dbSchema'
import { handleError } from '@/utils/errorHandler'
import { useTheme } from '@/utils/useTheme'
import { loadERSchema } from './er/data/schemaLoader'
import {
  displayRelationType,
  inferRelationTypes,
  mergeAiRelations,
  relationKey,
  removeAiRelations,
  reverseRelationKey,
  uniqueKeysOf,
} from './er/data/infer'
import type { ERRelation, ERTable } from './er/data/types'
import LegendBar from './er/panels/LegendBar.vue'
import RelationPanel from './er/panels/RelationPanel.vue'
import type { LegendItem, RelationRow } from './er/panels/types'
import { computeLayout } from './er/render/layout'
import {
  NODE_WIDTH,
  buildNodePorts,
  buildPortGroups,
  colNameByPortId,
  columnIndexFor,
  createErTableHtml,
  getNodeHeightByState,
  pickPortPair,
  type ERNodeData,
} from './er/render/nodeHtml'
import { erOrthRouter } from './er/render/route'
import { getERTheme, originColor, relationColor, type ERTheme } from './er/render/theme'

// 表数量超过该阈值时启用异步/虚拟渲染，提升大数据量下的性能
const VIRTUAL_THRESHOLD = 50
// 关系数超过该阈值时从正交路由回退到 smooth，避免交叉过多导致布局与渲染变慢
const ORTH_ROUTE_LIMIT = 200

const props = defineProps<{
  tabId?: string
  connId?: string
  schema?: string
  dbType?: string
  tableName?: string
}>()

const emit = defineEmits<{
  (e: 'openDataBrowser', payload: { connId: string; schema: string; tableName: string; dbType: string }): void
}>()

const dbSchemaProxy = useDbSchemaStore()
const { currentTheme } = useTheme()

// 注册 ER 表节点（HTML 节点：唯一几何来源在 er/render/nodeHtml.ts）
Shape.HTML.register({
  shape: 'er-table',
  width: NODE_WIDTH,
  height: 60,
  effect: ['data'],
  html: createErTableHtml,
})

// 注册 ER 正交路由：内置 orth 会沿目标节点端口列折返，改用节点 bbox 计算 Z 形折线
Graph.registerRouter('er-orth', erOrthRouter)

const loading = ref(false)
const aiAnalyzing = ref(false)
const containerRef = useTemplateRef('containerRef')
const layoutType = ref<'TB' | 'LR'>('TB')
const showAllTables = ref(true)
const showPanel = ref(true)
const focusNeighbors = ref(false)
const unsupportedDb = ref(false)
const loadError = ref('')
const resolvedDbType = ref('')
const themeVersion = ref(0)
const themeColors = ref<ERTheme>(getERTheme(currentTheme.value === 'dark'))

// 搜索状态
const searchQuery = ref('')
const currentMatchIndex = ref(0)
const hoveredNodeId = ref<string | null>(null)

// 折叠状态（组件存活期间持久化）
const collapsedNodes = ref(new Set<string>())
// 手动拖动后的节点位置（在显式重新布局前保持）
const manualPositions = ref(new Map<string, { x: number; y: number }>())
// 图例过滤：隐藏的类别/来源 key
const hiddenLegend = ref(new Set<string>())
const selectedRelationKey = ref('')

const edgeTooltip = ref({
  visible: false,
  x: 0,
  y: 0,
  relationType: '',
  source: '',
  target: '',
  sourceCol: '',
  targetCol: '',
  constraintName: '',
  origin: '',
  confidence: '',
  reason: '',
})

let graph: any = null
let edgeMap = new Map<string, any>()
let labelsHidden = false
let maxZIndex = 1
let mousedownHandler: ((e: MouseEvent) => void) | null = null
let lastHeaderClick = { tableId: null as string | null, time: 0 }
let resizeObserver: ResizeObserver | null = null

const allNodes = ref<ERNodeData[]>([])
const allEdges = ref<ERRelation[]>([])

const nodeMap = computed(() => new Map(allNodes.value.map(node => [node.id, node])))

function edgeCategoryKey(relation: ERRelation): string {
  if (relation.relationType === '1:1') return 'c-11'
  if (relation.relationType === 'N:M') return 'c-nm'
  return 'c-1n'
}

function edgeOriginKey(relation: ERRelation): string {
  return `o-${relation.origin || 'fk'}`
}

function isEdgeHidden(relation: ERRelation): boolean {
  return hiddenLegend.value.has(edgeCategoryKey(relation)) || hiddenLegend.value.has(edgeOriginKey(relation))
}

const filteredNodes = computed<ERNodeData[]>(() => {
  let nodes = allNodes.value
  if (!showAllTables.value) {
    const connected = new Set<string>()
    allEdges.value.forEach(edge => {
      connected.add(edge.source)
      connected.add(edge.target)
    })
    nodes = nodes.filter(node => connected.has(node.id))
  }
  return [...nodes].sort((a, b) => a.label.localeCompare(b.label))
})

const filteredEdges = computed<ERRelation[]>(() => {
  const nodeIds = new Set(filteredNodes.value.map(node => node.id))
  return allEdges.value.filter(edge => nodeIds.has(edge.source) && nodeIds.has(edge.target))
})

const displayedEdges = computed<ERRelation[]>(() => filteredEdges.value.filter(edge => !isEdgeHidden(edge)))
const hiddenEdgeCount = computed(() => filteredEdges.value.length - displayedEdges.value.length)

const matchedNodeIds = computed<string[]>(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return []
  return filteredNodes.value
    .filter(node =>
      node.label.toLowerCase().includes(q) ||
      (node.comment || '').toLowerCase().includes(q) ||
      (node.columns || []).some(col => col.name.toLowerCase().includes(q) || (col.comment || '').toLowerCase().includes(q))
    )
    .map(node => node.id)
})

const matchedNodeIdSet = computed(() => new Set(matchedNodeIds.value))

const legendItems = computed<LegendItem[]>(() => {
  const theme = themeColors.value
  const edges = filteredEdges.value
  const count = (predicate: (edge: ERRelation) => boolean) => edges.filter(predicate).length
  return [
    { key: 'c-11', label: '1:1', color: theme.rel11, count: count(e => e.relationType === '1:1'), active: !hiddenLegend.value.has('c-11') },
    { key: 'c-1n', label: 'N:1（子:父）', color: theme.rel1N, count: count(e => e.relationType !== '1:1' && e.relationType !== 'N:M'), active: !hiddenLegend.value.has('c-1n') },
    { key: 'c-nm', label: 'N:M', color: theme.relNM, count: count(e => e.relationType === 'N:M'), active: !hiddenLegend.value.has('c-nm') },
    { key: 'o-fk', label: '物理外键', color: theme.edgeFk, count: count(e => (e.origin || 'fk') === 'fk'), active: !hiddenLegend.value.has('o-fk') },
    { key: 'o-ai', label: 'AI 推断', color: theme.edgeAi, count: count(e => e.origin === 'ai'), active: !hiddenLegend.value.has('o-ai') },
    { key: 'o-manual', label: '手动', color: theme.edgeManual, count: count(e => e.origin === 'manual'), active: !hiddenLegend.value.has('o-manual') },
  ]
})

const relationRows = computed<RelationRow[]>(() => {
  return [...filteredEdges.value]
    .sort((a, b) => `${a.source}.${a.sourceCol}->${a.target}`.localeCompare(`${b.source}.${b.sourceCol}->${b.target}`))
    .map(relation => ({
      key: relation.key || relationKey(relation),
      relationType: displayRelationType(relation.relationType),
      typeKey: relation.relationType === '1:1' ? '11' : relation.relationType === 'N:M' ? 'nm' : '1n',
      origin: relation.origin || 'fk',
      source: relation.source,
      target: relation.target,
      sourceCol: relation.sourceCol,
      targetCol: relation.targetCol,
      confidence: relation.confidence,
      constraintName: relation.constraintName || relation.constraintId || '',
    }))
})

const emptyState = computed<{ message: string; action: 'showAll' | 'refresh' } | null>(() => {
  if (loading.value) return null
  if (unsupportedDb.value) {
    const type = resolvedDbType.value || props.dbType || ''
    return { message: `暂不支持该数据库类型${type ? `（${type}）` : ''}，目前支持 MySQL / MariaDB / Oracle / SQLite`, action: 'refresh' }
  }
  if (loadError.value) return { message: `加载失败：${loadError.value}`, action: 'refresh' }
  if (allNodes.value.length === 0) return { message: '该数据库没有表', action: 'refresh' }
  if (filteredNodes.value.length === 0) return { message: '当前无关联表，可开启“显示所有表”或使用 AI 分析关系', action: 'showAll' }
  return null
})

// ==================== 工具函数 ====================

function escapeSelector(value: string): string {
  const css = (globalThis as { CSS?: { escape?: (v: string) => string } }).CSS
  if (css?.escape) return css.escape(value)
  return value.replace(/(["\\])/g, '\\$1')
}

function nodeElement(nodeId: string): HTMLElement | null {
  return containerRef.value?.querySelector(`[data-node-id="${escapeSelector(nodeId)}"]`) as HTMLElement | null
}

function rowElement(nodeId: string, index: number): HTMLElement | null {
  return containerRef.value?.querySelector(
    `[data-node-id="${escapeSelector(nodeId)}"] [data-row-index="${index}"]`
  ) as HTMLElement | null
}

function findRelationByKey(key: string): ERRelation | undefined {
  return allEdges.value.find(relation => relation.key === key)
}

function uniqueEdgeKey(relation: ERRelation, existing: ERRelation[]): string {
  const base = `${relationKey(relation)}|${relation.origin || 'fk'}|${relation.constraintName || relation.constraintId || ''}`
  let key = base
  let index = 1
  while (existing.some(item => item.key === key)) {
    key = `${base}#${index++}`
  }
  return key
}

function assignEdgeKeys(relations: ERRelation[]): void {
  const groups = new Map<string, number>()
  relations.forEach(relation => {
    const base = `${relationKey(relation)}|${relation.origin || 'fk'}|${relation.constraintName || relation.constraintId || ''}`
    const count = groups.get(base) || 0
    groups.set(base, count + 1)
    relation.key = count === 0 ? base : `${base}#${count}`
  })
}

function neighborNodeIds(nodeId: string): Set<string> {
  const neighbors = new Set<string>([nodeId])
  filteredEdges.value.forEach(edge => {
    if (edge.source === nodeId) neighbors.add(edge.target)
    if (edge.target === nodeId) neighbors.add(edge.source)
  })
  return neighbors
}

function getTimestamp(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`
}

function download(uri: string, filename: string): void {
  const link = document.createElement('a')
  link.download = filename
  link.href = uri
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

// ==================== 数据加载 ====================

async function loadData(preserve = false): Promise<void> {
  loading.value = true
  unsupportedDb.value = false
  loadError.value = ''
  const previousEdges = preserve ? allEdges.value.slice() : []
  const previousNodes = preserve ? allNodes.value.slice() : []
  const previousPositions = preserve ? new Map(manualPositions.value) : new Map<string, { x: number; y: number }>()
  allNodes.value = []
  allEdges.value = []
  selectedRelationKey.value = ''
  try {
    const loaded = await loadERSchema({ connId: props.connId || '', schema: props.schema || '' })
    resolvedDbType.value = loaded.dbType || props.dbType || ''
    allNodes.value = loaded.tables.map(table => ({
      id: table.name,
      label: table.name,
      name: table.name,
      type: table.type,
      comment: table.comment,
      columns: table.columns,
      uniqueKeys: table.uniqueKeys,
    }))
    allEdges.value = loaded.relations.map(relation => ({ ...relation, origin: 'fk' as const }))
    inferRelationTypes(allNodes.value as unknown as ERTable[], allEdges.value)
    if (preserve) restorePreservedRelations(previousEdges)
    assignEdgeKeys(allEdges.value)
    if (preserve) {
      const existing = new Set(allNodes.value.map(node => node.id))
      const kept = new Map<string, { x: number; y: number }>()
      previousPositions.forEach((position, id) => {
        if (existing.has(id)) kept.set(id, position)
      })
      manualPositions.value = kept
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    if (/暂不支持该数据库类型/.test(message)) {
      unsupportedDb.value = true
    } else {
      if (preserve) {
        // 刷新失败时保留原图，避免用户丢失当前布局与关系；错误由 toast 提示
        allNodes.value = previousNodes
        allEdges.value = previousEdges
      } else {
        loadError.value = message
      }
      handleError(err, '加载ER图数据')
    }
  } finally {
    loading.value = false
    await nextTick()
    await rebuildGraph()
  }
}

/** 刷新时恢复手动/AI 关系：校验端点与字段，物理外键去重、手动关系保留 */
function restorePreservedRelations(previousEdges: ERRelation[]): void {
  const tableMap = new Map(allNodes.value.map(node => [node.id, node]))
  const keys = new Set<string>()
  allEdges.value.forEach(relation => {
    keys.add(relationKey(relation))
    keys.add(reverseRelationKey(relation))
  })

  let restored = 0
  for (const relation of previousEdges) {
    if (relation.origin !== 'manual' && relation.origin !== 'ai') continue
    const source = tableMap.get(relation.source)
    const target = tableMap.get(relation.target)
    if (!source || !target || source.id === target.id) continue
    if (relation.sourceCol && source.columns && !source.columns.some(col => col.name === relation.sourceCol)) continue
    if (relation.targetCol && target.columns && !target.columns.some(col => col.name === relation.targetCol)) continue

    const forward = relationKey(relation)
    const reverse = reverseRelationKey(relation)
    if (relation.origin === 'ai' && (keys.has(forward) || keys.has(reverse))) continue
    if (relation.origin === 'manual' && keys.has(forward)) continue

    allEdges.value.push({ ...relation, key: undefined })
    keys.add(forward)
    keys.add(reverse)
    restored++
  }
  if (restored > 0) {
    ElMessage.info(`已保留 ${restored} 条手动/AI 关系`)
  }
}

// ==================== 图实例 ====================

function destroyGraph(): void {
  if (mousedownHandler && containerRef.value) {
    containerRef.value.removeEventListener('mousedown', mousedownHandler, true)
    mousedownHandler = null
  }
  if (graph) {
    graph.dispose()
    graph = null
  }
  edgeMap = new Map()
  labelsHidden = false
  hoveredNodeId.value = null
  selectedRelationKey.value = ''
  edgeTooltip.value.visible = false
}

function edgeLabelConfig(relation: ERRelation, theme: ERTheme) {
  return {
    attrs: {
      text: { text: displayRelationType(relation.relationType), fontSize: 12, fill: theme.labelText, fontWeight: 600 },
      rect: {
        fill: relationColor(theme, relation.relationType),
        stroke: '#ffffff',
        'stroke-width': 1,
        rx: 4,
        ry: 4,
      },
    },
    position: { distance: 0.5 },
  }
}

function edgeDash(relation: ERRelation): string | null {
  const sourceCollapsed = collapsedNodes.value.has(relation.source)
  const targetCollapsed = collapsedNodes.value.has(relation.target)
  if (sourceCollapsed || targetCollapsed) return '4 3'
  if (relation.origin === 'ai') return '6 4'
  return null
}

function applyEdgeStyle(edge: any, relation: ERRelation): void {
  const theme = themeColors.value
  const selected = relation.key === selectedRelationKey.value
  edge.setVisible(!isEdgeHidden(relation))
  const color = selected ? theme.edgeSelected : originColor(theme, relation.origin)
  const lowConfidence = relation.origin === 'ai' && relation.confidence === 'low'
  edge.attr({
    wrap: { strokeWidth: theme.edgeWrapWidth },
    line: {
      stroke: color,
      strokeWidth: selected ? theme.edgeHoverWidth : theme.edgeWidth,
      opacity: selected ? 1 : lowConfidence ? theme.edgeAiLowOpacity : 1,
      strokeDasharray: edgeDash(relation),
      targetMarker: { name: 'classic', width: 8, height: 6, fill: color, stroke: color },
      sourceMarker: { name: 'circle', r: 3, fill: color, stroke: color },
    },
  })
  if (labelsHidden) {
    edge.setLabels([])
  } else {
    edge.setLabels([edgeLabelConfig(relation, theme)])
  }
}

function applyEdgeVisibility(): void {
  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (!relation) return
    edge.setVisible(!isEdgeHidden(relation))
  })
}

function clearEndpointHighlight(): void {
  containerRef.value?.querySelectorAll('.er-col-row.is-edge-active').forEach(el => el.classList.remove('is-edge-active'))
}

function highlightEdgeEndpoints(relation: ERRelation): void {
  clearEndpointHighlight()
  const sourceIndex = columnIndexFor(nodeMap.value.get(relation.source), relation.sourceCol)
  const targetIndex = columnIndexFor(nodeMap.value.get(relation.target), relation.targetCol)
  if (sourceIndex >= 0) rowElement(relation.source, sourceIndex)?.classList.add('is-edge-active')
  if (targetIndex >= 0) rowElement(relation.target, targetIndex)?.classList.add('is-edge-active')
}

function refreshVisualState(): void {
  if (!graph || !containerRef.value) return
  const theme = themeColors.value
  const q = searchQuery.value.trim().toLowerCase()
  const searchActive = q.length > 0 && matchedNodeIdSet.value.size > 0
  const focusActive = focusNeighbors.value && hoveredNodeId.value !== null
  const focusSet = focusActive && hoveredNodeId.value ? neighborNodeIds(hoveredNodeId.value) : null

  filteredNodes.value.forEach(node => {
    const el = nodeElement(node.id)
    if (!el) return
    let opacity = '1'
    let borderColor = theme.nodeBorder
    let borderWidth = '2px'
    if (searchActive) {
      if (matchedNodeIdSet.value.has(node.id)) {
        borderColor = theme.edgeHover
        borderWidth = '3px'
      } else {
        opacity = '0.25'
      }
    }
    if (focusSet && !focusSet.has(node.id)) {
      opacity = '0.25'
    } else if (focusActive && !searchActive) {
      borderColor = hoveredNodeId.value === node.id ? theme.edgeHover : theme.nodeBorder
    } else if (!searchActive && hoveredNodeId.value === node.id) {
      borderColor = theme.edgeHover
    }
    el.style.opacity = opacity
    el.style.borderColor = borderColor
    el.style.borderWidth = borderWidth
  })

  containerRef.value.querySelectorAll('.er-col-row.is-active').forEach(el => el.classList.remove('is-active'))
  if (searchActive) {
    filteredNodes.value.forEach(node => {
      if (!matchedNodeIdSet.value.has(node.id)) return
      ;(node.columns || []).forEach((col, index) => {
        if (col.name.toLowerCase().includes(q) || (col.comment || '').toLowerCase().includes(q)) {
          rowElement(node.id, index)?.classList.add('is-active')
        }
      })
    })
  }

  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (!relation) return
    const baseOpacity = relation.origin === 'ai' && relation.confidence === 'low' ? theme.edgeAiLowOpacity : 1
    let dim = false
    if (searchActive) {
      dim = !(matchedNodeIdSet.value.has(relation.source) || matchedNodeIdSet.value.has(relation.target))
    }
    if (focusSet) {
      dim = dim || !(focusSet.has(relation.source) && focusSet.has(relation.target))
    }
    edge.attr('line/opacity', baseOpacity * (dim ? 0.15 : 1))
  })

  if (selectedRelationKey.value) {
    const selected = findRelationByKey(selectedRelationKey.value)
    if (selected) highlightEdgeEndpoints(selected)
  }
}

function applyLabelVisibility(scale: number): void {
  const hide = scale < 0.7
  if (hide === labelsHidden) return
  labelsHidden = hide
  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (relation) applyEdgeStyle(edge, relation)
  })
  refreshVisualState()
}

async function rebuildGraph(): Promise<void> {
  destroyGraph()
  if (!containerRef.value) return

  const nodes = filteredNodes.value
  const layoutEdges = filteredEdges.value
  const renderEdges = filteredEdges.value
  themeColors.value = getERTheme(currentTheme.value === 'dark')
  containerRef.value.style.setProperty('--er-row-active-bg', themeColors.value.rowActiveBg)
  if (nodes.length === 0) return

  const colors = themeColors.value
  const useAsync = nodes.length > VIRTUAL_THRESHOLD
  const useOrth = layoutEdges.length <= ORTH_ROUTE_LIMIT

  graph = new Graph({
    container: containerRef.value,
    width: containerRef.value.clientWidth,
    height: containerRef.value.clientHeight,
    background: { color: 'transparent' },
    grid: { visible: true, size: 10, type: 'dot', args: { color: colors.gridColor } },
    panning: { enabled: true },
    mousewheel: {
      enabled: true,
      factor: 1.1,
      minScale: 0.3,
      maxScale: 3,
      zoomAtMousePosition: true,
    },
    connecting: {
      snap: true,
      allowLoop: false,
      allowNode: false,
      allowEdge: false,
      allowPort: true,
      allowBlank: false,
      allowMulti: true,
      highlight: true,
      connectionPoint: 'anchor',
      anchor: 'center',
      createEdge() {
        return new Shape.Edge({
          zIndex: 0,
          ...(useOrth
            ? { router: { name: 'er-orth' }, connector: { name: 'rounded', args: { radius: 12 } } }
            : { connector: { name: 'smooth' } }),
          attrs: {
            wrap: { strokeWidth: colors.edgeWrapWidth },
            line: {
              stroke: colors.edgeManual,
              strokeWidth: colors.edgeWidth,
              targetMarker: { name: 'classic', width: 8, height: 6, fill: colors.edgeManual, stroke: colors.edgeManual },
              sourceMarker: { name: 'circle', r: 3, fill: colors.edgeManual, stroke: colors.edgeManual },
            },
          },
          labels: [edgeLabelConfig({ source: '', target: '', sourceCol: '', targetCol: '', constraintName: '', relationType: '1:N', origin: 'manual' }, colors)],
        })
      },
      validateConnection({ sourceMagnet, targetMagnet }: any) {
        return !!(sourceMagnet && targetMagnet)
      },
    },
    interacting: { nodeMovable: true },
    async: useAsync,
    virtual: useAsync,
  })

  graph.use(new Export())
  graph.use(new Keyboard())
  graph.use(new Selection({ enabled: true, multiple: false, rubberband: false, filter: ['edge'] }))

  graph.on('scale', ({ sx }: { sx: number }) => applyLabelVisibility(sx))

  registerGraphEvents()
  registerContainerEvents()

  maxZIndex = 1
  const layoutPositions = await computeLayout({
    nodes,
    edges: layoutEdges,
    rankdir: layoutType.value,
    collapsed: collapsedNodes.value,
  })

  nodes.forEach(node => {
    const collapsed = collapsedNodes.value.has(node.id)
    const nodeData: ERNodeData = {
      ...node,
      collapsed,
      junction: !!node.junction,
      themeVersion: themeVersion.value,
    }
    const nodeConfig: any = {
      id: node.id,
      shape: 'er-table',
      width: NODE_WIDTH,
      height: getNodeHeightByState(nodeData, collapsed),
      data: nodeData,
      zIndex: 1,
    }
    const manual = manualPositions.value.get(node.id)
    if (manual) {
      nodeConfig.x = manual.x
      nodeConfig.y = manual.y
    } else if (layoutPositions.has(node.id)) {
      const position = layoutPositions.get(node.id)!
      nodeConfig.x = position.x
      nodeConfig.y = position.y
    }
    if (!collapsed && (node.columns || []).length > 0) {
      nodeConfig.ports = buildNodePorts(nodeData, colors)
    }
    graph.addNode(nodeConfig)
  })

  renderEdges.forEach(relation => addGraphEdge(relation))
  applyEdgeVisibility()

  if (manualPositions.value.size < nodes.length) {
    graph.positionContent('center', { padding: 40 })
  }

  setupResizeObserver()

  if (useAsync) {
    graph.on('render:done', () => refreshVisualState())
    await nextTick()
  } else {
    await nextTick()
    refreshVisualState()
  }
}

function addGraphEdge(relation: ERRelation): void {
  if (!graph || !relation.key) return
  const sourceNode = nodeMap.value.get(relation.source)
  const targetNode = nodeMap.value.get(relation.target)
  if (!sourceNode || !targetNode) return
  const sourceCell = graph.getCellById(relation.source)
  const targetCell = graph.getCellById(relation.target)
  if (!sourceCell || !targetCell) return

  const sourcePosition = sourceCell.getPosition()
  const targetPosition = targetCell.getPosition()
  const sourceIndex = columnIndexFor(sourceNode, relation.sourceCol)
  const targetIndex = columnIndexFor(targetNode, relation.targetCol)
  const sides = pickPortPair(sourcePosition, targetPosition, sourceIndex, targetIndex)
  const collapsed = collapsedNodes.value
  const useOrth = filteredEdges.value.length <= ORTH_ROUTE_LIMIT

  const config: any = {
    source: { cell: relation.source },
    target: { cell: relation.target },
    zIndex: 0,
    ...(useOrth
      ? { router: { name: 'er-orth' }, connector: { name: 'rounded', args: { radius: 12 } } }
      : { connector: { name: 'smooth' } }),
    attrs: {},
    data: { ...relation },
  }
  if (
    sides.sourcePort &&
    !collapsed.has(relation.source) &&
    sourceCell.hasPort(sides.sourcePort) &&
    sourceIndex >= 0
  ) {
    config.source.port = sides.sourcePort
  }
  if (
    sides.targetPort &&
    !collapsed.has(relation.target) &&
    targetCell.hasPort(sides.targetPort) &&
    targetIndex >= 0
  ) {
    config.target.port = sides.targetPort
  }

  const edge = graph.addEdge(config)
  edgeMap.set(relation.key, edge)
  applyEdgeStyle(edge, relation)
}

function registerGraphEvents(): void {
  if (!graph) return

  graph.on('node:click', ({ node }: { node: any }) => {
    maxZIndex++
    node.setZIndex(maxZIndex)
  })

  graph.on('node:moved', ({ node }: { node: any }) => {
    const position = node.getPosition()
    manualPositions.value.set(node.id, { x: position.x, y: position.y })
  })

  graph.on('node:mouseenter', ({ node }: { node: any }) => {
    hoveredNodeId.value = node.id
    refreshVisualState()
  })

  graph.on('node:mouseleave', ({ node }: { node: any }) => {
    if (hoveredNodeId.value === node.id) hoveredNodeId.value = null
    refreshVisualState()
  })

  graph.on('edge:mouseenter', ({ edge, e }: { edge: any; e: MouseEvent }) => {
    const relation = findRelationByKey(edge.getData()?.key)
    if (!relation) return
    const theme = themeColors.value
    edge.setZIndex(3)
    edge.attr('line/stroke', theme.edgeHover)
    edge.attr('line/strokeWidth', theme.edgeHoverWidth)
    highlightEdgeEndpoints(relation)

    const rect = containerRef.value?.getBoundingClientRect()
    if (rect) {
      const maxX = Math.max(8, rect.width - 340)
      const maxY = Math.max(8, rect.height - 120)
      edgeTooltip.value = {
        visible: true,
        x: Math.min(Math.max(0, e.clientX - rect.left + 12), maxX),
        y: Math.min(Math.max(0, e.clientY - rect.top + 12), maxY),
        relationType: displayRelationType(relation.relationType),
        source: relation.source,
        target: relation.target,
        sourceCol: relation.sourceCol || '',
        targetCol: relation.targetCol || '',
        constraintName: relation.constraintName || relation.constraintId || '',
        origin: relation.origin || 'fk',
        confidence: relation.confidence || '',
        reason: relation.reason || '',
      }
    }
  })

  graph.on('edge:mouseleave', ({ edge }: { edge: any }) => {
    edgeTooltip.value.visible = false
    const relation = findRelationByKey(edge.getData()?.key)
    if (relation) {
      applyEdgeStyle(edge, relation)
      if (relation.key !== selectedRelationKey.value) clearEndpointHighlight()
    }
    edge.setZIndex(relation && relation.key === selectedRelationKey.value ? 2 : 0)
    refreshVisualState()
  })

  graph.on('edge:click', ({ edge }: { edge: any }) => {
    const key = edge.getData()?.key
    if (key) setSelectedRelation(key, false)
  })

  graph.on('edge:dblclick', ({ edge }: { edge: any }) => {
    const key = edge.getData()?.key
    if (key) cycleRelation(key)
  })

  graph.on('blank:click', () => {
    clearSelection()
  })

  graph.on('edge:connected', ({ edge, isNew, currentCell, currentPort }: any) => {
    if (!isNew) return
    const sourceCell = edge.getSourceCell()
    const sourcePortId = edge.getSourcePortId()
    if (!sourceCell || !currentCell || !sourcePortId || !currentPort) {
      edge.remove()
      return
    }
    const sourceNode = nodeMap.value.get(sourceCell.id)
    const targetNode = nodeMap.value.get(currentCell.id)
    if (!sourceNode || !targetNode) {
      edge.remove()
      return
    }
    const relation: ERRelation = {
      source: sourceCell.id,
      sourceCol: colNameByPortId(sourceNode, sourcePortId),
      target: currentCell.id,
      targetCol: colNameByPortId(targetNode, currentPort),
      constraintName: '',
      relationType: '1:N',
      origin: 'manual',
    }
    relation.key = uniqueEdgeKey(relation, allEdges.value)
    edge.setData({ ...relation })
    allEdges.value.push(relation)
    edgeMap.set(relation.key, edge)
    applyEdgeStyle(edge, relation)
    setSelectedRelation(relation.key, true)
    refreshVisualState()
  })

  graph.bindKey(['del', 'backspace'], () => {
    if (!graph) return false
    const selectedEdges = graph.getSelectedCells().filter((cell: any) => cell.isEdge())
    if (selectedEdges.length === 0) return false
    selectedEdges.forEach((edge: any) => {
      const key = edge.getData()?.key
      if (key) deleteRelation(key)
    })
    return false
  })
}

function setupResizeObserver(): void {
  if (!containerRef.value) return
  if (!resizeObserver) {
    resizeObserver = new ResizeObserver(() => {
      if (!graph || !containerRef.value) return
      graph.resize(containerRef.value.clientWidth, containerRef.value.clientHeight)
    })
  }
  resizeObserver.disconnect()
  resizeObserver.observe(containerRef.value)
}

/** 容器级 mousedown：折叠按钮、表头双击浏览数据、拖节点时暂停画布平移、端口拖线状态 */
function registerContainerEvents(): void {
  if (!containerRef.value) return
  mousedownHandler = (event: MouseEvent) => {
    const target = event.target as HTMLElement
    if (target.closest('.x6-port')) {
      containerRef.value?.classList.add('connecting')
      const onPortUp = () => {
        containerRef.value?.classList.remove('connecting')
        document.removeEventListener('mouseup', onPortUp)
      }
      document.addEventListener('mouseup', onPortUp)
    }
    const toggle = target.closest('.er-collapse-toggle') as HTMLElement | null
    if (toggle) {
      event.stopPropagation()
      event.preventDefault()
      const nodeEl = toggle.closest('[data-node-id]') as HTMLElement | null
      const nodeId = nodeEl?.getAttribute('data-node-id')
      if (nodeId) toggleCollapse(nodeId)
      return
    }
    const header = target.closest('.er-table-header') as HTMLElement | null
    if (header) {
      const nodeEl = header.closest('[data-node-id]') as HTMLElement | null
      const nodeId = nodeEl?.getAttribute('data-node-id')
      if (nodeId && graph) {
        maxZIndex++
        graph.getCellById(nodeId)?.setZIndex(maxZIndex)
      }
      const tableId = header.getAttribute('data-table-id')
      const now = Date.now()
      if (tableId && lastHeaderClick.tableId === tableId && now - lastHeaderClick.time < 400) {
        lastHeaderClick = { tableId: null, time: 0 }
        emit('openDataBrowser', {
          connId: props.connId || '',
          schema: props.schema || '',
          tableName: tableId,
          dbType: resolvedDbType.value || props.dbType || '',
        })
        return
      }
      lastHeaderClick = { tableId, time: now }
    } else {
      lastHeaderClick = { tableId: null, time: 0 }
      if (target.closest('[data-node-id]') && graph) {
        graph.disablePanning()
        const onUp = () => {
          graph?.enablePanning()
          document.removeEventListener('mouseup', onUp)
        }
        document.addEventListener('mouseup', onUp)
      }
    }
  }
  containerRef.value.addEventListener('mousedown', mousedownHandler, true)
}

// ==================== 选中/关系编辑 ====================

function setSelectedRelation(key: string, center = false): void {
  selectedRelationKey.value = key
  edgeMap.forEach((edge, edgeKey) => {
    const relation = findRelationByKey(edgeKey)
    if (relation) applyEdgeStyle(edge, relation)
    edge.setZIndex(edgeKey === key ? 2 : 0)
  })
  const edge = edgeMap.get(key)
  const relation = findRelationByKey(key)
  if (edge && graph) {
    graph.select(edge)
    if (center) graph.centerCell(edge)
  }
  clearEndpointHighlight()
  if (relation) highlightEdgeEndpoints(relation)
  refreshVisualState()
}

function clearSelection(): void {
  if (!selectedRelationKey.value) return
  selectedRelationKey.value = ''
  graph?.cleanSelection()
  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (relation) applyEdgeStyle(edge, relation)
    edge.setZIndex(0)
  })
  clearEndpointHighlight()
  refreshVisualState()
}

function selectRelationFromPanel(key: string): void {
  const relation = findRelationByKey(key)
  if (!relation) return
  // 面板是主要入口：若该关系被图例过滤，先自动恢复显示
  if (isEdgeHidden(relation)) {
    const next = new Set(hiddenLegend.value)
    next.delete(edgeCategoryKey(relation))
    next.delete(edgeOriginKey(relation))
    hiddenLegend.value = next
    applyEdgeVisibility()
  }
  setSelectedRelation(key, true)
}

function deleteRelation(key: string): void {
  const index = allEdges.value.findIndex(relation => relation.key === key)
  if (index < 0) return
  allEdges.value.splice(index, 1)
  const edge = edgeMap.get(key)
  if (edge) edge.remove()
  edgeMap.delete(key)
  if (selectedRelationKey.value === key) {
    selectedRelationKey.value = ''
    graph?.cleanSelection()
    clearEndpointHighlight()
  }
  refreshVisualState()
}

function cycleRelation(key: string): void {
  const relation = findRelationByKey(key)
  if (!relation) return
  const current = relation.relationType || '1:N'
  const next = current === '1:N' ? '1:1' : current === '1:1' ? 'N:M' : '1:N'
  relation.relationType = next
  const edge = edgeMap.get(key)
  if (edge) applyEdgeStyle(edge, relation)
  refreshVisualState()
}

// ==================== 折叠/展开 ====================

function rebuildPorts(node: any, data: ERNodeData): void {
  node.removePorts()
  if (data.collapsed) return
  node.setProp('ports/groups', buildPortGroups(themeColors.value))
  const ports = buildNodePorts(data, themeColors.value)
  node.addPorts(ports.items)
}

function updateEdgesDash(nodeId: string): void {
  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (!relation) return
    if (relation.source === nodeId || relation.target === nodeId) applyEdgeStyle(edge, relation)
  })
}

function toggleCollapse(nodeId: string): void {
  if (collapsedNodes.value.has(nodeId)) collapsedNodes.value.delete(nodeId)
  else collapsedNodes.value.add(nodeId)

  const node = graph?.getCellById(nodeId)
  if (!node) return
  const collapsed = collapsedNodes.value.has(nodeId)
  const data = node.getData() || {}
  node.setData({ ...data, collapsed, themeVersion: ++themeVersion.value })
  const sourceData = nodeMap.value.get(nodeId)
  if (!sourceData) return
  node.resize(NODE_WIDTH, getNodeHeightByState(sourceData, collapsed))
  rebuildPorts(node, { ...sourceData, collapsed })
  updateEdgesDash(nodeId)
  nextTick(() => refreshVisualState())
}

function onShowAllTablesChange(): void {
  rebuildGraph()
}

function onLayoutChange(): void {
  manualPositions.value.clear()
  ElMessage.info('已重置手动布局')
  rebuildGraph()
}

function enableShowAll(): void {
  showAllTables.value = true
  rebuildGraph()
}

// ==================== 搜索 ====================

function applySearchHighlight(): void {
  refreshVisualState()
}

function focusNextMatch(): void {
  const matched = matchedNodeIds.value
  if (matched.length === 0) return
  if (currentMatchIndex.value >= matched.length) currentMatchIndex.value = 0
  const nodeId = matched[currentMatchIndex.value]
  const node = graph?.getCellById(nodeId)
  if (node) {
    graph.centerCell(node)
    maxZIndex++
    node.setZIndex(maxZIndex)
  }
  currentMatchIndex.value = (currentMatchIndex.value + 1) % matched.length
}

function onSearchClear(): void {
  currentMatchIndex.value = 0
  applySearchHighlight()
}

watch(searchQuery, () => {
  currentMatchIndex.value = 0
  nextTick(() => applySearchHighlight())
})

watch(focusNeighbors, () => {
  refreshVisualState()
})

// ==================== 图例 ====================

function toggleLegend(key: string): void {
  const next = new Set(hiddenLegend.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  hiddenLegend.value = next
  applyEdgeVisibility()
  if (selectedRelationKey.value) {
    const relation = findRelationByKey(selectedRelationKey.value)
    if (relation && isEdgeHidden(relation)) clearSelection()
  }
  refreshVisualState()
}

// ==================== 刷新 ====================

async function refreshSchema(): Promise<void> {
  await loadData(true)
}

// ==================== 主题 ====================

async function applyTheme(): Promise<void> {
  themeColors.value = getERTheme(currentTheme.value === 'dark')
  if (!graph) return
  const theme = themeColors.value
  containerRef.value?.style.setProperty('--er-row-active-bg', theme.rowActiveBg)
  const version = ++themeVersion.value
  allNodes.value.forEach(node => {
    const cell = graph?.getCellById(node.id)
    if (!cell) return
    cell.setData({ ...cell.getData(), themeVersion: version })
    ;(cell.getPorts?.() || []).forEach((port: { id: string }) => {
      cell.setPortProp(port.id, 'attrs/circle/stroke', theme.portStroke)
      cell.setPortProp(port.id, 'attrs/circle/fill', theme.portFill)
    })
  })
  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (relation) applyEdgeStyle(edge, relation)
  })
  graph.drawGrid({ type: 'dot', args: { color: theme.gridColor } })
  await nextTick()
  refreshVisualState()
}

watch(currentTheme, () => {
  applyTheme()
})

// ==================== 导出 ====================

function addExportStamp(svg: SVGSVGElement): SVGSVGElement {
  if (!graph) return svg
  const bbox = graph.getContentBBox()
  const ns = 'http://www.w3.org/2000/svg'
  const background = document.createElementNS(ns, 'rect')
  const label = document.createElementNS(ns, 'text')
  const stamp = `${props.schema || ''} · ${filteredNodes.value.length} 表 · ${displayedEdges.value.length} 关系 · ${getTimestamp()}`
  const width = Math.min(420, Math.max(220, stamp.length * 7 + 16))
  background.setAttribute('x', String(bbox.x + 4))
  background.setAttribute('y', String(bbox.y + 4))
  background.setAttribute('width', String(width))
  background.setAttribute('height', '20')
  background.setAttribute('rx', '4')
  background.setAttribute('fill', '#ffffff')
  background.setAttribute('opacity', '0.92')
  label.setAttribute('x', String(bbox.x + 10))
  label.setAttribute('y', String(bbox.y + 19))
  label.setAttribute('fill', '#475569')
  label.setAttribute('font-size', '12')
  label.setAttribute('font-family', 'sans-serif')
  label.textContent = stamp
  svg.appendChild(background)
  svg.appendChild(label)
  return svg
}

async function withPrintTheme<T>(task: () => Promise<T>): Promise<T> {
  if (!graph) return task()
  const nodes = graph.getNodes()
  const version = ++themeVersion.value
  nodes.forEach((node: any) => node.setData({ ...node.getData(), print: true, themeVersion: version }))
  edgeMap.forEach((edge, key) => {
    const relation = findRelationByKey(key)
    if (!relation) return
    edge.attr({
      wrap: { strokeWidth: 24 },
      line: {
        stroke: '#334155',
        strokeWidth: 2.5,
        opacity: 1,
        strokeDasharray: edgeDash(relation),
        targetMarker: { name: 'classic', width: 9, height: 7, fill: '#334155', stroke: '#334155' },
        sourceMarker: { name: 'circle', r: 3.5, fill: '#334155', stroke: '#334155' },
      },
    })
    edge.setLabels([edgeLabelConfig(relation, getERTheme(false))])
  })
  await new Promise(resolve => setTimeout(resolve, 150))
  try {
    return await task()
  } finally {
    nodes.forEach((node: any) => node.setData({ ...node.getData(), print: false, themeVersion: ++themeVersion.value }))
    edgeMap.forEach((edge, key) => {
      const relation = findRelationByKey(key)
      if (relation) applyEdgeStyle(edge, relation)
    })
    await nextTick()
    refreshVisualState()
  }
}

function handleExport(command: string): void {
  if (command === 'png') void exportPNG()
  else if (command === 'svg') void exportSVG()
}

async function exportPNG(): Promise<void> {
  if (!graph) return
  await withPrintTheme(
    () =>
      new Promise<void>(resolve => {
        graph!.toPNG(
          (dataUri: string) => {
            download(dataUri, `er_diagram_${props.schema || 'schema'}_${getTimestamp()}.png`)
            resolve()
          },
          {
            padding: 20,
            backgroundColor: '#ffffff',
            ratio: 2,
            beforeSerialize: addExportStamp,
          }
        )
      })
  )
}

async function exportSVG(): Promise<void> {
  if (!graph) return
  await withPrintTheme(
    () =>
      new Promise<void>(resolve => {
        graph!.toSVG(
          (svg: string) => {
            const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
            const url = URL.createObjectURL(blob)
            download(url, `er_diagram_${props.schema || 'schema'}_${getTimestamp()}.svg`)
            URL.revokeObjectURL(url)
            resolve()
          },
          {
            // 节点样式以内联为主，关闭 copyStyles 可显著降低大图导出耗时
            copyStyles: false,
            stylesheet: '.x6-edge path { vector-effect: non-scaling-stroke; }',
            beforeSerialize: addExportStamp,
          }
        )
      })
  )
}

function fitGraph(): void {
  if (graph) graph.zoomToFit({ padding: 40, maxScale: 2 })
}

// ==================== AI 分析 ====================

async function analyzeRelationsWithAI(): Promise<void> {
  if (allNodes.value.length === 0) {
    ElMessage.warning('当前没有表，无法分析关系')
    return
  }
  if (allNodes.value.length > 30) {
    try {
      await ElMessageBox.confirm(
        `当前共 ${allNodes.value.length} 张表，AI 分析将仅取前 30 张（按当前排序）。是否继续？`,
        '提示',
        { type: 'warning' }
      )
    } catch {
      return
    }
  }

  aiAnalyzing.value = true
  try {
    const sentNodes = allNodes.value.slice(0, 30)
    const tablesForAI = sentNodes.map(node => ({
      name: node.id,
      comment: node.comment || '',
      columns: (node.columns || []).map(column => ({
        name: column.name,
        type: column.type,
        comment: column.comment || '',
        primaryKey: !!column.primaryKey,
        unique: uniqueKeysOf(node).some(key => key.length === 1 && key[0] === column.name),
      })),
    }))
    // 只把物理外键传给 AI：旧 AI 边会在合并前被清除，不能当作已知关系发送（否则被删后不再返回）。
    const existingRelations = allEdges.value
      .filter(relation => (relation.origin || 'fk') === 'fk')
      .map(relation => ({
        source: relation.source,
        sourceCol: relation.sourceCol || '',
        target: relation.target,
        targetCol: relation.targetCol || '',
        relationType: relation.relationType || '1:N',
        confidence: 'high' as const,
        reason: '已存在的物理外键关系',
      }))

    const resp = await analyzeERRelations({
      connId: props.connId || '',
      schema: props.schema || '',
      dbType: resolvedDbType.value || props.dbType || dbSchemaProxy.getDbType(props.schema || '') || '',
      tables: tablesForAI,
      existingRelations,
    })
    const inferred: ERRelation[] = (resp.data?.data?.relations || []).map(relation => ({
      source: relation.source,
      sourceCol: relation.sourceCol || '',
      target: relation.target,
      targetCol: relation.targetCol || '',
      constraintName: '',
      relationType: relation.relationType,
      confidence: relation.confidence,
      reason: relation.reason || '',
    }))
    if (inferred.length === 0) {
      ElMessage.info('AI 未推断出表关系，可尝试手动编辑')
      return
    }

    const withoutAi = removeAiRelations(allEdges.value)
    const sentTables: ERTable[] = sentNodes.map(node => ({
      name: node.id,
      type: node.type || 'TABLE',
      comment: node.comment || '',
      columns: (node.columns || []).map(column => ({
        name: column.name,
        type: column.type,
        comment: column.comment || '',
        primaryKey: !!column.primaryKey,
      })),
      uniqueKeys: node.uniqueKeys,
    }))
    const { accepted, skipped } = mergeAiRelations(inferred, sentTables, withoutAi)
    allEdges.value = [...withoutAi, ...accepted]
    inferRelationTypes(allNodes.value as unknown as ERTable[], allEdges.value)
    assignEdgeKeys(allEdges.value)
    await rebuildGraph()
    ElMessage.success(`AI 推断出 ${accepted.length} 条关系${skipped > 0 ? `（跳过 ${skipped} 条重复/无效）` : ''}`)
  } catch (err) {
    handleError(err, 'AI 分析表关系')
  } finally {
    aiAnalyzing.value = false
  }
}

// ==================== 生命周期 ====================

onMounted(() => {
  loadData(false)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  resizeObserver = null
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
  padding: 8px 12px 4px;
}
.er-toolbar-spacer {
  flex: 1;
}
.er-toolbar-check {
  font-size: 12px;
}
.er-toolbar-hint {
  font-size: 12px;
  color: var(--text-tertiary);
}
.er-search-input {
  width: 220px;
}
.er-search-count {
  color: var(--db-accent, #409eff);
  font-weight: 600;
}
.er-legend-row {
  padding: 0 12px 6px;
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
}
.er-body {
  display: flex;
  flex: 1;
  min-height: 0;
  gap: 8px;
  padding: 8px 12px 0;
}
.er-canvas-wrap {
  position: relative;
  display: flex;
  flex: 1;
  min-width: 0;
}
.er-canvas {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--border-primary);
  border-radius: 6px;
  overflow: hidden;
}
.er-empty-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  pointer-events: none;
  z-index: 5;
}
.er-empty-text {
  padding: 8px 16px;
  font-size: 13px;
  color: var(--text-tertiary, #909399);
  background: var(--bg-primary, #fff);
  border: 1px solid var(--border-primary, #e4e7ed);
  border-radius: 6px;
}
.er-empty-overlay :deep(.el-button) {
  pointer-events: auto;
}
.er-footer {
  padding: 4px 12px 8px;
  font-size: 12px;
  color: var(--text-tertiary);
}
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
:deep(.el-radio-button__inner) {
  font-size: 12px;
  padding: 4px 12px;
}
:deep(.el-input__wrapper) {
  font-size: 12px;
}
/* 缩放不变细：线宽与包裹命中区在所有缩放下保持固定物理像素 */
.er-canvas :deep(.x6-edge path) {
  vector-effect: non-scaling-stroke;
}
/* 端口连线圆点：默认隐藏，悬停节点或拖拽连线时显示 */
.er-canvas :deep(.x6-port-body circle) {
  opacity: 0;
  transition: opacity 0.15s ease;
}
.er-canvas :deep(.x6-node:hover .x6-port-body circle),
.er-canvas.connecting :deep(.x6-port-body circle) {
  opacity: 1;
}
/* 搜索命中字段行 / 关系两端字段行高亮 */
.er-canvas :deep(.er-col-row.is-active) {
  background: #eaf3ff;
}
.er-canvas :deep(.er-col-row.is-edge-active) {
  background: var(--er-row-active-bg, #eaf3ff);
  box-shadow: inset 2px 0 0 var(--db-accent, #409eff);
}
</style>
