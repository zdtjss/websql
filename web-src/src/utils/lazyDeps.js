let mdInstance = null
let mdInitPromise = null

async function initMarkdownRenderer(apiBase = '') {
  if (mdInstance) return mdInstance
  if (mdInitPromise) return mdInitPromise

  mdInitPromise = (async () => {
    const [{ default: MarkdownIt }, { default: texmath }, { default: katex }, hljs] = await Promise.all([
      import('markdown-it'),
      import('markdown-it-texmath'),
      import('katex'),
      initHljs(),
      import('katex/dist/katex.min.css'),
    ])

    const md = new MarkdownIt({
      html: true,
      breaks: true,
      linkify: true,
      typographer: false,
      highlight: function (str, lang) {
        if (lang && hljs && hljs.getLanguage && hljs.getLanguage(lang)) {
          try {
            return hljs.highlight(str, { language: lang, ignoreIllegals: true }).value
          } catch (_) {}
        }
        return ''
      },
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
let currentMermaidTheme = 'light'

/** 根据主题返回 mermaid initialize 的完整配置 */
function buildMermaidConfig(theme) {
  const config = {
    startOnLoad: false,
    theme: 'base',
    securityLevel: 'loose',
    fontFamily: '"Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
    // Flowchart: 圆润曲线，舒适间距
    flowchart: {
      htmlLabels: true,
      curve: 'basis',
      padding: 16,
      nodeSpacing: 60,
      rankSpacing: 60,
      diagramPadding: 20,
      useMaxWidth: true,
    },
    // Mindmap: 宽松节点
    mindmap: { padding: 20, maxNodeWidth: 220 },
    // XY Chart: 精致图表
    xyChart: { titlePadding: 12, titleFontSize: 15, width: 700, height: 400 },
    // Pie: 优雅饼图
    pie: { textPosition: 0.72, useMaxWidth: true },
    // Sequence: 紧凑时序
    sequence: { diagramMarginX: 16, diagramMarginY: 16, actorMargin: 60, width: 160, height: 50, boxMargin: 8, boxTextMargin: 6, noteMargin: 12, messageMargin: 40, mirrorActors: true },
    // Gantt
    gantt: { titleTopMargin: 16, barHeight: 24, barGap: 6, topPadding: 60, leftPadding: 80, fontSize: 12, sectionFontSize: 13, numberSectionStyles: 4 },
  }

  if (theme === 'dark') {
    // ═══════════════════════════════════════════════
    //  暗色模式 - 深邃优雅 (灵感: Figma Dark / Linear)
    //  深蓝灰底色 + 高饱和宝石色点缀
    // ═══════════════════════════════════════════════
    config.themeVariables = {
      primaryColor: '#5b8def', primaryTextColor: '#e8ecf4', primaryBorderColor: '#6e9cf5',
      lineColor: '#7ba4e8', secondaryColor: '#7c6bc4', tertiaryColor: '#4a9e7e',
      background: '#181825', mainBkg: '#5b8def', nodeBorder: '#6e9cf5',
      clusterBkg: '#1e1e30', clusterBorder: '#3d4460',
      titleColor: '#f0f3f8', edgeLabelBackground: 'transparent',
      textColor: '#e8ecf4', nodeTextColor: '#ffffff',
      defaultLinkColor: '#7ba4e8', arrowheadColor: '#7ba4e8',
      // Sequence
      actorBkg: '#2a2d42', actorBorder: '#6e9cf5', actorTextColor: '#f0f3f8',
      actorLineColor: '#5b8def', signalColor: '#e8ecf4', signalTextColor: '#e8ecf4',
      labelBoxBkgColor: '#252840', labelBoxBorderColor: '#6e9cf5', labelTextColor: '#e8ecf4',
      loopTextColor: '#c8d0e0', noteBkgColor: '#3d3552', noteBorderColor: '#9b7ae8',
      noteTextColor: '#f0f3f8', activationBkgColor: '#2f3350', activationBorderColor: '#5b8def',
      sequenceNumberColor: '#181825',
      // Timeline cScale（宝石色系12色环）
      cScale0: '#5b8def', cScale1: '#7c6bc4', cScale2: '#4a9e7e', cScale3: '#d4915e',
      cScale4: '#c75b8f', cScale5: '#4db6c4', cScale6: '#8b6ec7', cScale7: '#5cb88a',
      cScale8: '#c9a04e', cScale9: '#b85b7a', cScale10: '#4aaab5', cScale11: '#9e7cd4',
      cScaleLabel0: '#fff', cScaleLabel1: '#fff', cScaleLabel2: '#fff', cScaleLabel3: '#fff',
      cScaleLabel4: '#fff', cScaleLabel5: '#fff', cScaleLabel6: '#fff', cScaleLabel7: '#fff',
      cScaleLabel8: '#fff', cScaleLabel9: '#fff', cScaleLabel10: '#fff', cScaleLabel11: '#fff',
      // Gantt
      taskBkgColor: '#5b8def', activeTaskBkgColor: '#7c6bc4', doneTaskBkgColor: '#4a9e7e',
      taskTextColor: '#ffffff', taskTextOutsideColor: '#c8d0e0', activeTaskTextColor: '#ffffff',
      doneTaskBorderColor: '#6fc9a0', gridColor: '#2a2d42', todayLineColor: '#e85d6f',
      sectionBkgColor: '#1e1e30', altSectionBkgColor: '#232338',
      taskBorderColor: '#6e9cf5', taskBorderDarkColor: '#80acf8',
      sectionBkgColor2: '#232338', altSectionBkgColor2: '#282845',
      altSectionBkgColorOpacity: 0.7, sectionBkgColorOpacity: 0.7,
      critBorderColor: '#e85d6f', critBkgColor: '#c44e5e', doneCritBkgColor: '#7c6bc4',
      // Pie（鲜艳宝石色）
      pie1: '#5b8def', pie2: '#7c6bc4', pie3: '#4a9e7e', pie4: '#d4915e',
      pie5: '#c75b8f', pie6: '#4db6c4', pie7: '#8b6ec7', pie8: '#5cb88a',
      pie9: '#c9a04e', pie10: '#b85b7a', pie11: '#4aaab5', pie12: '#9e7cd4',
      pieTitleTextSize: '15px', pieTitleTextColor: '#f0f3f8',
      pieSectionTextSize: '13px', pieSectionTextColor: '#ffffff',
      pieLegendTextSize: '12px', pieLegendTextColor: '#c8d0e0',
      pieStrokeColor: '#181825', pieStrokeWidth: '2px',
      pieOuterStrokeWidth: '0px', pieOuterStrokeColor: 'transparent', pieOpacity: '0.92',
      // XY Chart
      xyChart: { backgroundColor: 'transparent', titleColor: '#f0f3f8', xAxisTitleColor: '#c8d0e0', xAxisLabelColor: '#a0aab8', xAxisTickColor: '#3d4460', xAxisLineColor: '#3d4460', yAxisTitleColor: '#c8d0e0', yAxisLabelColor: '#a0aab8', yAxisTickColor: '#3d4460', yAxisLineColor: '#3d4460', plotColorPalette: '#5b8def,#7c6bc4,#4a9e7e,#d4915e,#c75b8f,#4db6c4,#8b6ec7,#5cb88a' },
      // Class / State / ER
      classText: '#e8ecf4', labelColor: '#e8ecf4', altBackground: '#232338',
      compositeBackground: '#1e1e30', compositeBorder: '#3d4460', compositeTitle: '#f0f3f8',
      entityBkg: '#2a2d42', entityBorder: '#6e9cf5',
      // Journey
      fillType0: '#5b8def', fillType1: '#7c6bc4', fillType2: '#4a9e7e', fillType3: '#d4915e',
      fillType4: '#c75b8f', fillType5: '#4db6c4', fillType6: '#8b6ec7', fillType7: '#5cb88a',
      // Quadrant
      quadrant1Fill: '#2a3f5e', quadrant2Fill: '#3a2e55', quadrant3Fill: '#2a4540', quadrant4Fill: '#4a3a2a',
      quadrant1TextFill: '#b8d4ff', quadrant2TextFill: '#d4b8ff', quadrant3TextFill: '#b8ffd4', quadrant4TextFill: '#ffd4b8',
      quadrantPointFill: '#e85d6f', quadrantPointTextFill: '#f0f3f8',
      quadrantXAxisTextFill: '#c8d0e0', quadrantYAxisTextFill: '#c8d0e0', quadrantTitleFill: '#f0f3f8',
      quadrantInternalBorderStrokeFill: '#3d4460', quadrantExternalBorderStrokeFill: '#5b8def',
      // Git
      git0: '#5b8def', git1: '#7c6bc4', git2: '#4a9e7e', git3: '#d4915e',
      git4: '#c75b8f', git5: '#4db6c4', git6: '#8b6ec7', git7: '#5cb88a',
      gitBranchLabel0: '#fff', gitBranchLabel1: '#fff', gitBranchLabel2: '#fff', gitBranchLabel3: '#fff',
      gitBranchLabel4: '#fff', gitBranchLabel5: '#fff', gitBranchLabel6: '#fff', gitBranchLabel7: '#fff',
      gitInv0: '#181825', commitLabelColor: '#f0f3f8', commitLabelBackground: '#2a2d42',
      tagLabelColor: '#ffffff', tagLabelBackground: '#7c6bc4', tagLabelBorder: '#9b7ae8',
    }
  } else {
    // ═══════════════════════════════════════════════
    //  日间模式 - 清透灵动 (灵感: Linear / Apple HIG)
    //  纯白底 + 柔和渐变色 + 鲜明但不刺眼的强调色
    // ═══════════════════════════════════════════════
    config.themeVariables = {
      primaryColor: '#e8f1fd', primaryTextColor: '#1a2332', primaryBorderColor: '#a8ccf5',
      lineColor: '#4d8fdb', secondaryColor: '#f0ebff', tertiaryColor: '#e6f7f0',
      background: '#ffffff', mainBkg: '#e8f1fd', nodeBorder: '#a8ccf5',
      clusterBkg: '#f7faff', clusterBorder: '#d0e3f7',
      titleColor: '#1a2332', edgeLabelBackground: 'transparent',
      textColor: '#1a2332', nodeTextColor: '#1a2332',
      defaultLinkColor: '#4d8fdb', arrowheadColor: '#4d8fdb',
      // Sequence
      actorBkg: '#e8f1fd', actorBorder: '#a8ccf5', actorTextColor: '#1a2332',
      actorLineColor: '#4d8fdb', signalColor: '#1a2332', signalTextColor: '#1a2332',
      labelBoxBkgColor: '#f7faff', labelBoxBorderColor: '#a8ccf5', labelTextColor: '#1a2332',
      loopTextColor: '#3d5068', noteBkgColor: '#fef8e8', noteBorderColor: '#e8c547',
      noteTextColor: '#6b4e00', activationBkgColor: '#e0f0ff', activationBorderColor: '#4d8fdb',
      sequenceNumberColor: '#ffffff',
      // Timeline cScale（柔和彩虹12色）
      cScale0: '#e8f1fd', cScale1: '#f0ebff', cScale2: '#e6f7f0', cScale3: '#fef8e8',
      cScale4: '#fef0f5', cScale5: '#e6f5f8', cScale6: '#f5eeff', cScale7: '#e8faf0',
      cScale8: '#fef5e0', cScale9: '#ffe8ef', cScale10: '#e0f5f0', cScale11: '#fff0e6',
      cScaleLabel0: '#1a4e8a', cScaleLabel1: '#5b21b6', cScaleLabel2: '#0a6e4e', cScaleLabel3: '#7a5200',
      cScaleLabel4: '#9d174d', cScaleLabel5: '#0e5e6f', cScaleLabel6: '#5b21b6', cScaleLabel7: '#0a6e4e',
      cScaleLabel8: '#7a5200', cScaleLabel9: '#9d174d', cScaleLabel10: '#0a6e4e', cScaleLabel11: '#9a3412',
      // Gantt
      taskBkgColor: '#e8f1fd', activeTaskBkgColor: '#a8ccf5', doneTaskBkgColor: '#e6f7f0',
      taskTextColor: '#1a2332', taskTextOutsideColor: '#3d5068', activeTaskTextColor: '#1a3a6e',
      doneTaskBorderColor: '#6ee7b7', gridColor: '#eef3f9', todayLineColor: '#e85d6f',
      sectionBkgColor: '#f7faff', altSectionBkgColor: '#faf8ff',
      taskBorderColor: '#a8ccf5', taskBorderDarkColor: '#7db5f0',
      sectionBkgColor2: '#f0f7ff', altSectionBkgColor2: '#f5f0ff',
      altSectionBkgColorOpacity: 0.6, sectionBkgColorOpacity: 0.6,
      critBorderColor: '#e85d6f', critBkgColor: '#ffe0e5', doneCritBkgColor: '#fef0d0',
      // Pie（优雅鲜艳12色）
      pie1: '#4d8fdb', pie2: '#7c6bc4', pie3: '#3daa7e', pie4: '#e8a838',
      pie5: '#d45d8a', pie6: '#3db5c4', pie7: '#6366f1', pie8: '#4abf8a',
      pie9: '#e88838', pie10: '#a855f7', pie11: '#22bfcf', pie12: '#7dc428',
      pieTitleTextSize: '15px', pieTitleTextColor: '#1a2332',
      pieSectionTextSize: '13px', pieSectionTextColor: '#ffffff',
      pieLegendTextSize: '12px', pieLegendTextColor: '#4a5568',
      pieStrokeColor: '#ffffff', pieStrokeWidth: '2.5px',
      pieOuterStrokeWidth: '0px', pieOuterStrokeColor: 'transparent', pieOpacity: '0.92',
      // XY Chart
      xyChart: { backgroundColor: 'transparent', titleColor: '#1a2332', xAxisTitleColor: '#4a5568', xAxisLabelColor: '#6b7a8d', xAxisTickColor: '#dce4ee', xAxisLineColor: '#dce4ee', yAxisTitleColor: '#4a5568', yAxisLabelColor: '#6b7a8d', yAxisTickColor: '#dce4ee', yAxisLineColor: '#dce4ee', plotColorPalette: '#4d8fdb,#7c6bc4,#3daa7e,#e8a838,#d45d8a,#3db5c4,#6366f1,#4abf8a' },
      // Class / State / ER
      classText: '#1a2332', labelColor: '#1a2332', altBackground: '#f7faff',
      compositeBackground: '#f7faff', compositeBorder: '#d0e3f7', compositeTitle: '#1a2332',
      entityBkg: '#e8f1fd', entityBorder: '#a8ccf5',
      // Journey
      fillType0: '#e8f1fd', fillType1: '#f0ebff', fillType2: '#e6f7f0', fillType3: '#fef8e8',
      fillType4: '#fef0f5', fillType5: '#e6f5f8', fillType6: '#f5eeff', fillType7: '#e8faf0',
      // Quadrant
      quadrant1Fill: '#eef5ff', quadrant2Fill: '#f5f0ff', quadrant3Fill: '#eefaf5', quadrant4Fill: '#fff8ee',
      quadrant1TextFill: '#1a4e8a', quadrant2TextFill: '#5b21b6', quadrant3TextFill: '#0a6e4e', quadrant4TextFill: '#7a5200',
      quadrantPointFill: '#e85d6f', quadrantPointTextFill: '#1a2332',
      quadrantXAxisTextFill: '#4a5568', quadrantYAxisTextFill: '#4a5568', quadrantTitleFill: '#1a2332',
      quadrantInternalBorderStrokeFill: '#e0e8f2', quadrantExternalBorderStrokeFill: '#a8ccf5',
      // Git
      git0: '#4d8fdb', git1: '#7c6bc4', git2: '#3daa7e', git3: '#e8a838',
      git4: '#d45d8a', git5: '#3db5c4', git6: '#6366f1', git7: '#4abf8a',
      gitBranchLabel0: '#1a4e8a', gitBranchLabel1: '#4a2a8a', gitBranchLabel2: '#0a5e3e', gitBranchLabel3: '#7a5200',
      gitBranchLabel4: '#8a1a4a', gitBranchLabel5: '#0a5e6e', gitBranchLabel6: '#2a2a8a', gitBranchLabel7: '#0a5e3e',
      gitInv0: '#ffffff', commitLabelColor: '#1a2332', commitLabelBackground: '#e8f1fd',
      tagLabelColor: '#ffffff', tagLabelBackground: '#7c6bc4', tagLabelBorder: '#9b7ae8',
    }
  }
  return config
}

async function initMermaid(theme = 'light') {
  if (mermaidInstance) return mermaidInstance
  if (mermaidInitPromise) return mermaidInitPromise

  mermaidInitPromise = (async () => {
    const { default: mermaid } = await import('mermaid')
    mermaid.initialize(buildMermaidConfig(theme))
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
    mermaidInstance.initialize(buildMermaidConfig(theme))
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
    const [
      { default: hljs },
      { default: hljsSql },
      { default: hljsJs },
      { default: hljsJson },
      { default: hljsBash },
      { default: hljsPython },
      { default: hljsTs },
    ] = await Promise.all([
      import('highlight.js/lib/core'),
      import('highlight.js/lib/languages/sql'),
      import('highlight.js/lib/languages/javascript'),
      import('highlight.js/lib/languages/json'),
      import('highlight.js/lib/languages/bash'),
      import('highlight.js/lib/languages/python'),
      import('highlight.js/lib/languages/typescript'),
      import('highlight.js/styles/github-dark.css'),
    ])
    hljs.registerLanguage('sql', hljsSql)
    hljs.registerLanguage('mysql', hljsSql)
    hljs.registerLanguage('mariadb', hljsSql)
    hljs.registerLanguage('javascript', hljsJs)
    hljs.registerLanguage('js', hljsJs)
    hljs.registerLanguage('json', hljsJson)
    hljs.registerLanguage('bash', hljsBash)
    hljs.registerLanguage('shell', hljsBash)
    hljs.registerLanguage('python', hljsPython)
    hljs.registerLanguage('py', hljsPython)
    hljs.registerLanguage('typescript', hljsTs)
    hljs.registerLanguage('ts', hljsTs)
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
