import { lookupRoute } from './route-table'
import type { SSEOptions, SSEStreamHandle } from './sse'

function getAuthHeader(): string {
  return sessionStorage.getItem('authentication') || ''
}

export function wailsSSEStream(options: SSEOptions): SSEStreamHandle {
  const route = lookupRoute(options.url)
  if (!route) {
    options.onError?.(new Error(`桌面模式不支持的路由: ${options.url}`))
    return {
      signal: new AbortController().signal,
      abort: () => {},
    }
  }

  const app = window.go?.desktop?.App
  if (!app?.StartStream || !app?.CancelStream) {
    options.onError?.(new Error('Wails runtime 未就绪'))
    return {
      signal: new AbortController().signal,
      abort: () => {},
    }
  }

  const sessionId = `sse_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`
  const controller = new AbortController()

  if (options.signal) {
    if (options.signal.aborted) {
      controller.abort()
    } else {
      options.signal.addEventListener('abort', () => controller.abort(), { once: true })
    }
  }

  const eventName = `sse:${sessionId}:data`
  const doneEvent = `sse:${sessionId}:done`
  const errorEvent = `sse:${sessionId}:error`

  const onEvent = (window as any).runtime?.EventsOn as
    | ((name: string, cb: (...args: any[]) => void) => void)
    | undefined

  const offEvent = (window as any).runtime?.EventsOff as
    | ((name: string, ...cb: Array<(...args: any[]) => void>) => void)
    | undefined

  const dataCb = (data: string) => {
    if (data === '[DONE]') {
      options.onDone?.()
      return
    }
    options.onMessage(data)
  }
  const doneCb = () => options.onDone?.()
  const errorCb = (err: string) => options.onError?.(new Error(err))

  onEvent?.(eventName, dataCb)
  onEvent?.(doneEvent, doneCb)
  onEvent?.(errorEvent, errorCb)

  const cleanup = () => {
    offEvent?.(eventName, dataCb)
    offEvent?.(doneEvent, doneCb)
    offEvent?.(errorEvent, errorCb)
  }

  void app
    .StartStream({
      sessionId,
      module: route.module,
      method: route.method,
      authorization: getAuthHeader(),
      body: options.body,
      params: options.url.includes('?')
        ? Object.fromEntries(new URLSearchParams(options.url.split('?')[1]))
        : undefined,
    })
    .catch((err) => {
      cleanup()
      options.onError?.(err instanceof Error ? err : new Error(String(err)))
    })

  controller.signal.addEventListener('abort', () => {
    void app.CancelStream?.(sessionId).catch(() => {})
    cleanup()
  })

  return {
    signal: controller.signal,
    abort: () => {
      controller.abort()
      void app.CancelStream?.(sessionId).catch(() => {})
      cleanup()
    },
  }
}
