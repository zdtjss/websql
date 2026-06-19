package export

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// ExportHTMLInput HTML 导出参数
type ExportHTMLInput struct {
	SQL      string `json:"sql" jsonschema_description:"用于导出的 SELECT SQL（与 content 二选一）"`
	Content  string `json:"content" jsonschema_description:"报告内容（Markdown 格式，与 sql 二选一。支持标题、段落、列表、表格、代码块、mermaid 图表、数学公式）"`
	FileName string `json:"fileName" jsonschema_description:"文件名（不含扩展名）"`
	Title    string `json:"title" jsonschema_description:"报告标题"`
}

// ExportHTMLOutput HTML 导出结果
type ExportHTMLOutput struct {
	Message     string `json:"message"`
	DownloadURL string `json:"downloadUrl"`
	FileType    string `json:"fileType"`
}

// NewExportHTMLFunc 创建 HTML 导出工具函数
func NewExportHTMLFunc(conn *sqlx.DB) func(ctx context.Context, input *ExportHTMLInput) (*ExportHTMLOutput, error) {
	return func(ctx context.Context, input *ExportHTMLInput) (*ExportHTMLOutput, error) {
		var markdownContent string

		if input.Content != "" {
			// content 模式：直接使用原始 Markdown，交由前端 marked.js 渲染
			markdownContent = input.Content
		} else if input.SQL != "" {
			// SQL 模式：查询数据并生成 Markdown 分析报告
			qr, err := QueryForExport(conn, input.SQL)
			if err != nil {
				return nil, err
			}
			markdownContent = generateMarkdownFromQuery(qr, input.Title)
		} else {
			return nil, fmt.Errorf("必须提供 sql 或 content 参数")
		}

		title := input.Title
		if title == "" {
			title = "数据分析报告"
		}

		fullHTML := wrapHTMLTemplate(title, markdownContent)

		fileName := SanitizeFileName(input.FileName, "report")
		EnsureExportsDir()
		filePath := fmt.Sprintf("exports/%s.html", fileName)
		if err := os.WriteFile(filePath, []byte(fullHTML), 0644); err != nil {
			return nil, fmt.Errorf("保存 HTML 失败：%w", err)
		}

		url := fmt.Sprintf("/exports/%s.html", fileName)
		log.Printf("[Tool:export_html] 成功 - url=%s\n", url)

		return &ExportHTMLOutput{
			Message:     fmt.Sprintf("已生成 HTML 报告，[点击下载](%s)", url),
			DownloadURL: url,
			FileType:    "html",
		}, nil
	}
}

// generateMarkdownFromQuery 从查询结果生成 Markdown 分析报告
// 输出纯 Markdown 文本，由前端 marked.js 渲染为 HTML
func generateMarkdownFromQuery(qr *QueryResult, title string) string {
	var sb strings.Builder

	if title == "" {
		title = "数据分析报告"
	}

	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString(fmt.Sprintf("> 生成时间：%s　数据行数：%d\n\n",
		time.Now().Format("2006-01-02 15:04:05"), len(qr.Data)))

	// 统计摘要
	if len(qr.Data) > 0 {
		numericCols := DetectNumericCols(qr)
		if len(numericCols) > 0 {
			sb.WriteString("## 统计摘要\n\n")
			sb.WriteString("| 字段 | 最小值 | 最大值 | 平均值 |\n")
			sb.WriteString("|------|--------|--------|--------|\n")
			for _, col := range numericCols {
				min, max, avg, _ := CalcNumericStats(qr, col)
				sb.WriteString(fmt.Sprintf("| %s | %.2f | %.2f | %.2f |\n",
					escapeMarkdownTableCell(col), min, max, avg))
			}
			sb.WriteString("\n")
		}
	}

	// 数据明细
	sb.WriteString("## 数据明细\n\n")
	if len(qr.Data) == 0 {
		sb.WriteString("无数据\n")
		return sb.String()
	}

	// 表头
	headers := make([]string, len(qr.Columns))
	separators := make([]string, len(qr.Columns))
	for i, col := range qr.Columns {
		headers[i] = escapeMarkdownTableCell(col)
		separators[i] = "------"
	}
	sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	sb.WriteString("|" + strings.Join(separators, "|") + "|\n")

	// 数据行
	maxRows := 1000
	for i, row := range qr.Data {
		if i >= maxRows {
			sb.WriteString(fmt.Sprintf("| ... 共 %d 行，仅显示前 %d 行 |\n", len(qr.Data), maxRows))
			break
		}
		cells := make([]string, len(qr.Columns))
		for j, col := range qr.Columns {
			val := fmt.Sprintf("%v", row[col])
			cells[j] = escapeMarkdownTableCell(val)
		}
		sb.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}

	return sb.String()
}

// escapeMarkdownTableCell 转义 Markdown 表格单元格中的特殊字符
func escapeMarkdownTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// wrapHTMLTemplate 将 Markdown 内容包装在完整的 HTML 模板中
// Markdown 原文放在 <script type="text/markdown"> 中，由前端 marked.js 渲染
func wrapHTMLTemplate(title, markdownBody string) string {
	// 转义 </script> 防止截断
	escapedBody := strings.ReplaceAll(markdownBody, "</script>", "<\\/script>")
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
:root {
  --bg: #ffffff;
  --fg: #1a1a2e;
  --fg-muted: #6c757d;
  --border: #dee2e6;
  --code-bg: #f8f9fa;
  --table-stripe: #f8f9fa;
  --table-header: #343a40;
  --table-header-fg: #ffffff;
  --link: #007bff;
  --blockquote-bg: #f8f9fa;
  --blockquote-border: #007bff;
  --hr-color: #dee2e6;
  --task-checked: #28a745;
  --toolbar-bg: rgba(255,255,255,0.95);
  --toolbar-fg: #495057;
  --modal-bg: rgba(0,0,0,0.5);
}
[data-theme="dark"] {
  --bg: #1a1a2e;
  --fg: #e0e0e0;
  --fg-muted: #999;
  --border: #333;
  --code-bg: #16213e;
  --table-stripe: #16213e;
  --table-header: #0f3460;
  --table-header-fg: #e0e0e0;
  --link: #64b5f6;
  --blockquote-bg: #16213e;
  --blockquote-border: #64b5f6;
  --hr-color: #333;
  --task-checked: #4caf50;
  --toolbar-bg: rgba(26,26,46,0.95);
  --toolbar-fg: #ccc;
  --modal-bg: rgba(0,0,0,0.7);
}
* { box-sizing: border-box; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans SC", "Microsoft YaHei", sans-serif;
  line-height: 1.8;
  color: var(--fg);
  background: var(--bg);
  max-width: 960px;
  margin: 0 auto;
  padding: 40px 20px;
  transition: background 0.3s, color 0.3s;
}
h1 { font-size: 1.8em; border-bottom: 2px solid var(--border); padding-bottom: 0.3em; margin-top: 1.5em; }
h2 { font-size: 1.4em; border-bottom: 1px solid var(--border); padding-bottom: 0.2em; margin-top: 1.2em; }
h3 { font-size: 1.2em; margin-top: 1em; }
h4 { font-size: 1.05em; margin-top: 0.9em; }
h5 { font-size: 0.95em; margin-top: 0.8em; color: var(--fg-muted); }
h6 { font-size: 0.9em; margin-top: 0.7em; color: var(--fg-muted); text-transform: uppercase; letter-spacing: 0.05em; }
p { margin: 0.6em 0; }
a { color: var(--link); text-decoration: none; }
a:hover { text-decoration: underline; }
hr { border: none; border-top: 2px solid var(--hr-color); margin: 2em 0; }
code {
  background: var(--code-bg);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: "Fira Code", "Cascadia Code", Consolas, "Courier New", monospace;
  font-size: 0.9em;
}
pre {
  background: var(--code-bg);
  padding: 16px;
  border-radius: 8px;
  overflow-x: auto;
  border: 1px solid var(--border);
}
pre code {
  background: none;
  padding: 0;
  font-size: 0.875em;
  line-height: 1.6;
}
blockquote {
  border-left: 4px solid var(--blockquote-border);
  background: var(--blockquote-bg);
  margin: 1em 0;
  padding: 0.5em 1em;
  color: var(--fg-muted);
  border-radius: 0 4px 4px 0;
}
blockquote p { margin: 0.3em 0; }
table {
  border-collapse: collapse;
  width: 100%%;
  font-size: 0.9em;
  margin: 1em 0;
}
th, td {
  border: 1px solid var(--border);
  padding: 8px 12px;
  text-align: left;
}
th {
  background: var(--table-header);
  color: var(--table-header-fg);
  font-weight: 600;
}
tbody tr:nth-child(even) { background: var(--table-stripe); }
img {
  max-width: 100%%;
  height: auto;
  border-radius: 4px;
}
.task-list-item input { margin-right: 0.5em; }
.task-list-item.checked { color: var(--task-checked); }
.theme-toggle {
  position: fixed;
  top: 20px;
  right: 20px;
  background: var(--code-bg);
  border: 1px solid var(--border);
  border-radius: 50%%;
  width: 40px;
  height: 40px;
  cursor: pointer;
  font-size: 1.2em;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.3s;
  z-index: 1000;
}
.theme-toggle:hover { transform: scale(1.1); }

/* ===== Mermaid 交互容器 ===== */
.mermaid-wrapper {
  text-align: center;
  margin: 1.5em 0;
  padding: 1em;
  background: var(--code-bg);
  border-radius: 8px;
  border: 1px solid var(--border);
  position: relative;
}
.mermaid-wrapper.is-fullscreen {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  z-index: 9999;
  margin: 0;
  border-radius: 0;
  display: flex;
  flex-direction: column;
  padding: 0;
}
.mermaid-toolbar {
  display: flex;
  gap: 4px;
  justify-content: flex-end;
  padding: 4px 8px;
  background: var(--toolbar-bg);
  border-radius: 4px;
  margin-bottom: 8px;
  position: relative;
  z-index: 10;
}
.mermaid-wrapper.is-fullscreen .mermaid-toolbar {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 10001;
  box-shadow: 0 2px 8px rgba(0,0,0,0.2);
}
.mermaid-toolbar button {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  width: 30px;
  height: 30px;
  cursor: pointer;
  font-size: 14px;
  color: var(--toolbar-fg);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  transition: background 0.2s;
}
.mermaid-toolbar button:hover {
  background: var(--code-bg);
  border-color: var(--link);
}
.mermaid-stage {
  overflow: auto;
  flex: 1;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  cursor: grab;
  position: relative;
}
.mermaid-stage:active { cursor: grabbing; }
.mermaid-wrapper.is-fullscreen .mermaid-stage {
  padding: 20px;
}
.mermaid-canvas {
  transform-origin: center center;
  transition: transform 0.05s ease-out;
  display: inline-block;
}
.mermaid-canvas.no-transition { transition: none; }
.mermaid-canvas svg { max-width: none; }

/* ===== 源码查看模态框 ===== */
.mermaid-source-modal {
  display: none;
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: var(--modal-bg);
  z-index: 10002;
  align-items: center;
  justify-content: center;
}
.mermaid-source-modal.active { display: flex; }
.mermaid-source-modal-content {
  background: var(--bg);
  border-radius: 8px;
  width: 80%%;
  max-width: 700px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0,0,0,0.3);
}
.mermaid-source-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.mermaid-source-modal-header h3 { margin: 0; font-size: 1em; }
.mermaid-source-modal-body {
  padding: 16px;
  overflow: auto;
  flex: 1;
}
.mermaid-source-modal-body pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}
.mermaid-copy-btn {
  background: var(--link);
  color: #fff;
  border: none;
  border-radius: 4px;
  padding: 4px 12px;
  cursor: pointer;
  font-size: 0.85em;
}
.mermaid-copy-btn:hover { opacity: 0.9; }

@media print {
  .theme-toggle, .mermaid-toolbar { display: none; }
  body { max-width: none; padding: 0; }
  pre, table, .mermaid-wrapper { page-break-inside: avoid; }
  img { max-width: 100%% !important; }
}
</style>
</head>
<body data-theme="light">
<button class="theme-toggle" onclick="toggleTheme()" title="切换主题">🌙</button>
<div id="markdown-source" type="text/markdown" style="display:none">%s</div>
<div id="rendered-content"></div>

<!-- 源码查看模态框 -->
<div class="mermaid-source-modal" id="mermaidSourceModal">
  <div class="mermaid-source-modal-content">
    <div class="mermaid-source-modal-header">
      <h3>Mermaid 源码</h3>
      <button class="mermaid-copy-btn" onclick="copyMermaidSource()">复制</button>
    </div>
    <div class="mermaid-source-modal-body">
      <pre><code id="mermaidSourceCode"></code></pre>
    </div>
  </div>
</div>

<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/katex.min.css">
<script src="https://cdn.jsdelivr.net/npm/marked@12/marked.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/dompurify@3/dist/purify.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/highlight.js@11/lib/common.min.js"></script>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/highlight.js@11/styles/github.min.css" id="hljs-light">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/highlight.js@11/styles/github-dark.min.css" id="hljs-dark" disabled>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/katex.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/contrib/auto-render.min.js"
  onload="renderMath();"></script>
<script>
var currentMermaidSource = "";

function renderMath() {
  if (window.katex && window.renderMathInElement) {
    renderMathInElement(document.getElementById('rendered-content'), {
      delimiters: [
        {left: "$$", right: "$$", display: true},
        {left: "$", right: "$", display: false}
      ],
      throwOnError: false
    });
  }
}

function toggleTheme() {
  var body = document.body;
  var isDark = body.getAttribute('data-theme') === 'dark';
  body.setAttribute('data-theme', isDark ? 'light' : 'dark');
  document.querySelector('.theme-toggle').textContent = isDark ? '🌙' : '☀️';
  document.getElementById('hljs-light').disabled = !isDark;
  document.getElementById('hljs-dark').disabled = isDark;
  if (window.mermaid) {
    mermaid.initialize({ startOnLoad: false, theme: isDark ? 'dark' : 'default', securityLevel: 'loose' });
    rerenderMermaid();
  }
}

// ===== Markdown 渲染 =====
function renderMarkdown() {
  var mdSource = document.getElementById('markdown-source').textContent;
  if (!mdSource) return;

  // marked 配置：GFM、breaks
  marked.setOptions({
    gfm: true,
    breaks: false,
    headerIds: false,
    mangle: false
  });

  var rawHtml = marked.parse(mdSource);
  var cleanHtml = DOMPurify.sanitize(rawHtml, {
    ADD_TAGS: ['foreignObject', 'desc', 'title'],
    ADD_ATTR: ['viewBox', 'preserveAspectRatio', 'class', 'id', 'style', 'xmlns', 'd', 'x', 'y', 'x1', 'y1', 'x2', 'y2', 'cx', 'cy', 'r', 'rx', 'ry', 'width', 'height', 'fill', 'stroke', 'stroke-width', 'points', 'transform', 'text-anchor', 'dominant-baseline', 'font-size', 'font-family', 'font-weight', 'marker-end', 'marker-start', 'href', 'target', 'alt', 'src', 'colspan', 'rowspan', 'align', 'valign', 'checked', 'disabled', 'type', 'data-*']
  });

  document.getElementById('rendered-content').innerHTML = cleanHtml;

  // 后处理：代码高亮
  if (window.hljs) {
    document.querySelectorAll('pre code').forEach(function(block) {
      if (block.className.indexOf('language-mermaid') === -1) {
        hljs.highlightElement(block);
      }
    });
  }

  // 后处理：Mermaid 块替换为交互容器
  processMermaidBlocks();

  // 后处理：数学公式
  renderMath();
}

// ===== Mermaid 交互处理 =====
function processMermaidBlocks() {
  var mermaidBlocks = document.querySelectorAll('code.language-mermaid');
  mermaidBlocks.forEach(function(codeEl, idx) {
    var pre = codeEl.parentElement;
    if (!pre || pre.dataset.mermaidProcessed) return;
    pre.dataset.mermaidProcessed = '1';

    var source = codeEl.textContent;
    var wrapper = document.createElement('div');
    wrapper.className = 'mermaid-wrapper';
    wrapper.dataset.mermaidSource = source;

    var toolbar = document.createElement('div');
    toolbar.className = 'mermaid-toolbar';
    toolbar.innerHTML =
      '<button data-action="zoom-in" title="放大">＋</button>' +
      '<button data-action="zoom-out" title="缩小">－</button>' +
      '<button data-action="reset" title="还原">↺</button>' +
      '<button data-action="fullscreen" title="全屏">⛶</button>' +
      '<button data-action="source" title="源码">&lt;/&gt;</button>';

    var stage = document.createElement('div');
    stage.className = 'mermaid-stage';

    var canvas = document.createElement('div');
    canvas.className = 'mermaid-canvas';
    canvas.id = 'mermaid-canvas-' + idx;

    stage.appendChild(canvas);
    wrapper.appendChild(toolbar);
    wrapper.appendChild(stage);

    pre.parentNode.replaceChild(wrapper, pre);

    // 渲染 mermaid
    var renderId = 'mermaid-' + Date.now() + '-' + idx;
    canvas.id = renderId;
    canvas.textContent = source;

    if (window.mermaid) {
      try {
        mermaid.run({ nodes: [canvas] });
      } catch(e) {
        canvas.textContent = 'Mermaid 渲染失败: ' + e.message;
      }
    }

    // 绑定交互
    bindMermaidInteractions(wrapper, canvas, stage, source);
  });
}

function bindMermaidInteractions(wrapper, canvas, stage, source) {
  var scale = 1;
  var translateX = 0;
  var translateY = 0;
  var isDragging = false;
  var dragStartX = 0, dragStartY = 0;
  var startTranslateX = 0, startTranslateY = 0;

  function updateTransform() {
    canvas.style.transform = 'translate(' + translateX + 'px,' + translateY + 'px) scale(' + scale + ')';
  }

  function zoomBy(factor) {
    var newScale = scale * factor;
    if (newScale < 0.3) newScale = 0.3;
    if (newScale > 5) newScale = 5;
    scale = newScale;
    updateTransform();
  }

  function reset() {
    scale = 1;
    translateX = 0;
    translateY = 0;
    updateTransform();
  }

  // 工具栏按钮
  wrapper.querySelector('.mermaid-toolbar').addEventListener('click', function(e) {
    var btn = e.target.closest('button');
    if (!btn) return;
    var action = btn.dataset.action;
    switch(action) {
      case 'zoom-in': zoomBy(1.2); break;
      case 'zoom-out': zoomBy(1/1.2); break;
      case 'reset': reset(); break;
      case 'fullscreen': toggleFullscreen(wrapper); break;
      case 'source': showSource(source); break;
    }
  });

  // 滚轮缩放
  stage.addEventListener('wheel', function(e) {
    e.preventDefault();
    if (e.deltaY < 0) zoomBy(1.1);
    else zoomBy(1/1.1);
  }, { passive: false });

  // 拖拽平移
  stage.addEventListener('mousedown', function(e) {
    if (e.target.closest('button')) return;
    isDragging = true;
    dragStartX = e.clientX;
    dragStartY = e.clientY;
    startTranslateX = translateX;
    startTranslateY = translateY;
    canvas.classList.add('no-transition');
    e.preventDefault();
  });

  document.addEventListener('mousemove', function(e) {
    if (!isDragging) return;
    translateX = startTranslateX + (e.clientX - dragStartX);
    translateY = startTranslateY + (e.clientY - dragStartY);
    updateTransform();
  });

  document.addEventListener('mouseup', function() {
    if (isDragging) {
      isDragging = false;
      canvas.classList.remove('no-transition');
    }
  });

  // 触摸支持
  var touchStartX = 0, touchStartY = 0;
  var touchStartTranslateX = 0, touchStartTranslateY = 0;
  stage.addEventListener('touchstart', function(e) {
    if (e.touches.length === 1) {
      touchStartX = e.touches[0].clientX;
      touchStartY = e.touches[0].clientY;
      touchStartTranslateX = translateX;
      touchStartTranslateY = translateY;
    }
  }, { passive: true });

  stage.addEventListener('touchmove', function(e) {
    if (e.touches.length === 1) {
      e.preventDefault();
      translateX = touchStartTranslateX + (e.touches[0].clientX - touchStartX);
      translateY = touchStartTranslateY + (e.touches[0].clientY - touchStartY);
      updateTransform();
    }
  }, { passive: false });
}

function toggleFullscreen(wrapper) {
  if (wrapper.classList.contains('is-fullscreen')) {
    wrapper.classList.remove('is-fullscreen');
    // 恢复 body 滚动
    document.body.style.overflow = '';
  } else {
    wrapper.classList.add('is-fullscreen');
    document.body.style.overflow = 'hidden';
  }
}

// ESC 退出全屏
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    var fullscreenWrapper = document.querySelector('.mermaid-wrapper.is-fullscreen');
    if (fullscreenWrapper) {
      fullscreenWrapper.classList.remove('is-fullscreen');
      document.body.style.overflow = '';
    }
    var modal = document.getElementById('mermaidSourceModal');
    if (modal.classList.contains('active')) {
      modal.classList.remove('active');
    }
  }
});

// 点击模态框背景关闭
document.getElementById('mermaidSourceModal').addEventListener('click', function(e) {
  if (e.target === this) {
    this.classList.remove('active');
  }
});

function showSource(source) {
  currentMermaidSource = source;
  document.getElementById('mermaidSourceCode').textContent = source;
  document.getElementById('mermaidSourceModal').classList.add('active');
}

function copyMermaidSource() {
  if (navigator.clipboard) {
    navigator.clipboard.writeText(currentMermaidSource).then(function() {
      alert('已复制到剪贴板');
    });
  } else {
    var ta = document.createElement('textarea');
    ta.value = currentMermaidSource;
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); alert('已复制到剪贴板'); } catch(e) {}
    document.body.removeChild(ta);
  }
}

// 重新渲染所有 mermaid（主题切换时调用）
function rerenderMermaid() {
  document.querySelectorAll('.mermaid-wrapper').forEach(function(wrapper) {
    var source = wrapper.dataset.mermaidSource;
    if (!source) return;
    var canvas = wrapper.querySelector('.mermaid-canvas');
    if (!canvas) return;
    // 清除旧的 SVG
    canvas.innerHTML = '';
    canvas.removeAttribute('data-processed');
    canvas.textContent = source;
    try {
      mermaid.run({ nodes: [canvas] });
    } catch(e) {}
  });
}

// 初始化 Mermaid
if (window.mermaid) {
  mermaid.initialize({ startOnLoad: false, theme: 'default', securityLevel: 'loose' });
}

// 页面加载后渲染
window.addEventListener('load', function() {
  renderMarkdown();
});
</script>
</body>
</html>`, html.EscapeString(title), escapedBody)
}
