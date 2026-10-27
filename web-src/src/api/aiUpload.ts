/**
 * AI 对话文件上传 API（新上传链路，与 legacy uploadExcel 并存）。
 *
 * 接口契约（后端 internal/ai/agent/upload_handler.go）：
 *   - POST /ai/agent/uploadFile          上传暂存（不解析内容，返回 fileId/kind/expireAt）
 *   - POST /ai/agent/uploadFile/delete   删除暂存文件
 *   - GET  /ai/agent/uploadFile/content  读取文件内容（卡片预览用）
 *   - GET  /ai/agent/fileSupport         环境能力自检（解析器可用性/体积上限）
 */
import http from './index'
import type { AxiosResponse } from 'axios'
import type { ApiResponse } from './auth'
import type { FileSupport } from '@/views/ai/utils/fileTypes'

/** 上传成功返回的暂存文件信息 */
export interface UploadFileResult {
  fileId: string
  fileName: string
  fileSize: number
  fileExt: string
  /** 内容类别：table | text | doc | image | video */
  fileKind: string
  /** 兼容字段：excel | csv | markdown | docx | pdf | txt | image | video */
  fileType: string
  mime: string
  /** 当前生效的解析实现（go-xxx / python:xxx），仅在读取内容时使用 */
  parser: string
  /** 是否走了降级实现（解析质量可能有限） */
  degraded: boolean
  /** 暂存过期时间（秒级时间戳） */
  expireAt: number
}

/** 上传选项 */
export interface UploadChatFileOptions {
  /** 当前选中的模型 ID（后端据此做能力前置校验；缺省用全局选中模型） */
  modelId?: string
  /** 上传进度回调（0-100） */
  onProgress?: (percent: number) => void
  /** 取消信号 */
  signal?: AbortSignal
  /** 登录过期回调（401 时触发） */
  onSessionExpired?: () => void
}

/** 上传错误（携带后端业务文案） */
export class UploadFileError extends Error {
  /** 是否登录过期 */
  readonly unauthorized: boolean

  constructor(message: string, unauthorized = false) {
    super(message)
    this.name = 'UploadFileError'
    this.unauthorized = unauthorized
  }
}

/**
 * 上传文件到服务端暂存区（XHR 实现：axios 不便获取上传进度的细粒度事件）。
 *
 * 失败时抛出 UploadFileError（message 为后端业务文案或网络错误提示）。
 */
export function uploadChatFile(file: File, options: UploadChatFileOptions = {}): Promise<UploadFileResult> {
  const apiBase = import.meta.env.VITE_API_URL || ''
  const url = apiBase + '/ai/agent/uploadFile'

  return new Promise<UploadFileResult>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', url, true)
    xhr.setRequestHeader('Authorization', sessionStorage.getItem('authentication') || '')

    const form = new FormData()
    form.append('file', file)
    if (options.modelId) form.append('modelId', options.modelId)

    if (options.onProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && e.total > 0) {
          options.onProgress!(Math.round((e.loaded / e.total) * 100))
        }
      }
    }

    xhr.onload = () => {
      let parsed: { code?: number; msg?: string; data?: UploadFileResult } = {}
      try {
        parsed = JSON.parse(xhr.responseText || '{}')
      } catch {
        /* 响应非 JSON：按 HTTP 状态兜底 */
      }
      if (xhr.status === 401 || parsed.code === 401) {
        options.onSessionExpired?.()
        reject(new UploadFileError(parsed.msg || '登录已过期，请重新登录', true))
        return
      }
      if (xhr.status >= 200 && xhr.status < 300 && parsed.code === 200 && parsed.data?.fileId) {
        resolve(parsed.data)
        return
      }
      reject(new UploadFileError(parsed.msg || `上传失败（HTTP ${xhr.status}）`))
    }

    xhr.onerror = () => reject(new UploadFileError('网络异常，文件上传失败'))
    xhr.ontimeout = () => reject(new UploadFileError('上传超时，请重试'))
    xhr.onabort = () => reject(new UploadFileError('上传已取消'))

    if (options.signal) {
      if (options.signal.aborted) {
        reject(new UploadFileError('上传已取消'))
        return
      }
      options.signal.addEventListener('abort', () => xhr.abort(), { once: true })
    }

    xhr.send(form)
  })
}

/** 删除暂存文件，对应 POST /ai/agent/uploadFile/delete */
export function deleteChatFile(fileId: string): Promise<AxiosResponse<ApiResponse>> {
  return http.post('/ai/agent/uploadFile/delete', { fileId })
}

/** 环境能力自检，对应 GET /ai/agent/fileSupport */
export function fetchFileSupport(): Promise<AxiosResponse<ApiResponse<FileSupport>>> {
  return http.get('/ai/agent/fileSupport')
}

/** 拼接暂存文件的内容访问地址（卡片下载/预览；需携带 Authorization 头，故用 blob 拉取） */
export function uploadFileContentUrl(fileId: string): string {
  const apiBase = import.meta.env.VITE_API_URL || ''
  return apiBase + '/ai/agent/uploadFile/content?fileId=' + encodeURIComponent(fileId)
}

/** 暂存文件内容（blob + 服务端回填的文件名） */
export interface ChatFileContent {
  blob: Blob
  /** 服务端 Content-Disposition 中的文件名（缺失时由调用方回退到本地文件名） */
  fileName?: string
}

/**
 * 拉取暂存文件内容（卡片点击下载/图片预览/视频播放）。
 *
 * 内容接口需要 Authorization 头，无法直接用于 <img>/<video>/<a> 的 src/href，
 * 因此统一用 fetch 取 blob，由调用方转成 ObjectURL 使用。
 * 失败时抛出 Error（message 为后端业务文案，如「文件不存在或已过期」）。
 */
export async function fetchChatFileContent(fileId: string): Promise<ChatFileContent> {
  const resp = await fetch(uploadFileContentUrl(fileId), {
    method: 'GET',
    headers: { Authorization: sessionStorage.getItem('authentication') || '' },
  })
  if (!resp.ok) {
    let msg = `文件读取失败（HTTP ${resp.status}）`
    try {
      const data = await resp.json()
      if (data?.msg) msg = data.msg
    } catch {
      /* 响应非 JSON：沿用 HTTP 状态提示 */
    }
    throw new Error(msg)
  }
  return {
    blob: await resp.blob(),
    fileName: parseContentDispositionFileName(resp.headers.get('Content-Disposition')),
  }
}

/** 解析 Content-Disposition 中的文件名（兼容 RFC 5987 的 filename* 与普通 filename） */
function parseContentDispositionFileName(disposition: string | null): string | undefined {
  if (!disposition) return undefined
  const encoded = /filename\*=(?:[^']*)''([^;]+)/i.exec(disposition)
  if (encoded) {
    try {
      return decodeURIComponent(encoded[1].trim())
    } catch {
      /* 解码失败回退普通 filename */
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(disposition)
  return plain ? plain[1].trim() : undefined
}
