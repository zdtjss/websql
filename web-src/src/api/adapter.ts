import type { AxiosResponse } from 'axios'

import { httpAdapter } from './adapter-http'
import { wailsAdapter } from './adapter-wails'
import type { SSEOptions, SSEStreamHandle } from './sse'

export interface ApiResponse<T = unknown> {
  code: number
  msg?: string
  data: T
}

export interface RequestConfig {
  method: 'GET' | 'POST'
  url: string
  params?: Record<string, any>
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
  responseType?: 'json' | 'blob'
  skipGlobalError?: boolean
}

export interface ApiAdapter {
  request<T = unknown>(config: RequestConfig): Promise<AxiosResponse<ApiResponse<T>>>
  requestBlob(config: RequestConfig): Promise<AxiosResponse<Blob>>
  streamSSE(options: SSEOptions): SSEStreamHandle
}

const isWails = import.meta.env.VITE_WAILS === 'true'

export const api: ApiAdapter = isWails ? wailsAdapter : httpAdapter
