<template>
  <!-- 通用 ECharts 封装组件：支持深色模式、自适应尺寸、加载态 -->
  <div ref="containerRef" class="echart-container" :style="containerStyle" role="img" aria-label="图表">
    <div v-if="loading" class="echart-loading">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span>加载中...</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, computed, markRaw, shallowRef, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'
// 按需引入 echarts 模块，减小打包体积
import * as echarts from 'echarts/core'
import { LineChart, BarChart, PieChart } from 'echarts/charts'
import {
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent,
  DataZoomComponent,
  MarkLineComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

// 注册必需的 echarts 模块（仅注册一次）
echarts.use([
  LineChart,
  BarChart,
  PieChart,
  TitleComponent,
  TooltipComponent,
  LegendComponent,
  GridComponent,
  DataZoomComponent,
  MarkLineComponent,
  CanvasRenderer,
])

const props = withDefaults(
  defineProps<{
    /** ECharts 配置对象 */
    option: Record<string, any>
    /** 图表高度，默认 300px */
    height?: string
    /** 加载状态 */
    loading?: boolean
    /** 主题：默认跟随 data-theme 属性 */
    theme?: 'light' | 'dark' | 'auto'
  }>(),
  {
    height: '300px',
    loading: false,
    theme: 'auto',
  },
)

const containerRef = shallowRef<HTMLDivElement | null>(null)
// 使用 markRaw 避免 Vue 响应式系统包装 echarts 实例，防止性能问题
const chartInstance = shallowRef<echarts.ECharts | null>(null)
let resizeObserver: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

const containerStyle = computed(() => ({
  height: props.height,
  width: '100%',
}))

/** 根据当前主题（含 auto 跟随系统）决定 echarts 主题名 */
function resolveTheme(): 'dark' | undefined {
  if (props.theme === 'dark') return 'dark'
  if (props.theme === 'light') return undefined
  // auto：读取 documentElement 的 data-theme 属性
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : undefined
}

/** 创建 echarts 实例并应用配置 */
function createChart() {
  if (!containerRef.value) return
  // 销毁旧实例
  disposeChart()
  const instance = echarts.init(containerRef.value, resolveTheme(), { renderer: 'canvas' })
  chartInstance.value = markRaw(instance)
  if (props.option) {
    instance.setOption(props.option, true)
  }
}

/** 安全销毁当前 echarts 实例 */
function disposeChart() {
  if (chartInstance.value) {
    chartInstance.value.dispose()
    chartInstance.value = null
  }
}

/** 监听 option 变化，增量更新配置 */
watch(
  () => props.option,
  (newOption) => {
    if (chartInstance.value && newOption) {
      // 第二个参数 true 表示不合并配置（notMerge），保证状态切换时干净
      chartInstance.value.setOption(newOption, true)
    } else if (newOption && containerRef.value) {
      createChart()
    }
  },
  { deep: true },
)

onMounted(() => {
  createChart()

  // 使用 ResizeObserver 监听容器尺寸变化，自动 resize 图表
  if (containerRef.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      chartInstance.value?.resize()
    })
    resizeObserver.observe(containerRef.value)
  }

  // 监听 documentElement 的 data-theme 属性变化，切换 echarts 主题
  themeObserver = new MutationObserver(() => {
    // 主题变化需重建实例以应用新主题
    createChart()
  })
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  })
})

onBeforeUnmount(() => {
  // 清理观察者与实例，避免内存泄漏
  resizeObserver?.disconnect()
  resizeObserver = null
  themeObserver?.disconnect()
  themeObserver = null
  disposeChart()
})

// 暴露实例方法，便于父组件调用（如手动 resize、dispatchAction）
defineExpose({
  getInstance: () => chartInstance.value,
  resize: () => chartInstance.value?.resize(),
})
</script>

<style scoped>
.echart-container {
  position: relative;
  min-width: 0;
}

.echart-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--db-card-bg, #fff);
  opacity: 0.7;
  color: var(--db-text-secondary, #606266);
  font-size: 13px;
  z-index: 1;
}
</style>
