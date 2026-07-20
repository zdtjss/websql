<template>
  <Transition name="find-bar-slide">
    <div v-if="visible" class="websql-find-bar" role="search" aria-label="页面内查找与替换">
      <!-- 查找行 -->
      <div class="find-row">
        <input
          ref="inputRef"
          v-model="query"
          class="find-input"
          type="text"
          placeholder="查找..."
          spellcheck="false"
          autocomplete="off"
          aria-label="查找内容"
          @keydown.enter.prevent="onFindEnter"
          @keydown.escape.prevent="close"
        />
        <button
          class="find-btn"
          :class="{ 'find-btn-active': showReplace }"
          type="button"
          title="展开/折叠替换"
          aria-label="替换"
          @click="toggleReplace"
        >
          <svg viewBox="0 0 16 16" width="14" height="14"><path fill="currentColor" d="M2 4h12v2H2zm0 4h8v2H2zm0 4h12v2H2z"/></svg>
        </button>
        <span class="find-count" aria-live="polite">{{ countText }}</span>
        <button
          class="find-btn"
          type="button"
          title="上一个 (Shift+Enter)"
          aria-label="上一个"
          @click="findPrev"
        >
          <svg viewBox="0 0 16 16" width="14" height="14"><path fill="currentColor" d="M8 4l5 5H3z"/></svg>
        </button>
        <button
          class="find-btn"
          type="button"
          title="下一个 (Enter)"
          aria-label="下一个"
          @click="findNext"
        >
          <svg viewBox="0 0 16 16" width="14" height="14"><path fill="currentColor" d="M8 12l5-5H3z"/></svg>
        </button>
        <button
          class="find-btn find-close"
          type="button"
          title="关闭 (Esc)"
          aria-label="关闭查找"
          @click="close"
        >
          <svg viewBox="0 0 16 16" width="14" height="14"><path fill="currentColor" d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5"/></svg>
        </button>
      </div>
      <!-- 替换行 -->
      <div v-if="showReplace" class="find-row">
        <input
          ref="replaceInputRef"
          v-model="replacement"
          class="find-input"
          type="text"
          placeholder="替换为..."
          spellcheck="false"
          autocomplete="off"
          aria-label="替换内容"
          @keydown.enter.prevent="onReplaceEnter"
          @keydown.escape.prevent="close"
        />
        <span class="find-spacer"></span>
        <button
          class="find-action-btn"
          type="button"
          title="替换当前匹配"
          @click="replaceCurrent"
        >替换</button>
        <button
          class="find-action-btn"
          type="button"
          title="替换所有匹配"
          @click="replaceAll"
        >全部替换</button>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'

/**
 * 页面内查找/替换栏 + 全局快捷键处理（桌面模式专用）。
 *
 * 由前端 document keydown 监听器统一处理所有快捷键，不依赖 Wails KeyBindings：
 * - Ctrl/Cmd+F：打开查找栏；已打开则聚焦查找输入框（并自动展开替换栏以便使用替换）
 * - Ctrl/Cmd+加号：页面放大（通过 /desktop/zoom 端点调用 Wails ZoomIn）
 * - Ctrl/Cmd+减号：页面缩小
 * - Ctrl/Cmd+0：还原缩放
 * - Esc：关闭查找栏（仅在查找栏自身或其输入框有焦点时生效，避免干扰
 *   Element Plus 对话框、Mermaid 全屏、内联编辑等场景的 Esc 行为）
 *
 * 替换实现：window.find() 选中匹配 → document.execCommand('insertText') 替换选区。
 * execCommand 虽已废弃但仍被 Chromium/WebKitGTK/WKWebView 广泛支持，
 * 适用于 contenteditable、input、textarea；CodeMirror 等富文本编辑器需自行处理。
 */

const visible = ref(false)
const showReplace = ref(false)
const query = ref('')
const replacement = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
const replaceInputRef = ref<HTMLInputElement | null>(null)
const countText = ref('')

let lastQuery = ''

function toggle(show?: boolean) {
  visible.value = show === undefined ? !visible.value : show
  if (visible.value) {
    nextTick(() => {
      inputRef.value?.focus()
      inputRef.value?.select()
    })
  } else {
    clearSelection()
    countText.value = ''
    showReplace.value = false
  }
}

function toggleReplace() {
  showReplace.value = !showReplace.value
  if (showReplace.value) {
    nextTick(() => {
      replaceInputRef.value?.focus()
    })
  }
}

;(window as any).__websqlToggleFindBar = toggle

function onFindEnter(e: KeyboardEvent) {
  if (e.shiftKey) {
    findPrev()
  } else {
    findNext()
  }
}

function onReplaceEnter(e: KeyboardEvent) {
  // 在替换输入框中：Enter 替换当前并跳到下一个，Shift+Enter 替换当前并跳到上一个
  if (e.shiftKey) {
    replaceCurrent()
    findPrev()
  } else {
    replaceCurrent()
    findNext()
  }
}

function findNext() {
  if (!query.value) return
  const found = doFind(query.value, false, lastQuery !== query.value)
  lastQuery = query.value
  countText.value = found ? '' : '未找到'
}

function findPrev() {
  if (!query.value) return
  const found = doFind(query.value, true, false)
  lastQuery = query.value
  countText.value = found ? '' : '未找到'
}

function doFind(text: string, backwards: boolean, wrap: boolean): boolean {
  try {
    // @ts-ignore - window.find 是非标准但广泛支持的 API
    return window.find(text, false, backwards, wrap, false, false, false)
  } catch {
    return false
  }
}

/** 替换当前选中的匹配文本 */
function replaceCurrent() {
  if (!query.value) return
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) {
    if (!doFind(query.value, false, lastQuery !== query.value)) {
      countText.value = '未找到'
      return
    }
  }
  const selectedText = sel?.toString() || ''
  if (selectedText !== query.value) {
    if (!doFind(query.value, false, true)) {
      countText.value = '未找到'
      return
    }
  }
  try {
    // @ts-ignore - execCommand 已废弃但仍广泛支持
    document.execCommand('insertText', false, replacement.value)
  } catch {
    replaceWithSelection(replacement.value)
  }
}

/** 替换所有匹配 */
function replaceAll() {
  if (!query.value) return
  let count = 0
  clearSelection()
  try {
    (document.body as any).focus?.()
  } catch {
    // 静默
  }
  const maxIter = 10000
  for (let i = 0; i < maxIter; i++) {
    if (!doFind(query.value, false, false)) break
    const sel = window.getSelection()
    if (!sel || sel.toString() !== query.value) break
    try {
      // @ts-ignore
      document.execCommand('insertText', false, replacement.value)
      count++
    } catch {
      replaceWithSelection(replacement.value)
      count++
    }
  }
  clearSelection()
  countText.value = count > 0 ? `已替换 ${count} 处` : '未找到'
}

function replaceWithSelection(text: string) {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0) return
  const range = sel.getRangeAt(0)
  range.deleteContents()
  range.insertNode(document.createTextNode(text))
  range.collapse(false)
  sel.removeAllRanges()
  sel.addRange(range)
}

function clearSelection() {
  try {
    const sel = window.getSelection()
    if (sel) sel.removeAllRanges()
  } catch {
    // 静默
  }
}

function close() {
  toggle(false)
}

/** 调用桌面端 /desktop/zoom 端点控制 WebView 缩放 */
function requestZoom(action: 'in' | 'out' | 'reset') {
  try {
    const apiBase = import.meta.env.VITE_API_URL || ''
    void fetch(`${apiBase}/api/desktop/zoom?action=${action}`, { method: 'POST' })
  } catch {
    // 静默
  }
}

/**
 * 判断当前是否应优先把 Esc 交给页面其他组件处理。
 * 以下场景下查找栏不应消费 Esc，避免干扰：
 * - Element Plus 对话框/抽屉/消息框打开（.el-dialog__wrapper、.el-drawer、.el-message-box 等）
 * - Mermaid 图全屏（.mermaid-fullscreen-overlay）
 * - 任意全屏元素（fullscreenElement）
 * 此时即便查找栏可见，也放行 Esc 给页面。
 */
function shouldYieldEscapeToPage(): boolean {
  // 1. 浏览器全屏元素（如 Mermaid 全屏、video 全屏等）
  if (document.fullscreenElement) return true
  // 2. Mermaid 自定义全屏遮罩
  if (document.querySelector('.mermaid-fullscreen-overlay')) return true
  // 3. Element Plus 弹层：对话框/抽屉/消息框/弹出层
  if (document.querySelector(
    '.el-dialog__wrapper:not([style*="display: none"]) .el-dialog,' +
    '.el-overlay:not([style*="display: none"]),' +
    '.el-drawer__container:not([style*="display: none"]),' +
    '.el-message-box'
  )) return true
  return false
}

/**
 * 全局 keydown 处理：在 capture 阶段拦截，确保在 CodeMirror 等编辑器之前处理。
 * 桌面模式下 Wails 已禁用 WebView2 浏览器加速键（AreBrowserAcceleratorKeysEnabled=false），
 * 所以 Ctrl+F 等不会触发原生行为，keydown 事件可正常到达 document。
 */
function handleGlobalKeydown(e: KeyboardEvent) {
  // Esc：查找栏可见时处理关闭，但必须避让页面上其他需要 Esc 的场景
  if (e.key === 'Escape' && visible.value) {
    if (shouldYieldEscapeToPage()) {
      // 页面有更高优先级的 Esc 消费者（对话框/Mermaid 全屏等），不拦截
      return
    }
    e.preventDefault()
    e.stopPropagation()
    close()
    return
  }

  const mod = e.ctrlKey || e.metaKey
  if (!mod) return

  const key = e.key.toLowerCase()

  // Ctrl/Cmd+F：打开查找栏；已打开则聚焦查找输入框并展开替换栏
  if (key === 'f') {
    e.preventDefault()
    e.stopPropagation()
    if (!visible.value) {
      toggle(true)
    } else {
      if (!showReplace.value) {
        showReplace.value = true
      }
      nextTick(() => {
        inputRef.value?.focus()
        inputRef.value?.select()
      })
    }
    return
  }

  // Ctrl/Cmd+加号 或 Ctrl/Cmd+Shift+=（同一物理键）：放大
  if (key === '+' || (e.shiftKey && key === '=')) {
    e.preventDefault()
    e.stopPropagation()
    requestZoom('in')
    return
  }

  // Ctrl/Cmd+减号：缩小
  if (key === '-') {
    e.preventDefault()
    e.stopPropagation()
    requestZoom('out')
    return
  }

  // Ctrl/Cmd+0：还原缩放
  if (key === '0') {
    e.preventDefault()
    e.stopPropagation()
    requestZoom('reset')
    return
  }
}

// 使用 capture 阶段，确保在编辑器（CodeMirror 等）的 keydown 处理之前拦截
document.addEventListener('keydown', handleGlobalKeydown, true)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleGlobalKeydown, true)
  delete (window as any).__websqlToggleFindBar
})
</script>

<style scoped>
.websql-find-bar {
  position: fixed;
  top: 12px;
  right: 24px;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 6px 8px;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  font-size: 13px;
}

.find-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.find-input {
  width: 180px;
  padding: 4px 8px;
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 4px;
  background: var(--el-fill-color-blank, #fff);
  color: var(--el-text-color-primary, #303133);
  font-size: 13px;
  outline: none;
}

.find-input:focus {
  border-color: var(--el-color-primary, #409eff);
}

.find-count {
  min-width: 48px;
  color: var(--el-text-color-secondary, #909399);
  font-size: 12px;
  text-align: center;
}

.find-spacer {
  min-width: 48px;
}

.find-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--el-text-color-regular, #606266);
  cursor: pointer;
  border-radius: 4px;
}

.find-btn:hover {
  background: var(--el-fill-color, #f5f7fa);
  color: var(--el-color-primary, #409eff);
}

.find-btn-active {
  background: var(--el-color-primary-light-9, #ecf5ff);
  color: var(--el-color-primary, #409eff);
}

.find-close:hover {
  color: var(--el-color-danger, #f56c6c);
}

.find-action-btn {
  padding: 4px 10px;
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 4px;
  background: var(--el-fill-color-blank, #fff);
  color: var(--el-text-color-regular, #606266);
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}

.find-action-btn:hover {
  border-color: var(--el-color-primary, #409eff);
  color: var(--el-color-primary, #409eff);
}

.find-bar-slide-enter-active,
.find-bar-slide-leave-active {
  transition: transform 0.18s ease, opacity 0.18s ease;
}

.find-bar-slide-enter-from,
.find-bar-slide-leave-to {
  transform: translateY(-12px);
  opacity: 0;
}
</style>
