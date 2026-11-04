import type { AxiosResponse } from 'axios'
import type { ApiResponse } from '@/api/auth'

/** 业务错误：HTTP 200 但 code != 200 时抛出，调用方按 msg 展示。 */
export class BizError extends Error {
  constructor(public code: number, message: string) {
    super(message)
    this.name = 'BizError'
  }
}

/**
 * 校验业务 code 并返回 data。
 * 后端存在 HTTP 200 + code 400/403/404 的契约，axios 不会自动 reject。
 */
export function unwrap<T>(resp: AxiosResponse<ApiResponse<T>>): T {
  const body = resp?.data
  if (!body || body.code !== 200) {
    throw new BizError(body?.code ?? 500, body?.msg || '操作失败')
  }
  return body.data
}
