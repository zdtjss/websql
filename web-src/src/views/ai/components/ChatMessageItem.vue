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

<style scoped>
/* ========== 聊天消息容器 ========== */
.chat-messages {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 8px 5px;
  min-height: 0;
  background: rgba(255, 255, 255, 0.9);
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.06);
  overflow-x: hidden;
}

/* 自定义滚动条 - 蓝灰色 */
.chat-messages::-webkit-scrollbar {
  width: 6px;
}

.chat-messages::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.03);
  border-radius: 3px;
}

.chat-messages::-webkit-scrollbar-thumb {
  background: linear-gradient(180deg, #546e7a 0%, #37474f 100%);
  border-radius: 3px;
  transition: background 0.3s ease;
}

.chat-messages::-webkit-scrollbar-thumb:hover {
  background: linear-gradient(180deg, #607d8b 0%, #455a64 100%);
}

/* ========== 聊天气泡 ========== */
.chat-bubble {
  border-radius: 16px;
  padding: 12px 16px;
  font-size: 14px;
  line-height: 1.6;
  position: relative;
  animation: slideIn 0.3s ease-out;
  backdrop-filter: blur(10px);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  transition: all 0.3s ease;
}

.chat-bubble:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
}

@keyframes slideIn {
  from {
    opacity: 0;
    transform: translateY(10px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 用户消息气泡 - 浅蓝渐变 */
.chat-bubble.user {
  align-self: flex-end;
  background: linear-gradient(135deg, #64b5f6 0%, #f0f0f0 100%);
  color: #fff;
  border-bottom-right-radius: 4px;
  box-shadow: 0 4px 12px rgba(100, 181, 246, 0.25);
}

.chat-bubble.user .bubble-label {
  color: rgba(255, 255, 255, 0.95);
}

/* AI 消息气泡 - 冷白色 */
.chat-bubble.assistant {
  align-self: flex-start;
  background: linear-gradient(135deg, #ffffff 0%, #f5f5f5 100%);
  color: #212121;
  border-bottom-left-radius: 4px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.chat-bubble.assistant .bubble-label {
  color: #546e7a;
}

/* ========== 标签样式 ========== */
.bubble-label {
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 4px;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}

.bubble-content {
  word-break: break-word;
}

/* ========== AI 消息操作栏（复制/重试） ========== */
.msg-action-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-top: 6px;
  padding-top: 4px;
  border-top: 1px solid rgba(0, 0, 0, 0.05);
  opacity: 0;
  transition: opacity 0.2s ease;
  height: 0;
  overflow: hidden;
}
.chat-bubble.assistant:hover .msg-action-bar {
  opacity: 1;
  height: auto;
}
.msg-action-btn {
  font-size: 12px !important;
  color: #909399;
  padding: 2px 6px !important;
  height: 24px;
}
.msg-action-btn:hover {
  color: #409eff;
}
.msg-action-btn.is-disabled {
  opacity: 0.4;
}
.msg-action-text {
  margin-left: 2px;
}

/* ========== 思考过程块 - 冷色调 ========== */
.thinking-block {
  border: 1px solid rgba(84, 110, 122, 0.2);
  border-radius: 12px;
  background: linear-gradient(135deg, rgba(236, 239, 241, 0.6) 0%, rgba(224, 228, 230, 0.4) 100%);
  padding: 12px;
  margin: 8px 0;
  backdrop-filter: blur(10px);
  box-shadow: 0 2px 8px rgba(84, 110, 122, 0.1);
  transition: all 0.3s ease;
}

.thinking-block:hover {
  box-shadow: 0 4px 12px rgba(84, 110, 122, 0.15);
  transform: translateX(4px);
}

.thinking-label {
  font-size: 13px;
  color: #37474f;
  margin-bottom: 8px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  transition: color 0.3s ease;
}

.thinking-label:hover {
  color: #546e7a;
}

.thinking-content {
  font-size: 13px;
  color: #455a64;
  word-break: break-word;
  max-height: 400px;
  overflow-y: auto;
  margin: 0;
  padding: 8px 12px;
  background: rgba(255, 255, 255, 0.7);
  border-radius: 8px;
  line-height: 1.6;
}

.thinking-content :deep(p) {
  margin-top: 0;
  margin-bottom: 8px;
}

.thinking-content :deep(p:last-child) {
  margin-bottom: 0;
}

.thinking-content :deep(code) {
  padding: 2px 6px;
  background: rgba(0, 0, 0, 0.06);
  border-radius: 4px;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  color: #c62828;
}

.thinking-content :deep(pre) {
  margin: 8px 0;
  padding: 12px;
  background: rgba(0, 0, 0, 0.04);
  border-radius: 6px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.5;
}

.thinking-content :deep(pre code) {
  padding: 0;
  background: transparent;
  color: inherit;
}

.thinking-content::-webkit-scrollbar {
  width: 4px;
}

.thinking-content::-webkit-scrollbar-thumb {
  background: #78909c;
  border-radius: 2px;
}

/* ========== 工具调用块 - 青绿色 ========== */
.tool-call-block {
  font-size: 13px;
  color: #00796b;
  padding: 10px 14px;
  background: linear-gradient(135deg, rgba(224, 242, 241, 0.9) 0%, rgba(178, 223, 219, 0.7) 100%);
  border-radius: 10px;
  border: 1px solid rgba(0, 121, 107, 0.2);
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  box-shadow: 0 2px 8px rgba(0, 121, 107, 0.1);
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {

  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.85;
  }
}

/* ========== SQL 代码块 - VSCode 风格 ========== */
.sql-pre {
  margin: 0;
  padding: 12px;
  overflow: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  background: linear-gradient(180deg, #1e1e1e 0%, #252526 100%);
  color: #d4d4d4;
  border-radius: 8px;
  border: 1px solid #3c3c3c;
  box-shadow: inset 0 2px 8px rgba(0, 0, 0, 0.5);
}

.sql-pre::-webkit-scrollbar {
  height: 6px;
}

.sql-pre::-webkit-scrollbar-thumb {
  background: #505050;
  border-radius: 3px;
}

.cursor-blink {
  animation: blink 1s step-start infinite;
  font-size: 14px;
  color: #569cd6;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

/* ========== Markdown 样式（基础容器，子元素样式在 unscoped 块中） ========== */
.markdown-body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif;
  font-size: 14px;
  line-height: 1.7;
  color: #1f2937;
  word-wrap: break-word;
  overflow-wrap: break-word;
}


</style>
