import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

// 组件库与上传通道在 node 环境下不可用，全部打桩；只验证队列/能力切换逻辑
const mocks = vi.hoisted(() => ({
  info: vi.fn(),
  error: vi.fn(),
  upload: vi.fn(),
}))

vi.mock('element-plus', () => ({
  ElMessage: Object.assign(vi.fn(), {
    info: mocks.info,
    error: mocks.error,
    success: vi.fn(),
    warning: vi.fn(),
  }),
}))

vi.mock('@/api/aiUpload', () => ({
  UploadFileError: class UploadFileError extends Error {},
  deleteChatFile: vi.fn(async () => ({})),
  fetchFileSupport: vi.fn(async () => ({ data: { data: null } })),
  uploadChatFile: mocks.upload,
}))

import { useFileUpload } from '../useFileUpload'

/** 构造测试文件 */
function imageFile(name = 'shot.png'): File {
  return new File([new Uint8Array([137, 80, 78, 71])], name, { type: 'image/png' })
}

function makeQueue(selectedId: string, models: { id: string; model: string; capabilities?: string[] | null }[]) {
  const selectedModel = ref(selectedId)
  const aiModelList = ref(models)
  const queue = useFileUpload({
    selectedModel,
    aiModelList,
    handleSessionExpired: () => {},
  })
  return { selectedModel, aiModelList, queue }
}

const textModel = { id: 'm-text', model: 'deepseek-chat', capabilities: null }
const visionModel = { id: 'm-vision', model: 'gemma4:cloud', capabilities: ['vision'] }

describe('useFileUpload 模型能力自动切换', () => {
  beforeEach(() => {
    mocks.info.mockReset()
    mocks.error.mockReset()
    URL.createObjectURL = vi.fn(() => 'blob:mock') as unknown as typeof URL.createObjectURL
    URL.revokeObjectURL = vi.fn() as unknown as typeof URL.revokeObjectURL
    mocks.upload.mockReset()
    mocks.upload.mockImplementation(async (file: File) => ({
      fileId: 'fx1',
      fileName: file.name,
      fileExt: '.png',
      fileKind: 'image',
      fileSize: file.size,
      mime: file.type,
      expireAt: 4102444800,
    }))
  })

  it('当前模型不支持图片、但存在已配置视觉能力的模型时，自动切换并提示', () => {
    const { selectedModel, queue } = makeQueue('m-text', [textModel, visionModel])

    queue.addFiles([imageFile()])

    expect(selectedModel.value).toBe('m-vision')
    expect(mocks.info).toHaveBeenCalledTimes(1)
    expect(String(mocks.info.mock.calls[0][0])).toContain('gemma4:cloud')
    expect(mocks.error).not.toHaveBeenCalled()
    expect(queue.items.value).toHaveLength(1)
  })

  it('系统里没有可用视觉模型时保持原选择并按原文案拒绝', () => {
    const { selectedModel, queue } = makeQueue('m-text', [textModel])

    queue.addFiles([imageFile('wall.jpg')])

    expect(selectedModel.value).toBe('m-text')
    expect(queue.items.value).toHaveLength(0)
    expect(mocks.error).toHaveBeenCalledTimes(1)
    expect(String(mocks.error.mock.calls[0][0])).toContain('不支持图片输入')
  })

  it('非图片/视频文件不触发模型切换', () => {
    const { selectedModel, queue } = makeQueue('m-text', [textModel, visionModel])

    queue.addFiles([new File(['a,b'], 'data.csv', { type: 'text/csv' })])

    expect(selectedModel.value).toBe('m-text')
    expect(mocks.info).not.toHaveBeenCalled()
    expect(queue.items.value).toHaveLength(1)
  })

  it('切换后再次加入图片不会重复提示', () => {
    const { selectedModel, queue } = makeQueue('m-text', [textModel, visionModel])

    queue.addFiles([imageFile('a.png')])
    queue.addFiles([imageFile('b.png')])

    expect(selectedModel.value).toBe('m-vision')
    expect(mocks.info).toHaveBeenCalledTimes(1)
    expect(queue.items.value).toHaveLength(2)
  })
})
