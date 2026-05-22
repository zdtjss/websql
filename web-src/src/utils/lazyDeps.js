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
let mermaidIdCounter = 0

async function initMermaid() {
  if (mermaidInstance) return mermaidInstance
  if (mermaidInitPromise) return mermaidInitPromise

  mermaidInitPromise = (async () => {
    const { default: mermaid } = await import('mermaid')
    mermaid.initialize({
      startOnLoad: false,
      theme: 'default',
      securityLevel: 'loose',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
    })
    mermaidInstance = mermaid
    return mermaidInstance
  })()

  return mermaidInitPromise
}

export async function getMermaid() {
  return await initMermaid()
}

export function getNextMermaidId() {
  return 'mermaid-' + (++mermaidIdCounter)
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
  })
}
