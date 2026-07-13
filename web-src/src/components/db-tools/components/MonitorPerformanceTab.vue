<template>
  <!-- 性能趋势 Tab：实时采样与 ECharts 趋势展示 -->
  <div style="min-height: 200px;" role="region" aria-label="性能趋势">
    <div class="perf-toolbar">
      <!-- 模式切换：实时 / 历史 -->
      <el-radio-group v-model="mode" size="small" @change="onModeChange">
        <el-radio-button label="realtime">实时</el-radio-button>
        <el-radio-button label="history">历史趋势</el-radio-button>
      </el-radio-group>
      <div style="flex: 1;"></div>
      <!-- 实时模式控件 -->
      <template v-if="mode === 'realtime'">
        <span class="perf-tip">采样间隔 5 秒，保留最近 {{ TREND_MAX }} 个样本</span>
        <el-button size="small" :type="paused ? 'success' : 'warning'" :aria-pressed="paused" :aria-label="paused ? '继续刷新' : '暂停刷新'" @click="togglePause">
          {{ paused ? '继续' : '暂停' }}
        </el-button>
        <el-button size="small" @click="clearTrend" aria-label="清空趋势历史">清空</el-button>
      </template>
      <!-- 历史模式控件 -->
      <template v-else>
        <el-select v-model="historyMetric" size="small" style="width: 140px;" aria-label="选择指标" @change="loadHistory">
          <el-option v-for="m in HISTORY_METRICS" :key="m.key" :label="m.label" :value="m.key" />
        </el-select>
        <el-select v-model="historyRange" size="small" style="width: 150px;" aria-label="选择时间范围" @change="loadHistory">
          <el-option label="最近 1 小时" value="1h" />
          <el-option label="最近 24 小时" value="24h" />
          <el-option label="最近 7 天" value="7d" />
          <el-option label="最近 30 天" value="30d" />
        </el-select>
        <el-button size="small" @click="loadHistory" :loading="historyLoading" aria-label="刷新历史数据">刷新</el-button>
      </template>
    </div>

    <!-- 实时模式内容 -->
    <template v-if="mode === 'realtime'">
      <el-empty v-if="trendHistory.length === 0" description="等待采样数据..." :image-size="60" />
      <template v-else>
        <!-- 主趋势图：QPS / TPS / 连接数 / 缓冲池命中率 多线合并 -->
        <EChart :option="trendChartOption" height="320px" />
        <!-- 指标统计卡片：展示当前值与最值/均值 -->
        <el-row :gutter="12" style="margin-top: 12px;">
          <el-col v-for="metric in trendMetrics" :key="metric.key" :span="6" style="margin-bottom: 12px;">
            <div class="trend-card">
              <div class="trend-header">
                <span class="trend-title">{{ metric.label }}</span>
                <span class="trend-current" :style="{ color: metric.color }">{{ metric.display(metric.latest) }}</span>
              </div>
              <div class="trend-stats">
                <span>最小 {{ metric.display(metric.min) }}</span>
                <span>最大 {{ metric.display(metric.max) }}</span>
                <span>平均 {{ metric.display(metric.avg) }}</span>
              </div>
            </div>
          </el-col>
        </el-row>
      </template>
    </template>

    <!-- 历史模式内容 -->
    <template v-else>
      <el-empty v-if="!historyLoading && historyPoints.length === 0" description="暂无历史数据" :image-size="60" />
      <EChart v-else :option="historyChartOption" height="320px" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { getMonitorMetrics, getMonitorResources, getMonitorHistory } from '@/api/conn'
import { handleError } from '@/utils/errorHandler'
import EChart from '@/components/common/EChart.vue'

// 趋势样本上限
const TREND_MAX = 30

// 趋势指标定义：颜色 + 数值格式化函数
const TREND_METRIC_DEFS = [
  { key: 'qps', label: 'QPS', color: '#409eff', display: (v: number) => Number(v).toFixed(1) },
  { key: 'tps', label: 'TPS', color: '#67c23a', display: (v: number) => Number(v).toFixed(1) },
  { key: 'connections', label: '连接数', color: '#e6a23c', display: (v: number) => String(Math.round(v)) },
  { key: 'bufferHitRate', label: '缓冲池命中率', color: '#9b59b6', display: (v: number) => Number(v).toFixed(1) + '%' },
] as const

// 历史指标定义：key（前端）→ metric（后端 metric_name）+ 显示配置
const HISTORY_METRICS = [
  { key: 'qps', metric: 'qps', label: 'QPS', color: '#409eff', display: (v: number) => Number(v).toFixed(1) },
  { key: 'tps', metric: 'tps', label: 'TPS', color: '#67c23a', display: (v: number) => Number(v).toFixed(1) },
  { key: 'connections', metric: 'connections', label: '连接数', color: '#e6a23c', display: (v: number) => String(Math.round(v)) },
  { key: 'buffer_pool_hit_rate', metric: 'buffer_pool_hit_rate', label: '缓冲池命中率', color: '#9b59b6', display: (v: number) => Number(v).toFixed(1) + '%' },
  { key: 'slow_queries', metric: 'slow_queries', label: '慢查询', color: '#f56c6c', display: (v: number) => String(Math.round(v)) },
  { key: 'lock_waits', metric: 'lock_waits', label: '锁等待', color: '#e6a23c', display: (v: number) => String(Math.round(v)) },
] as const

// 时间范围配置：value → { interval, durationMs }
const HISTORY_RANGE_CONFIG: Record<string, { interval: 'raw' | '5min' | '1hour'; durationMs: number }> = {
  '1h': { interval: 'raw', durationMs: 60 * 60 * 1000 },
  '24h': { interval: '5min', durationMs: 24 * 60 * 60 * 1000 },
  '7d': { interval: '1hour', durationMs: 7 * 24 * 60 * 60 * 1000 },
  '30d': { interval: '1hour', durationMs: 30 * 24 * 60 * 60 * 1000 },
}

interface TrendSample {
  qps: number
  tps: number
  connections: number
  bufferHitRate: number
  ts: number
}

interface HistoryPoint {
  timestamp: string
  value: number
}

const props = defineProps<{
  connId?: string
  schema?: string
  active: boolean
}>()

// 实时模式数据
const trendHistory = ref<TrendSample[]>([])
const paused = ref(false)
let trendTimer: ReturnType<typeof setInterval> | null = null
// 缓冲池命中率：实时采样时复用，避免每次采样都调用 resources 接口
let cachedBufferHitRate = 0

// 历史模式数据
const mode = ref<'realtime' | 'history'>('realtime')
const historyMetric = ref<string>('qps')
const historyRange = ref<string>('1h')
const historyPoints = ref<HistoryPoint[]>([])
const historyLoading = ref(false)

// 格式化时间戳为 HH:mm:ss，用于 X 轴展示
function formatTrendTime(ts: number): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 格式化为后端可解析的时间字符串 "YYYY-MM-DD HH:mm:ss"
function formatDateTime(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

// 根据时间范围格式化 X 轴时间标签
function formatHistoryTime(ts: string, range: string): string {
  const d = new Date(ts.replace(/-/g, '/'))
  const pad = (n: number) => String(n).padStart(2, '0')
  // 1h 显示 HH:mm:ss，24h 显示 HH:mm，7d/30d 显示 MM-DD HH:mm
  if (range === '1h') return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  if (range === '24h') return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 各指标当前值与最值/均值统计（不再构造 SVG 点，趋势由 ECharts 渲染）
const trendMetrics = computed(() => {
  const hist = trendHistory.value
  if (hist.length === 0) return []
  return TREND_METRIC_DEFS.map(def => {
    const values = hist.map(h => Number(h[def.key]) || 0)
    const latest = values[values.length - 1]
    const min = Math.min(...values)
    const max = Math.max(...values)
    const avg = values.reduce((a, b) => a + b, 0) / values.length
    return { ...def, latest, min, max, avg }
  })
})

// ECharts 主趋势图配置：QPS / TPS / 连接数 共用左 Y 轴，缓冲池命中率使用右 Y 轴（百分比）
const trendChartOption = computed(() => {
  const hist = trendHistory.value
  const xData = hist.map(h => formatTrendTime(h.ts))
  const buildSeries = (def: typeof TREND_METRIC_DEFS[number], yAxisIndex = 0) => ({
    name: def.label,
    type: 'line',
    yAxisIndex,
    smooth: true,
    showSymbol: false,
    areaStyle: { opacity: 0.15 },
    lineStyle: { width: 2 },
    itemStyle: { color: def.color },
    data: hist.map(h => Number(h[def.key]) || 0),
  })
  return {
    tooltip: {
      trigger: 'axis',
      // 自定义 tooltip：显示时间 + 各指标值（含单位）
      formatter: (params: any[]) => {
        if (!params || params.length === 0) return ''
        const time = params[0].axisValue
        const lines = params.map(p => {
          const def = TREND_METRIC_DEFS.find(d => d.label === p.seriesName)
          const val = def ? def.display(p.value) : p.value
          return `${p.marker}${p.seriesName}: ${val}`
        })
        return [time, ...lines].join('<br/>')
      },
    },
    legend: {
      data: TREND_METRIC_DEFS.map(d => d.label),
      top: 0,
    },
    grid: { left: 50, right: 60, top: 40, bottom: 30 },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: xData,
      axisLabel: { fontSize: 10 },
    },
    yAxis: [
      {
        type: 'value',
        name: 'QPS/TPS/连接',
        axisLabel: { fontSize: 10 },
        scale: true,
      },
      {
        type: 'value',
        name: '命中率(%)',
        min: 0,
        max: 100,
        axisLabel: { fontSize: 10, formatter: '{value}%' },
      },
    ],
    series: [
      buildSeries(TREND_METRIC_DEFS[0], 0), // QPS
      buildSeries(TREND_METRIC_DEFS[1], 0), // TPS
      buildSeries(TREND_METRIC_DEFS[2], 0), // 连接数
      buildSeries(TREND_METRIC_DEFS[3], 1), // 缓冲池命中率（右轴）
    ],
  }
})

// 加载资源以获取缓冲池命中率（实时采样时复用）
async function loadBufferHitRate() {
  if (!props.connId) return
  try {
    const res = await getMonitorResources(props.connId, props.schema || '')
    cachedBufferHitRate = res.data?.data?.dbResources?.bufferPoolHitRate ?? 0
  } catch {
    // 忽略：使用默认值 0
  }
}

async function sampleTrend() {
  if (!props.connId) return
  try {
    const res = await getMonitorMetrics(props.connId)
    // 真实快照在 res.data.data（见 loadMetrics 注释）
    const m = res.data?.data
    const sample: TrendSample = {
      qps: m?.qps ?? 0,
      tps: m?.tps ?? 0,
      connections: m?.connections ?? 0,
      bufferHitRate: cachedBufferHitRate,
      ts: Date.now(),
    }
    trendHistory.value.push(sample)
    if (trendHistory.value.length > TREND_MAX) {
      trendHistory.value.shift()
    }
  } catch (e) { handleError(e, '采样性能趋势') }
}

function startAutoRefresh() {
  stopAutoRefresh()
  trendTimer = setInterval(sampleTrend, 5000)
}

function stopAutoRefresh() {
  if (trendTimer) { clearInterval(trendTimer); trendTimer = null }
}

function togglePause() {
  paused.value = !paused.value
  if (paused.value) stopAutoRefresh()
  else startAutoRefresh()
}

function clearTrend() {
  trendHistory.value = []
}

// 加载历史趋势数据：调用 /monitor/history API
async function loadHistory() {
  if (!props.connId) return
  const cfg = HISTORY_RANGE_CONFIG[historyRange.value]
  if (!cfg) return
  const metricDef = HISTORY_METRICS.find(m => m.key === historyMetric.value)
  if (!metricDef) return

  historyLoading.value = true
  try {
    const now = new Date()
    const from = new Date(now.getTime() - cfg.durationMs)
    const res = await getMonitorHistory(props.connId, metricDef.metric, formatDateTime(from), formatDateTime(now), cfg.interval)
    historyPoints.value = res.data?.data?.points || []
  } catch (e) {
    handleError(e, '加载历史趋势')
    historyPoints.value = []
  } finally {
    historyLoading.value = false
  }
}

// 历史趋势 ECharts 配置：单指标折线图
const historyChartOption = computed(() => {
  const points = historyPoints.value
  const metricDef = HISTORY_METRICS.find(m => m.key === historyMetric.value)
  if (!metricDef || points.length === 0) return {}

  const xData = points.map(p => formatHistoryTime(p.timestamp, historyRange.value))
  const values = points.map(p => Number(p.value) || 0)
  // 计算最值/均值用于 tooltip 展示
  const min = Math.min(...values)
  const max = Math.max(...values)
  const avg = values.reduce((a, b) => a + b, 0) / values.length

  return {
    title: {
      text: `${metricDef.label}（最小 ${metricDef.display(min)} / 最大 ${metricDef.display(max)} / 平均 ${metricDef.display(avg)}）`,
      textStyle: { fontSize: 12, fontWeight: 'normal' },
      left: 'center',
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any[]) => {
        if (!params || params.length === 0) return ''
        return `${params[0].axisValue}<br/>${params[0].marker}${metricDef.label}: ${metricDef.display(params[0].value)}`
      },
    },
    grid: { left: 60, right: 30, top: 50, bottom: 40 },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: xData,
      axisLabel: { fontSize: 10 },
    },
    yAxis: {
      type: 'value',
      name: metricDef.label,
      axisLabel: { fontSize: 10 },
      scale: true,
    },
    series: [{
      name: metricDef.label,
      type: 'line',
      smooth: true,
      showSymbol: false,
      areaStyle: { opacity: 0.15 },
      lineStyle: { width: 2 },
      itemStyle: { color: metricDef.color },
      data: values,
    }],
  }
})

// 切换实时/历史模式
function onModeChange(modeVal: string | number | boolean | undefined) {
  const m = String(modeVal)
  if (m === 'realtime') {
    // 切回实时模式：恢复自动刷新
    stopAutoRefresh()
    if (!paused.value) startAutoRefresh()
  } else {
    // 切到历史模式：停止实时采样并加载历史数据
    stopAutoRefresh()
    loadHistory()
  }
}

// 首次激活时启动实时采样（或加载历史数据）
watch(
  () => props.active,
  (active) => {
    if (active) {
      if (mode.value === 'realtime') {
        // 首次激活：加载缓冲池命中率并启动采样
        if (!trendTimer && !paused.value) {
          loadBufferHitRate()
          startAutoRefresh()
        }
      } else {
        loadHistory()
      }
    } else {
      // 离开 Tab 时停止实时采样，避免后台无用轮询
      stopAutoRefresh()
    }
  },
  { immediate: true },
)

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
/* 性能趋势 Tab */
.perf-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}

.perf-tip {
  color: var(--db-text-tertiary);
  font-size: 12px;
}

.trend-card {
  background: var(--db-card-bg);
  border: 1px solid var(--db-card-border);
  border-radius: 8px;
  padding: 12px;
  box-shadow: var(--db-card-shadow);
}

.trend-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 6px;
}

.trend-title {
  font-size: 13px;
  color: var(--db-text-secondary);
}

.trend-current {
  font-size: 18px;
  font-weight: 700;
  font-family: 'JetBrains Mono', monospace;
}

.trend-stats {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--db-text-tertiary);
  margin-top: 4px;
}
</style>
