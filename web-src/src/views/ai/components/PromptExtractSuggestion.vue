<template>
  <div class="prompt-suggestion" role="status" aria-live="polite">
    <button
      type="button"
      class="prompt-suggestion-main"
      title="把本次对话保存为常用提示词，下次同类需求可直接复用"
      @click="emit('extract')"
    >
      <el-icon :size="14"><MagicStick /></el-icon>
      <span class="prompt-suggestion-text">存为常用提示词?</span>
    </button>
    <button
      type="button"
      class="prompt-suggestion-close"
      title="关闭"
      aria-label="关闭"
      @click="emit('dismiss', true)"
    >
      <el-icon :size="12"><Close /></el-icon>
    </button>
    <!-- 倒计时进度条：鼠标悬停或聚焦时暂停，5 秒后自动关闭 -->
    <span class="prompt-suggestion-progress" aria-hidden="true" @animationend="emit('dismiss', false)"></span>
  </div>
</template>

<script setup lang="ts">
import { Close, MagicStick } from '@element-plus/icons-vue'

const emit = defineEmits<{
  /** 用户点击提示条 */
  (e: 'extract'): void
  /** 关闭建议（manual=true 表示用户手动关闭） */
  (e: 'dismiss', manual: boolean): void
}>()
</script>

<style scoped>
/* 右下角紧凑提示条：只占一小块空间，不遮挡消息内容 */
.prompt-suggestion {
  position: relative;
  display: inline-flex;
  align-items: center;
  height: 30px;
  padding: 0 2px 0 10px;
  border-radius: 999px;
  background: var(--bg-primary, #fff);
  border: 1px solid var(--border-primary, #dcdfe6);
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.12);
  overflow: hidden;
  pointer-events: auto;
}

.prompt-suggestion-main {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 4px;
  border: 0;
  background: transparent;
  color: var(--text-secondary, #606266);
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition: color 0.2s ease;
}

.prompt-suggestion-main:hover {
  color: var(--accent-color, #007acc);
}

.prompt-suggestion-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  margin-left: 2px;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--text-tertiary, #909399);
  cursor: pointer;
  transition: color 0.2s ease, background 0.2s ease;
}

.prompt-suggestion-close:hover {
  color: var(--text-primary, #303133);
  background: var(--bg-hover, rgba(0, 0, 0, 0.06));
}

.prompt-suggestion-progress {
  position: absolute;
  left: 0;
  bottom: 0;
  width: 100%;
  height: 2px;
  background: var(--accent-color, #007acc);
  transform-origin: left center;
  animation: prompt-suggestion-countdown 5s linear forwards;
}

.prompt-suggestion:hover .prompt-suggestion-progress,
.prompt-suggestion:focus-within .prompt-suggestion-progress {
  animation-play-state: paused;
}

@keyframes prompt-suggestion-countdown {
  from {
    transform: scaleX(1);
  }

  to {
    transform: scaleX(0);
  }
}
</style>
