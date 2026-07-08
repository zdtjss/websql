import type { AxiosResponse } from 'axios'

import type { ApiAdapter, ApiResponse, RequestConfig } from './adapter'
import { lookupRoute } from './route-table'
import { wailsSSEStream } from './adapter-sse-wails'
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

  const runtime = await ensureRuntime()
  const req: WailsInvokeRequest = {
    module: route.module,
    method: route.method,
    authorization: getAuthHeader(),
    params: config.params,
    body: config.body,
  }

  const raw = (await runtime.Call.ByName('main.DesktopApp.Invoke', req)) as ApiResponse<T>
  const result = raw ?? { code: 500, msg: '空响应' }
  return buildAxiosResponse<ApiResponse<T>>(result)
}

async function invokeWailsBlob(config: RequestConfig): Promise<AxiosResponse<Blob>> {
  const route = lookupRoute(config.url)
  if (!route) {
    throw new Error(`桌面模式不支持的路由: ${config.url}`)
  }

  const runtime = await ensureRuntime()
  const req: WailsInvokeRequest = {
    module: route.module,
    method: route.method,
    authorization: getAuthHeader(),
    params: config.params,
    body: config.body,
  }

  const result = (await runtime.Call.ByName('main.DesktopApp.InvokeBlob', req)) as {
    path: string
    filename: string
    mime: string
  }
  const savePath = (await runtime.Call.ByName('main.DesktopApp.SaveFileDialog', result.filename)) as string
  if (savePath) {
    const fileData = (await runtime.Call.ByName('main.DesktopApp.ReadFile', { path: result.path })) as { data: number[] }
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
