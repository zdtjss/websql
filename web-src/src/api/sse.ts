import { sanitizeError } from '@/utils/errorHandler.js'

export interface SSEOptions {
  url: string
  body?: unknown
  method?: 'POST' | 'GET'
  headers?: Record<string, string>
  onMessage: (data: string) => void
  onError?: (error: Error) => void
  onDone?: () => void
  signal?: AbortSignal
}

export interface SSEStreamHandle {
  readonly signal: AbortSignal
  abort: () => void
}

export class SSEError extends Error {
  readonly status?: number
  constructor(message: string, status?: number) {
    super(message)
    this.name = 'SSEError'
    this.status = status
  }
}

function getAuthHeader(): string {
  return sessionStorage.getItem('authentication') || ''
}

function getBaseUrl(): string {
  return import.meta.env.VITE_API_URL || ''
}

export function streamSSE(options: SSEOptions): SSEStreamHandle {
  const {
    url,
    body,
    method = 'POST',
    headers = {},
    onMessage,
    onError,
    onDone,
    signal: externalSignal,
  } = options

  const controller = new AbortController()

  if (externalSignal) {
    if (externalSignal.aborted) {
      controller.abort()
    } else {
      externalSignal.addEventListener('abort', () => controller.abort(), { once: true })
    }
  }

  const finalHeaders: Record<string, string> = {
    Authorization: getAuthHeader(),
    ...headers,
  }

  let fetchBody: BodyInit | undefined
  if (body !== undefined) {
    if (body instanceof FormData) {
      fetchBody = body
    } else if (typeof body === 'string') {
      fetchBody = body
    } else {
      fetchBody = JSON.stringify(body)
      if (!finalHeaders['Content-Type']) {
        finalHeaders['Content-Type'] = 'application/json'
      }
    }
  }

  const fetchInit: RequestInit = {
    method,
    headers: finalHeaders,
    signal: controller.signal,
  }
  if (fetchBody !== undefined) {
    fetchInit.body = fetchBody
  }

  const fullUrl = getBaseUrl() + url

  void (async () => {
    let settled = false
    let dataLines: string[] = []
    let doneSentinel = false

    const finish = (err?: Error): void => {
      if (settled) return
      settled = true
      if (err) {
        onError?.(err)
      } else {
        onDone?.()
      }
    }

    const flushData = (): void => {
      if (dataLines.length === 0) return
      const data = dataLines.join('\n').trim()
      dataLines = []
      if (!data) return
      if (data === '[DONE]') {
        doneSentinel = true
        return
      }
      onMessage(data)
    }

    const processLine = (rawLine: string): void => {
      const line = rawLine.replace(/\r$/, '')
      if (line === '') {
        flushData()
        return
      }
      if (line.startsWith(':')) {
        return
      }
      if (line.startsWith('data:')) {
        const payload = line.slice(5)
        dataLines.push(payload.startsWith(' ') ? payload.slice(1) : payload)
      }
    }

    try {
      const resp = await fetch(fullUrl, fetchInit)

      if (!resp.ok) {
        let msg = `请求失败: HTTP ${resp.status}`
        try {
          const data = (await resp.json()) as { msg?: unknown }
          if (typeof data.msg === 'string' && data.msg) {
            msg = sanitizeError(data.msg)
          }
        } catch {
          // 响应体非 JSON
        }
        throw new SSEError(msg, resp.status)
      }

      if (!resp.body) {
        throw new SSEError('响应体为空，无法读取流')
      }

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''

      while (!doneSentinel) {
        const { done, value } = await reader.read()
        if (value) {
          buf += decoder.decode(value, { stream: true })
        }
        if (done) {
          buf += decoder.decode()
          if (buf) {
            processLine(buf)
          }
          flushData()
          break
        }
        const lines = buf.split('\n')
        buf = lines.pop() ?? ''
        for (const line of lines) {
          processLine(line)
          if (doneSentinel) break
        }
      }

      finish()
    } catch (err) {
      if (err instanceof Error) {
        if (err.name === 'AbortError') {
          finish()
        } else {
          finish(err)
        }
      } else {
        finish(new Error(String(err)))
      }
    }
  })()

  return {
    signal: controller.signal,
    abort: () => controller.abort(),
  }
}
