const STACK_LINE_PATTERN = /^\s*(goroutine\s+\d+|runtime\/|stack\s+trace|panic\s+recovered|fatal\s+error|panic\(|\.go:\d+\s*$)/i
const LOG_PREFIX_PATTERN = /^\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2}\s+/
const HEX_ADDR_PATTERN = /0x[0-9a-fA-F]+/g

const CREDENTIAL_PATTERNS = [
  { pattern: /(password|passwd|pwd|secret|token|api[_-]?key)\s*[=:]\s*\S+/gi, replace: '$1=***' },
  { pattern: /(dsn|data\s*source\s*name)\s*[=:]\s*\S+/gi, replace: '$1=***' },
  { pattern: /(authorization|cookie)\s*[:=]\s*\S+/gi, replace: '$1=***' },
]

const IP_PORT_PATTERN = /\b(\d{1,3}\.\d{1,3})\.\d{1,3}\.\d{1,3}(:\d+)\b/g
const FILE_PATH_PATTERN = /(\/tmp\/|\/var\/|\/home\/|[A-Z]:\\|\\Users\\)[^\s]*/gi

function extractRawMsg(err) {
  if (!err) return ''
  if (err instanceof Error) return err.message || ''
  if (typeof err === 'string') return err
  if (typeof err === 'object' && err.message) return err.message
  try { return String(err) } catch { return '' }
}

function redactCredentials(msg) {
  for (const cr of CREDENTIAL_PATTERNS) {
    msg = msg.replace(cr.pattern, cr.replace)
  }
  msg = msg.replace(IP_PORT_PATTERN, '$1.***$2')
  return msg
}

function redactFilePaths(msg) {
  return msg.replace(FILE_PATH_PATTERN, '***')
}

function extractErrorMsg(msg) {
  msg = (msg || '').trim()
  if (!msg) return '系统错误'

  const lines = msg.split('\n')
  const meaningfulLines = []

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

export function sanitizeError(err) {
  const rawMsg = extractRawMsg(err)
  return extractErrorMsg(rawMsg)
}

export function sanitizeErrMsg(prefix, err) {
  return prefix + sanitizeError(err)
}
