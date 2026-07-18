<template>
  <!-- Inline edit status bar -->
  <div v-if="editor.hasInlineChanges.value" class="db-inline-bar">
    <span class="inline-change-hint">
      <template v-if="editor.inlineChangeCount.value > 0">{{ editor.inlineChangeCount.value }} 个单元格已修改</template>
      <template v-if="editor.inlineChangeCount.value > 0 && (editor.newRowUids.value.size > 0 || editor.pendingDeleteKeys.value.size > 0)">，</template>
      <template v-if="editor.newRowUids.value.size > 0">{{ editor.newRowUids.value.size }} 行待新增</template>
      <template v-if="editor.newRowUids.value.size > 0 && editor.pendingDeleteKeys.value.size > 0">，</template>
      <template v-if="editor.pendingDeleteKeys.value.size > 0">{{ editor.pendingDeleteKeys.value.size }} 行待删除</template>
    </span>
    <el-button size="small" type="primary" :loading="editor.savingInline.value" @click="editor.saveInlineChanges">保存更改</el-button>
    <el-button size="small" @click="editor.discardInlineChanges">放弃更改</el-button>
  </div>

  <!-- Table area -->
  <div
    class="table-wrapper"
    style="flex: 1; overflow: hidden;"
    @paste="editor.handlePaste"
    @keydown="editor.onTableKeydown"
    @mouseup="editor.onTableMouseUp"
    @mouseleave="editor.onTableMouseUp"
    @contextmenu.prevent="editor.onTableContextMenu"
    tabindex="0"
  >
    <el-table
      :data="props.rows"
      height="100%"
      style="width: 100%"
      :row-key="props.getRowKey"
      stripe
      border
      :row-class-name="editor.rowClassName"
      :cell-class-name="editor.cellClassFn"
    >
      <!-- Row index column -->
      <el-table-column type="index" width="60" fixed :index="props.rowIndexOffset" resizable />

      <!-- Data columns -->
      <el-table-column
        v-for="col in props.dataColumns"
        :key="col.name"
        :prop="col.name"
        :min-width="Math.max(150, col.name.length * 14 + 60)"
        resizable
      >
        <template #header>
          <div style="display: flex; align-items: center; gap: 5px;">
            <span :title="col.comment || col.name" style="cursor: pointer;">
              {{ col.name }}
            </span>
            <span
              :class="['col-filter-icon', { 'col-filter-active': props.isColumnFiltered(col.name) }]"
              :title="props.isColumnFiltered(col.name) ? '已设置过滤条件（点击编辑）' : '设置过滤条件'"
              :data-col="col.name"
              @click.stop="emit('open-column-filter', col, ($event.currentTarget as HTMLElement))"
            >
              <el-icon :size="14"><Filter /></el-icon>
              <span v-if="props.isColumnFiltered(col.name)" class="col-filter-dot"></span>
            </span>
            <el-icon
              :size="14"
              style="cursor: pointer;"
              title="排序"
              @click.stop="props.handleSort(col.name)"
            >
              <component :is="getSortIconComponent(col.name)" />
            </el-icon>
          </div>
        </template>
        <template #default="scope">
          <div
            class="inline-cell"
            :class="{
              'cell-selected-sel': editor.isCellInSelection(scope.$index, col.name),
              'cell-focused': editor.isCellFocused(scope.$index, col.name),
            }"
            @mousedown="editor.onCellMouseDown(scope.$index, col.name, $event)"
            @mousemove="editor.onCellMouseMove(scope.$index, col.name)"
            @mouseenter="editor.onCellMouseEnter(scope.$index, col.name)"
            @dblclick.stop="editor.startInlineEdit(scope.row, col.name)"
            @click="editor.activeCellIndex.value = scope.$index; editor.activeColName.value = col.name"
            @contextmenu.prevent.stop="editor.onCellContextMenu(scope.$index, col.name, $event)"
          >
            <template v-if="editor.isEditingCell(scope.row, col.name)">
              <el-date-picker
                v-if="props.isDateColumn(col.name)"
                v-model="editor.editingValue.value"
                type="datetime"
                value-format="YYYY-MM-DDTHH:mm:ss"
                placeholder="选择日期"
                size="small"
                class="inline-edit-input"
                @keyup.escape="editor.cancelInlineEdit()"
                @visible-change="(visible: boolean) => { if (!visible) editor.commitInlineEdit() }"
              />
              <el-input
                v-else
                :ref="(el: any) => editor.setEditInputRef(el)"
                v-model="editor.editingValue.value"
                size="small"
                class="inline-edit-input"
                @keyup.enter="editor.commitInlineEdit()"
                @keyup.escape="editor.cancelInlineEdit()"
                @blur="editor.commitInlineEdit()"
              />
            </template>
            <span
              v-else
              :class="{ 'cell-changed': editor.isCellChanged(scope.row, col.name) }"
              :title="editor.formatCellTitle(editor.getRowValue(scope.row, col.name))"
            >
              <template v-if="editor.getRowValue(scope.row, col.name) !== null && editor.getRowValue(scope.row, col.name) !== undefined && editor.getRowValue(scope.row, col.name) !== ''">{{ editor.getRowValue(scope.row, col.name) }}</template>
              <span v-else-if="editor.getRowValue(scope.row, col.name) === null || editor.getRowValue(scope.row, col.name) === undefined" class="null-placeholder">NULL</span>
              <span v-else class="empty-placeholder"></span>
            </span>
          </div>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <!-- Context menu (teleported to body) -->
  <Teleport to="body">
    <div
      v-if="editor.contextMenuVisible.value"
      class="db-context-menu"
      :style="{ left: editor.contextMenuPos.value.x + 'px', top: editor.contextMenuPos.value.y + 'px' }"
      @click.stop
      @contextmenu.prevent
    >
      <div class="ctx-item" @click="editor.ctxCopy"><span class="ctx-icon">📋</span>复制 <span class="ctx-shortcut">Ctrl+C</span></div>
      <div class="ctx-item" @click="editor.ctxPaste"><span class="ctx-icon">📄</span>粘贴 <span class="ctx-shortcut">Ctrl+V</span></div>
      <div class="ctx-divider"></div>
      <div class="ctx-item" @click="editor.ctxEditDetail"><span class="ctx-icon">📝</span>详细编辑</div>
      <div class="ctx-item" @click="editor.ctxClearCells"><span class="ctx-icon">🗑️</span>清空单元格 <span class="ctx-shortcut">Delete</span></div>
      <div class="ctx-item" @click="editor.ctxSetNull"><span class="ctx-icon">∅</span>设为 NULL</div>
      <div class="ctx-divider"></div>
      <div class="ctx-item" @click="editor.ctxInsertRowAbove"><span class="ctx-icon">⬆️</span>上方插入行</div>
      <div class="ctx-item" @click="editor.ctxInsertRowBelow"><span class="ctx-icon">⬇️</span>下方插入行</div>
      <div class="ctx-item" @click="editor.ctxDeleteRows"><span class="ctx-icon">❌</span>删除选中行</div>
      <div class="ctx-divider"></div>
      <div class="ctx-item" :class="{ disabled: !editor.canFillDown.value }" @click="editor.ctxFillDown"><span class="ctx-icon">⬇️</span>向下填充 <span class="ctx-shortcut">Ctrl+D</span></div>
      <div class="ctx-item" @click="editor.ctxCopyRow"><span class="ctx-icon">📑</span>复制整行</div>
      <div class="ctx-divider"></div>
      <div class="ctx-item" :class="{ disabled: editor.undoStack.value.length === 0 }" @click="editor.ctxUndo"><span class="ctx-icon">↩️</span>撤销 <span class="ctx-shortcut">Ctrl+Z</span></div>
      <div class="ctx-item" :class="{ disabled: editor.redoStack.value.length === 0 }" @click="editor.ctxRedo"><span class="ctx-icon">↪️</span>重做 <span class="ctx-shortcut">Ctrl+Y</span></div>
    </div>
  </Teleport>
</template>

<script lang="ts" setup>
import { toRef } from 'vue'
import { ArrowDown, ArrowUp, Filter, Sort } from '@element-plus/icons-vue'
import { useTableEditor } from '../composables/useTableEditor'
import type { DataColumn } from '../composables/useDataQuery'

const props = defineProps<{
  rows: Record<string, any>[]
  dataColumns: DataColumn[]
  pkColumns: string[]
  effectiveDbType: string
  connId: string
  schema: string
  tableName: string
  rowIndexOffset: number
  sortColumn: string
  sortOrder: 'ascending' | 'descending' | null
  isDateColumn: (colName: string) => boolean
  getRowKey: (row: Record<string, any>) => string
  isColumnFiltered: (colName: string) => boolean
  handleSort: (colName: string) => void
  loadData: () => Promise<void>
}>()

const emit = defineEmits<{
  (e: 'open-column-filter', col: DataColumn, triggerEl: HTMLElement): void
  (e: 'edit-row', row: Record<string, any>): void
}>()

// Create writable refs from props for the composable.
// `toRef` on a reactive prop creates a ref whose .value reflects the prop.
// In-place array mutations (push/splice) operate on the same array reference
// the parent holds, so changes propagate up without prop reassignment.
const rowsRef = toRef(props, 'rows')
const dataColumnsRef = toRef(props, 'dataColumns')
const pkColumnsRef = toRef(props, 'pkColumns')

const editor = useTableEditor({
  connId: () => props.connId,
  schema: () => props.schema,
  tableName: () => props.tableName,
  effectiveDbType: () => props.effectiveDbType,
  rows: rowsRef,
  dataColumns: dataColumnsRef,
  pkColumns: pkColumnsRef,
  getRowKey: props.getRowKey,
  loadData: props.loadData,
  onEditRow: (row) => emit('edit-row', row),
})

// Map sort state to an icon component (composable stays UI-agnostic)
function getSortIconComponent(colName: string) {
  if (props.sortColumn !== colName) return Sort
  return props.sortOrder === 'ascending' ? ArrowUp : ArrowDown
}
</script>

<style scoped>
.db-inline-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 14px;
  background: var(--bg-inline-bar);
  border-bottom: 1px solid var(--border-inline);
}

.db-inline-bar .inline-change-hint {
  font-size: 13px;
  color: var(--accent-color);
  font-weight: 500;
}

.inline-cell {
  min-height: 24px;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  user-select: text;
}

.inline-cell > span {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: block;
  width: 100%;
  user-select: text;
}

.null-placeholder {
  color: var(--text-tertiary, #bbb);
  font-style: italic;
  font-size: 0.85em;
  user-select: none;
}

.inline-edit-input {
  width: 100%;
}

.inline-edit-input :deep(.el-input__inner) {
  padding: 0 4px;
  height: 28px;
}

:deep(.row-new td) {
  border-left: 3px solid #67c23a;
  background-color: #f0f9eb !important;
}

[data-theme="dark"] :deep(.row-new td) {
  background-color: #1a2a1a !important;
  border-left-color: #4caf50;
}

:deep(.row-changed td) {
  border-bottom: 2px solid #faad14;
}

:deep(.row-deleted td) {
  background-color: #fef0f0 !important;
  text-decoration: line-through;
  color: #f56c6c;
}

[data-theme="dark"] :deep(.row-deleted td) {
  background-color: #2a1a1a !important;
  color: #f89898;
}

/* Column header filter icon */
.col-filter-icon {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  color: #c0c4cc;
  transition: all 0.15s ease;
}

.col-filter-icon:hover {
  color: #409eff;
  background-color: rgba(64, 158, 255, 0.08);
}

.col-filter-icon.col-filter-active {
  color: #409eff;
  background-color: rgba(64, 158, 255, 0.1);
}

.col-filter-dot {
  position: absolute;
  top: 1px;
  right: 1px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background-color: #f56c6c;
  box-shadow: 0 0 0 1.5px #fff;
}

[data-theme="dark"] .col-filter-icon:hover {
  background-color: rgba(99, 179, 237, 0.12);
  color: #63b3ed;
}

[data-theme="dark"] .col-filter-icon.col-filter-active {
  color: #63b3ed;
  background-color: rgba(99, 179, 237, 0.1);
}

[data-theme="dark"] .col-filter-dot {
  background-color: #fc8181;
  box-shadow: 0 0 0 1.5px #1a202c;
}
</style>

<style>
/* Cell range selection (global — applied to el-table cells via cell-class-name) */
.cell-range-selected {
  background-color: #68a6eb !important;
  outline: none !important;
  outline-offset: -1px;
}

/* Context menu styles (global — teleported to body) */
.db-context-menu {
  position: fixed;
  z-index: 9999;
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  padding: 4px 0;
  min-width: 200px;
  font-size: 13px;
}

[data-theme="dark"] .db-context-menu {
  background: #1e1e1e;
  border-color: #3a3a3a;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
}

.db-context-menu .ctx-item {
  padding: 7px 14px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #303133;
  transition: background 0.15s;
}

[data-theme="dark"] .db-context-menu .ctx-item {
  color: #e0e0e0;
}

.db-context-menu .ctx-item:hover {
  background: #f5f7fa;
}

[data-theme="dark"] .db-context-menu .ctx-item:hover {
  background: #2a2a2a;
}

.db-context-menu .ctx-item.disabled {
  color: #c0c4cc;
  cursor: not-allowed;
  pointer-events: none;
}

.db-context-menu .ctx-item .ctx-icon {
  width: 18px;
  text-align: center;
  flex-shrink: 0;
}

.db-context-menu .ctx-item .ctx-shortcut {
  margin-left: auto;
  color: #909399;
  font-size: 11px;
}

.db-context-menu .ctx-divider {
  height: 1px;
  margin: 4px 8px;
  background: #ebeef5;
}

[data-theme="dark"] .db-context-menu .ctx-divider {
  background: #3a3a3a;
}
</style>
