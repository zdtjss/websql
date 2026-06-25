/**
 * 错误处理工具
 *
 * 提供：
 * 1. 从任意错误对象中提取原始消息
 * 2. 清理错误消息中的敏感信息（凭据、IP、文件路径）
 * 3. 过滤堆栈/日志前缀等噪声
 * 4. 从 axios 错误中提取后端业务消息
 * 5. 统一错误处理（展示 + 返回消息）
 */

// 匹配 Go/通用堆栈行、panic、fatal 等
const STACK_LINE_PATTERN = /^\s*(goroutine\s+\d+|runtime\/|stack\s+trace|panic\s+recovered|fatal\s+error|panic\(|\.go:\d+\s*$)/i
// 匹配日志前缀，如 2024/01/01 12:00:00
const LOG_PREFIX_PATTERN = /^\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2}\s+/
// 匹配十六进制地址（如 0xc000010028）
const HEX_ADDR_PATTERN = /0x[0-9a-fA-F]+/g

// 凭据脱敏规则：将 password=xxx、token=xxx 等替换为 ***
const CREDENTIAL_PATTERNS: { pattern: RegExp; replace: string }[] = [
  { pattern: /(password|passwd|pwd|secret|token|api[_-]?key)\s*[=:]\s*\S+/gi, replace: '$1=***' },
  { pattern: /(dsn|data\s*source\s*name)\s*[=:]\s*\S+/gi, replace: '$1=***' },
  { pattern: /(authorization|cookie)\s*[:=]\s*\S+/gi, replace: '$1=***' },
]

// IP:端口脱敏（保留前两段，后两段打码）
const IP_PORT_PATTERN = /\b(\d{1,3}\.\d{1,3})\.\d{1,3}\.\d{1,3}(:\d+)\b/g
// 服务器文件路径脱敏
const FILE_PATH_PATTERN = /(\/tmp\/|\/var\/|\/home\/|[A-Z]:\\|\\Users\\)[^\s]*/gi

/** axios 错误响应结构（仅声明需要使用的字段） */
export interface AxiosErrorResponse {
  data?: unknown
  status: number
  statusText?: string
  headers?: unknown
  config?: unknown
}

/** 类 axios 错误对象结构（仅取需要访问的字段） */
interface AxiosLikeError {
  response?: AxiosErrorResponse
  code?: string
  message?: string
}

/** handleError 的可选配置 */
export interface HandleErrorOptions {
  /** 是否静默处理（不弹出 ElMessage 提示） */
  silent?: boolean
}

/** 判断值是否为普通对象（非 null） */
function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null
}

/**
 * 从任意错误对象中提取原始消息字符串
 * @param err 错误对象（任意类型）
 * @returns 原始消息字符串
 */
function extractRawMsg(err: unknown): string {
  if (!err) return ''
  if (err instanceof Error) return err.message || ''
  if (typeof err === 'string') return err
  if (isPlainObject(err)) {
    const msg = err.message
    if (msg) return String(msg)
  }
  try { return String(err) } catch { return '' }
}

/** 对消息中的凭据、IP、文件路径等敏感信息进行脱敏 */
function redactCredentials(msg: string): string {
  for (const cr of CREDENTIAL_PATTERNS) {
    msg = msg.replace(cr.pattern, cr.replace)
  }
  msg = msg.replace(IP_PORT_PATTERN, '$1.***$2')
  return msg
}

/** 对消息中的服务器文件路径进行脱敏 */
function redactFilePaths(msg: string): string {
  return msg.replace(FILE_PATH_PATTERN, '***')
}

/**
 * 清理原始消息：去除堆栈行、日志前缀、十六进制地址，
 * 并对敏感信息脱敏，最终返回用户可读的错误消息
 * @param msg 原始消息
 * @returns 清理后的消息
 */
function extractErrorMsg(msg: string): string {
  msg = (msg || '').trim()
  if (!msg) return '系统错误'

  const lines = msg.split('\n')
  const meaningfulLines: string[] = []

  for (const line of lines) {
    let trimmed = line.trim()
    if (!trimmed) continue

    if (STACK_LINE_PATTERN.test(trimmed)) continue

    let cleaned = trimmed.replace(LOG_PREFIX_PATTERN, '').trim()
    if (!cleaned) continue

    if (STACK_LINE_PATTERN.test(cleaned)) continue

    if (cleaned.startsWith('PANIC:')) {
      cleaned = cleaned.slice(6).trim()
      if (!cleaned || STACK_LINE_PATTERN.test(cleaned)) continue
    }

    cleaned = cleaned.replace(HEX_ADDR_PATTERN, '').trim()
    if (cleaned) meaningfulLines.push(cleaned)
  }

  if (meaningfulLines.length === 0) return '系统内部错误'

  let result = meaningfulLines.join('; ')
  result = redactCredentials(result)
  result = redactFilePaths(result)

  if (result.length > 500) {
    result = result.substring(0, 500) + '...'
  }

  return result
}

/**
 * 清理错误对象为可读字符串
 * @param err 错误对象
 * @returns 清理后的错误消息
 */
export function sanitizeError(err: unknown): string {
  const rawMsg = extractRawMsg(err)
  return extractErrorMsg(rawMsg)
}

/**
 * 拼接前缀与清理后的错误消息
 * @param prefix 前缀文本
 * @param err 错误对象
 * @returns 拼接后的消息
 */
export function sanitizeErrMsg(prefix: string, err: unknown): string {
  return prefix + sanitizeError(err)
}

/**
 * 从 axios 错误中提取后端返回的业务错误消息
 * @param err axios 错误对象
 * @returns 用户可读的错误消息
 */
function extractAxiosErrorMsg(err: unknown): string {
  const e = (isPlainObject(err) ? err : {}) as AxiosLikeError
  // 网络错误（无响应）
  if (!e.response) {
    if (e.code === 'ECONNABORTED' || e.message?.includes('timeout')) {
      return '请求超时，请检查网络或稍后重试'
    }
    if (e.message?.includes('Network Error')) {
      return '网络连接失败，请检查网络'
    }
    return sanitizeError(err)
  }
  // 有 HTTP 响应
  const resp = e.response
  const respData = resp.data
  // 后端统一响应格式 { code, msg, data }
  if (respData && typeof respData === 'object') {
    const dataObj = respData as { msg?: unknown }
    if (dataObj.msg) {
      return extractErrorMsg(String(dataObj.msg))
    }
    // blob 错误体
    if (respData instanceof Blob) {
      return `请求失败 (HTTP ${resp.status})`
    }
  }
  // 纯文本
  if (typeof respData === 'string' && respData) {
    return extractErrorMsg(respData)
  }
  return `请求失败 (HTTP ${resp.status})`
}

/**
 * 统一错误处理：从 axios 错误中提取业务消息并展示给用户
 * @param err 错误对象（通常是 axios 错误）
 * @param context 操作上下文描述，如"加载备份列表"
 * @param options 可选配置 { silent: false }
 * @returns 提取出的错误消息
 */
export function handleError(
  err: unknown,
  context: string = '',
  options: HandleErrorOptions = {}
): string {
  const msg = extractAxiosErrorMsg(err) || sanitizeError(err)
  const display = context ? `${context}失败: ${msg}` : msg
  if (!options.silent) {
    // 动态导入避免循环依赖
    import('element-plus').then(({ ElMessage }) => {
      ElMessage.error(display)
    })
  }
  return display
}
