<template>
  <div class="view-mode-switch" role="group" aria-label="交互模式切换">
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      class="view-mode-switch__item"
      :class="{ 'is-active': opt.value === active }"
      :aria-current="opt.value === active ? 'page' : undefined"
      :title="opt.hint"
      @click="switchTo(opt.value)"
    >
      <el-icon :size="14"><component :is="opt.icon" /></el-icon>
      <span class="view-mode-switch__label">{{ opt.label }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
/**
 * 交互模式切换：在「AI 对话」与「经典视图」之间切换。
 * 两个页面复用同一控件，保证进出入口始终可见、文案与样式一致。
 */
import { computed, type Component } from 'vue'
import { useRouter } from 'vue-router'
import { Grid, MagicStick } from '@element-plus/icons-vue'

interface ViewModeOption {
  value: 'ai' | 'classical'
  label: string
  icon: Component
  hint: string
}

const props = withDefaults(defineProps<{
  /** 当前所在模式 */
  active: 'ai' | 'classical'
  /** 是否展示「经典视图」入口（远程模式由角色权限决定） */
  showClassic?: boolean
}>(), { showClassic: true })

const router = useRouter()

const options = computed<ViewModeOption[]>(() => {
  const list: ViewModeOption[] = [
    { value: 'ai', label: 'AI 对话', icon: MagicStick, hint: '大模型对话：用自然语言查数、分析、生成 SQL、导出文件' },
  ]
  if (props.showClassic) {
    list.push({ value: 'classical', label: '经典视图', icon: Grid, hint: '经典视图：数据库树、SQL 编辑器、表管理与数据浏览' })
  }
  return list
})

function switchTo(mode: 'ai' | 'classical'): void {
  if (mode === props.active) return
  router.push(mode === 'ai' ? '/ai' : '/classical')
}
</script>

<style scoped>
.view-mode-switch {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border-radius: 999px;
  background: var(--bg-tertiary);
  border: 1px solid var(--border-primary);
}

.view-mode-switch__item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border: none;
  border-radius: 999px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 12.5px;
  font-weight: 500;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition: color 0.2s ease, background-color 0.2s ease, box-shadow 0.2s ease;
}

.view-mode-switch__item:hover {
  color: var(--accent-color);
  background: var(--bg-hover);
}

.view-mode-switch__item.is-active {
  color: #fff;
  cursor: default;
  background: linear-gradient(135deg, var(--accent-color) 0%, #6aa9ff 100%);
  box-shadow: 0 2px 8px rgba(64, 158, 255, 0.35);
}

/* 深色主题下底色与侧栏接近，加深背景与描边保证控件边界清晰 */
[data-theme='dark'] .view-mode-switch {
  background: var(--bg-secondary);
}

[data-theme='dark'] .view-mode-switch__item.is-active {
  background: linear-gradient(135deg, var(--accent-color) 0%, #2f8fd6 100%);
  box-shadow: 0 2px 8px rgba(0, 122, 204, 0.45);
}
</style>
