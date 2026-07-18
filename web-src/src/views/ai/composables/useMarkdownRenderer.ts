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
    await switchMermaidTheme(theme === 'dark' ? 'dark' : 'light')
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
            // 注意：不能在此处使用 svgCache 命中分支返回缓存的 SVG HTML。
            // 原因：本函数返回的 HTML 会经过 sanitizeHtml (DOMPurify)，
            // 其配置 FORBID_TAGS: ['script', 'style'] 会移除 mermaid SVG 中的 <style> 标签。
            // xychart-beta / pie 等图表的 SVG 高度依赖 <style> 定义字体、颜色、动画、布局，
            // 移除后图表样式会严重错乱，表现为"不能渲染"。
            // 因此统一返回 data-mermaid-processed="false" 占位符，
            // 由 renderSingleMermaid 直接 el.innerHTML 注入缓存（绕过 sanitizeHtml）。
            //
            // 另外，data-mermaid-source 属性值必须使用 base64 编码（而非 HTML 实体转义）。
            // 原因：DOMPurify 会检测属性值中的 <br/>、--> 等模式（即使已转义为
            // &lt;br/&gt;、--&gt;，DOMPurify 解析属性时会先解码实体再判断），并直接移除
            // 整个 data-mermaid-source 属性，导致 renderSingleMermaid 取不到源码，
            // graph TD/LR、xychart-beta 等含 <br/> 或 --> 的图表无法渲染。
            // base64 为纯 ASCII，DOMPurify 不会破坏。
            const source = token.content.trim()
            const id = getNextMermaidId()
            let encodedSource = ''
            try { encodedSource = btoa(encodeURIComponent(source)) } catch (_) { /* ignore */ }
            const escaped = token.content
              .replace(/&/g, '&amp;')
              .replace(/</g, '&lt;')
              .replace(/>/g, '&gt;')
              .replace(/"/g, '&quot;')
            return `<div class="mermaid-container" data-mermaid-id="${id}" data-mermaid-source="${encodedSource}" data-mermaid-processed="false"><pre class="mermaid-source-preview"><code>📊 Mermaid\n${escaped}</code></pre></div>`
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

  /**
   * Mermaid v11.16 支持的所有图表类型关键字（用于自动检测 / 校验）
   * 包含: flowchart, graph, sequence, class, state, er, gantt, pie, gitGraph,
   *       journey, mindmap, timeline, quadrantChart, sankey, xychart-beta,
   *       block-beta, packet-beta, architecture-beta, C4Context/C4Container/
   *       C4Component/C4Deployment/C4Dynamic, requirement, zenuml, kanban,
   *       radar-beta, ishikawa(-beta), venn-beta, treemap, wardley-beta,
   *       swimlane-beta, cynefin-beta, railroad-beta 系列
   */
  const MERMAID_SUPPORTED_KEYWORDS = /^(graph\s+(TD|TB|BT|RL|LR)|flowchart\s+(TD|TB|BT|RL|LR)|sequenceDiagram|classDiagram|classDiagram-v2|stateDiagram|stateDiagram-v2|erDiagram|gantt|pie|gitGraph|journey|mindmap|timeline|quadrantChart|sankey-beta|sankey|xychart-beta|xychart|block-beta|packet-beta|architecture-beta|C4Context|C4Container|C4Component|C4Deployment|C4Dynamic|requirementDiagram|requirement|zenuml|kanban|radar-beta|ishikawa(-beta)?|venn-beta|treemap|wardley-beta|swimlane-beta|cynefin-beta|railroad-beta|railroad-ebnf-beta|railroad-abnf-beta|railroad-peg-beta)/m

  /**
   * AI 可能生成但 Mermaid v11.16 仍不支持的图表类型
   * 这些类型需要以友好的 fallback 卡片渲染，而非报错
   */
  const UNSUPPORTED_DIAGRAM_KEYWORDS = /^(funnel|treeview)/m

  /** 自动检测未被 code fence 包裹的 mermaid 代码并包裹 */
  function autoWrapMermaidCode(text: string): string {
    if (!text) return ''

    // 同时检测支持的和不支持的类型，都作为 mermaid fence 包裹
    const allKeywords = new RegExp(
      MERMAID_SUPPORTED_KEYWORDS.source + '|' + UNSUPPORTED_DIAGRAM_KEYWORDS.source, 'm'
    )

    // 如果已经全部包裹在 code fence 中，跳过
    // 但如果还有未包裹的 mermaid 关键字，继续处理
    const fenceBlocks: { start: number; end: number }[] = []
    const fenceRegex = /```[\s\S]*?```/g
    let fenceMatch: RegExpExecArray | null
    while ((fenceMatch = fenceRegex.exec(text)) !== null) {
      fenceBlocks.push({ start: fenceMatch.index, end: fenceMatch.index + fenceMatch[0].length })
    }

    function isInsideFence(idx: number): boolean {
      return fenceBlocks.some(b => idx >= b.start && idx < b.end)
    }

    // 多次迭代处理所有未包裹的 mermaid 代码块
    let result = text
    let safetyCounter = 0
    const MAX_ITERATIONS = 10

    while (safetyCounter++ < MAX_ITERATIONS) {
      // 重新计算 fence 位置（每次迭代文本可能变化）
      const currentFences: { start: number; end: number }[] = []
      const currentFenceRegex = /```[\s\S]*?```/g
      let cf: RegExpExecArray | null
      while ((cf = currentFenceRegex.exec(result)) !== null) {
        currentFences.push({ start: cf.index, end: cf.index + cf[0].length })
      }

      // 用全局搜索找到第一个不在 fence 内的关键字
      const globalKw = new RegExp(allKeywords.source, 'gm')
      let match: RegExpExecArray | null
      let wrapIdx = -1
      while ((match = globalKw.exec(result)) !== null) {
        const mIdx = match.index
        const inside = currentFences.some(b => mIdx >= b.start && mIdx < b.end)
        if (!inside) {
          wrapIdx = mIdx
          break
        }
        // 跳过包含该关键字的 fence
        const enclosingFence = currentFences.find(b => mIdx >= b.start && mIdx < b.end)
        if (enclosingFence) {
          globalKw.lastIndex = enclosingFence.end
        }
      }

      if (wrapIdx === -1) break
      result = wrapSingleMermaidBlock(result, wrapIdx, allKeywords)
    }

    return result
  }

  /** 包裹单个位于 startIdx 处的 mermaid 代码块 */
  function wrapSingleMermaidBlock(text: string, startIdx: number, _allKeywords: RegExp): string {
    // 如果此关键字前面紧邻有 %%{init:...}%% 指令，将其纳入 mermaid 块
    let actualStart = startIdx
    const textBefore = text.substring(0, startIdx).trimEnd()
    // 查找最近的 %%{init}%% 指令（在 textBefore 末尾）
    const initEndIdx = textBefore.lastIndexOf('}%%')
    if (initEndIdx !== -1) {
      const initStartSearch = textBefore.lastIndexOf('%%{init', initEndIdx)
      if (initStartSearch !== -1) {
        // 检查 init 指令和关键字之间只有空白
        const afterInit = textBefore.substring(initEndIdx + 3) // after '}%%'
        if (/^\s*$/.test(afterInit)) {
          // 确保 init 不在已有 fence 内部
          const betweenFromInit = textBefore.substring(initStartSearch)
          if (!betweenFromInit.includes('```')) {
            actualStart = initStartSearch
          }
        }
      }
    }

    const before = text.substring(0, actualStart).trimEnd()
    const afterStart = text.substring(actualStart)

    const lines = afterStart.split('\n')
    let endLineIdx = lines.length
    let foundEmptyLine = false
    let consecutiveEmpty = 0

    for (let i = 1; i < lines.length; i++) {
      const line = lines[i]
      const trimmedLine = line.trim()

      // 遇到已有的 code fence 标记（来自前一轮包裹）立即停止
      if (/^```/.test(trimmedLine)) {
        endLineIdx = i
        break
      }

      if (trimmedLine === '') {
        consecutiveEmpty++
        foundEmptyLine = true
        // 连续两个空行 → 明确的段落分隔
        if (consecutiveEmpty >= 2) {
          endLineIdx = i - 1
          break
        }
        continue
      }
      consecutiveEmpty = 0

      if (foundEmptyLine) {
        // 先检测是否是新的图表起始关键字（优先级最高）
        const combinedKeywords = new RegExp(
          MERMAID_SUPPORTED_KEYWORDS.source + '|' + UNSUPPORTED_DIAGRAM_KEYWORDS.source, 'm'
        )
        const isNewDiagram = combinedKeywords.test(trimmedLine)
        if (isNewDiagram) {
          endLineIdx = i
          break
        }
        // 空行之后的 %% 指令通常是下一个图的 %%{init}%% 而非当前图的注释，
        // 因此不将 %% 视为 mermaid 延续行，以正确分割相邻图表
        //
        // 不将 %% 开头的行视为空行后的延续行：
        // - %%{init}%% 属于下一个图，由 wrapSingleMermaidBlock 的 actualStart 反向查找关联
        // - 普通 %% 注释在空行后极少出现在当前图内
        if (/^%%/.test(trimmedLine)) {
          endLineIdx = i
          break
        }
        const hasArrowSyntax = /-->|---|==>|\.\-\>|==\.|\|/.test(trimmedLine)
        const hasNodeBracket = /[\[\]\(\)\{\}]/.test(trimmedLine)
        const isMermaidKeyword = /^(style|classDef|click|linkStyle|subgraph|end|class\s|section|title|accTitle|accDescr|direction|root)\b/.test(trimmedLine)
        const isMermaidLine = hasArrowSyntax || hasNodeBracket || isMermaidKeyword
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

  /**
   * 检测 mermaid 源码是否属于不支持的图表类型。
   * 如果是，返回匹配到的类型名称；否则返回 null。
   */
  function detectUnsupportedDiagramType(source: string): string | null {
    const match = source.match(UNSUPPORTED_DIAGRAM_KEYWORDS)
    return match ? match[1] : null
  }

  /** 为不支持的图表类型构建友好的 fallback HTML 卡片 */
  function buildUnsupportedFallbackHtml(source: string, diagramType: string): string {
    const escapedSource = source.replace(/</g, '&lt;').replace(/>/g, '&gt;')
    // 提取 title（如果有）
    const titleMatch = source.match(/(?:^|\n)\s*title\s+"?([^"\n]+)"?/i)
    const title = titleMatch ? titleMatch[1].trim() : ''
    // 提取数据行
    const dataLines = source.split('\n')
      .filter(line => {
        const t = line.trim()
        return t && !t.startsWith('%%') && !UNSUPPORTED_DIAGRAM_KEYWORDS.test(t) && !/^\s*title\s/i.test(t)
      })
      .slice(0, 12)

    // 解析 key-value 数据
    interface DataItem { label: string; value: number; rawValue: string }
    const items: DataItem[] = []
    for (const line of dataLines) {
      const t = line.trim()
      const kvMatch = t.match(/^"([^"]+)"\s*:\s*(.+)$/)
      if (kvMatch) {
        const numVal = parseFloat(kvMatch[2].trim())
        items.push({ label: kvMatch[1], value: isNaN(numVal) ? 0 : numVal, rawValue: kvMatch[2].trim() })
      }
    }

    // 色板（与 mermaid 主色板一致）
    const colors = ['#4d8fdb', '#7c6bc4', '#3daa7e', '#e8a838', '#d45d8a', '#3db5c4', '#6366f1', '#4abf8a', '#e88838', '#a855f7', '#22bfcf', '#7dc428']
    const maxVal = Math.max(...items.map(d => d.value), 1)

    // 根据类型选择图标
    const iconMap: Record<string, string> = { funnel: '🔻', treeview: '🌳' }
    const icon = iconMap[diagramType] || '📊'

    // 构建可视化漏斗/条形图 HTML
    let barsHtml = ''
    if (items.length > 0) {
      const isFunnel = diagramType === 'funnel'
      barsHtml = items.map((item, i) => {
        const pct = maxVal > 0 ? (item.value / maxVal) * 100 : 0
        const width = isFunnel ? Math.max(pct, 18) : Math.max(pct, 12)
        const color = colors[i % colors.length]
        const barStyle = isFunnel
          ? `width:${width}%;background:${color};border-radius:${6 + i}px;margin:0 auto;`
          : `width:${width}%;background:linear-gradient(90deg, ${color} 0%, ${color}dd 100%);border-radius:8px;`
        return `<div class="mermaid-viz-bar-row${isFunnel ? ' funnel' : ''}">` +
          `<div class="mermaid-viz-bar-label">${item.label.replace(/</g, '&lt;').replace(/<br\/?>/gi, ' ')}</div>` +
          `<div class="mermaid-viz-bar-track">` +
            `<div class="mermaid-viz-bar-fill" style="${barStyle}">` +
              `<span class="mermaid-viz-bar-value">${item.rawValue}</span>` +
            `</div>` +
          `</div>` +
        `</div>`
      }).join('')
    } else {
      // 没有可解析的 key-value 数据，显示原始行
      barsHtml = dataLines.map(line => {
        const t = line.trim()
        return `<div class="mermaid-viz-bar-row"><div class="mermaid-viz-bar-label" style="flex:1">${t.replace(/</g, '&lt;')}</div></div>`
      }).join('')
    }

    return `<div class="mermaid-content-wrapper">` +
      `<div class="mermaid-unsupported-card">` +
        `<div class="mermaid-unsupported-header">` +
          `<span class="mermaid-unsupported-icon">${icon}</span>` +
          `<div class="mermaid-unsupported-header-text">` +
            `<span class="mermaid-unsupported-type">${diagramType.charAt(0).toUpperCase() + diagramType.slice(1)} Chart</span>` +
            (title ? `<span class="mermaid-unsupported-title">${title.replace(/</g, '&lt;').replace(/>/g, '&gt;')}</span>` : '') +
          `</div>` +
        `</div>` +
        `<div class="mermaid-unsupported-body">` +
          `<div class="mermaid-viz-bars">${barsHtml}</div>` +
        `</div>` +
        `<div class="mermaid-unsupported-notice">` +
          `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 16v.01M12 8v4"/></svg>` +
          `<span>Mermaid v11 暂不支持 ${diagramType} 类型，数据以可视化形式呈现</span>` +
        `</div>` +
      `</div>` +
      `<pre class="mermaid-source-preview" style="display:none;"><code>${escapedSource}</code></pre>` +
    `</div>` +
    `<div class="mermaid-toolbar">` +
      `<button class="mermaid-tb-btn" data-action="toggle-source" title="源码/图表">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>` +
      `</button>` +
      `<button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>` +
      `</button>` +
    `</div>`
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

  /** 后置修正 mermaid SVG 中的深色矩形填充 + 注入美化滤镜 */
  function patchMermaidSvgColors(container: HTMLElement): void {
    const isDark = document.documentElement.getAttribute('data-theme') === 'dark'
    const lineColor = isDark ? '#7ba4e8' : '#4d8fdb'
    const textColor = isDark ? '#e8ecf4' : '#1a2332'
    // ── 颜色检测辅助函数 ──
    function isDarkColor(color: string, threshold: number = 60): boolean {
      if (!color || color === 'none' || color === 'transparent') return false
      if (/^(#000|#000000|black|#1f1f1f|#1f2020|#0d0d0d|#111|#222|#333|#1a1a2e|#2c3e50|#34495e|#16213e)$/i.test(color)) return true
      const hex6 = color.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i)
      if (hex6) {
        const r = parseInt(hex6[1], 16), g = parseInt(hex6[2], 16), b = parseInt(hex6[3], 16)
        return r <= threshold && g <= threshold && b <= threshold
      }
      const hex3 = color.match(/^#([0-9a-f])([0-9a-f])([0-9a-f])$/i)
      if (hex3) {
        const r = parseInt(hex3[1] + hex3[1], 16), g = parseInt(hex3[2] + hex3[2], 16), b = parseInt(hex3[3] + hex3[3], 16)
        return r <= threshold && g <= threshold && b <= threshold
      }
      const rgbMatch = color.match(/rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/)
      if (rgbMatch) return (+rgbMatch[1] <= threshold && +rgbMatch[2] <= threshold && +rgbMatch[3] <= threshold)
      return false
    }
    function isDarkFill(fill: string): boolean { return isDarkColor(fill, 60) }
    function hasUserFill(el: Element): boolean {
      const attrFill = el.getAttribute('fill') || ''
      if (attrFill && !isDarkFill(attrFill) && attrFill !== 'none' && attrFill !== 'transparent') return true
      return !!(el as SVGElement).style?.fill
    }
    function hasUserStroke(el: Element): boolean { return !!(el as SVGElement).style?.stroke }

    const svgEl = container.querySelector('svg')
    if (!svgEl) return

    const bgColor = isDark ? '#181825' : '#ffffff'
    const nodeFill = isDark ? '#2a2d42' : '#e8f1fd'
    const nodeStroke = isDark ? '#6e9cf5' : '#a8ccf5'
    const taskFill = isDark ? '#5b8def' : '#e8f1fd'
    const gridColor = isDark ? '#2a2d42' : '#eef3f9'

    // ── 注入 SVG <defs>：阴影滤镜 ──
    const defsId = 'mermaid-elegant-defs'
    if (!svgEl.querySelector(`#${defsId}`)) {
      const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs')
      defs.id = defsId
      // 节点柔和阴影
      const shadowColor = isDark ? 'rgba(0,0,0,0.35)' : 'rgba(77,143,219,0.12)'
      defs.innerHTML = `
        <filter id="mermaid-node-shadow" x="-8%" y="-8%" width="116%" height="124%">
          <feDropShadow dx="0" dy="2" stdDeviation="3" flood-color="${shadowColor}" flood-opacity="1"/>
        </filter>
        <filter id="mermaid-glow" x="-10%" y="-10%" width="120%" height="120%">
          <feGaussianBlur in="SourceAlpha" stdDeviation="2" result="blur"/>
          <feFlood flood-color="${isDark ? '#5b8def' : '#4d8fdb'}" flood-opacity="${isDark ? '0.2' : '0.1'}" result="color"/>
          <feComposite in="color" in2="blur" operator="in" result="shadow"/>
          <feMerge><feMergeNode in="shadow"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>`
      svgEl.insertBefore(defs, svgEl.firstElementChild)
    }

    // ── 注入画布背景 ──
    const vb = svgEl.viewBox?.baseVal
    const bgW = vb?.width || svgEl.clientWidth || 2000
    const bgH = vb?.height || svgEl.clientHeight || 800
    const bgRect = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
    bgRect.setAttribute('x', '0')
    bgRect.setAttribute('y', '0')
    bgRect.setAttribute('width', String(bgW))
    bgRect.setAttribute('height', String(bgH))
    bgRect.style.setProperty('fill', bgColor, 'important')
    bgRect.setAttribute('class', 'mermaid-canvas-bg')
    const firstChild = svgEl.firstElementChild
    if (firstChild && firstChild.tagName.toLowerCase() === 'defs') {
      firstChild.insertAdjacentElement('afterend', bgRect)
    } else if (firstChild) {
      svgEl.insertBefore(bgRect, firstChild)
    } else {
      svgEl.appendChild(bgRect)
    }

    // ── 注入 CSS 规则覆盖深色遗留 + 添加圆角/阴影 ──
    const overrideCss = `
      .section0, .section1, .section2, .section3, .section4,
      .section5, .section6, .section7, .section8, .section9,
      .section--alt, .section-alt {
        fill: ${taskFill} !important; opacity: 0.6; }
      .task .section-bg, .task rect { fill: ${taskFill} !important; stroke: ${nodeStroke} !important; rx: 6; ry: 6; }
      .grid line, .grid path { stroke: ${gridColor} !important; }
      .today { stroke: #e85d6f !important; stroke-width: 2; }
      .node rect, .node circle, .node ellipse, .node polygon { rx: 10; ry: 10; }
      .cluster rect { rx: 12; ry: 12; stroke-dasharray: 6 3; }
      .edgePath path { stroke-linecap: round; }
      text { font-family: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", sans-serif; }
    `
    const styleEl = document.createElementNS('http://www.w3.org/2000/svg', 'style')
    styleEl.textContent = overrideCss
    svgEl.insertBefore(styleEl, svgEl.firstElementChild)

    // ── 修正画布级深色背景 rect ──
    const rootRects = svgEl.querySelectorAll(':scope > rect:not(.mermaid-canvas-bg)')
    const svgWidth = vb?.width || svgEl.clientWidth || 0
    const svgHeight = vb?.height || svgEl.clientHeight || 0
    rootRects.forEach((rect) => {
      const w = parseFloat(rect.getAttribute('width') || '0')
      const h = parseFloat(rect.getAttribute('height') || '0')
      const isCanvasBg = (w >= svgWidth * 0.9 && h >= svgHeight * 0.9) || (w >= 500 && h >= 100)
      if (!isCanvasBg) return
      const attrFill = rect.getAttribute('fill') || ''
      const styleFill = (rect as SVGElement).style.fill || ''
      let effectiveFill = styleFill || attrFill
      if (!effectiveFill || effectiveFill === 'none') {
        try { const c = getComputedStyle(rect).fill; if (c && c !== 'none' && c !== 'rgba(0, 0, 0, 0)') effectiveFill = c } catch { /* */ }
      }
      if (effectiveFill && isDarkColor(effectiveFill, 120)) {
        ;(rect as SVGElement).style.setProperty('fill', bgColor, 'important')
      }
    })

    // ── 通用图形元素深色修正 + 应用阴影滤镜 ──
    const shapeEls = container.querySelectorAll('svg rect, svg path, svg polygon')
    shapeEls.forEach((el) => {
      if (hasUserFill(el)) return
      const attrFill = el.getAttribute('fill') || ''
      if (!attrFill || attrFill === 'none') return
      if (isDarkFill(attrFill)) {
        el.setAttribute('fill', nodeFill)
        if (!hasUserStroke(el)) {
          const curStroke = el.getAttribute('stroke') || ''
          if (!curStroke || isDarkFill(curStroke)) el.setAttribute('stroke', nodeStroke)
        }
      }
    })

    // ── 为 flowchart 节点添加阴影 ──
    const nodeShapes = container.querySelectorAll('svg .node rect, svg .node polygon, svg .node circle, svg .node ellipse')
    nodeShapes.forEach((el) => {
      if (!el.getAttribute('filter')) {
        el.setAttribute('filter', 'url(#mermaid-node-shadow)')
      }
    })

    // ── Gantt/Timeline 专用修正 ──
    const ganttSelectors = 'svg .task rect, svg .task path, svg .section rect, svg .section path, svg g[class*="task"] rect, svg g[class*="task"] path, svg g[class*="section"] rect, svg g[class*="section"] path, svg g[class*="period"] rect, svg g[class*="period"] path, svg g[class*="event"] rect, svg g[class*="event"] path'
    container.querySelectorAll(ganttSelectors).forEach((el) => {
      if (hasUserFill(el)) return
      const svgItem = el as SVGElement
      const attrFill = el.getAttribute('fill') || ''
      const styleFill = svgItem.style.fill || ''
      let effectiveFill = styleFill || attrFill
      if (!effectiveFill || effectiveFill === 'none') {
        try { const c = getComputedStyle(el).fill; if (c && c !== 'none') effectiveFill = c } catch { /* */ }
      }
      if (effectiveFill && isDarkColor(effectiveFill, 100)) {
        svgItem.style.setProperty('fill', taskFill, 'important')
        if (!hasUserStroke(el)) svgItem.style.setProperty('stroke', nodeStroke, 'important')
      }
    })

    // ── 线条颜色修正 ──
    container.querySelectorAll('svg line').forEach((el) => {
      if (hasUserStroke(el)) return
      const stroke = el.getAttribute('stroke') || ''
      const styleStroke = (el as SVGElement).style.stroke || ''
      const effective = styleStroke || stroke
      if (effective && isDarkFill(effective)) {
        if (styleStroke) (el as SVGElement).style.stroke = lineColor
        else el.setAttribute('stroke', lineColor)
      }
    })

    // ── Marker 箭头颜色 ──
    container.querySelectorAll('svg marker path, svg marker polygon, svg marker polyline').forEach((el) => {
      if (hasUserFill(el)) return
      const fill = el.getAttribute('fill') || ''
      const stroke = el.getAttribute('stroke') || ''
      if (isDarkFill(fill)) el.setAttribute('fill', lineColor)
      if (isDarkFill(stroke) && !hasUserStroke(el)) el.setAttribute('stroke', lineColor)
      const sf = (el as SVGElement).style.fill
      if (sf && isDarkFill(sf)) (el as SVGElement).style.fill = lineColor
      const ss = (el as SVGElement).style.stroke
      if (ss && isDarkFill(ss) && !hasUserStroke(el)) (el as SVGElement).style.stroke = lineColor
    })

    // ── Path 元素深色 stroke（线条类） ──
    container.querySelectorAll('svg path').forEach((el) => {
      if (hasUserStroke(el)) return
      const fill = el.getAttribute('fill') || ''
      const stroke = el.getAttribute('stroke') || ''
      if ((!fill || fill === 'none') && stroke && isDarkFill(stroke)) el.setAttribute('stroke', lineColor)
      const ss = (el as SVGElement).style.stroke
      if ((!fill || fill === 'none') && ss && isDarkFill(ss)) (el as SVGElement).style.stroke = lineColor
    })

    // ── 文字颜色统一修正（跳过有用户自定义 classDef 样式的节点） ──
    const nodeTextSelectors = 'svg .node text, svg .node foreignObject div, svg g[class*="period"] text, svg g[class*="period"] foreignObject div, svg g[class*="event"] text, svg g[class*="event"] foreignObject div, svg g[class*="task"] text, svg g[class*="task"] foreignObject div, svg g[class*="section"] text, svg g[class*="section"] foreignObject div, svg [class*="timeline"] text, svg [class*="timeline"] foreignObject div, svg [class*="gantt"] text, svg [class*="gantt"] foreignObject div, svg .taskText, svg .taskText text, svg .sectionTitle, svg .sectionTitle text'
    container.querySelectorAll(nodeTextSelectors).forEach((el) => {
      // 检测该文本所属的节点是否有用户自定义填充色（classDef 通过 fill 属性或 style 注入）
      // 如果有，说明用户指定了 color，不应覆盖
      const parentNode = el.closest('.node, g[class*="task"], g[class*="section"], g[class*="period"], g[class*="event"]')
      if (parentNode) {
        const shape = parentNode.querySelector('rect, polygon, circle, ellipse, path')
        if (shape) {
          const svgShape = shape as SVGElement
          // 检查 inline style fill
          if (svgShape.style?.fill) return
          // 检查 fill 属性（mermaid 通过 classDef 设置的颜色通常作为属性而非 inline style）
          const attrFill = shape.getAttribute('fill') || ''
          // 排除 mermaid 默认填充色，仅跳过用户自定义颜色
          const defaultFills = ['#e8e8e8', '#f4f4f4', '#ffffff', '#fff', 'white', '#eee', '#f9f9f9', '#cccccc', '#ccc']
          if (attrFill && !defaultFills.includes(attrFill.toLowerCase())) return
        }
      }
      const t = el as SVGElement | HTMLElement
      t.style.color = textColor
      t.style.fill = textColor
    })
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

    _mermaidMutating = true
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
        const nearBottom = msgContainer.value.scrollHeight - msgContainer.value.scrollTop - msgContainer.value.clientHeight < 80
        if (nearBottom) {
          msgContainer.value.scrollTop = msgContainer.value.scrollHeight
        }
      }
    }
    // 延迟恢复，确保浏览器合并 DOM 变更
    nextTick(() => { _mermaidMutating = false })
  }

  /**
   * 预处理 mermaid 源码：修正 AI 生成的常见语法问题
   * - title "xxx" → title xxx（去掉引号，pie 等不接受带引号的 title；xychart-beta 需要保留引号）
   * - 去除 %%{init:...}%% 中 mermaid 不识别的非法属性
   */
  function sanitizeMermaidSource(source: string): string {
    if (!source) return source
    let result = source
    // 去掉 title 行两端的双引号: `title "xxx"` → `title xxx`
    // xychart-beta 的 title 含特殊字符（括号、~、中文等）时必须保留引号，否则词法解析失败
    if (!/^xychart(-beta)?\b/m.test(result)) {
      result = result.replace(/^(\s*title\s+)"([^"]+)"(\s*)$/gm, '$1$2$3')
    }
    // mindmap 节点不支持 <br/> 标签，且节点文本中的 ()  [] 会被解析器误认为节点形状语法
    // （如 "生产管理本部量大(696)但0提交" 中的 (696) 触发 NODE_ID 解析错误）
    // 统一替换为空格以避免解析错误
    if (/^mindmap\b/m.test(result)) {
      result = result.replace(/<br\s*\/?>/gi, ' ')
      result = result.replace(/[()\[\]]/g, ' ')
    }
    return result
  }

  /** 渲染单个 mermaid 容器（带 2 次重试，处理动态 import 失败） */
  async function renderSingleMermaid(el: HTMLElement): Promise<void> {
    const id = el.getAttribute('data-mermaid-id')
    // data-mermaid-source 为 base64(encodeURIComponent(source)) 编码的源码
    // （fence 规则中已编码，避免 DOMPurify 移除含 <br/> 或 --> 的属性）
    const encodedSource = el.getAttribute('data-mermaid-source')
    let source: string | null = null
    if (encodedSource) {
      try {
        source = decodeURIComponent(atob(encodedSource))
      } catch (_) {
        source = null
      }
    }
    if (!id || !source) return
    const trimmed = sanitizeMermaidSource(source.trim())
    if (!trimmed || trimmed.length < 5) return

    el.setAttribute('data-mermaid-processed', 'true')

    // svgCache 命中：直接注入缓存的 HTML（绕过 sanitizeHtml，保留 <style> 标签）
    // 注意：fence 规则不再返回缓存 SVG，统一走此路径，避免 DOMPurify 移除 <style>
    const svgCache = getMermaidSvgCache()
    if (svgCache.has(trimmed)) {
      el.innerHTML = svgCache.get(trimmed)
      return
    }

    // 检测是否为不支持的图表类型 → 渲染 fallback 卡片
    const unsupportedType = detectUnsupportedDiagramType(trimmed)
    if (unsupportedType) {
      const fallbackHtml = buildUnsupportedFallbackHtml(trimmed, unsupportedType)
      el.innerHTML = fallbackHtml
      el.classList.add('mermaid-unsupported')
      return
    }

    const MAX_RETRIES = 2
    let lastError: unknown = null

    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
      try {
        const mermaidLib = await getMermaid()
        const renderId = 'mermaid-r-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 6)
        const { svg } = await mermaidLib.render(renderId, trimmed)
        const innerHtml = buildMermaidInnerHtml(svg, source)
        el.innerHTML = innerHtml
        // 后置修正：强制替换 SVG 中残留的深色矩形填充
        patchMermaidSvgColors(el)
        // 更新缓存时也要包含修正后的 HTML
        const patchedInnerHtml = el.innerHTML
        svgCache.set(trimmed, patchedInnerHtml)
        // 不再调用 invalidateMermaidMsgCache：该函数会在每个 mermaid 块渲染成功后
        // 清空消息的 _renderedHtml 缓存，导致 Vue 下次渲染时重新生成全新 HTML，
        // 替换掉所有已渲染/待渲染的 mermaid 容器，引发级联重渲染，使大量图表显示为源码。
        // SVG 缓存 (svgCache) 已确保相同源码不会重复渲染，无需消息级缓存失效。
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

    // 渲染失败 - 显示友好的错误卡片（含源码 + 复制按钮）
    console.warn('Mermaid render error for source:', trimmed.substring(0, 100), lastError)
    const escapedSource = source.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    const errorMsg = lastError ? String((lastError as Error).message || '').substring(0, 120) : '未知错误'
    el.innerHTML = `<div class="mermaid-content-wrapper">` +
      `<div class="mermaid-error-card">` +
        `<div class="mermaid-error-header">` +
          `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>` +
          `<span>图表渲染失败</span>` +
        `</div>` +
        `<div class="mermaid-error-detail">${errorMsg.replace(/</g, '&lt;')}</div>` +
        `<pre class="mermaid-source-preview"><code>${escapedSource}</code></pre>` +
      `</div>` +
    `</div>` +
    `<div class="mermaid-toolbar">` +
      `<button class="mermaid-tb-btn" data-action="copy-source" title="复制源码">` +
        `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>` +
      `</button>` +
    `</div>`
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
  /** 防止 observer 对自身引起的 DOM 变化重复触发 */
  let _mermaidMutating = false

  function setupMermaidObserver(): void {
    if (mermaidObserver) return
    mermaidObserver = new MutationObserver((mutations) => {
      // 忽略自身引起的 DOM 变化，防止微循环
      if (_mermaidMutating) return
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
          _mermaidMutating = true
          void doRenderMermaidBlocks(true).then(() => {
            // 渲染完成后恢复自定义高度（仅在有自定义高度时）
            if (mermaidCustomHeights.size > 0) {
              reapplyAllMermaidCustomHeights()
            }
            // 延迟恢复标记，确保浏览器合并本轮 DOM 变更
            nextTick(() => { _mermaidMutating = false })
          })
          mermaidObserverTimer = null
        }, 100)
      }
    })
    // 优先监听消息容器，避免全树监听引起的性能问题和文本选择干扰
    const target = msgContainer.value || document.body
    mermaidObserver.observe(target, { childList: true, subtree: true })
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

    // 全屏 SVG 也需要颜色修正（全屏容器不在 .mermaid-container 内）
    const fsContainer = overlay.querySelector('.mermaid-fullscreen-container') as HTMLElement
    if (fsContainer) patchMermaidSvgColors(fsContainer)

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
