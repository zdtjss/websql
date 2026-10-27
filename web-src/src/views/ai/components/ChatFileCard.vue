<template>
  <div
    class="file-card"
    :class="[`kind-${file.kind || 'unknown'}`, {
      'is-error': file.status === 'error',
      'is-expired': expired,
      'is-clickable': canOpen,
    }]"
    :title="clickHint"
    @click="handleCardClick"
  >
    <!-- 图片缩略图（仅本地会话有预览地址；点击由卡片统一点击处理） -->
    <el-image
      v-if="isImage && file.previewUrl && !expired"
      :src="file.previewUrl"
      fit="cover"
      class="file-thumb"
    />
    <div v-else class="file-icon">{{ loading ? '⏳' : kindIcon(file.kind) }}</div>

    <div class="file-info">
      <div class="file-name" :title="file.name">{{ file.name }}</div>
      <div class="file-meta">
        <span>{{ kindLabel(file.kind) }} · {{ formatFileSize(file.size) }}</span>
        <span v-if="file.status === 'uploading'" class="file-state">上传中 {{ file.progress }}%</span>
        <span v-else-if="file.status === 'pending'" class="file-state">等待上传…</span>
        <span v-else-if="file.status === 'error'" class="file-state error">{{ file.error || '上传失败' }}</span>
        <span v-else-if="expired" class="file-state expired">已过期，需重新上传</span>
        <span v-else-if="loading" class="file-state">加载中…</span>
      </div>
      <div v-if="file.status === 'uploading'" class="file-progress-bar">
        <div class="file-progress-inner" :style="{ width: file.progress + '%' }"></div>
      </div>
    </div>

    <!-- 待发送区操作（历史卡片不展示） -->
    <div v-if="mode === 'pending'" class="file-actions">
      <el-button
        v-if="file.status === 'error'"
        size="small"
        text
        type="primary"
        title="重试上传"
        @click.stop="emit('retry', file)"
      >
        <el-icon><RefreshRight /></el-icon>
      </el-button>
      <el-button size="small" text type="danger" title="移除文件" @click.stop="emit('remove', file)">
        <el-icon><Close /></el-icon>
      </el-button>
    </div>

    <!-- 图片预览 / 视频播放（内容需带 Authorization 拉取，故用 blob 地址） -->
    <el-dialog
      v-model="mediaVisible"
      :title="file.name"
      width="720px"
      append-to-body
      align-center
      class="file-preview-dialog"
    >
      <img v-if="previewKind === 'image'" :src="mediaUrl" alt="" class="preview-image" />
      <video v-else :src="mediaUrl" controls autoplay class="preview-video"></video>
      <template #footer>
        <el-button size="small" @click="downloadMedia">下载</el-button>
        <el-button size="small" type="primary" @click="mediaVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
/**
 * 文件卡片（类微信）：
 *   - 输入区待发送文件（mode=pending）：含上传进度/失败重试/移除；
 *   - 聊天历史文件消息（mode=history）：展示名称/类别/大小，过期态提示。
 *
 * 点击行为（上传完成且未过期）：图片预览、视频播放、其余文件下载。
 */
import { computed, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Close, RefreshRight } from '@element-plus/icons-vue'
import { formatFileSize, kindIcon, kindLabel } from '../utils/fileTypes'
import { isExpired, type FileAttachmentItem } from '../utils/uploadState'
import { fetchChatFileContent } from '@/api/aiUpload'

const props = withDefaults(
  defineProps<{
    /** 文件附件项（队列项或历史快照） */
    file: FileAttachmentItem
    /** 展示模式：pending=待发送区（含操作）/ history=聊天历史 */
    mode?: 'pending' | 'history'
  }>(),
  { mode: 'history' },
)

const emit = defineEmits<{
  /** 重试上传（仅 pending 模式） */
  (e: 'retry', file: FileAttachmentItem): void
  /** 移除文件（仅 pending 模式） */
  (e: 'remove', file: FileAttachmentItem): void
}>()

/** 图片 / 视频类（可预览/播放） */
const isImage = computed(() => props.file.kind === 'image')
const isVideo = computed(() => props.file.kind === 'video')

/** 暂存过期（仅 ready 状态有意义；expireAt=0 表示未知，不标记过期） */
const expired = computed(() => props.file.status === 'ready' && isExpired(props.file))

/** 是否可点击打开（上传完成、有 fileId、未过期） */
const canOpen = computed(() => props.file.status === 'ready' && !!props.file.fileId && !expired.value)

/** 点击提示（悬停 title） */
const clickHint = computed(() => {
  if (!canOpen.value) return ''
  if (isImage.value) return '点击预览图片'
  if (isVideo.value) return '点击播放视频'
  return '点击下载文件'
})

/** 内容加载中（下载/预览拉取阶段） */
const loading = ref(false)
const mediaVisible = ref(false)
const previewKind = ref<'image' | 'video'>('image')
const mediaUrl = ref('')
/** 由 blob 生成的地址（组件卸载时回收；本地预览地址不回收，由上传队列/会话负责） */
let mediaObjectUrl = ''

/** 解析可用的媒体地址：本地会话优先用预览地址，历史会话按 fileId 拉取 */
async function resolveMediaUrl(): Promise<string> {
  if (props.file.previewUrl) return props.file.previewUrl
  if (mediaObjectUrl) return mediaObjectUrl
  const { blob } = await fetchChatFileContent(props.file.fileId)
  mediaObjectUrl = URL.createObjectURL(blob)
  return mediaObjectUrl
}

/** 图片预览 / 视频播放 */
async function openMedia(): Promise<void> {
  loading.value = true
  try {
    mediaUrl.value = await resolveMediaUrl()
    previewKind.value = isImage.value ? 'image' : 'video'
    mediaVisible.value = true
  } catch (e) {
    ElMessage.error((e as Error)?.message || '文件加载失败')
  } finally {
    loading.value = false
  }
}

/** 下载文件（blob + a[download]，兼容带 Authorization 的接口） */
async function downloadFile(): Promise<void> {
  loading.value = true
  try {
    const { blob, fileName } = await fetchChatFileContent(props.file.fileId)
    triggerDownload(URL.createObjectURL(blob), fileName || props.file.name)
  } catch (e) {
    ElMessage.error((e as Error)?.message || '文件下载失败')
  } finally {
    loading.value = false
  }
}

/** 预览弹窗内下载：直接复用已加载的媒体地址，无需再次请求（地址仍被预览使用，不回收） */
function downloadMedia(): void {
  if (mediaUrl.value) {
    triggerDownload(mediaUrl.value, props.file.name, false)
  }
}

/** 触发浏览器下载（下载后延迟回收本次专用的 blob 地址，避免下载中断） */
function triggerDownload(url: string, fileName: string, revokeAfterUse = true): void {
  const a = document.createElement('a')
  a.href = url
  a.download = fileName
  document.body.appendChild(a)
  a.click()
  a.remove()
  if (revokeAfterUse && url.startsWith('blob:')) {
    setTimeout(() => URL.revokeObjectURL(url), 2000)
  }
}

/** 卡片点击：图片预览 / 视频播放 / 其他下载 */
async function handleCardClick(): Promise<void> {
  if (loading.value) return
  if (expired.value) {
    ElMessage.warning('文件已过期，需重新上传')
    return
  }
  if (!canOpen.value) return
  if (isImage.value || isVideo.value) {
    await openMedia()
    return
  }
  await downloadFile()
}

onUnmounted(() => {
  if (mediaObjectUrl) {
    URL.revokeObjectURL(mediaObjectUrl)
    mediaObjectUrl = ''
  }
})
</script>

<style scoped>
.file-card {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 180px;
  max-width: 320px;
  padding: 8px 10px;
  border-radius: 10px;
  border: 1px solid rgba(64, 158, 255, 0.18);
  background: linear-gradient(135deg, rgba(236, 245, 255, 0.95) 0%, rgba(245, 247, 250, 0.9) 100%);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.05);
  transition: box-shadow 0.2s ease, border-color 0.2s ease;
}

.file-card:hover {
  box-shadow: 0 2px 10px rgba(64, 158, 255, 0.18);
}

.file-card.is-clickable {
  cursor: pointer;
}

.file-card.is-error {
  border-color: rgba(245, 108, 108, 0.35);
  background: linear-gradient(135deg, rgba(254, 240, 240, 0.95) 0%, rgba(250, 245, 245, 0.9) 100%);
}

.file-card.is-expired {
  opacity: 0.72;
}

.file-thumb {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  flex-shrink: 0;
}

.file-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  background: rgba(64, 158, 255, 0.1);
}

.file-info {
  flex: 1;
  min-width: 0;
}

.file-name {
  font-size: 13px;
  font-weight: 600;
  color: #1f2937;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.file-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 2px;
  font-size: 11px;
  color: #6b7280;
  white-space: nowrap;
  overflow: hidden;
}

.file-state {
  color: #409eff;
}

.file-state.error {
  color: #f56c6c;
  overflow: hidden;
  text-overflow: ellipsis;
}

.file-state.expired {
  color: #e6a23c;
}

.file-progress-bar {
  margin-top: 5px;
  height: 3px;
  border-radius: 2px;
  background: rgba(64, 158, 255, 0.15);
  overflow: hidden;
}

.file-progress-inner {
  height: 100%;
  border-radius: 2px;
  background: linear-gradient(90deg, #409eff, #79bbff);
  transition: width 0.2s ease;
}

.file-actions {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.file-actions :deep(.el-button) {
  padding: 2px 4px;
}

/* 预览弹窗内容 */
.preview-image {
  display: block;
  max-width: 100%;
  max-height: 70vh;
  margin: 0 auto;
}

.preview-video {
  display: block;
  width: 100%;
  max-height: 70vh;
  background: #000;
  border-radius: 6px;
}

/* ========== 暗色主题 ========== */
[data-theme='dark'] .file-card {
  border-color: rgba(137, 180, 250, 0.25);
  background: linear-gradient(135deg, rgba(40, 44, 60, 0.95) 0%, rgba(32, 36, 48, 0.9) 100%);
}

[data-theme='dark'] .file-card.is-error {
  border-color: rgba(245, 108, 108, 0.4);
}

[data-theme='dark'] .file-name {
  color: var(--text-primary);
}

[data-theme='dark'] .file-meta {
  color: var(--text-secondary);
}

[data-theme='dark'] .file-icon {
  background: rgba(137, 180, 250, 0.15);
}
</style>
