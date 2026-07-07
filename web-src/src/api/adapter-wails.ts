import type { AxiosResponse } from 'axios'

import type { ApiAdapter, ApiResponse, RequestConfig } from './adapter'
import { lookupRoute } from './route-table'
import { wailsSSEStream } from './adapter-sse-wails'
import type { SSEOptions, SSEStreamHandle } from './sse'

declare global {
  interface Window {
    go?: {
      desktop?: {
        App?: {
          Invoke?: (req: unknown) => Promise<unknown>
          InvokeBlob?: (req: unknown) => Promise<{ path: string; filename: string; mime: string }>
          StartStream?: (req: unknown) => Promise<void>
          CancelStream?: (sessionId: string) => Promise<void>
          OpenFileDialog?: (filters?: unknown) => Promise<string>
          SaveFileDialog?: (filename?: string) => Promise<string>
          WriteFile?: (path: string, data: unknown) => Promise<void>
        }
      }
    }
  }
}

function getAuthHeader(): string {
  return sessionStorage.getItem('authentication') || ''
}

function buildAxiosResponse<T>(data: T, status = 200): AxiosResponse<T> {
  return {
    data,
    status,
    statusText: 'OK',
    headers: {},
    config: {} as any,
  }
}

interface WailsInvokeRequest {
  module: string
  method: string
  authorization: string
  params?: Record<string, any>
  body?: unknown
  isUpload?: boolean
  filePath?: string
}

async function invokeWails<T>(config: RequestConfig): Promise<AxiosResponse<ApiResponse<T>>> {
  const route = lookupRoute(config.url)
  if (!route) {
    throw new Error(`桌面模式不支持的路由: ${config.url}`)
  }

  const app = window.go?.desktop?.App
  if (!app?.Invoke) {
    throw new Error('Wails runtime 未就绪')
  }

  const req: WailsInvokeRequest = {
    module: route.module,
    method: route.method,
    authorization: getAuthHeader(),
    params: config.params,
    body: config.body,
  }

  const raw = await app.Invoke(req)
  const result = (raw as ApiResponse<T>) ?? { code: 500, msg: '空响应' }
  return buildAxiosResponse<ApiResponse<T>>(result)
}

async function invokeWailsBlob(config: RequestConfig): Promise<AxiosResponse<Blob>> {
  const route = lookupRoute(config.url)
  if (!route) {
    throw new Error(`桌面模式不支持的路由: ${config.url}`)
  }

  const app = window.go?.desktop?.App
  if (!app?.InvokeBlob || !app?.SaveFileDialog) {
    throw new Error('Wails runtime 未就绪')
  }

  const req: WailsInvokeRequest = {
    module: route.module,
    method: route.method,
    authorization: getAuthHeader(),
    params: config.params,
    body: config.body,
  }

  const result = await app.InvokeBlob(req)
  const savePath = await app.SaveFileDialog(result.filename)
  if (savePath) {
    const fileData = (await app.Invoke({
      module: 'fileio',
      method: 'ReadFile',
      params: { path: result.path },
    })) as { data: number[] }
    const bytes = new Uint8Array(fileData.data ?? [])
    const blob = new Blob([bytes], { type: result.mime })
    return buildAxiosResponse<Blob>(blob)
  }
  return buildAxiosResponse<Blob>(new Blob())
}

export const wailsAdapter: ApiAdapter = {
  request<T = unknown>(config: RequestConfig): Promise<AxiosResponse<ApiResponse<T>>> {
    return invokeWails<T>(config)
  },

  requestBlob(config: RequestConfig): Promise<AxiosResponse<Blob>> {
    return invokeWailsBlob(config)
  },

  streamSSE(options: SSEOptions): SSEStreamHandle {
    return wailsSSEStream(options)
  },
}
