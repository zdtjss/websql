<template>
  <div class="er-legend" role="group" aria-label="关系图例与过滤">
    <span class="er-legend-label">图例</span>
    <button
      v-for="item in items"
      :key="item.key"
      type="button"
      class="er-legend-chip"
      :class="{ 'is-off': !item.active }"
      :aria-pressed="item.active"
      :title="item.active ? `隐藏${item.label}` : `显示${item.label}`"
      @click="$emit('toggle', item.key)"
    >
      <span class="er-legend-dot" :style="{ background: item.color }" />
      <span class="er-legend-text">{{ item.label }}</span>
      <span class="er-legend-count">{{ item.count }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import type { LegendItem } from './types'

defineProps<{ items: LegendItem[] }>()
defineEmits<{ (e: 'toggle', key: string): void }>()
</script>

<style scoped>
.er-legend {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.er-legend-label {
  font-size: 11px;
  color: var(--text-tertiary, #909399);
}
.er-legend-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--text-secondary, #606266);
  background: transparent;
  border: 1px solid var(--border-primary, #e4e7ed);
  border-radius: 10px;
  cursor: pointer;
  line-height: 18px;
}
.er-legend-chip:hover {
  border-color: var(--db-accent, #409eff);
}
.er-legend-chip.is-off {
  opacity: 0.45;
  text-decoration: line-through;
}
.er-legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.er-legend-count {
  font-weight: 600;
  color: var(--text-primary, #303133);
}
</style>
