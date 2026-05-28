let mdInstance = null
let mdInitPromise = null

async function initMarkdownRenderer(apiBase = '') {
  if (mdInstance) return mdInstance
  if (mdInitPromise) return mdInitPromise

  mdInitPromise = (async () => {
    const [{ default: MarkdownIt }, { default: texmath }, { default: katex }] = await Promise.all([
      import('markdown-it'),
      import('markdown-it-texmath'),
      import('katex'),
      import('katex/dist/katex.min.css'),
    ])

    const md = new MarkdownIt({
      html: true,
      breaks: true,
      linkify: true,
      typographer: false,
    })

    md.use(texmath, {
      engine: katex,
      delimiters: 'dollars',
      katexOptions: { throwOnError: false, strict: false },
    })

    md.renderer.rules.link_open = function (tokens, idx, options, env, self) {
      const token = tokens[idx]
      const hrefIndex = token.attrIndex('href')

      if (hrefIndex >= 0) {
        let href = token.attrs[hrefIndex][1]

        if (href && href.startsWith('/') && !href.startsWith('//')) {
          href = apiBase + href
          token.attrs[hrefIndex][1] = href
        }

        if (href && href.includes('/exports/')) {
          token.attrPush(['data-export-link', 'true'])
        }

        const targetIndex = token.attrIndex('target')
        if (targetIndex < 0) {
          token.attrPush(['target', '_blank'])
        } else {
          token.attrs[targetIndex][1] = '_blank'
        }

        if (href.startsWith('http://') || href.startsWith('https://')) {
          token.attrPush(['rel', 'noopener noreferrer'])
        }
      }

      return self.renderToken(tokens, idx, options)
    }

    const defaultTableRender = md.renderer.rules.table_open
    md.renderer.rules.table_open = function (tokens, idx, options, env, self) {
      return '<div class="table-wrapper"><table>'
    }
    const defaultTableCloseRender = md.renderer.rules.table_close
    md.renderer.rules.table_close = function (tokens, idx, options, env, self) {
      return '</table></div>'
    }

    mdInstance = md
    return md
  })()

  return mdInitPromise
}

export async function getMarkdownRenderer(apiBase = '') {
  return await initMarkdownRenderer(apiBase)
}

let mermaidInstance = null
let mermaidInitPromise = null
let currentMermaidTheme = 'default'

async function initMermaid(theme = 'default') {
  if (mermaidInstance) return mermaidInstance
  if (mermaidInitPromise) return mermaidInitPromise

  mermaidInitPromise = (async () => {
    const { default: mermaid } = await import('mermaid')
    mermaid.initialize({
      startOnLoad: false,
      theme,
      securityLevel: 'loose',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
    })
    currentMermaidTheme = theme
    mermaidInstance = mermaid

    // 预热：用一个最简单的 flowchart 触发 mermaid 加载 flowDiagram 模块
    // 这样后续真正渲染时不会因为动态 import 失败而报错
    try {
      await mermaid.render('mermaid-warmup-' + Date.now(), 'flowchart LR\n    A-->B')
    } catch (_) {
      // 预热失败不影响后续使用，mermaid 会在下次 render 时重试
    }

    return mermaidInstance
  })()

  return mermaidInitPromise
}

export async function getMermaid() {
  return await initMermaid(currentMermaidTheme)
}

export async function switchMermaidTheme(theme) {
  if (theme === currentMermaidTheme && mermaidInstance) return
  currentMermaidTheme = theme
  if (mermaidInstance) {
    mermaidInstance.initialize({ theme })
  } else {
    await initMermaid(theme)
  }
}

export function getNextMermaidId() {
  return 'mermaid-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 6)
}

const mermaidSvgCache = new Map()

export function getMermaidSvgCache() {
  return mermaidSvgCache
}

export function clearMermaidSvgCache() {
  mermaidSvgCache.clear()
}

let hljsInstance = null
let hljsInitPromise = null

async function initHljs() {
  if (hljsInstance) return hljsInstance
  if (hljsInitPromise) return hljsInitPromise

  hljsInitPromise = (async () => {
    const [{ default: hljs }, { default: hljsSql }] = await Promise.all([
      import('highlight.js/lib/core'),
      import('highlight.js/lib/languages/sql'),
      import('highlight.js/styles/stackoverflow-light.css'),
    ])
    hljs.registerLanguage('sql', hljsSql)
    hljs.registerLanguage('mysql', hljsSql)
    hljs.registerLanguage('mariadb', hljsSql)
    hljsInstance = hljs
    return hljsInstance
  })()

  return hljsInitPromise
}

export async function getHljs() {
  return await initHljs()
}

export async function highlightSql(text) {
  if (!text) return ''
  try {
    const hljs = await getHljs()
    return hljs.highlight(text, { language: 'sql' }).value
  } catch {
    return text
  }
}

export function preloadHeavyDeps() {
  requestIdleCallback(() => {
    initMarkdownRenderer()
    initHljs()
    // 预加载 mermaid（包括 flowchart 模块），避免首次渲染时动态 import 失败
    initMermaid()
  })
}
