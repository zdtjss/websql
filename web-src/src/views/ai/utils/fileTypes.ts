/**
 * 上传文件类型矩阵（前端唯一事实源之一）。
 *
 * 与后端 internal/ai/agent/fileparse/matrix.go 的 extKinds 保持一致：
 * 新增格式时两边同步登记。前端用于「文件选择阶段」的前置校验、
 * 文件卡片图标/类别展示，以及模型能力（vision/video）匹配提示。
 */

/** 文件内容类别（与后端 fileparse.Kind 对齐） */
export type FileKind = 'table' | 'text' | 'doc' | 'image' | 'video'

/** 扩展名 → 内容类别 */
export const EXT_KIND_MAP: Record<string, FileKind> = {
  '.xlsx': 'table',
  '.xls': 'table',
  '.csv': 'table',
  '.txt': 'text',
  '.md': 'text',
  '.markdown': 'text',
  '.docx': 'doc',
  '.pdf': 'doc',
  '.jpg': 'image',
  '.jpeg': 'image',
  '.png': 'image',
  '.mp4': 'video',
}

/** 支持的扩展名列表（稳定顺序） */
export const SUPPORTED_EXTS: string[] = Object.keys(EXT_KIND_MAP)

/** el-upload 的 accept 属性值 */
export const ACCEPT_ATTR = SUPPORTED_EXTS.join(',')

/** 支持格式的展示文案（错误提示用） */
export const SUPPORTED_EXTS_TEXT = SUPPORTED_EXTS.map((e) => e.slice(1)).join('/')

/** 图片类上传上限（需经 base64 注入多模态请求；与后端 imageMaxSize 一致） */
export const IMAGE_MAX_SIZE = 10 << 20
/** 其余类别上传上限（与后端 defaultMaxSize 一致） */
export const DEFAULT_MAX_SIZE = 20 << 20

/** 模型能力标识（与后端 system.CapVision / CapVideo 一致） */
export type ModelCapability = 'vision' | 'video'

/** 归一化扩展名（小写、前导点） */
export function normalizeExt(ext: string): string {
  const e = (ext || '').trim().toLowerCase()
  if (!e) return ''
  return e.startsWith('.') ? e : '.' + e
}

/** 从文件名提取扩展名（小写含点） */
export function extOf(fileName: string): string {
  const idx = (fileName || '').lastIndexOf('.')
  if (idx < 0) return ''
  return fileName.slice(idx).toLowerCase()
}

/** 扩展名 → 类别（未知返回 ''） */
export function kindOfExt(ext: string): FileKind | '' {
  return EXT_KIND_MAP[normalizeExt(ext)] || ''
}

/** 类别图标（emoji，与既有聊天 UI 风格一致） */
export function kindIcon(kind: string): string {
  switch (kind) {
    case 'table':
      return '📊'
    case 'text':
      return '📄'
    case 'doc':
      return '📃'
    case 'image':
      return '🖼️'
    case 'video':
      return '🎬'
    default:
      return '📎'
  }
}

/** 类别中文名（与后端 kindLabelCN 一致） */
export function kindLabel(kind: string): string {
  switch (kind) {
    case 'table':
      return '表格'
    case 'text':
      return '文本'
    case 'doc':
      return '文档'
    case 'image':
      return '图片'
    case 'video':
      return '视频'
    default:
      return '文件'
  }
}

/** 格式化文件大小（B/KB/MB/GB） */
export function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0B'
  const trim = (n: number) => String(Math.round(n * 10) / 10)
  if (bytes >= 1 << 30) return trim(bytes / (1 << 30)) + 'GB'
  if (bytes >= 1 << 20) return trim(bytes / (1 << 20)) + 'MB'
  if (bytes >= 1 << 10) return trim(bytes / (1 << 10)) + 'KB'
  return Math.round(bytes) + 'B'
}

/** 类别 → 所需模型能力（与后端 RequiredCapability 一致） */
export function requiredCapability(kind: string): ModelCapability | '' {
  if (kind === 'image') return 'vision'
  if (kind === 'video') return 'video'
  return ''
}

/** 能力不匹配提示（与后端 CapabilityRejectText 文案一致） */
export function capabilityRejectText(kind: string): string {
  if (kind === 'image') {
    return '当前所选 AI 模型不支持图片输入，请切换到视觉模型（如 gpt-4o / qwen-vl 系列）后再上传，或移除该文件'
  }
  if (kind === 'video') {
    return '当前所选 AI 模型不支持视频输入，请切换到支持视频的模型（如 gemini / qwen-vl 系列），或移除该文件'
  }
  return '当前所选 AI 模型不支持该文件类型'
}

/** /ai/agent/fileSupport 返回的单扩展名能力 */
export interface FileSupportExt {
  ext: string
  kind: string
  parser: string
  supported: boolean
  degraded: boolean
  maxSize: number
}

/** /ai/agent/fileSupport 响应（环境能力自检） */
export interface FileSupport {
  exts: FileSupportExt[]
  pythonAvailable: boolean
  pythonModules: string[]
  storeTTLSeconds: number
}

/** 单文件校验选项 */
export interface FileValidationOptions {
  /** 环境能力自检结果（未加载时跳过「解析器可用性」检查，由后端兜底） */
  support?: FileSupport | null
  /** 当前选中模型的生效能力集合（vision/video） */
  modelCapabilities?: string[]
}

/** 该扩展名允许的最大体积（字节）；0 表示不限（Markdown 保持既有行为） */
export function allowedSize(ext: string, support?: FileSupport | null): number {
  const e = normalizeExt(ext)
  const info = support?.exts?.find((item) => normalizeExt(item.ext) === e)
  if (info && Number.isFinite(info.maxSize)) return info.maxSize
  if (e === '.md' || e === '.markdown') return 0
  if (kindOfExt(e) === 'image') return IMAGE_MAX_SIZE
  return DEFAULT_MAX_SIZE
}

/**
 * 上传前置校验（顺序与后端 HandleUploadFile 一致）：
 * 扩展名白名单 → 环境解析能力 → 体积（空/超限）→ 模型能力。
 *
 * 返回空字符串表示通过；否则返回面向用户的中文错误文案。
 */
export function validateFile(
  file: { name: string; size: number },
  options: FileValidationOptions = {},
): string {
  const ext = extOf(file.name)
  const kind = kindOfExt(ext)
  if (!kind) {
    return `仅支持 ${SUPPORTED_EXTS_TEXT} 格式的文件`
  }

  const info = options.support?.exts?.find((item) => normalizeExt(item.ext) === ext)
  if (info && !info.supported) {
    return `当前环境暂不支持 ${kindLabel(kind)} 类型文件的内容解析（${ext}）`
  }

  if (file.size <= 0) {
    return '文件内容为空'
  }

  const maxSize = info && Number.isFinite(info.maxSize) ? info.maxSize : allowedSize(ext, options.support)
  if (maxSize > 0 && file.size > maxSize) {
    return `文件大小不能超过 ${formatFileSize(maxSize)}`
  }

  const required = requiredCapability(kind)
  if (required && !(options.modelCapabilities || []).includes(required)) {
    return capabilityRejectText(kind)
  }

  return ''
}
