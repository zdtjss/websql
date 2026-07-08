import { lookupRoute } from './route-table'
import type { SSEOptions, SSEStreamHandle } from './sse'

type WailsRuntime = typeof import('@wailsio/runtime')

let _runtime: WailsRuntime | null = null

async function ensureRuntime(): Promise<WailsRuntime> {
  if (!_runtime) {
    _runtime = await import('@wailsio/runtime')
  }
  return _runtime
}

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

  const dataCb = (event: { data?: unknown }) => {
    const data = event.data
    if (data === '[DONE]') {
      options.onDone?.()
      return
    }
    options.onMessage(typeof data === 'string' ? data : JSON.stringify(data))
  }
  const doneCb = (_event?: unknown) => options.onDone?.()
  const errorCb = (event: { data?: unknown }) =>
    options.onError?.(new Error(typeof event.data === 'string' ? event.data : 'SSE 错误'))

  const cancels: Array<() => void> = []
  let cleanedUp = false

  const cleanup = () => {
    if (cleanedUp) return
    cleanedUp = true
    cancels.forEach((cancel) => cancel())
    cancels.length = 0
  }

  void ensureRuntime()
    .then((runtime) => {
      cancels.push(runtime.Events.On(eventName, dataCb))
      cancels.push(runtime.Events.On(doneEvent, doneCb))
      cancels.push(runtime.Events.On(errorEvent, errorCb))

      return runtime.Call.ByName('main.DesktopApp.StartStream', {
        sessionId,
        module: route.module,
        method: route.method,
        authorization: getAuthHeader(),
        body: options.body,
        params: options.url.includes('?')
          ? Object.fromEntries(new URLSearchParams(options.url.split('?')[1]))
          : undefined,
      })
    })
    .catch((err) => {
      cleanup()
      options.onError?.(err instanceof Error ? err : new Error(String(err)))
    })

  controller.signal.addEventListener('abort', () => {
    void ensureRuntime()
      .then((runtime) => runtime.Call.ByName('main.DesktopApp.CancelStream', sessionId))
      .catch(() => {})
      .finally(cleanup)
  })

  return {
    signal: controller.signal,
    abort: () => {
      controller.abort()
      void ensureRuntime()
        .then((runtime) => runtime.Call.ByName('main.DesktopApp.CancelStream', sessionId))
        .catch(() => {})
        .finally(cleanup)
    },
  }
}
