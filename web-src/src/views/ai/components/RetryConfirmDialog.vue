<template>
  <div v-if="visible" class="retry-confirm-block">
    <div style="color:#e6a23c;font-weight:600;margin-bottom:8px;">⚠️ 工具调用多次失败</div>
    <div style="font-size:13px;color:#606266;margin-bottom:12px;">{{ message }}</div>
    <div style="display:flex;gap:8px;">
      <el-button size="small" type="primary" @click="emit('continue')">继续尝试</el-button>
      <el-button size="small" @click="emit('abort')">放弃</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 重试确认弹框：工具调用多次失败时，提示用户继续尝试或放弃。
 */
defineProps<{
  /** 是否显示 */
  visible: boolean
  /** 提示消息内容 */
  message: string
}>()

const emit = defineEmits<{
  /** 继续尝试 */
  (e: 'continue'): void
  /** 放弃 */
  (e: 'abort'): void
}>()
</script>
