/**
 * 监控面板 AI 分析（SSE 流式）共用逻辑。
 *
 * 原 DatabaseMonitorPanel.vue 中 "服务器变量" 与 "状态指标" 两个 Tab
 * 共用一套 AI 分析状态（通过 aiKind 区分）。拆分组件后，两个 Tab 各自
 * 持有一份独立状态，但底层请求/解析逻辑完全相同，故抽取为本 composable。
 */
import { computed, ref, shallowRef } from 'vue'
import { ElMessage } from 'element-plus'
import { getMarkdownRenderer } from '@/utils/lazyDeps'

export type MonitorAIKind = 'variables' | 'status'

export interface MonitorAISourceItem {
  name: string
  value: string | number | null | undefined
}

export interface UseMonitorAIOptions {
  /** 分析类型：variables | status */
  kind: MonitorAIKind
  /** 当前连接 ID getter */
  connId: () => string | undefined
  /** 数据库类型 getter（由 variables/status 接口填充） */
  dbType: () => string
  /** 数据库版本 getter */
  dbVersion: () => string
  /** 取当前已过滤的待分析数据列表 */
  getSourceList: () => MonitorAISourceItem[]
}

export function useMonitorAI(options: UseMonitorAIOptions) {
  const aiAnalyzing = ref(false)
  const aiContent = ref('')
  const aiThinking = ref('')
  const aiError = ref('')
  const aiExpanded = ref(false)
  let aiAbortController: AbortController | null = null
  // markdown 渲染器实例（懒加载），使用 shallowRef 避免被 Vue 深度响应化
  const mdRenderer = shallowRef<{ render: (src: string) => string } | null>(null)

  async function ensureMdRenderer() {
    if (mdRenderer.value) return mdRenderer.value
    mdRenderer.value = await getMarkdownRenderer()
    return mdRenderer.value
  }

  // AI 分析结果渲染为 HTML
  const renderedAIContent = computed(() => {
    if (!aiContent.value || !mdRenderer.value) return ''
    try {
      return mdRenderer.value.render(aiContent.value)
    } catch {
      return aiContent.value
    }
  })

  // 发起 AI 分析（SSE 流式）
  async function runAIAnalyze() {
    if (aiAnalyzing.value) return
    const sourceList = options.getSourceList()
    if (!sourceList || sourceList.length === 0) {
      ElMessage.warning('当前没有可分析的数据')
      return
    }

    stopAIAnalyze()
    aiAnalyzing.value = true
    aiContent.value = ''
    aiThinking.value = ''
    aiError.value = ''
    aiExpanded.value = true

    const controller = new AbortController()
    aiAbortController = controller

    try {
      await ensureMdRenderer()
      const auth = sessionStorage.getItem('authentication') || ''
      const connId = options.connId()
      const resp = await fetch('/api/monitor/aiAnalyze', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': auth,
        },
        body: JSON.stringify({
          connId,
          kind: options.kind,
          dbType: options.dbType(),
          version: options.dbVersion(),
          data: sourceList.map(r => ({ name: r.name, value: String(r.value ?? '') })),
        }),
        signal: controller.signal,
      })

      // 非 SSE 响应（如 AI 未配置返回的 JSON 错误）：按 JSON 解析错误信息
      const contentType = resp.headers.get('Content-Type') || ''
      if (!contentType.includes('text/event-stream')) {
        let msg = 'AI 服务请求失败 (HTTP ' + resp.status + ')'
        try {
          const errData = await resp.json()
          if (errData?.msg) msg = errData.msg
        } catch { /* ignore */ }
        aiError.value = msg
        return
      }

      const reader = resp.body!.getReader()
      const decoder = new TextDecoder()
      let buf = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() || ''

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const data = line.slice(6).trim()
          if (!data) continue

          let chunk: { type?: string; content?: string }
          try {
            chunk = JSON.parse(data)
          } catch {
            continue
          }

          switch (chunk.type) {
            case 'thinking':
              aiThinking.value += chunk.content || ''
              break
            case 'content':
              aiContent.value += chunk.content || ''
              break
            case 'error':
              aiError.value = chunk.content || 'AI 处理出错'
              break
            case 'done':
              break
          }
        }
      }
    } catch (e: unknown) {
      const err = e as { name?: string; message?: string }
      if (err?.name !== 'AbortError') {
        aiError.value = 'AI 服务请求失败: ' + (err?.message || '未知错误')
      }
    } finally {
      aiAnalyzing.value = false
      aiAbortController = null
      // 分析完成后保持展开，让用户看到结果；无任何内容时折叠
      if (!aiThinking.value && !aiContent.value && !aiError.value) {
        aiExpanded.value = false
      }
    }
  }

  function stopAIAnalyze() {
    if (aiAbortController) {
      aiAbortController.abort()
      aiAbortController = null
    }
  }

  function toggleAIExpand() {
    aiExpanded.value = !aiExpanded.value
  }

  return {
    aiAnalyzing,
    aiContent,
    aiThinking,
    aiError,
    aiExpanded,
    renderedAIContent,
    runAIAnalyze,
    stopAIAnalyze,
    toggleAIExpand,
  }
}
