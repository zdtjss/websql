<template>
  <aside class="er-relation-panel" aria-label="关系列表">
    <div class="er-rp-header">
      <span class="er-rp-title">关系列表</span>
      <span class="er-rp-count">{{ filteredRows.length }} / {{ rows.length }}</span>
    </div>
    <div class="er-rp-search">
      <input
        v-model="query"
        type="search"
        class="er-rp-search-input"
        placeholder="搜索表名 / 字段 / 约束名"
        aria-label="搜索关系"
      />
    </div>
    <ul class="er-rp-list" role="listbox" aria-label="关系条目">
      <li
        v-for="row in filteredRows"
        :key="row.key"
        class="er-rp-item"
        :class="{ 'is-selected': row.key === selectedKey }"
        role="option"
        :aria-selected="row.key === selectedKey"
        tabindex="0"
        @click="$emit('select', row.key)"
        @keydown.enter.prevent="$emit('select', row.key)"
        @keydown.delete.prevent="$emit('delete', row.key)"
        @keydown.backspace.prevent="$emit('delete', row.key)"
      >
        <div class="er-rp-line">
          <span class="er-rp-type" :class="`type-${row.typeKey}`">{{ row.relationType }}</span>
          <span class="er-rp-origin" :class="`origin-${row.origin}`">{{ originLabel(row.origin) }}</span>
          <span v-if="row.confidence" class="er-rp-confidence">{{ row.confidence }}</span>
        </div>
        <div class="er-rp-path">
          <b>{{ row.source }}</b><span v-if="row.sourceCol">.{{ row.sourceCol }}</span>
          <span class="er-rp-arrow">→</span>
          <b>{{ row.target }}</b><span v-if="row.targetCol">.{{ row.targetCol }}</span>
        </div>
        <div v-if="row.constraintName" class="er-rp-constraint">约束: {{ row.constraintName }}</div>
        <div class="er-rp-actions">
          <button type="button" class="er-rp-btn" @click.stop="$emit('cycle', row.key)">切换类型</button>
          <button type="button" class="er-rp-btn danger" @click.stop="$emit('delete', row.key)">删除</button>
        </div>
      </li>
      <li v-if="filteredRows.length === 0" class="er-rp-empty">没有匹配的关系</li>
    </ul>
  </aside>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { RelationRow } from './types'

const props = defineProps<{
  rows: RelationRow[]
  selectedKey?: string
}>()

defineEmits<{
  (e: 'select', key: string): void
  (e: 'delete', key: string): void
  (e: 'cycle', key: string): void
}>()

const query = ref('')

const filteredRows = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.rows
  return props.rows.filter(row =>
    [row.source, row.target, row.sourceCol, row.targetCol, row.constraintName || '']
      .some(field => field.toLowerCase().includes(q))
  )
})

function originLabel(origin: string): string {
  if (origin === 'ai') return 'AI'
  if (origin === 'manual') return '手动'
  return '外键'
}
</script>

<style scoped>
.er-relation-panel {
  display: flex;
  flex-direction: column;
  width: 300px;
  min-width: 240px;
  height: 100%;
  border-left: 1px solid var(--border-primary, #e4e7ed);
  background: var(--bg-primary, #fff);
  overflow: hidden;
}
.er-rp-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-primary, #e4e7ed);
}
.er-rp-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary, #303133);
}
.er-rp-count {
  font-size: 11px;
  color: var(--text-tertiary, #909399);
}
.er-rp-search {
  padding: 8px 12px;
}
.er-rp-search-input {
  box-sizing: border-box;
  width: 100%;
  height: 28px;
  padding: 0 8px;
  font-size: 12px;
  color: var(--text-primary, #303133);
  background: var(--bg-tertiary, #f5f7fa);
  border: 1px solid var(--border-primary, #e4e7ed);
  border-radius: 4px;
  outline: none;
}
.er-rp-search-input:focus {
  border-color: var(--db-accent, #409eff);
}
.er-rp-list {
  flex: 1;
  min-height: 0;
  margin: 0;
  padding: 0 8px 8px;
  list-style: none;
  overflow-y: auto;
}
.er-rp-item {
  margin-bottom: 6px;
  padding: 8px;
  border: 1px solid var(--border-primary, #e4e7ed);
  border-radius: 6px;
  cursor: pointer;
  outline: none;
}
.er-rp-item:hover,
.er-rp-item:focus-visible {
  border-color: var(--db-accent, #409eff);
}
.er-rp-item.is-selected {
  border-color: var(--db-accent, #409eff);
  box-shadow: 0 0 0 2px rgba(64, 158, 255, 0.18);
}
.er-rp-line {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.er-rp-type {
  padding: 0 6px;
  font-size: 10px;
  font-weight: 600;
  color: #fff;
  border-radius: 3px;
  background: #2f7fd1;
}
.er-rp-type.type-11 {
  background: #3f8f2c;
}
.er-rp-type.type-nm {
  background: #b8791f;
}
.er-rp-origin {
  font-size: 10px;
  color: var(--text-tertiary, #909399);
}
.er-rp-origin.origin-ai {
  color: #6f5fc0;
}
.er-rp-origin.origin-manual {
  color: #b8791f;
}
.er-rp-confidence {
  margin-left: auto;
  font-size: 10px;
  color: var(--text-tertiary, #909399);
}
.er-rp-path {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-primary, #303133);
  word-break: break-all;
}
.er-rp-arrow {
  margin: 0 4px;
  color: var(--text-tertiary, #909399);
}
.er-rp-constraint {
  margin-top: 2px;
  font-size: 11px;
  color: var(--text-tertiary, #909399);
  word-break: break-all;
}
.er-rp-actions {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}
.er-rp-btn {
  padding: 2px 8px;
  font-size: 11px;
  color: var(--text-secondary, #606266);
  background: transparent;
  border: 1px solid var(--border-primary, #e4e7ed);
  border-radius: 4px;
  cursor: pointer;
}
.er-rp-btn:hover {
  border-color: var(--db-accent, #409eff);
  color: var(--db-accent, #409eff);
}
.er-rp-btn.danger:hover {
  border-color: #f56c6c;
  color: #f56c6c;
}
.er-rp-empty {
  padding: 24px 8px;
  font-size: 12px;
  color: var(--text-tertiary, #909399);
  text-align: center;
}
</style>
