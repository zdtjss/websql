<template>
  <!-- 思考过程（历史中的，可折叠） -->
  <div v-if="msg.role === 'thinking'" class="thinking-block">
    <div class="thinking-label" style="cursor:pointer;" @click="emit('toggle-thinking', msg)">
      💭 思考过程 <span style="font-size:11px;">{{ msg.collapsed ? '▶ 展开' : '▼ 折叠' }}</span>
    </div>
    <div v-show="!msg.collapsed" class="thinking-content markdown-body" v-html="getCachedHtml(msg)"></div>
  </div>

  <!-- 用户消息 -->
  <div v-else-if="msg.role === 'user'" :class="['chat-bubble', 'user']">
    <div class="bubble-label">你</div>
    <div class="bubble-content markdown-body" v-html="getCachedHtml(msg)"></div>
  </div>

  <!-- AI 消息 -->
  <div v-else-if="msg.role === 'assistant'" :class="['chat-bubble', 'assistant']">
    <div class="bubble-label">AI</div>
    <div v-if="msg.hasSql" class="bubble-content">
      <pre class="sql-pre"><code v-html="highlightSql(msg.content)" /></pre>
    </div>
    <div v-else class="bubble-content markdown-body" v-html="getCachedHtml(msg)"></div>
    <!-- AI 消息操作栏：复制、重试（hover 时显示） -->
    <div class="msg-action-bar">
      <el-button
        class="msg-action-btn"
        size="small"
        text
        :title="msg._copied ? '已复制' : '复制'"
        @click="emit('copy', msg)"
      >
        <el-icon><CopyDocument /></el-icon>
        <span v-if="msg._copied" class="msg-action-text">已复制</span>
      </el-button>
      <el-button
        class="msg-action-btn"
        size="small"
        text
        title="重试"
        :loading="retryingMsg === msg"
        :disabled="!canRetry || loading"
        @click="emit('retry', msg)"
      >
        <el-icon v-if="retryingMsg !== msg"><RefreshRight /></el-icon>
        <span v-if="retryingMsg !== msg" class="msg-action-text">重试</span>
      </el-button>
    </div>
  </div>

  <!-- 工具调用 -->
  <div v-else-if="msg.role === 'tool_call'" class="tool-call-block">
    <span>🔧 {{ msg.content }}</span>
  </div>
</template>

<script setup lang="ts">
/**
 * 单条聊天消息渲染组件。
 * 根据消息角色（thinking / user / assistant / tool_call）渲染不同样式。
 */
import { CopyDocument, RefreshRight } from '@element-plus/icons-vue'
import type { ChatMessage } from '../composables/useChatHistory'

defineProps<{
  /** 消息对象 */
  msg: ChatMessage
  /** 是否可以重试 */
  canRetry: boolean
  /** 是否正在加载（禁用重试按钮） */
  loading: boolean
  /** 当前正在重试的消息引用 */
  retryingMsg: ChatMessage | null
  /** 获取缓存的 HTML 渲染结果 */
  getCachedHtml: (msg: ChatMessage) => string
  /** SQL 高亮函数 */
  highlightSql: (text: string) => string
}>()

const emit = defineEmits<{
  /** 切换思考块折叠状态 */
  (e: 'toggle-thinking', msg: ChatMessage): void
  /** 复制消息 */
  (e: 'copy', msg: ChatMessage): void
  /** 重试消息 */
  (e: 'retry', msg: ChatMessage): void
}>()
</script>
