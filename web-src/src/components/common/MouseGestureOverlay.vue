<template>
  <canvas
    v-if="enabled"
    ref="canvasRef"
    class="mouse-gesture-canvas"
    aria-hidden="true"
  />
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

/**
 * 桌面模式鼠标右键手势覆盖层（参考 Edge 鼠标手势）。
 *
 * 支持的手势：
 *   ← 左拖：后退
 *   → 右拖：前进
 *   ↑ 上拖：刷新
 *
 * 仅在桌面模式（sessionStorage.isDesktop === 'true'）下激活。
 * 右键单击不拖动时保留原生右键菜单，拖动超过阈值才识别为手势并阻止菜单。
 */

const GESTURE_THRESHOLD = 14 // 最小拖动距离（px），低于此视为右键单击
const TRAIL_COLOR = 'rgba(0, 120, 215, 0.85)'
const TRAIL_WIDTH = 3
const HINT_BG = '#0078d7'

type GestureName = 'back' | 'forward' | 'refresh'
interface Point { x: number; y: number }

const router = useRouter()
const canvasRef = ref<HTMLCanvasElement | null>(null)
const enabled = ref(false)

let canvasCtx: CanvasRenderingContext2D | null = null
let dpr = 1

// 手势状态（非响应式，避免频繁触发渲染）
let startPos: Point | null = null
let lastPos: Point | null = null
let points: Point[] = []
let gestureActive = false // 是否已超过阈值，进入手势模式
let currentHint: { text: string; x: number; y: number } | null = null

const HINT_TEXT: Record<GestureName, string> = {
  back: '后退',
  forward: '前进',
  refresh: '刷新',
}

/** 判断手势方向：取起点到当前点的位移主方向 */
function detectGesture(start: Point, end: Point): GestureName | null {
  const dx = end.x - start.x
  const dy = end.y - start.y
  const dist = Math.hypot(dx, dy)
  if (dist < GESTURE_THRESHOLD) return null
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx > 0 ? 'forward' : 'back'
  }
  // 垂直方向仅向上识别为刷新，向下忽略（避免误触）
  return dy < 0 ? 'refresh' : null
}

/** 执行手势对应的导航操作 */
function executeGesture(g: GestureName) {
  switch (g) {
    case 'back':
      router.back()
      break
    case 'forward':
      router.forward()
      break
    case 'refresh':
      window.location.reload()
      break
  }
}

// ---- Canvas 绘制 ----

function resizeCanvas() {
  const canvas = canvasRef.value
  if (!canvas) return
  dpr = window.devicePixelRatio || 1
  canvas.width = window.innerWidth * dpr
  canvas.height = window.innerHeight * dpr
  canvas.style.width = window.innerWidth + 'px'
  canvas.style.height = window.innerHeight + 'px'
  canvasCtx = canvas.getContext('2d')
  if (canvasCtx) {
    canvasCtx.scale(dpr, dpr)
  }
}

function clearCanvas() {
  if (!canvasCtx || !canvasRef.value) return
  canvasCtx.clearRect(0, 0, canvasRef.value.width, canvasRef.value.height)
}

function drawTrail() {
  if (!canvasCtx || points.length < 2) return
  clearCanvas()

  // 绘制轨迹线
  canvasCtx.strokeStyle = TRAIL_COLOR
  canvasCtx.lineWidth = TRAIL_WIDTH
  canvasCtx.lineCap = 'round'
  canvasCtx.lineJoin = 'round'
  canvasCtx.beginPath()
  canvasCtx.moveTo(points[0].x, points[0].y)
  for (let i = 1; i < points.length; i++) {
    canvasCtx.lineTo(points[i].x, points[i].y)
  }
  canvasCtx.stroke()

  // 起点圆圈
  const p0 = points[0]
  canvasCtx.fillStyle = TRAIL_COLOR
  canvasCtx.beginPath()
  canvasCtx.arc(p0.x, p0.y, 5, 0, Math.PI * 2)
  canvasCtx.fill()

  // 手势提示
  if (currentHint) {
    drawHint(currentHint.text, currentHint.x, currentHint.y)
  }
}

function drawHint(text: string, x: number, y: number) {
  if (!canvasCtx) return
  const fontSize = 14
  canvasCtx.font = `${fontSize}px "Microsoft YaHei", "Segoe UI", sans-serif`
  const metrics = canvasCtx.measureText(text)
  const padX = 10
  const padY = 6
  const boxW = metrics.width + padX * 2
  const boxH = fontSize + padY * 2
  // 提示框显示在起点右上方，超出视口则翻转
  let bx = x + 14
  let by = y - boxH - 10
  if (bx + boxW > window.innerWidth - 4) bx = x - boxW - 14
  if (by < 4) by = y + 14

  canvasCtx.fillStyle = HINT_BG
  canvasCtx.beginPath()
  // roundRect 在旧版 WebView2 中可能缺失，降级为 fillRect
  if (typeof canvasCtx.roundRect === 'function') {
    canvasCtx.roundRect(bx, by, boxW, boxH, 6)
  } else {
    canvasCtx.rect(bx, by, boxW, boxH)
  }
  canvasCtx.fill()

  canvasCtx.fillStyle = '#fff'
  canvasCtx.textBaseline = 'middle'
  canvasCtx.fillText(text, bx + padX, by + boxH / 2 + 0.5)
}

// ---- 事件处理 ----

function onMouseDown(e: MouseEvent) {
  if (e.button !== 2) return // 仅右键
  startPos = { x: e.clientX, y: e.clientY }
  lastPos = { ...startPos }
  points = [{ ...startPos }]
  gestureActive = false
  currentHint = null
}

function onMouseMove(e: MouseEvent) {
  if (!startPos) return
  lastPos = { x: e.clientX, y: e.clientY }
  const dist = Math.hypot(lastPos.x - startPos.x, lastPos.y - startPos.y)

  if (!gestureActive && dist > GESTURE_THRESHOLD) {
    gestureActive = true
  }

  if (gestureActive) {
    // 简化轨迹：仅在移动距离足够时记录点，避免点过密
    const last = points[points.length - 1]
    if (Math.hypot(lastPos.x - last.x, lastPos.y - last.y) > 2) {
      points.push({ ...lastPos })
    }
    // 实时更新提示
    const g = detectGesture(startPos, lastPos)
    currentHint = g ? { text: HINT_TEXT[g], x: startPos.x, y: startPos.y } : null
    drawTrail()
  }
}

function onMouseUp(e: MouseEvent) {
  if (e.button !== 2 || !startPos) return
  if (gestureActive && lastPos) {
    const g = detectGesture(startPos, lastPos)
    if (g) executeGesture(g)
  }
  // 不在此处重置：contextmenu 事件在 mouseup 之后触发，需要读到 gestureActive
}

function onContextMenu(e: MouseEvent) {
  if (gestureActive) {
    e.preventDefault()
    e.stopPropagation()
  }
  resetGesture()
}

function resetGesture() {
  startPos = null
  lastPos = null
  points = []
  gestureActive = false
  currentHint = null
  clearCanvas()
}

// 兜底：如果 contextmenu 未触发（极端情况），延迟清除状态
let fallbackTimer: number | null = null
function scheduleFallbackClear() {
  if (fallbackTimer) clearTimeout(fallbackTimer)
  fallbackTimer = window.setTimeout(() => {
    if (gestureActive) resetGesture()
  }, 200)
}

// 包装 onMouseUp 以添加兜底清理
function onMouseUpWrapped(e: MouseEvent) {
  onMouseUp(e)
  if (gestureActive) scheduleFallbackClear()
}

// ---- 生命周期 ----

function handleResize() {
  resizeCanvas()
}

onMounted(() => {
  // 仅桌面模式启用
  if (sessionStorage.getItem('isDesktop') !== 'true') return
  enabled.value = true

  // 等待 canvas 渲染后初始化
  nextTick(() => {
    resizeCanvas()
  })

  // 使用捕获阶段，确保在其他处理器之前拦截
  window.addEventListener('mousedown', onMouseDown, true)
  window.addEventListener('mousemove', onMouseMove, true)
  window.addEventListener('mouseup', onMouseUpWrapped, true)
  window.addEventListener('contextmenu', onContextMenu, true)
  window.addEventListener('resize', handleResize)
})

onBeforeUnmount(() => {
  window.removeEventListener('mousedown', onMouseDown, true)
  window.removeEventListener('mousemove', onMouseMove, true)
  window.removeEventListener('mouseup', onMouseUpWrapped, true)
  window.removeEventListener('contextmenu', onContextMenu, true)
  window.removeEventListener('resize', handleResize)
  if (fallbackTimer) clearTimeout(fallbackTimer)
})
</script>

<style scoped>
.mouse-gesture-canvas {
  position: fixed;
  inset: 0;
  z-index: 99999;
  pointer-events: none;
}
</style>
