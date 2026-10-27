import { describe, expect, it } from 'vitest'
import {
  ACCEPT_ATTR,
  SUPPORTED_EXTS,
  allowedSize,
  capabilityRejectText,
  extOf,
  formatFileSize,
  kindIcon,
  kindLabel,
  kindOfExt,
  normalizeExt,
  requiredCapability,
  validateFile,
  type FileSupport,
} from '../fileTypes'

const MB = 1 << 20

/** 构造能力自检结果（默认全部支持） */
function makeSupport(overrides: Partial<Record<string, Partial<FileSupport['exts'][number]>>> = {}): FileSupport {
  return {
    exts: SUPPORTED_EXTS.map((ext) => ({
      ext,
      kind: kindOfExt(ext) as string,
      parser: 'go',
      supported: true,
      degraded: false,
      maxSize: kindOfExt(ext) === 'image' ? 10 * MB : 20 * MB,
      ...(overrides[ext] || {}),
    })),
    pythonAvailable: false,
    pythonModules: [],
    storeTTLSeconds: 1800,
  }
}

describe('扩展名与类别', () => {
  it('extOf 提取小写扩展名', () => {
    expect(extOf('销售数据.XLSX')).toBe('.xlsx')
    expect(extOf('noext')).toBe('')
    expect(extOf('a.b.csv')).toBe('.csv')
  })

  it('normalizeExt 归一化扩展名', () => {
    expect(normalizeExt('XLSX')).toBe('.xlsx')
    expect(normalizeExt(' .csv ')).toBe('.csv')
    expect(normalizeExt('')).toBe('')
  })

  it('kindOfExt 覆盖全部支持格式', () => {
    expect(kindOfExt('.xlsx')).toBe('table')
    expect(kindOfExt('.csv')).toBe('table')
    expect(kindOfExt('.markdown')).toBe('text')
    expect(kindOfExt('.pdf')).toBe('doc')
    expect(kindOfExt('.png')).toBe('image')
    expect(kindOfExt('.mp4')).toBe('video')
    expect(kindOfExt('.exe')).toBe('')
  })

  it('accept 属性包含全部扩展名', () => {
    for (const ext of SUPPORTED_EXTS) {
      expect(ACCEPT_ATTR).toContain(ext)
    }
    expect(ACCEPT_ATTR).toContain('.docx')
    expect(ACCEPT_ATTR).toContain('.mp4')
  })

  it('类别图标与中文名', () => {
    expect(kindIcon('table')).toBe('📊')
    expect(kindLabel('video')).toBe('视频')
    expect(kindLabel('unknown')).toBe('文件')
  })
})

describe('formatFileSize', () => {
  it('按量级格式化', () => {
    expect(formatFileSize(0)).toBe('0B')
    expect(formatFileSize(512)).toBe('512B')
    expect(formatFileSize(2048)).toBe('2KB')
    expect(formatFileSize(2.5 * MB)).toBe('2.5MB')
    expect(formatFileSize(1.25 * (1 << 30))).toBe('1.3GB')
    expect(formatFileSize(Number.NaN)).toBe('0B')
  })
})

describe('模型能力映射', () => {
  it('图片要求 vision、视频要求 video', () => {
    expect(requiredCapability('image')).toBe('vision')
    expect(requiredCapability('video')).toBe('video')
    expect(requiredCapability('table')).toBe('')
  })

  it('拒绝文案给出可操作指引', () => {
    expect(capabilityRejectText('image')).toContain('视觉模型')
    expect(capabilityRejectText('video')).toContain('视频')
  })
})

describe('allowedSize', () => {
  it('Markdown 不限大小，图片 10MB，其余 20MB', () => {
    expect(allowedSize('.md')).toBe(0)
    expect(allowedSize('.markdown')).toBe(0)
    expect(allowedSize('.png')).toBe(10 * MB)
    expect(allowedSize('.csv')).toBe(20 * MB)
  })

  it('优先采用能力自检结果中的上限', () => {
    const support = makeSupport({ '.csv': { maxSize: 5 * MB } })
    expect(allowedSize('.csv', support)).toBe(5 * MB)
  })
})

describe('validateFile', () => {
  it('未知格式拒绝', () => {
    expect(validateFile({ name: 'malware.exe', size: 10 })).toContain('仅支持')
  })

  it('环境不支持时给出明确提示', () => {
    const support = makeSupport({ '.pdf': { supported: false, parser: 'none' } })
    const msg = validateFile({ name: '报告.pdf', size: 1024 }, { support })
    expect(msg).toContain('暂不支持')
    expect(msg).toContain('文档')
  })

  it('空文件拒绝', () => {
    expect(validateFile({ name: 'a.txt', size: 0 })).toContain('为空')
  })

  it('超限拒绝并展示上限', () => {
    const msg = validateFile({ name: 'big.png', size: 11 * MB })
    expect(msg).toContain('10MB')
    // Markdown 不限大小
    expect(validateFile({ name: 'doc.md', size: 50 * MB })).toBe('')
  })

  it('图片需要视觉能力，视频需要视频能力', () => {
    const textModel = { modelCapabilities: [] as string[] }
    expect(validateFile({ name: 'a.png', size: 1024 }, textModel)).toContain('不支持图片输入')
    expect(validateFile({ name: 'a.mp4', size: 1024 }, textModel)).toContain('不支持视频输入')
    // vision 可放行图片，但视频仍需 video 能力
    const visionOnly = { modelCapabilities: ['vision'] }
    expect(validateFile({ name: 'a.png', size: 1024 }, visionOnly)).toBe('')
    expect(validateFile({ name: 'a.mp4', size: 1024 }, visionOnly)).not.toBe('')
    // video 能力隐含 vision（与后端 NormalizeCapabilities 一致）
    const full = { modelCapabilities: ['vision', 'video'] }
    expect(validateFile({ name: 'a.mp4', size: 1024 }, full)).toBe('')
  })

  it('表格/文本/文档类无能力要求', () => {
    expect(validateFile({ name: 'a.csv', size: 1024 }, { modelCapabilities: [] })).toBe('')
    expect(validateFile({ name: 'a.docx', size: 1024 }, { modelCapabilities: [] })).toBe('')
    expect(validateFile({ name: 'a.txt', size: 1024 })).toBe('')
  })

  it('能力自检未加载时跳过解析器检查（后端兜底）', () => {
    expect(validateFile({ name: 'a.docx', size: 1024 }, { support: null })).toBe('')
  })
})
