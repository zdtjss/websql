import { computed, ref, type Ref } from 'vue'
import { ElMessage } from 'element-plus'
import {
  UploadFileError,
  deleteChatFile,
  fetchFileSupport,
  uploadChatFile,
} from '@/api/aiUpload'
import type { FileSupport, ModelCapability } from '../utils/fileTypes'
import { capabilityRejectText, extOf, kindOfExt, requiredCapability, validateFile } from '../utils/fileTypes'
import {
  UPLOAD_CONCURRENCY,
  createFileItem,
  hasUploading,
  markRetry,
  markUploadFailure,
  markUploadProgress,
  markUploadStart,
  markUploadSuccess,
  nextUploadIndex,
  type FileAttachmentItem,
} from '../utils/uploadState'
import { collectSendAttachments } from '../utils/attachments'

/** useFileUpload 依赖的外部上下文 */
export interface UseFileUploadDeps {
  /** 当前选中的模型 ID（上传时传给后端做能力前置校验） */
  selectedModel: Ref<string>
  /** AI 模型列表（读取生效能力集合做前端前置校验） */
  aiModelList: Ref<{ id: string; model: string; capabilities?: string[] | null }[]>
  /** 登录过期处理 */
  handleSessionExpired: () => void
}

/**
 * AI 对话文件上传队列 composable（新上传链路）。
 *
 * 职责：
 *   - 多文件队列：加入时前置校验（格式/体积/模型能力），并发 2 上传，失败可重试/删除；
 *   - 上传走 XHR（带进度），上传成功仅落暂存元数据（后端延迟解析，不阻塞对话）；
 *   - 环境能力自检（fileSupport）懒加载，用于「文件选择阶段」给出准确提示；
 *   - 队列项（FileAttachmentItem）发送后由 useChatStream 快照进聊天历史（类微信文件卡片）。
 *
 * 注意：原始 File 对象仅存内存 Map（不放进响应式项，避免大文件挂到 Vue 响应式代理上）。
 */
export function useFileUpload(deps: UseFileUploadDeps) {
  const { selectedModel, aiModelList, handleSessionExpired } = deps

  /** 待发送文件队列（含上传中/失败项） */
  const items = ref<FileAttachmentItem[]>([])

  /** 环境能力自检结果（懒加载，失败静默：校验交由后端兜底） */
  const fileSupport = ref<FileSupport | null>(null)
  let supportLoaded = false

  /** 是否有文件正在上传（发送按钮据此提示等待） */
  const uploading = computed(() => hasUploading(items.value))

  /** 可随消息发送的文件（上传完成且未过期） */
  const readyItems = computed(() => collectSendAttachments(items.value))

  /** 是否有可发送文件 */
  const hasReady = computed(() => readyItems.value.length > 0)

  /** 原始文件对象（localId → File，发送/移除时清理） */
  const rawFiles = new Map<string, File>()

  /** 进行中的上传控制器（localId → AbortController，移除/重置时中止） */
  const aborters = new Map<string, AbortController>()

  /** 当前选中模型的生效能力集合（未配置按空数组=仅文本，与后端 fail-closed 一致） */
  function currentModelCapabilities(): string[] {
    const model = aiModelList.value.find((m) => m.id === selectedModel.value)
    return model?.capabilities || []
  }

  /**
   * 确保当前模型具备文件所需能力；缺失时自动切换到系统里已配置该能力的模型。
   *
   * 典型场景：用户在系统设置里给某个模型勾选了「图片理解」，但对话页当前选中的
   * 是另一个纯文本模型——这里自动切到已配置能力的模型并提示，避免用户以为配置未生效。
   * 没有任何可用模型时不做切换，后续校验会给出原有拒绝文案。
   */
  function ensureModelCapability(required: ModelCapability): string[] {
    const caps = currentModelCapabilities()
    if (caps.includes(required)) return caps

    const candidate = aiModelList.value.find((m) => (m.capabilities || []).includes(required))
    if (!candidate) return caps

    selectedModel.value = candidate.id
    const label = required === 'vision' ? '图片' : '视频'
    ElMessage.info(`已自动切换模型为 ${candidate.model}（支持${label}输入）`)
    return candidate.capabilities || []
  }

  /** 加载环境能力自检（幂等；失败不阻断，上传时由后端兜底校验） */
  async function ensureFileSupport(): Promise<FileSupport | null> {
    if (supportLoaded) return fileSupport.value
    try {
      const resp = await fetchFileSupport()
      fileSupport.value = (resp.data?.data as FileSupport) || null
      supportLoaded = true
    } catch {
      /* 忽略：能力自检失败时仍可上传，由后端给出准确错误 */
    }
    return fileSupport.value
  }

  /** 释放本地预览地址 */
  function revokePreview(item: FileAttachmentItem): void {
    if (item.previewUrl) {
      try {
        URL.revokeObjectURL(item.previewUrl)
      } catch {
        /* ignore */
      }
      item.previewUrl = undefined
    }
  }

  /**
   * 加入文件：逐个前置校验（格式 → 环境能力 → 体积 → 模型能力），
   * 通过后入队并自动调度上传；被拒文件聚合为一条错误提示。
   *
   * 若唯一失败原因是模型能力、且系统里已有具备该能力的模型，会先自动切换再复校，
   * 避免为无效文件（格式/体积不合法）白白切换模型。
   */
  function addFiles(files: File[]): void {
    if (files.length === 0) return
    const rejected: string[] = []
    for (const file of files) {
      const kind = kindOfExt(extOf(file.name))
      const required = requiredCapability(kind)
      const options = { support: fileSupport.value, modelCapabilities: currentModelCapabilities() }
      let err = validateFile({ name: file.name, size: file.size }, options)
      if (err && required && err === capabilityRejectText(kind)) {
        options.modelCapabilities = ensureModelCapability(required)
        err = validateFile({ name: file.name, size: file.size }, options)
      }
      if (err) {
        rejected.push(`${file.name}：${err}`)
        continue
      }
      const item = createFileItem(file)
      if (item.kind === 'image' || item.kind === 'video') {
        item.previewUrl = URL.createObjectURL(file)
      }
      rawFiles.set(item.localId, file)
      items.value.push(item)
    }

    if (rejected.length > 0) {
      const head = rejected[0]
      const rest = rejected.length > 1 ? `（另有 ${rejected.length - 1} 个文件被拒绝）` : ''
      ElMessage.error(head + rest)
    }
    startUploads()
  }

  /** 按并发上限调度 pending 项上传 */
  function startUploads(): void {
    // 队列在调度过程中会被 splice/增删，逐轮重算下一个可调度下标
    for (let guard = 0; guard < items.value.length + 1; guard++) {
      const idx = nextUploadIndex(items.value, UPLOAD_CONCURRENCY)
      if (idx < 0) return
      const item = items.value[idx]
      markUploadStart(item)
      void uploadOne(item)
    }
  }

  /** 上传单个文件（成功后回填服务端元数据；失败置 error 可重试） */
  async function uploadOne(item: FileAttachmentItem): Promise<void> {
    const raw = rawFiles.get(item.localId)
    if (!raw) {
      markUploadFailure(item, '文件已失效，请重新选择')
      return
    }
    const aborter = new AbortController()
    aborters.set(item.localId, aborter)
    try {
      const result = await uploadChatFile(raw, {
        modelId: selectedModel.value,
        onProgress: (percent) => markUploadProgress(item, percent),
        onSessionExpired: handleSessionExpired,
        signal: aborter.signal,
      })
      markUploadSuccess(item, result)
    } catch (e) {
      // 主动取消（项已从队列移除）：不标记错误，静默结束
      if (aborters.get(item.localId) !== aborter) return
      const err = e as UploadFileError
      markUploadFailure(item, err?.message || '上传失败，请重试')
    } finally {
      aborters.delete(item.localId)
      // 补位：当前项结束后调度后续 pending
      startUploads()
    }
  }

  /** 重试失败项 */
  function retry(localId: string): void {
    const item = items.value.find((i) => i.localId === localId)
    if (!item || !markRetry(item)) return
    startUploads()
  }

  /** 移除队列项（上传中的中止请求；已上传成功的同步删除服务端暂存与本地预览） */
  function remove(localId: string): void {
    const idx = items.value.findIndex((i) => i.localId === localId)
    if (idx < 0) return
    const item = items.value[idx]
    cancelUpload(localId)
    revokePreview(item)
    rawFiles.delete(item.localId)
    items.value.splice(idx, 1)
    if (item.fileId) {
      // 已暂存的文件删除失败无碍（TTL 到期自动清理），静默处理
      void deleteChatFile(item.fileId).catch(() => { /* ignore */ })
    }
  }

  /** 中止进行中的上传（aborters 中删除条目 = 标记为主动取消，供 catch 静默） */
  function cancelUpload(localId: string): void {
    aborters.get(localId)?.abort()
    aborters.delete(localId)
  }

  /**
   * 标记已随消息发送：从待发送队列移除（历史卡片持有同一对象快照，预览地址不回收）。
   * 服务端暂存文件不删除——同轮后续追问仍可能经 read_file_data 读取。
   */
  function markSent(sentItems: FileAttachmentItem[]): void {
    const ids = new Set(sentItems.map((i) => i.localId))
    if (ids.size === 0) return
    items.value = items.value.filter((i) => !ids.has(i.localId))
    for (const id of ids) rawFiles.delete(id)
  }

  /** 重置队列（新建会话/清空时调用）：中止上传、释放预览、清理已暂存文件 */
  function reset(): void {
    for (const item of items.value) {
      cancelUpload(item.localId)
      revokePreview(item)
      if (item.fileId) {
        void deleteChatFile(item.fileId).catch(() => { /* ignore */ })
      }
    }
    items.value = []
    rawFiles.clear()
    aborters.clear()
  }

  return {
    items,
    fileSupport,
    uploading,
    readyItems,
    hasReady,
    ensureFileSupport,
    currentModelCapabilities,
    addFiles,
    retry,
    remove,
    markSent,
    reset,
  }
}
