<template>
  <!-- Active filter bar (chips for each active column filter) -->
  <div v-if="activeFilterTags.length > 0" class="db-filter-bar">
    <span class="filter-bar-label">
      <el-icon :size="13"><Filter /></el-icon>
      过滤
    </span>
    <div class="filter-tags-wrap">
      <span
        v-for="tag in activeFilterTags"
        :key="tag.colName"
        class="filter-chip"
        @click="openColumnFilterByName(tag.colName)"
      >
        <span class="filter-chip-col">{{ tag.colName }}</span>
        <span class="filter-chip-expr">{{ tag.operatorLabel }} {{ tag.displayValue }}</span>
        <span class="filter-chip-close" @click.stop="removeFilterTag(tag.colName)">×</span>
      </span>
    </div>
    <span class="filter-bar-clear" @click="clearAllFilters">全部清除</span>
  </div>

  <!-- Column filter popover (triggered from table header filter icons) -->
  <el-popover
    ref="columnFilterPopoverRef"
    :visible="columnFilterDialogVisible"
    placement="bottom-start"
    :width="320"
    :virtual-ref="filterTriggerRef"
    virtual-triggering
    :show-arrow="true"
    popper-class="col-filter-popper"
  >    <div class="col-filter-popover" @click.stop>
      <!-- Header -->
      <div class="col-filter-header">
        <div class="col-filter-title">
          <span class="col-filter-name">{{ currentColumn?.name || '' }}</span>
          <span v-if="currentColumn?.comment" class="col-filter-comment">（{{ currentColumn.comment }}）</span>
        </div>
        <span class="col-filter-close" @click="columnFilterDialogVisible = false">×</span>
      </div>

      <!-- Quick actions -->
      <div class="col-filter-quick">
        <span class="quick-chip" @click="applyQuickEquals">= 等于</span>
        <span class="quick-chip" @click="applyQuickFilter('IS NOT NULL')">≠ NULL</span>
        <span class="quick-chip" @click="applyQuickFilter('IS NULL')">= NULL</span>
        <span class="quick-chip" @click="applyQuickLike">包含</span>
      </div>

      <!-- Operator + Value -->
      <div class="col-filter-body">
        <el-select
          v-model="columnFilterOperator"
          style="width: 100%;"
          @click.stop
        >
          <el-option label="= 等于" value="=" />
          <el-option label="≠ 不等于" value="!=" />
          <el-option label="> 大于" value=">" />
          <el-option label="≥ 大于等于" value=">=" />
          <el-option label="< 小于" value="<" />
          <el-option label="≤ 小于等于" value="<=" />
          <el-option label="≈ LIKE" value="LIKE" />
          <el-option label="≉ NOT LIKE" value="NOT LIKE" />
          <el-option label="∅ IS NULL" value="IS NULL" />
          <el-option label="✓ IS NOT NULL" value="IS NOT NULL" />
          <el-option label="∈ IN" value="IN" />
          <el-option label="∉ NOT IN" value="NOT IN" />
        </el-select>

        <el-input
          v-if="!['IS NULL', 'IS NOT NULL'].includes(columnFilterOperator)"
          ref="filterValueInputRef"
          v-model="columnFilterValue"
          :type="['IN', 'NOT IN'].includes(columnFilterOperator) ? 'textarea' : 'text'"
          :rows="2"
          :placeholder="getOperatorPlaceholder(columnFilterOperator)"
          clearable
          @click.stop
          @keydown.enter.prevent="applyColumnFilter"
          style="margin-top: 8px;"
        />
      </div>

      <!-- Footer -->
      <div class="col-filter-footer">
        <span
          class="col-filter-clear-link"
          :class="{ disabled: !isCurrentColumnFiltered }"
          @click="isCurrentColumnFiltered && clearColumnFilter()"
        >清除</span>
        <div class="col-filter-actions">
          <el-button size="small" @click="columnFilterDialogVisible = false">取消</el-button>
          <el-button size="small" type="primary" @click="applyColumnFilter">应用过滤</el-button>
        </div>
      </div>
    </div>
  </el-popover>
</template>

<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef } from 'vue'
import { Filter } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { fmtVal, quoteId } from '@/utils/sqlHelper.ts'
import type { DataColumn } from '../composables/useDataQuery'

const props = defineProps<{
  dataColumns: DataColumn[]
  effectiveDbType: string
  /** Current WHERE clause — used to detect whether a column already has a filter */
  filterExpr: string
}>()

const emit = defineEmits<{
  /** Fired when the user applies a new filter condition. Parent must append to filterExpr + reload. */
  (e: 'filter-applied', payload: { colName: string; operator: string; value: string; condition: string }): void
  /** Fired when the user clears a single column's filter. Parent must remove from filterExpr + reload. */
  (e: 'filter-cleared', colName: string): void
  /** Fired when the user clicks "全部清除". Parent must empty filterExpr + reload. */
  (e: 'all-filters-cleared'): void
}>()

// ===== Filter dialog state =====
const columnFilterDialogVisible = ref(false)
const currentColumn = ref<DataColumn | null>(null)
const columnFilterOperator = ref('=')
const columnFilterValue = ref('')
const columnFilterPopoverRef = useTemplateRef('columnFilterPopoverRef')
const filterTriggerRef = ref<HTMLElement | null>(null)
const filterValueInputRef = ref<any>(null)

// Per-column filter conditions: { [colName]: { operator, value } }
const columnFilterConditions = ref<Record<string, { operator: string; value: string }>>({})

// ===== Computed: active filter tags for the filter bar =====
const activeFilterTags = computed(() => {
  const tags: { colName: string; operatorLabel: string; value: string; displayValue: string }[] = []
  const opLabels: Record<string, string> = {
    '=': '=', '!=': '≠', '>': '>', '>=': '≥',
    '<': '<', '<=': '≤', 'LIKE': '≈', 'NOT LIKE': '≉',
    'IS NULL': '为空', 'IS NOT NULL': '非空',
    'IN': '∈', 'NOT IN': '∉',
  }
  for (const [colName, cond] of Object.entries(columnFilterConditions.value)) {
    const operatorLabel = opLabels[cond.operator] || cond.operator
    const displayValue = cond.value
      ? (cond.value.length > 20 ? cond.value.slice(0, 20) + '…' : cond.value)
      : ''
    tags.push({ colName, operatorLabel, value: cond.value, displayValue })
  }
  return tags
})

const isCurrentColumnFiltered = computed(() => {
  if (!currentColumn.value) return false
  return isColumnFiltered(currentColumn.value.name)
})

// ===== Helpers =====
function isColumnFiltered(colName: string): boolean {
  if (!props.filterExpr.trim()) return false
  // A column is considered filtered if it appears in columnFilterConditions
  // (more reliable than parsing the WHERE clause)
  return !!columnFilterConditions.value[colName]
}

function getOperatorPlaceholder(op: string): string {
  switch (op) {
    case 'LIKE':
    case 'NOT LIKE':
      return '例如：%keyword%'
    case 'IN':
    case 'NOT IN':
      return '例如：value1,value2,value3'
    default:
      return '请输入值'
  }
}

function buildColumnCondition(): string {
  if (!currentColumn.value) return ''
  const colName = quoteId(currentColumn.value.name, props.effectiveDbType)
  const op = columnFilterOperator.value
  const val = columnFilterValue.value.trim()

  if (op === 'IS NULL') return `${colName} IS NULL`
  if (op === 'IS NOT NULL') return `${colName} IS NOT NULL`

  if (!val) {
    ElMessage({ message: '请输入值', type: 'warning' })
    return ''
  }

  if (op === 'IN' || op === 'NOT IN') {
    const values = val.split(',').map((v) => v.trim()).filter((v) => v)
    if (values.length === 0) {
      ElMessage({ message: '请至少输入一个值', type: 'warning' })
      return ''
    }
    const formatted = values.map((v) => fmtVal(v, props.effectiveDbType)).join(', ')
    return `${colName} ${op} (${formatted})`
  }

  return `${colName} ${op} ${fmtVal(val, props.effectiveDbType)}`
}

// ===== Public actions =====
function applyColumnFilter() {
  const condition = buildColumnCondition()
  if (!condition) return

  // Save the per-column condition
  if (currentColumn.value) {
    columnFilterConditions.value[currentColumn.value.name] = {
      operator: columnFilterOperator.value,
      value: columnFilterValue.value,
    }
  }

  columnFilterDialogVisible.value = false
  emit('filter-applied', {
    colName: currentColumn.value?.name || '',
    operator: columnFilterOperator.value,
    value: columnFilterValue.value,
    condition,
  })
  ElMessage({ message: '过滤条件已应用', type: 'success' })
}

function clearColumnFilter() {
  if (!currentColumn.value) return
  const colName = currentColumn.value.name
  delete columnFilterConditions.value[colName]
  columnFilterDialogVisible.value = false
  emit('filter-cleared', colName)
  ElMessage({ message: '该字段过滤已清除', type: 'success' })
}

function clearAllFilters() {
  columnFilterConditions.value = {}
  emit('all-filters-cleared')
}

function removeFilterTag(colName: string) {
  delete columnFilterConditions.value[colName]
  emit('filter-cleared', colName)
}

function applyQuickFilter(op: string) {
  columnFilterOperator.value = op
  columnFilterValue.value = ''
  applyColumnFilter()
}

function applyQuickEquals() {
  columnFilterOperator.value = '='
  columnFilterValue.value = ''
  nextTick(() => {
    filterValueInputRef.value?.focus?.()
  })
}

function applyQuickLike() {
  columnFilterOperator.value = 'LIKE'
  columnFilterValue.value = '%%'
  nextTick(() => {
    if (filterValueInputRef.value) {
      filterValueInputRef.value.focus?.()
      const input = filterValueInputRef.value.$el?.querySelector('input')
      if (input) input.setSelectionRange(1, 1)
    }
  })
}

// ===== Popover open logic =====
function openColumnFilter(col: DataColumn, triggerEl: HTMLElement) {
  if (currentColumn.value?.name === col.name && columnFilterDialogVisible.value) {
    columnFilterDialogVisible.value = false
    return
  }
  currentColumn.value = col
  filterTriggerRef.value = triggerEl

  const savedCondition = columnFilterConditions.value[col.name]
  if (savedCondition) {
    columnFilterOperator.value = savedCondition.operator
    columnFilterValue.value = savedCondition.value
  } else {
    columnFilterOperator.value = '='
    columnFilterValue.value = ''
  }

  columnFilterDialogVisible.value = true
  nextTick(() => {
    if (filterValueInputRef.value && !['IS NULL', 'IS NOT NULL'].includes(columnFilterOperator.value)) {
      filterValueInputRef.value.focus?.()
    }
  })
}

function openColumnFilterByName(colName: string) {
  const col = props.dataColumns.find((c) => c.name === colName)
  if (!col) return
  // Use the filter icon element in the table header as the trigger
  const headerEl = document.querySelector(`.col-filter-icon[data-col="${colName}"]`) as HTMLElement | null
  if (headerEl) filterTriggerRef.value = headerEl
  currentColumn.value = col
  const savedCondition = columnFilterConditions.value[colName]
  if (savedCondition) {
    columnFilterOperator.value = savedCondition.operator
    columnFilterValue.value = savedCondition.value
  } else {
    columnFilterOperator.value = '='
    columnFilterValue.value = ''
  }
  columnFilterDialogVisible.value = true
  nextTick(() => {
    if (filterValueInputRef.value && !['IS NULL', 'IS NOT NULL'].includes(columnFilterOperator.value)) {
      filterValueInputRef.value.focus?.()
    }
  })
}

// ===== Outside-click handling for the popover =====
function onFilterPopoverMouseDown(e: MouseEvent) {
  if (!columnFilterDialogVisible.value) return
  const target = e.target as HTMLElement
  if (target.closest('.el-popper')) return
  if (filterTriggerRef.value && (filterTriggerRef.value === target || filterTriggerRef.value.contains(target))) return
  columnFilterDialogVisible.value = false
}

if (typeof document !== 'undefined') {
  document.addEventListener('mousedown', onFilterPopoverMouseDown)
}

onBeforeUnmount(() => {
  if (typeof document !== 'undefined') {
    document.removeEventListener('mousedown', onFilterPopoverMouseDown)
  }
})

// Expose methods for the parent to call when the table emits open-column-filter events
defineExpose({
  openColumnFilter,
  openColumnFilterByName,
  isColumnFiltered,
})
</script>

<style scoped>
/* Filter bar */
.db-filter-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 14px;
  background: linear-gradient(to right, #f0f7ff, #f8fbff);
  border-bottom: 1px solid #e4ecf5;
  min-height: 36px;
}

.filter-bar-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #5a7fa8;
  font-weight: 500;
  white-space: nowrap;
  user-select: none;
}

.filter-tags-wrap {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  flex: 1;
}

.filter-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 8px;
  background: #fff;
  border: 1px solid #d9e4f0;
  border-radius: 12px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
  max-width: 220px;
}

.filter-chip:hover {
  border-color: #a3c4e8;
  box-shadow: 0 2px 6px rgba(64, 158, 255, 0.12);
  transform: translateY(-1px);
}

.filter-chip-col {
  font-weight: 600;
  color: #2c5282;
  white-space: nowrap;
}

.filter-chip-expr {
  color: #6b7c93;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100px;
}

.filter-chip-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  font-size: 12px;
  line-height: 1;
  color: #a0aec0;
  margin-left: 2px;
  transition: all 0.15s;
}

.filter-chip-close:hover {
  background: #fee2e2;
  color: #e53e3e;
}

.filter-bar-clear {
  font-size: 12px;
  color: #909399;
  cursor: pointer;
  white-space: nowrap;
  user-select: none;
  transition: color 0.15s;
}

.filter-bar-clear:hover {
  color: #e53e3e;
}

/* Filter popover */
.col-filter-popover {
  padding: 2px 0;
}

.col-filter-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 12px;
}

.col-filter-title {
  display: flex;
  align-items: baseline;
  gap: 4px;
}

.col-filter-name {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
  letter-spacing: -0.01em;
}

.col-filter-comment {
  font-size: 11px;
  color: #909399;
  font-weight: normal;
}

.col-filter-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  font-size: 16px;
  color: #c0c4cc;
  cursor: pointer;
  transition: all 0.15s;
  line-height: 1;
}

.col-filter-close:hover {
  color: #606266;
  background: #f5f5f5;
}

.col-filter-quick {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
}

.quick-chip {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 10px;
  font-size: 12px;
  color: #5a7fa8;
  background: #f0f7ff;
  border: 1px solid #dbe8f4;
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
  user-select: none;
}

.quick-chip:hover {
  color: #409eff;
  background: #ecf5ff;
  border-color: #b3d8ff;
  transform: translateY(-1px);
  box-shadow: 0 2px 4px rgba(64, 158, 255, 0.1);
}

.col-filter-body {
  display: flex;
  flex-direction: column;
}

.col-filter-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid #f0f0f0;
}

.col-filter-actions {
  display: flex;
  gap: 8px;
}

.col-filter-clear-link {
  font-size: 12px;
  color: #909399;
  cursor: pointer;
  transition: color 0.15s;
  user-select: none;
}

.col-filter-clear-link:hover {
  color: #e53e3e;
}

.col-filter-clear-link.disabled {
  color: #dcdfe6;
  cursor: not-allowed;
}

/* Dark mode */
[data-theme="dark"] .db-filter-bar {
  background: linear-gradient(to right, #1a2332, #1d2636);
  border-bottom-color: #2d3748;
}

[data-theme="dark"] .filter-bar-label {
  color: #7fb3d4;
}

[data-theme="dark"] .filter-chip {
  background: #2d3748;
  border-color: #4a5568;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

[data-theme="dark"] .filter-chip:hover {
  border-color: #63b3ed;
  box-shadow: 0 2px 6px rgba(99, 179, 237, 0.15);
}

[data-theme="dark"] .filter-chip-col {
  color: #90cdf4;
}

[data-theme="dark"] .filter-chip-expr {
  color: #a0aec0;
}

[data-theme="dark"] .filter-chip-close:hover {
  background: #422b2b;
  color: #fc8181;
}

[data-theme="dark"] .filter-bar-clear:hover {
  color: #fc8181;
}

[data-theme="dark"] .col-filter-close:hover {
  color: #e2e8f0;
  background: #2d3748;
}

[data-theme="dark"] .quick-chip {
  color: #7fb3d4;
  background: #1a2c40;
  border-color: #2d4a5e;
}

[data-theme="dark"] .quick-chip:hover {
  color: #90cdf4;
  background: #1e3a52;
  border-color: #4299e1;
}

[data-theme="dark"] .col-filter-footer {
  border-top-color: #2d3748;
}

[data-theme="dark"] .col-filter-clear-link {
  color: #718096;
}

[data-theme="dark"] .col-filter-clear-link:hover {
  color: #fc8181;
}

[data-theme="dark"] .col-filter-clear-link.disabled {
  color: #4a5568;
}
</style>

<style>
/* Popper customization (global — el-popover teleports to body) */
.col-filter-popper {
  border-radius: 10px !important;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.1), 0 2px 8px rgba(0, 0, 0, 0.06) !important;
  border: 1px solid #e8ecf0 !important;
}

[data-theme="dark"] .col-filter-popper {
  background: #1e2433 !important;
  border-color: #2d3748 !important;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.4), 0 2px 8px rgba(0, 0, 0, 0.3) !important;
}
</style>
