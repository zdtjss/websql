<template>
  <div ref="containerRef" class="chat-messages">
    <!-- 加载更早消息的按钮 -->
    <div v-if="hiddenMsgCount > 0" class="load-more-msgs" @click="emit('show-all')">
      ↑ 点击加载更早的 {{ hiddenMsgCount }} 条消息
    </div>

    <!-- 历史消息列表 -->
    <ChatMessageItem
      v-for="(msg, vIdx) in visibleMessages.msgs"
      :key="'h' + (visibleMessages.offset + vIdx)"
      :msg="msg"
      :can-retry="canRetry(msg)"
      :loading="loading"
      :retrying-msg="retryingMsg"
      :get-cached-html="getCachedHtml"
      :highlight-sql="highlightSql"
      @toggle-thinking="emit('toggle-thinking', $event)"
      @copy="emit('copy', $event)"
      @retry="emit('retry', $event)"
    />

    <!-- 实时思考过程（流式中） -->
    <div v-if="thinkingText && loading" class="thinking-block">
      <div class="thinking-label">💭 思考中...</div>
      <div class="thinking-content markdown-body" v-html="thinkingHtml"></div>
    </div>

    <!-- 流式输出中 -->
    <div v-if="streamingContent" class="chat-bubble assistant">
      <div class="bubble-label">AI</div>
      <div class="bubble-content markdown-body" v-html="streamingHtml"></div>
    </div>

    <!-- 危险SQL确认后的流式输出 -->
    <div v-if="streamingExecContent" class="chat-bubble assistant">
      <div class="bubble-label">AI</div>
      <div class="bubble-content markdown-body" v-html="streamingExecHtml"></div>
    </div>

    <div v-if="loading" style="color:#909399;font-size:13px;padding:4px 0;">AI 正在处理...</div>
  </div>
</template>

<script setup lang="ts">
/**
 * 聊天消息列表组件。
 * 渲染历史消息、实时思考过程、流式输出内容。
 */
import { ref } from 'vue'
import ChatMessageItem from './ChatMessageItem.vue'
import type { ChatMessage } from '../composables/useChatHistory'

defineProps<{
  /** 可见消息窗口（包含 msgs 数组和 offset 偏移量） */
  visibleMessages: { msgs: ChatMessage[]; offset: number }
  /** 被隐藏的更早消息数 */
  hiddenMsgCount: number
  /** 是否正在加载 */
  loading: boolean
  /** 实时思考文本 */
  thinkingText: string
  /** 思考文本渲染后的 HTML */
  thinkingHtml: string
  /** 流式输出内容 */
  streamingContent: string
  /** 流式输出渲染后的 HTML */
  streamingHtml: string
  /** 确认后的流式执行内容 */
  streamingExecContent: string
  /** 确认后的流式执行内容渲染后的 HTML */
  streamingExecHtml: string
  /** 当前正在重试的消息引用 */
  retryingMsg: ChatMessage | null
  /** 判断消息是否可重试 */
  canRetry: (msg: ChatMessage) => boolean
  /** 获取缓存的 HTML 渲染结果 */
  getCachedHtml: (msg: ChatMessage) => string
  /** SQL 高亮函数 */
  highlightSql: (text: string) => string
}>()

const emit = defineEmits<{
  /** 展开全部历史消息 */
  (e: 'show-all'): void
  /** 切换思考块折叠状态 */
  (e: 'toggle-thinking', msg: ChatMessage): void
  /** 复制消息 */
  (e: 'copy', msg: ChatMessage): void
  /** 重试消息 */
  (e: 'retry', msg: ChatMessage): void
}>()

/** 容器元素引用（父组件通过 ref 访问用于滚动控制） */
const containerRef = ref<HTMLElement | null>(null)
defineExpose({ containerRef })
</script>

<style scoped>
.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 0;
}
</style>
