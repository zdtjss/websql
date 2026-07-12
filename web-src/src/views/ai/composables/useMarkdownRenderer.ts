import { nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Ref } from 'vue'
import { sanitizeHtml } from '@/utils/sanitizeHtml'
import {
  getMarkdownRenderer,
  getMermaid,
  getNextMermaidId,
  getHljs,
  switchMermaidTheme,
  getMermaidSvgCache,
  clearMermaidSvgCache,
} from '@/utils/lazyDeps'
import type { ChatMessage } from './useChatHistory'

/** useMarkdownRenderer 依赖的外部上下文 */
export interface UseMarkdownRendererDeps {
  /** 聊天历史（用于缓存失效） */
  chatHistory: Ref<ChatMessage[]>
  /** 消息容器元素（用于滚动控制） */
  msgContainer: Ref<HTMLElement | null>
  /** 当前主题（用于切换 mermaid 主题） */
  currentTheme: Ref<string>
}

/**
 * Markdown / Mermaid / 代码高亮 渲染 composable。
 *
 * 负责：
 *   - markdown-it 实例的懒加载与 fence 覆盖（代码块包裹 + mermaid 占位）
 *   - renderMarkdown / getCachedHtml（带渲染缓存）
 *   - highlightSql（基于 highlight.js）
 *   - mermaid 图表的渲染、缩放、拖拽、全屏、导出
 *   - MutationObserver 自动检测新插入的 mermaid 容器
 *   - 主题切换时清缓存重渲染
 *
 * 注意：mermaid 交互事件绑定在 document 上，由 ChatView 在 onMounted 中调用
 *      返回的事件处理器进行绑定，onUnmounted 中解绑。
 */
export function useMarkdownRenderer(deps: UseMarkdownRendererDeps) {
  const { chatHistory, msgContainer, currentTheme } = deps

  const apiBase = import.meta.env.VITE_API_URL || ''

  let md: any = null
  let mdInitPromise: Promise<any> | null = null

  /** markdown-it 是否就绪 */
  const mdReady = ref(false)
  /** highlight.js 是否就绪 */
  const hljsReady = ref(false)
  let hljsLib: any = null

  // ── 主题切换：切换 mermaid 主题并清空所有渲染缓存 ──
  watch(currentTheme, async (theme) => {
    await switchMermaidTheme(theme === 'dark' ? 'dark' : 'default')
    clearMermaidSvgCache()
    chatHistory.value.forEach((msg) => {
      msg._renderedHtml = null
      msg._lastContent = null
    })
    nextTick(() => { void doRenderMermaidBlocks(false) })
  })

  // ── mdReady 后：清空所有未用 md 渲染的消息缓存，重渲染 mermaid ──
  watch(mdReady, (ready) => {
    if (ready && chatHistory.value.length > 0) {
      chatHistory.value.forEach((msg) => {
        if (msg._renderedWithMd === false) {
          msg._renderedHtml = null
          msg._lastContent = null
        }
      })
      nextTick(() => { void doRenderMermaidBlocks(false) })
    }
  })

  /** 懒加载 markdown-it 实例（模块级单例，本组件只附加一次 fence 覆盖） */
  async function ensureMd(): Promise<any> {
    if (md) return md
    if (mdInitPromise) return mdInitPromise

    mdInitPromise = (async () => {
      md = await getMarkdownRenderer(apiBase)

      // 幂等保护：getMarkdownRenderer 返回模块级单例，本组件实例级变量
      // 每次重挂载都会再次注册 fence 覆盖，必须在单例上打标记防止多层嵌套
      if (!md.__codeBlockWrapperApplied) {
        const defaultFenceRender = md.renderer.rules.fence || function (tokens: any, idx: number, options: any, env: any, self: any) {
          return self.renderToken(tokens, idx, options)
        }
        md.renderer.rules.fence = function (tokens: any, idx: number, options: any, env: any, self: any) {
          const token = tokens[idx]
          const info = token.info ? token.info.trim().toLowerCase() : ''
          if (info === 'mermaid') {
            const source = token.content.trim()
            const svgCache = getMermaidSvgCache()
            const id = getNextMermaidId()
            if (svgCache.has(source)) {
              return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-processed="true">${svgCache.get(source)}</div>`
            }
            const escaped = token.content
              .replace(/&/g, '&amp;')
              .replace(/</g, '&lt;')
              .replace(/>/g, '&gt;')
              .replace(/"/g, '&quot;')
            return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-source="${escaped}" data-mermaid-processed="false"><pre class="mermaid-source-preview"><code>📊 Mermaid\n${escaped}</code></pre></div>`
          }
          const lang = info || ''
          const rawCode = token.content
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
          let encodedContent = ''
          try { encodedContent = btoa(encodeURIComponent(rawCode)) } catch (_) { /* ignore */ }
          const defaultHtml = defaultFenceRender(tokens, idx, options, env, self)
          return `<div class="code-block-wrapper">` +
            `<div class="code-block-header">` +
              `<span class="code-block-lang">${lang}</span>` +
              `<button class="code-copy-btn" data-code="${encodedContent}" title="复制代码">复制</button>` +
            `</div>` +
            defaultHtml +
          `</div>`
        }
        md.__codeBlockWrapperApplied = true
      }
      return md
    })()

    return mdInitPromise
  }

  /** 并行加载 markdown-it 与 highlight.js */
  async function initHeavyDeps(): Promise<void> {
    await Promise.all([
      ensureMd().then(() => { mdReady.value = true }),
      getHljs().then((h: any) => { hljsLib = h; hljsReady.value = true }),
    ])
  }

  /** SQL 高亮（基于 hljs，未就绪时做 HTML 转义） */
  function highlightSql(text: string): string {
    if (!text) return ''
    void hljsReady.value
    if (hljsLib) {
      try {
        return hljsLib.highlight(text, { language: 'sql' }).value
      } catch {
        return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      }
    }
    return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  /** 自动检测未被 code fence 包裹的 mermaid 代码并包裹 */
  function autoWrapMermaidCode(text: string): string {
    if (!text) return ''
    if (/```mermaid/i.test(text)) return text

    const mermaidKeywords = /^(graph\s+(TD|TB|BT|RL|LR)|flowchart\s+(TD|TB|BT|RL|LR)|sequenceDiagram|classDiagram|stateDiagram|erDiagram|gantt|pie|gitGraph|journey|mindmap|timeline|quadrantChart|sankey|xychart|block-beta)/m
    if (!mermaidKeywords.test(text)) return text

    const match = text.match(mermaidKeywords)
    if (!match) return text

    const startIdx = match.index ?? 0
    const before = text.substring(0, startIdx).trimEnd()
    const afterStart = text.substring(startIdx)

    const lines = afterStart.split('\n')
    let endLineIdx = lines.length
    let foundEmptyLine = false

    for (let i = 1; i < lines.length; i++) {
      const line = lines[i]
      const trimmedLine = line.trim()

      if (trimmedLine === '') {
        foundEmptyLine = true
        continue
      }
      if (foundEmptyLine) {
        const isMermaidLine = /^\s+/.test(line) ||
          /^(style|classDef|click|linkStyle|subgraph|end|%%|class\s)/.test(trimmedLine)
        if (!isMermaidLine) {
          endLineIdx = i
          break
        }
        foundEmptyLine = false
      }
    }

    const mermaidContent = lines.slice(0, endLineIdx).join('\n').trimEnd()
    const after = lines.slice(endLineIdx).join('\n').trimStart()

    let result = ''
    if (before) result = before + '\n\n'
    result += '```mermaid\n' + mermaidContent + '\n```'
    if (after) result += '\n\n' + after
    return result
  }

  /** Markdown 预处理：mermaid 自动包裹、LaTeX 简化、链接处理等 */
  function preprocessMarkdown(text: string): string {
    if (!text) return ''
    text = autoWrapMermaidCode(text)

    const codeBlocks: string[] = []
    let processed = text.replace(/(```[\s\S]*?```|`[^`\n]+`)/g, (match) => {
      const placeholder = `\x00CB${codeBlocks.length}\x00`
      codeBlocks.push(match)
      return placeholder
    })
    processed = processed.replace(/\$\\(?:text|textbf|textit)\{([^}]+)\}\$/g, (_match, inner) => inner)
    processed = processed.replace(/\$\\(?:bm|mathit|mathrm|mathsf|mathtt)\{([^}]+)\}\$/g, (_match, inner) => inner)
    processed = processed.replace(/\*\*\[([^\]]+)\]\(([^)]+)\)\*\*/g, '[$1]($2)')
    processed = processed.replace(/`((\/|\.\/)[^`\s]+\.(xlsx|docx|pptx|html|csv|pdf|txt|zip|json|md))`/g, (_match, path: string) => {
      const filename = path.substring(path.lastIndexOf('/') + 1)
      return `[${filename}](${path})`
    })
    processed = processed.replace(/\[([^\]]+)\]\(([^)]*)\)/g, (match, linkText: string, url: string) => {
      if (!url || url.length === 0) return match
      let fullUrl = url
      if (url.startsWith('/') && !url.startsWith('//')) {
        fullUrl = apiBase + url
      }
      let exportAttr = ''
      if (fullUrl && fullUrl.includes('/exports/')) {
        exportAttr = ' data-export-link="true"'
      }
      return `<a href="${fullUrl}" target="_blank" rel="noopener noreferrer"${exportAttr}>${linkText}</a>`
    })
    processed = processed.replace(/\x00CB(\d+)\x00/g, (_, i: string) => {
      const block = codeBlocks[parseInt(i)]
      if (block.startsWith('```')) return block
      const inner = block.slice(1, -1)
      if (!inner.includes('$')) return block
      const mathRegex = /\$([^$\s](?:[^$]*[^$\s])?)\$/g
      const segments: string[] = []
      let lastIndex = 0
      let m: RegExpExecArray | null
      while ((m = mathRegex.exec(inner)) !== null) {
        const before = inner.substring(lastIndex, m.index)
        if (before.trim()) segments.push('`' + before.trim() + '`')
        segments.push(m[0])
        lastIndex = m.index + m[0].length
      }
      const after = inner.substring(lastIndex)
      if (after.trim()) segments.push('`' + after.trim() + '`')
      if (segments.length <= 1) return block
      const hasIdentifier = segments.some((s) => s.startsWith('`') && /[a-zA-Z\u4e00-\u9fff]/.test(s))
      if (!hasIdentifier) return block
      return segments.join(' ')
    })
    return processed
  }

  /** 渲染 markdown 文本为 HTML（带 sanitize） */
  function renderMarkdown(text: string): string {
    if (!text) return ''
    void mdReady.value
    if (!md) {
      return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\n/g, '<br>')
    }
    try {
      const processed = preprocessMarkdown(text)
      return sanitizeHtml(md.render(processed))
    } catch (e) {
      console.error('Markdown parse error:', e)
      return text
    }
  }

  /** 带缓存的渲染：用于历史消息，避免重复调用导致 mermaid ID 变化 */
  function getCachedHtml(msg: ChatMessage): string {
    if (!msg._renderedHtml || msg._lastContent !== msg.content || (mdReady.value && !msg._renderedWithMd)) {
      msg._renderedHtml = renderMarkdown(msg.content)
      msg._lastContent = msg.content
      msg._renderedWithMd = mdReady.value
    }
    return msg._renderedHtml
  }

  /** 构造 mermaid 容器的 innerHTML（SVG + 工具栏 + 拖拽手柄） */
  function buildMermaidInnerHtml(svg: string, source: string): string {
    const escapedSource = source.replace(/</g, '&lt;').replace(/>/g, '&gt;')
    return `<div class="mermaid-content-wrapper">` +
      `<div class="mermaid-svg-wrap" data-scale="1" data-translate-x="0" data-translate-y="0">${svg}</div>` +
      `<pre class="mermaid-source-preview" style="display:none;"><code>${escapedSource}</code></pre>` +
    `</div>` +
    `<div class="mermaid-resize-handle" title="拖拽调整高度">` +
      `<span class="mermaid-resize-dots">⋯</span>` +
    `</div>` +
    `<div class="mermaid-toolbar">` +
      `<button class="mermaid-tb-btn" data-action="zoom-out" title="缩小">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="8" y1="11" x2="14" y2="11"/></svg>` +
      `</button>` +
      `<button class="mermaid-tb-btn" data-action="zoom-reset" title="重置">100%</button>` +
      `<button class="mermaid-tb-btn" data-action="zoom-in" title="放大">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>` +
      `</button>` +
      `<button class="mermaid-tb-btn" data-action="toggle-source" title="源码/图表">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>` +
      `</button>` +
      `<button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>` +
      `</button>` +
      `<button class="mermaid-tb-btn" data-action="export-png" title="导出 PNG">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>` +
      `</button>` +
      `<button class="mermaid-tb-btn" data-action="export-svg" title="导出 SVG">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="12" y1="18" x2="12" y2="12"/><polyline points="9 15 12 18 15 15"/></svg>` +
      `</button>` +
       `<button class="mermaid-tb-btn" data-action="fullscreen" title="全屏">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>` +
      `</button>` +
    `</div>`
  }

  /** 渲染所有未处理的 mermaid 容器（并发 3 个） */
  async function doRenderMermaidBlocks(scrollAfter = true): Promise<void> {
    await nextTick()
    const containers = document.querySelectorAll('.mermaid-container[data-mermaid-processed="false"]')
    if (containers.length === 0) return

    const CONCURRENCY = 3
    const toRender = Array.from(containers)
    const batches: Element[][] = []
    for (let i = 0; i < toRender.length; i += CONCURRENCY) {
      batches.push(toRender.slice(i, i + CONCURRENCY))
    }

    for (const batch of batches) {
      await Promise.allSettled(batch.map((el) => renderSingleMermaid(el as HTMLElement)))
    }

    if (scrollAfter && toRender.length > 0) {
      await nextTick()
      if (msgContainer.value) {
        msgContainer.value.scrollTop = msgContainer.value.scrollHeight
      }
    }
  }

  /** 渲染单个 mermaid 容器（带 2 次重试，处理动态 import 失败） */
  async function renderSingleMermaid(el: HTMLElement): Promise<void> {
    const id = el.getAttribute('data-mermaid-id')
    const source = el.getAttribute('data-mermaid-source')
      ?.replace(/&quot;/g, '"')
      .replace(/&gt;/g, '>')
      .replace(/&lt;/g, '<')
      .replace(/&amp;/g, '&')
    if (!id || !source) return
    const trimmed = source.trim()
    if (!trimmed || trimmed.length < 5) return

    el.setAttribute('data-mermaid-processed', 'true')

    const MAX_RETRIES = 2
    let lastError: unknown = null

    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
      try {
        const mermaidLib = await getMermaid()
        const renderId = 'mermaid-r-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 6)
        const { svg } = await mermaidLib.render(renderId, trimmed)
        const innerHtml = buildMermaidInnerHtml(svg, source)
        el.innerHTML = innerHtml
        const svgCache = getMermaidSvgCache()
        svgCache.set(trimmed, innerHtml)
        invalidateMermaidMsgCache(trimmed)
        return
      } catch (e) {
        lastError = e
        const isImportError = e && (
          String((e as Error).message || '').includes('Failed to fetch dynamically imported module') ||
          String((e as Error).message || '').includes('Importing a module script failed') ||
          String((e as Error).message || '').includes('error loading dynamically imported module')
        )
        if (isImportError && attempt < MAX_RETRIES) {
          console.warn(`Mermaid dynamic import failed (attempt ${attempt + 1}), retrying...`)
          await new Promise((r) => setTimeout(r, 500 * (attempt + 1)))
          continue
        }
        break
      }
    }

    console.warn('Mermaid render error for source:', trimmed.substring(0, 100), lastError)
    const escapedSource = source.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    el.innerHTML = `<pre class="mermaid-error"><code>${escapedSource}</code></pre><div class="mermaid-error-hint">⚠️ 图表渲染失败，请刷新页面重试</div>`
  }

  /** 清除包含指定 mermaid 源码的消息的渲染缓存 */
  function invalidateMermaidMsgCache(mermaidSource: string): void {
    for (const msg of chatHistory.value) {
      if (msg.role === 'assistant' && msg.content && msg.content.includes('mermaid')) {
        msg._renderedHtml = null
        msg._lastContent = null
      }
    }
    void mermaidSource
  }

  /** 统计已闭合的 ```mermaid``` 代码块数量（用于流式增量渲染） */
  function countClosedMermaidBlocks(content: string): number {
    let closedCount = 0
    let searchFrom = 0
    while (true) {
      const startIdx = content.indexOf('```mermaid', searchFrom)
      if (startIdx === -1) break
      const afterStart = startIdx + '```mermaid'.length
      const lineEnd = content.indexOf('\n', afterStart)
      if (lineEnd === -1) break
      const closeIdx = content.indexOf('\n```', lineEnd)
      if (closeIdx === -1) break
      closedCount++
      searchFrom = closeIdx + 4
    }
    return closedCount
  }

  // ── MutationObserver：自动检测新插入的 mermaid 容器并渲染 ──
  let mermaidObserver: MutationObserver | null = null
  let mermaidObserverTimer: ReturnType<typeof setTimeout> | null = null

  function setupMermaidObserver(): void {
    if (mermaidObserver) return
    mermaidObserver = new MutationObserver((mutations) => {
      let hasNew = false
      for (const mutation of mutations) {
        if (mutation.type === 'childList') {
          for (const node of mutation.addedNodes) {
            if (node.nodeType !== 1) continue
            const el = node as Element
            if (el.matches && el.matches('.mermaid-container[data-mermaid-processed="false"]')) {
              hasNew = true
              break
            }
            if (el.querySelector && el.querySelector('.mermaid-container[data-mermaid-processed="false"]')) {
              hasNew = true
              break
            }
          }
        }
        if (mutation.type === 'childList' && (mutation.target as Element).querySelector) {
          if ((mutation.target as Element).querySelector('.mermaid-container[data-mermaid-processed="false"]')) {
            hasNew = true
          }
        }
        if (hasNew) break
      }
      if (hasNew) {
        if (mermaidObserverTimer) clearTimeout(mermaidObserverTimer)
        mermaidObserverTimer = setTimeout(() => {
          void doRenderMermaidBlocks(true)
          mermaidObserverTimer = null
        }, 100)
      }
      if (mermaidCustomHeights.size > 0) {
        reapplyAllMermaidCustomHeights()
      }
    })
    mermaidObserver.observe(document.body, { childList: true, subtree: true })
  }

  function teardownMermaidObserver(): void {
    if (mermaidObserver) {
      mermaidObserver.disconnect()
      mermaidObserver = null
    }
    if (mermaidObserverTimer) {
      clearTimeout(mermaidObserverTimer)
      mermaidObserverTimer = null
    }
  }

  // ── Mermaid 交互：缩放、拖拽、工具栏、全屏、导出 ──

  const mermaidDragState = {
    isDragging: false,
    startX: 0,
    startY: 0,
    startTx: 0,
    startTy: 0,
    activeContainer: null as HTMLElement | null,
  }

  const mermaidCustomHeights = new Map<string, number>()

  const mermaidResizeState = {
    isResizing: false,
    startY: 0,
    startHeight: 0,
    activeMermaidId: null as string | null,
  }

  function updateMermaidWrapTransform(wrap: HTMLElement): void {
    const s = parseFloat(wrap.dataset.scale || '1')
    const tx = parseFloat(wrap.dataset.translateX || '0')
    const ty = parseFloat(wrap.dataset.translateY || '0')
    wrap.style.transform = `translate(${tx}px, ${ty}px) scale(${s})`
  }

  function handleMermaidWheel(e: WheelEvent): void {
    if (!e.ctrlKey) return
    const container = (e.target as HTMLElement).closest('.mermaid-container') as HTMLElement | null
    if (!container) return
    e.preventDefault()
    e.stopPropagation()

    const wrap = container.querySelector('.mermaid-svg-wrap') as HTMLElement | null
    if (!wrap) return

    const oldScale = parseFloat(wrap.dataset.scale || '1')
    const delta = e.deltaY > 0 ? -0.1 : 0.1
    const newScale = Math.min(15, Math.max(0.25, +(oldScale + delta).toFixed(2)))
    if (newScale === oldScale) return

    const cw = container.querySelector('.mermaid-content-wrapper') as HTMLElement | null
    const rect = cw ? cw.getBoundingClientRect() : container.getBoundingClientRect()
    const mx = rect.width / 2
    const my = rect.height / 2

    const oldTx = parseFloat(wrap.dataset.translateX || '0')
    const oldTy = parseFloat(wrap.dataset.translateY || '0')

    const ratio = newScale / oldScale
    const newTx = mx - (mx - oldTx) * ratio
    const newTy = my - (my - oldTy) * ratio

    wrap.dataset.scale = String(newScale)
    wrap.dataset.translateX = String(+newTx.toFixed(1))
    wrap.dataset.translateY = String(+newTy.toFixed(1))
    updateMermaidWrapTransform(wrap)
  }

  function handleMermaidMouseDown(e: MouseEvent): void {
    if (e.button !== 0) return
    const container = (e.target as HTMLElement).closest('.mermaid-container') as HTMLElement | null
    if (!container) return
    if ((e.target as HTMLElement).closest('.mermaid-toolbar')) return
    if ((e.target as HTMLElement).closest('.mermaid-resize-handle')) return

    const btn = (e.target as HTMLElement).closest('.mermaid-tb-btn')
    if (btn) return

    const wrap = container.querySelector('.mermaid-svg-wrap') as HTMLElement | null
    if (!wrap) return

    wrap.classList.remove('smooth-transition')

    e.preventDefault()
    mermaidDragState.isDragging = true
    mermaidDragState.startX = e.clientX
    mermaidDragState.startY = e.clientY
    mermaidDragState.startTx = parseFloat(wrap.dataset.translateX || '0')
    mermaidDragState.startTy = parseFloat(wrap.dataset.translateY || '0')
    mermaidDragState.activeContainer = container

    document.body.classList.add('mermaid-dragging')
  }

  function handleMermaidToolbarClick(e: MouseEvent): void {
    const btn = (e.target as HTMLElement).closest('.mermaid-tb-btn[data-action]') as HTMLElement | null
    if (!btn) return
    const container = btn.closest('.mermaid-container') as HTMLElement | null
    if (!container) return
    const wrap = container.querySelector('.mermaid-svg-wrap') as HTMLElement | null
    const action = btn.dataset.action

    e.stopPropagation()

    switch (action) {
      case 'zoom-in': {
        if (!wrap) break
        wrap.classList.add('smooth-transition')
        const oldScale = parseFloat(wrap.dataset.scale || '1')
        const s = Math.min(15, +(oldScale + 0.25).toFixed(2))
        if (s !== oldScale) {
          const cw = container.querySelector('.mermaid-content-wrapper') as HTMLElement | null
          const rect = cw ? cw.getBoundingClientRect() : container.getBoundingClientRect()
          const mx = rect.width / 2
          const my = rect.height / 2
          const oldTx = parseFloat(wrap.dataset.translateX || '0')
          const oldTy = parseFloat(wrap.dataset.translateY || '0')
          const ratio = s / oldScale
          wrap.dataset.scale = String(s)
          wrap.dataset.translateX = String(+(mx - (mx - oldTx) * ratio).toFixed(1))
          wrap.dataset.translateY = String(+(my - (my - oldTy) * ratio).toFixed(1))
        }
        updateMermaidWrapTransform(wrap)
        setTimeout(() => wrap.classList.remove('smooth-transition'), 200)
        break
      }
      case 'zoom-out': {
        if (!wrap) break
        wrap.classList.add('smooth-transition')
        const oldScale = parseFloat(wrap.dataset.scale || '1')
        const s = Math.max(0.25, +(oldScale - 0.25).toFixed(2))
        if (s !== oldScale) {
          const cw = container.querySelector('.mermaid-content-wrapper') as HTMLElement | null
          const rect = cw ? cw.getBoundingClientRect() : container.getBoundingClientRect()
          const mx = rect.width / 2
          const my = rect.height / 2
          const oldTx = parseFloat(wrap.dataset.translateX || '0')
          const oldTy = parseFloat(wrap.dataset.translateY || '0')
          const ratio = s / oldScale
          wrap.dataset.scale = String(s)
          wrap.dataset.translateX = String(+(mx - (mx - oldTx) * ratio).toFixed(1))
          wrap.dataset.translateY = String(+(my - (my - oldTy) * ratio).toFixed(1))
        }
        updateMermaidWrapTransform(wrap)
        setTimeout(() => wrap.classList.remove('smooth-transition'), 200)
        break
      }
      case 'zoom-reset': {
        if (!wrap) break
        wrap.classList.add('smooth-transition')
        wrap.dataset.scale = '1'
        wrap.dataset.translateX = '0'
        wrap.dataset.translateY = '0'
        updateMermaidWrapTransform(wrap)
        setTimeout(() => wrap.classList.remove('smooth-transition'), 200)
        break
      }
      case 'fullscreen': {
        handleMermaidFullscreen(container)
        break
      }
      case 'toggle-source': {
        const src = container.querySelector('.mermaid-source-preview') as HTMLElement | null
        if (!src || !wrap) break
        const showing = src.style.display !== 'none'
        src.style.display = showing ? 'none' : 'block'
        wrap.style.display = showing ? 'block' : 'none'
        break
      }
      case 'copy-source': {
        const code = container.querySelector('.mermaid-source-preview code') as HTMLElement | null
        if (!code) break
        navigator.clipboard.writeText(code.textContent || '').then(() => {
          const origHtml = btn.innerHTML
          btn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#52c41a" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>'
          setTimeout(() => { btn.innerHTML = origHtml }, 1200)
        }).catch(() => { /* ignore */ })
        break
      }
      case 'export-png': {
        exportMermaidAsPNG(container)
        break
      }
      case 'export-svg': {
        exportMermaidAsSVG(container)
        break
      }
      case 'exit-fullscreen': {
        exitMermaidFullscreen()
        break
      }
    }
  }

  function exportMermaidAsSVG(container: HTMLElement): void {
    try {
      const svg = container.querySelector('.mermaid-svg-wrap svg') as SVGElement | null
      if (!svg) {
        ElMessage.error('未找到可导出的 SVG 图表')
        return
      }
      const svgString = new XMLSerializer().serializeToString(svg)
      const blob = new Blob([svgString], { type: 'image/svg+xml;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `mermaid_${Date.now()}.svg`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch (e) {
      console.error('导出 SVG 失败:', e)
      ElMessage.error('导出 SVG 失败')
    }
  }

  function exportMermaidAsPNG(container: HTMLElement): void {
    try {
      const svg = container.querySelector('.mermaid-svg-wrap svg') as SVGSVGElement | null
      if (!svg) {
        ElMessage.error('未找到可导出的 SVG 图表')
        return
      }
      const svgString = new XMLSerializer().serializeToString(svg)
      const dataUrl = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svgString)

      const img = new Image()
      img.onload = () => {
        const width = svg.width.baseVal.value || svg.getBoundingClientRect().width || 800
        const height = svg.height.baseVal.value || svg.getBoundingClientRect().height || 600
        const canvas = document.createElement('canvas')
        const scale = 2
        canvas.width = width * scale
        canvas.height = height * scale
        const ctx = canvas.getContext('2d')!
        ctx.fillStyle = '#ffffff'
        ctx.fillRect(0, 0, canvas.width, canvas.height)
        ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
        canvas.toBlob((blob) => {
          if (!blob) {
            ElMessage.error('生成 PNG 失败')
            return
          }
          const pngUrl = URL.createObjectURL(blob)
          const a = document.createElement('a')
          a.href = pngUrl
          a.download = `mermaid_${Date.now()}.png`
          document.body.appendChild(a)
          a.click()
          document.body.removeChild(a)
          URL.revokeObjectURL(pngUrl)
        }, 'image/png')
      }
      img.onerror = () => { ElMessage.error('加载 SVG 图像失败') }
      img.src = dataUrl
    } catch (e) {
      console.error('导出 PNG 失败:', e)
      ElMessage.error('导出 PNG 失败')
    }
  }

  function handleMermaidFullscreen(container: HTMLElement): void {
    const sourceEl = container.querySelector('.mermaid-source-preview code') as HTMLElement | null
    const svgWrap = container.querySelector('.mermaid-svg-wrap') as HTMLElement | null
    if (!svgWrap && !sourceEl) return

    const svgHtml = svgWrap ? svgWrap.innerHTML : ''
    const source = sourceEl ? sourceEl.textContent : ''

    const overlay = document.createElement('div')
    overlay.className = 'mermaid-fullscreen-overlay'
    overlay.innerHTML = `
      <div class="mermaid-fullscreen-container" data-mermaid-fullscreen="true">
        <div class="mermaid-fullscreen-content">
          <div class="mermaid-svg-wrap" data-scale="3" data-translate-x="0" data-translate-y="0">${svgHtml}</div>
        </div>
        <div class="mermaid-fullscreen-toolbar">
          <button class="mermaid-tb-btn" data-action="zoom-out" title="缩小">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="8" y1="11" x2="14" y2="11"/></svg>
          </button>
          <button class="mermaid-tb-btn" data-action="zoom-reset" title="重置">100%</button>
          <button class="mermaid-tb-btn" data-action="zoom-in" title="放大">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>
          </button>
          <button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
          </button>
          <button class="mermaid-tb-btn" data-action="export-png" title="导出 PNG">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          </button>
          <button class="mermaid-tb-btn" data-action="export-svg" title="导出 SVG">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="12" y1="18" x2="12" y2="12"/><polyline points="9 15 12 18 15 15"/></svg>
          </button>
          <button class="mermaid-tb-btn mermaid-tb-btn-exit" data-action="exit-fullscreen" title="退出全屏 (Esc)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
          </button>
        </div>
      </div>
    `

    ;(overlay as any)._mermaidSource = source

    document.body.appendChild(overlay)
    document.body.classList.add('mermaid-fullscreen-active')

    const fsContent = overlay.querySelector('.mermaid-fullscreen-content') as HTMLElement

    function updateFsTransform(fsWrap: HTMLElement): void {
      const s = parseFloat(fsWrap.dataset.scale || '1')
      const tx = parseFloat(fsWrap.dataset.translateX || '0')
      const ty = parseFloat(fsWrap.dataset.translateY || '0')
      fsWrap.style.transform = `translate(${tx}px, ${ty}px) scale(${s})`
    }

    const initialFsWrap = overlay.querySelector('.mermaid-svg-wrap') as HTMLElement | null
    if (initialFsWrap) updateFsTransform(initialFsWrap)

    overlay.addEventListener('click', (e: MouseEvent) => {
      const fsBtn = (e.target as HTMLElement).closest('.mermaid-tb-btn[data-action]') as HTMLElement | null
      if (!fsBtn) return
      e.stopPropagation()
      const fsAction = fsBtn.dataset.action
      const fsWrap = overlay.querySelector('.mermaid-svg-wrap') as HTMLElement | null

      switch (fsAction) {
        case 'zoom-in': {
          if (!fsWrap) break
          fsWrap.classList.add('smooth-transition')
          const s = Math.min(5, +(parseFloat(fsWrap.dataset.scale || '1') + 0.25).toFixed(2))
          fsWrap.dataset.scale = String(s)
          updateFsTransform(fsWrap)
          setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
          break
        }
        case 'zoom-out': {
          if (!fsWrap) break
          fsWrap.classList.add('smooth-transition')
          const s = Math.max(0.25, +(parseFloat(fsWrap.dataset.scale || '1') - 0.25).toFixed(2))
          fsWrap.dataset.scale = String(s)
          updateFsTransform(fsWrap)
          setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
          break
        }
        case 'zoom-reset': {
          if (!fsWrap) break
          fsWrap.classList.add('smooth-transition')
          fsWrap.dataset.scale = '1'
          fsWrap.dataset.translateX = '0'
          fsWrap.dataset.translateY = '0'
          updateFsTransform(fsWrap)
          setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
          break
        }
        case 'copy-source': {
          const src = (overlay as any)._mermaidSource || ''
          if (!src) break
          navigator.clipboard.writeText(src).then(() => {
            const origHtml = fsBtn.innerHTML
            fsBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#52c41a" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>'
            setTimeout(() => { fsBtn.innerHTML = origHtml }, 1200)
          }).catch(() => { /* ignore */ })
          break
        }
        case 'export-png': {
          exportMermaidAsPNG(overlay.querySelector('.mermaid-fullscreen-container') as HTMLElement)
          break
        }
        case 'export-svg': {
          exportMermaidAsSVG(overlay.querySelector('.mermaid-fullscreen-container') as HTMLElement)
          break
        }
        case 'exit-fullscreen': {
          exitMermaidFullscreen()
          break
        }
      }
    })

    const fsDrag = { isDragging: false, startX: 0, startY: 0, startTx: 0, startTy: 0 }

    fsContent.addEventListener('mousedown', (e: MouseEvent) => {
      if (e.button !== 0) return
      if ((e.target as HTMLElement).closest('.mermaid-fullscreen-toolbar')) return
      const fsWrap = overlay.querySelector('.mermaid-svg-wrap') as HTMLElement | null
      if (!fsWrap) return
      fsWrap.classList.remove('smooth-transition')
      e.preventDefault()
      fsDrag.isDragging = true
      fsDrag.startX = e.clientX
      fsDrag.startY = e.clientY
      fsDrag.startTx = parseFloat(fsWrap.dataset.translateX || '0')
      fsDrag.startTy = parseFloat(fsWrap.dataset.translateY || '0')
      fsContent.style.cursor = 'grabbing'
    })

    const fsMouseMove = (e: MouseEvent) => {
      if (!fsDrag.isDragging) return
      const fsWrap = overlay.querySelector('.mermaid-svg-wrap') as HTMLElement | null
      if (!fsWrap) return
      const dx = e.clientX - fsDrag.startX
      const dy = e.clientY - fsDrag.startY
      fsWrap.dataset.translateX = String(+(fsDrag.startTx + dx).toFixed(1))
      fsWrap.dataset.translateY = String(+(fsDrag.startTy + dy).toFixed(1))
      updateFsTransform(fsWrap)
    }

    const fsMouseUp = () => {
      if (!fsDrag.isDragging) return
      fsDrag.isDragging = false
      fsContent.style.cursor = 'grab'
    }

    document.addEventListener('mousemove', fsMouseMove)
    document.addEventListener('mouseup', fsMouseUp)

    fsContent.addEventListener('wheel', (e: WheelEvent) => {
      if (!e.ctrlKey) return
      e.preventDefault()
      const fsWrap = overlay.querySelector('.mermaid-svg-wrap') as HTMLElement | null
      if (!fsWrap) return
      fsWrap.classList.add('smooth-transition')
      const oldScale = parseFloat(fsWrap.dataset.scale || '1')
      const delta = e.deltaY > 0 ? -0.1 : 0.1
      const newScale = Math.min(15, Math.max(0.25, +(oldScale + delta).toFixed(2)))
      if (newScale === oldScale) return
      fsWrap.dataset.scale = String(newScale)
      updateFsTransform(fsWrap)
      setTimeout(() => fsWrap.classList.remove('smooth-transition'), 200)
    }, { passive: false })

    ;(overlay as any)._cleanup = () => {
      document.removeEventListener('mousemove', fsMouseMove)
      document.removeEventListener('mouseup', fsMouseUp)
    }

    overlay.addEventListener('mousedown', (e: MouseEvent) => {
      if (e.target === overlay) exitMermaidFullscreen()
    })
  }

  function exitMermaidFullscreen(): void {
    const overlay = document.querySelector('.mermaid-fullscreen-overlay') as (HTMLElement & { _cleanup?: () => void }) | null
    if (!overlay) return
    if (overlay._cleanup) overlay._cleanup()
    overlay.remove()
    document.body.classList.remove('mermaid-fullscreen-active')
  }

  function handleMermaidMouseMove(e: MouseEvent): void {
    if (!mermaidDragState.isDragging) return
    const container = mermaidDragState.activeContainer
    if (!container) return
    const wrap = container.querySelector('.mermaid-svg-wrap') as HTMLElement | null
    if (!wrap) return
    const dx = e.clientX - mermaidDragState.startX
    const dy = e.clientY - mermaidDragState.startY
    wrap.dataset.translateX = String(+(mermaidDragState.startTx + dx).toFixed(1))
    wrap.dataset.translateY = String(+(mermaidDragState.startTy + dy).toFixed(1))
    updateMermaidWrapTransform(wrap)
  }

  function handleMermaidMouseUp(): void {
    if (!mermaidDragState.isDragging) return
    mermaidDragState.isDragging = false
    mermaidDragState.activeContainer = null
    document.body.classList.remove('mermaid-dragging')
  }

  function handleMermaidKeyDown(e: KeyboardEvent): void {
    if (e.key === 'Control' && !e.repeat) {
      document.body.classList.add('mermaid-ctrl-held')
    }
  }

  function handleMermaidKeyUp(e: KeyboardEvent): void {
    if (e.key === 'Control') {
      document.body.classList.remove('mermaid-ctrl-held')
    }
  }

  function handleMermaidResizeDown(e: MouseEvent): void {
    if (e.button !== 0) return
    const handle = (e.target as HTMLElement).closest('.mermaid-resize-handle') as HTMLElement | null
    if (!handle) return
    const container = handle.closest('.mermaid-container') as HTMLElement | null
    if (!container) return
    const wrapper = container.querySelector('.mermaid-content-wrapper') as HTMLElement | null
    if (!wrapper) return
    const mermaidId = container.getAttribute('data-mermaid-id')
    if (!mermaidId) return

    e.preventDefault()
    e.stopPropagation()

    mermaidResizeState.isResizing = true
    mermaidResizeState.startY = e.clientY
    mermaidResizeState.startHeight = wrapper.offsetHeight
    mermaidResizeState.activeMermaidId = mermaidId

    document.body.classList.add('mermaid-resizing')
  }

  function handleMermaidResizeMove(e: MouseEvent): void {
    if (!mermaidResizeState.isResizing) return
    const mermaidId = mermaidResizeState.activeMermaidId
    if (!mermaidId) return
    const dy = e.clientY - mermaidResizeState.startY
    const newHeight = Math.max(100, mermaidResizeState.startHeight + dy)
    mermaidCustomHeights.set(mermaidId, newHeight)
    applyMermaidCustomHeight(mermaidId, newHeight)
  }

  function handleMermaidResizeUp(): void {
    if (!mermaidResizeState.isResizing) return
    mermaidResizeState.isResizing = false
    mermaidResizeState.activeMermaidId = null
    document.body.classList.remove('mermaid-resizing')
  }

  function applyMermaidCustomHeight(mermaidId: string, height: number): void {
    const container = document.querySelector(`.mermaid-container[data-mermaid-id="${mermaidId}"]`) as HTMLElement | null
    if (!container) return
    const wrapper = container.querySelector('.mermaid-content-wrapper') as HTMLElement | null
    if (wrapper) {
      wrapper.style.height = height + 'px'
      wrapper.style.maxHeight = 'none'
    }
    container.style.maxHeight = 'none'
  }

  function reapplyAllMermaidCustomHeights(): void {
    for (const [mermaidId, height] of mermaidCustomHeights) {
      applyMermaidCustomHeight(mermaidId, height)
    }
  }

  function handleEscKey(e: KeyboardEvent): void {
    if (e.key === 'Escape' || e.keyCode === 27) {
      const overlay = document.querySelector('.mermaid-fullscreen-overlay')
      if (overlay) {
        e.preventDefault()
        e.stopPropagation()
        exitMermaidFullscreen()
      }
    }
  }

  /** 全局点击：处理导出链接鉴权 */
  function handleExportLinkClick(e: MouseEvent): void {
    const link = (e.target as HTMLElement).closest('a[data-export-link]') as HTMLAnchorElement | null
    if (!link) return
    const authToken = sessionStorage.getItem('authentication')
    if (!authToken) return
    e.preventDefault()
    let href = link.getAttribute('href') || ''
    if (href.startsWith('/exports/') && !href.startsWith('/api/exports/')) {
      href = apiBase + href
    }
    const separator = href.includes('?') ? '&' : '?'
    href = href + separator + 'token=' + encodeURIComponent(authToken)
    window.open(href, '_blank')
  }

  /** 全局点击：处理代码块复制按钮 */
  function handleCodeCopyClick(e: MouseEvent): void {
    const btn = (e.target as HTMLElement).closest('.code-copy-btn') as HTMLElement | null
    if (!btn) return
    const encoded = btn.dataset.code
    if (!encoded) return
    try {
      const code = decodeURIComponent(atob(encoded))
      navigator.clipboard.writeText(code).then(() => {
        const orig = btn.textContent
        btn.textContent = '✓'
        setTimeout(() => { btn.textContent = orig }, 1500)
      })
    } catch (_) { /* ignore */ }
  }

  return {
    // 状态
    mdReady,
    hljsReady,
    // 初始化
    ensureMd,
    initHeavyDeps,
    // 渲染
    renderMarkdown,
    getCachedHtml,
    highlightSql,
    preprocessMarkdown,
    autoWrapMermaidCode,
    doRenderMermaidBlocks,
    countClosedMermaidBlocks,
    invalidateMermaidMsgCache,
    // Mermaid observer
    setupMermaidObserver,
    teardownMermaidObserver,
    // Mermaid 交互事件（绑定到 document）
    handleMermaidWheel,
    handleMermaidMouseDown,
    handleMermaidToolbarClick,
    handleMermaidMouseMove,
    handleMermaidMouseUp,
    handleMermaidKeyDown,
    handleMermaidKeyUp,
    handleMermaidResizeDown,
    handleMermaidResizeMove,
    handleMermaidResizeUp,
    handleEscKey,
    // 全局点击
    handleExportLinkClick,
    handleCodeCopyClick,
    // 全屏
    exitMermaidFullscreen,
  }
}
