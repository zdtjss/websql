import type { AxiosResponse } from 'axios'

import http from './index'
import { streamSSE, type SSEOptions, type SSEStreamHandle } from './sse'
import type { ApiAdapter, RequestConfig } from './adapter'

function toAxiosConfig(config: RequestConfig): Record<string, any> {
  const axiosConfig: Record<string, any> = {
    method: config.method,
    url: config.url,
    params: config.params,
    headers: config.headers,
    signal: config.signal,
    responseType: config.responseType,
    skipGlobalError: config.skipGlobalError,
  }
  if (config.body !== undefined) {
    axiosConfig.data = config.body
  }
  return axiosConfig
}

export const httpAdapter: ApiAdapter = {
  async request<T = unknown>(config: RequestConfig): Promise<AxiosResponse<any>> {
    const axiosConfig = toAxiosConfig(config)
    return http.request(axiosConfig) as Promise<AxiosResponse<any>>
  },

  async requestBlob(config: RequestConfig): Promise<AxiosResponse<Blob>> {
    const axiosConfig = toAxiosConfig({ ...config, responseType: 'blob' })
    return http.request(axiosConfig) as Promise<AxiosResponse<Blob>>
  },

  streamSSE(options: SSEOptions): SSEStreamHandle {
    return streamSSE(options)
  },
}
