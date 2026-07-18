import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import http from '@/api/index'
import { buildWhereCondition, fmtVal, quoteId } from '@/utils/sqlHelper.ts'
import type { Ref } from 'vue'
import type { DataColumn } from './useDataQuery'

export interface TableEditorParams {
  connId: () => string | undefined
  schema: () => string | undefined
  tableName: () => string | undefined
  effectiveDbType: () => string
  rows: Ref<Record<string, any>[]>
  dataColumns: Ref<DataColumn[]>
  pkColumns: Ref<string[]>
  getRowKey: (row: Record<string, any>) => string
  loadData: () => Promise<void>
  /** Called when the user picks "详细编辑" from the context menu. */
  onEditRow?: (row: Record<string, any>) => void
}

/**
 * Encapsulates all "interactive table" logic for the DataTableView:
 * - Inline cell editing (with date-picker support)
 * - Excel-style range selection (mouse drag, shift+click, shift+arrow)
 * - Keyboard navigation (arrows, tab, enter, F2, delete, ctrl+c/v/d/z/y)
 * - Right-click context menu (copy/paste/insert/delete/fill/undo/redo)
 * - Undo / redo stack (max 50 snapshots)
 * - Paste grid (multi-cell tab/newline delimited) with tiling
 * - Pending change tracking (changedRows / newRows / pendingDeleteRows)
 * - Save / discard inline changes (batched SQL execution)
 *
 * The composable mutates `rows` in place (push/splice) for new rows and
 * uses `changedRows` to overlay uncommitted cell values on top of the
 * original row data. `originalRows` is rebuilt whenever the parent's
 * `rows` ref is reassigned (i.e. on data reload).
 */
export function useTableEditor(params: TableEditorParams) {
  const { rows, dataColumns, pkColumns, getRowKey, loadData } = params

  // ===== Inline editing state =====
  const editingCell = ref<{ rowKey: string; colName: string } | null>(null)
  const editingValue = ref<string>('')
  const editingOriginalValue = ref<any>(null)
  const changedRows = ref<Record<string, Record<string, any>>>({})
  const originalRows = ref<Record<string, Record<string, any>>>({})
  const savingInline = ref(false)
  let editInputRef: any = null
  let _editInputSetupDone = false
  let _inputPasteHandler: ((e: ClipboardEvent) => void) | null = null

  // Inline new row state
  let nextRowUid = 1
  const newRowUids = ref<Set<number>>(new Set())
  const pendingDeleteKeys = ref<Set<string>>(new Set())

  // ===== Range selection state =====
  const activeCellIndex = ref(-1)
  const activeColName = ref('')
  const selStart = ref({ row: -1, col: -1 })
  const selEnd = ref({ row: -1, col: -1 })
  const selAnchor = ref({ row: -1, col: -1 })
  const pendingStart = ref<{ row: number; col: number } | null>(null)
  const pendingMoved = ref(false)

  const selectionBounds = computed(() => {
    if (selStart.value.row < 0 || selEnd.value.row < 0) return null
    return {
      rowMin: Math.min(selStart.value.row, selEnd.value.row),
      rowMax: Math.max(selStart.value.row, selEnd.value.row),
      colMin: Math.min(selStart.value.col, selEnd.value.col),
      colMax: Math.max(selStart.value.col, selEnd.value.col),
    }
  })

  const canFillDown = computed(() => {
    const bounds = selectionBounds.value
    return !!bounds && bounds.rowMin < bounds.rowMax
  })

  const inlineChangeCount = computed(() => {
    let count = 0
    Object.values(changedRows.value).forEach((row) => {
      count += Object.keys(row).length
    })
    return count
  })

  const hasInlineChanges = computed(
    () =>
      inlineChangeCount.value > 0 ||
      newRowUids.value.size > 0 ||
      pendingDeleteKeys.value.size > 0,
  )

  // ===== Context menu state =====
  const contextMenuVisible = ref(false)
  const contextMenuPos = ref({ x: 0, y: 0 })

  // ===== Undo / Redo stack =====
  const undoStack = ref<any[]>([])
  const redoStack = ref<any[]>([])
  const MAX_UNDO = 50

  // ===== Helpers =====
  function colNameToIndex(colName: string): number {
    return dataColumns.value.findIndex((c) => c.name === colName)
  }

  function isCellInSelection(rowIdx: number, colName: string): boolean {
    const bounds = selectionBounds.value
    if (!bounds) return false
    const colIdx = colNameToIndex(colName)
    return (
      rowIdx >= bounds.rowMin &&
      rowIdx <= bounds.rowMax &&
      colIdx >= bounds.colMin &&
      colIdx <= bounds.colMax
    )
  }

  function isCellFocused(rowIdx: number, colName: string): boolean {
    if (editingCell.value) return false
    return rowIdx === activeCellIndex.value && colName === activeColName.value
  }

  function cellClassFn({ rowIndex, columnIndex }: { rowIndex: number; columnIndex: number }): string {
    const bounds = selectionBounds.value
    if (!bounds) return ''
    // columnIndex includes the leading index column — offset by 1
    const dataColIdx = columnIndex - 1
    if (
      dataColIdx >= bounds.colMin &&
      dataColIdx <= bounds.colMax &&
      rowIndex >= bounds.rowMin &&
      rowIndex <= bounds.rowMax
    ) {
      return 'cell-range-selected'
    }
    return ''
  }

  function rowClassName({ row }: { row: Record<string, any> }): string {
    const key = getRowKey(row)
    if (pendingDeleteKeys.value.has(key)) return 'row-deleted'
    if (changedRows.value[key]) return 'row-changed'
    if (isNewRow(row)) return 'row-new'
    return ''
  }

  function isNewRow(row: Record<string, any>): boolean {
    return !!row._rowUid && newRowUids.value.has(row._rowUid)
  }

  function getRowValue(row: Record<string, any>, colName: string): any {
    const key = getRowKey(row)
    const changed = changedRows.value[key]
    if (changed && changed[colName] !== undefined) {
      return changed[colName]
    }
    return row[colName]
  }

  function isEditingCell(row: Record<string, any>, colName: string): boolean {
    if (!editingCell.value) return false
    return editingCell.value.rowKey === getRowKey(row) && editingCell.value.colName === colName
  }

  function isCellChanged(row: Record<string, any>, colName: string): boolean {
    const key = getRowKey(row)
    return !!changedRows.value[key] && changedRows.value[key][colName] !== undefined
  }

  function formatCellTitle(val: any): string {
    if (val === null || val === undefined) return 'NULL'
    return String(val)
  }

  function clearRangeSelection() {
    selStart.value = { row: -1, col: -1 }
    selEnd.value = { row: -1, col: -1 }
    selAnchor.value = { row: -1, col: -1 }
    pendingStart.value = null
    pendingMoved.value = false
  }

  // ===== Inline edit input ref management =====
  function setEditInputRef(el: any) {
    if (el) {
      editInputRef = el
      if (_editInputSetupDone) return
      _editInputSetupDone = true
      el.focus?.()
      el.select?.()
      const nativeInput = el.$el?.querySelector('input')
      if (nativeInput) {
        _inputPasteHandler = _createInputPasteHandler(nativeInput)
        nativeInput.addEventListener('paste', _inputPasteHandler, true)
      }
    }
  }

  function _createInputPasteHandler(nativeInput: HTMLInputElement) {
    return function (e: ClipboardEvent) {
      const text = e.clipboardData?.getData('text/plain')
      if (!text) return

      // Multi-cell paste → switch to grid paste mode
      if (text.includes('\t') || text.split(/\r?\n/).filter((l) => l).length > 1) {
        e.preventDefault()
        e.stopPropagation()
        e.stopImmediatePropagation()
        const cell = editingCell.value
        if (!cell) return
        const startRowIdx = rows.value.findIndex((r) => getRowKey(r) === cell.rowKey)
        const startColIdx = dataColumns.value.findIndex((c) => c.name === cell.colName)
        cancelInlineEdit()
        const grid = parsePasteGrid(text)
        if (grid.length > 0) {
          applyPasteGrid(grid, startRowIdx, startColIdx)
        }
        return
      }

      // Single-cell paste → insert at cursor
      e.preventDefault()
      e.stopPropagation()
      e.stopImmediatePropagation()
      const start = nativeInput.selectionStart ?? editingValue.value.length
      const end = nativeInput.selectionEnd ?? start
      const val = editingValue.value
      const newVal = val.substring(0, start) + text + val.substring(end)
      nativeInput.value = newVal
      editingValue.value = newVal
      const newPos = start + text.length
      nativeInput.setSelectionRange(newPos, newPos)
    }
  }

  function detachInputPasteHandler() {
    if (_inputPasteHandler && editInputRef) {
      const nativeInput = editInputRef.$el?.querySelector('input')
      if (nativeInput) nativeInput.removeEventListener('paste', _inputPasteHandler, true)
      _inputPasteHandler = null
    }
  }

  // ===== Inline edit lifecycle =====
  function startInlineEdit(row: Record<string, any>, colName: string) {
    const key = getRowKey(row)
    const changed = changedRows.value[key]
    const currentVal = changed && changed[colName] !== undefined ? changed[colName] : row[colName]
    editingOriginalValue.value = currentVal
    editingValue.value = currentVal ?? ''
    _editInputSetupDone = false
    editingCell.value = { rowKey: key, colName }
  }

  function startInlineEditReplace(row: Record<string, any>, colName: string, initialChar: string) {
    const key = getRowKey(row)
    const changed = changedRows.value[key]
    const currentVal = changed && changed[colName] !== undefined ? changed[colName] : row[colName]
    editingOriginalValue.value = currentVal
    editingValue.value = initialChar
    _editInputSetupDone = false
    editingCell.value = { rowKey: key, colName }
    setTimeout(() => {
      if (editInputRef) {
        const nativeInput = editInputRef.$el?.querySelector('input')
        if (nativeInput) nativeInput.setSelectionRange(initialChar.length, initialChar.length)
      }
    }, 0)
  }

  function commitInlineEdit() {
    if (!editingCell.value) return
    detachInputPasteHandler()
    const { rowKey, colName } = editingCell.value
    const newVal = editingValue.value
    const origVal = editingOriginalValue.value

    if (
      newVal === origVal ||
      (newVal === '' && origVal === null) ||
      (newVal === null && origVal === '')
    ) {
      // No actual change
      if (changedRows.value[rowKey]) {
        delete changedRows.value[rowKey][colName]
        if (Object.keys(changedRows.value[rowKey]).length === 0) {
          delete changedRows.value[rowKey]
        }
      }
    } else {
      pushUndoSnapshot()
      if (!changedRows.value[rowKey]) {
        changedRows.value[rowKey] = {}
      }
      if (origVal === null && newVal === '') {
        // User cleared a NULL field — treat as no change
        delete changedRows.value[rowKey][colName]
        if (Object.keys(changedRows.value[rowKey]).length === 0) {
          delete changedRows.value[rowKey]
        }
      } else {
        changedRows.value[rowKey][colName] = newVal
      }
    }

    // Move focus to the committed cell for keyboard nav
    const rowIdx = rows.value.findIndex((r) => getRowKey(r) === rowKey)
    const colIdx = dataColumns.value.findIndex((c) => c.name === colName)
    if (rowIdx >= 0 && colIdx >= 0) {
      activeCellIndex.value = rowIdx
      activeColName.value = colName
      selStart.value = { row: rowIdx, col: colIdx }
      selEnd.value = { row: rowIdx, col: colIdx }
      selAnchor.value = { row: rowIdx, col: colIdx }
    }

    editingCell.value = null
    editingValue.value = ''
    editingOriginalValue.value = null
  }

  function cancelInlineEdit() {
    detachInputPasteHandler()
    editingCell.value = null
    editingValue.value = ''
  }

  // ===== Undo / Redo =====
  function pushUndoSnapshot() {
    undoStack.value.push({
      changedRows: JSON.parse(JSON.stringify(changedRows.value)),
      newRowUids: new Set(newRowUids.value),
      pendingDeleteKeys: new Set(pendingDeleteKeys.value),
      rowsSnapshot: rows.value.map((r) => ({ ...r })),
    })
    if (undoStack.value.length > MAX_UNDO) {
      undoStack.value.shift()
    }
    redoStack.value = []
  }

  function undo() {
    if (undoStack.value.length === 0) return
    const snapshot = undoStack.value.pop()
    redoStack.value.push({
      changedRows: JSON.parse(JSON.stringify(changedRows.value)),
      newRowUids: new Set(newRowUids.value),
      pendingDeleteKeys: new Set(pendingDeleteKeys.value),
      rowsSnapshot: rows.value.map((r) => ({ ...r })),
    })
    applySnapshot(snapshot)
  }

  function redo() {
    if (redoStack.value.length === 0) return
    const snapshot = redoStack.value.pop()
    undoStack.value.push({
      changedRows: JSON.parse(JSON.stringify(changedRows.value)),
      newRowUids: new Set(newRowUids.value),
      pendingDeleteKeys: new Set(pendingDeleteKeys.value),
      rowsSnapshot: rows.value.map((r) => ({ ...r })),
    })
    applySnapshot(snapshot)
  }

  function applySnapshot(snapshot: any) {
    changedRows.value = snapshot.changedRows
    newRowUids.value = snapshot.newRowUids
    pendingDeleteKeys.value = snapshot.pendingDeleteKeys || new Set()
    // Mutate in place rather than reassigning — the rows ref may be derived
    // from a parent prop (toRef), and reassignment would not propagate up.
    rows.value.splice(0, rows.value.length, ...snapshot.rowsSnapshot)
  }

  // ===== Cell value manipulation =====
  function setCellValue(rowIdx: number, colName: string, newVal: any) {
    const row = rows.value[rowIdx]
    if (!row) return
    pushUndoSnapshot()
    const rowKey = getRowKey(row)
    const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined
    const origIsNull = origVal === null || origVal === undefined
    const isChanged = !(newVal === origVal || (newVal === '' && origIsNull) || (newVal === null && origIsNull))

    if (isChanged) {
      if (!changedRows.value[rowKey]) changedRows.value[rowKey] = {}
      changedRows.value[rowKey][colName] = newVal
    } else {
      if (changedRows.value[rowKey]) {
        delete changedRows.value[rowKey][colName]
        if (Object.keys(changedRows.value[rowKey]).length === 0) {
          delete changedRows.value[rowKey]
        }
      }
    }
  }

  function clearSelectedCells() {
    const bounds = selectionBounds.value
    if (!bounds) {
      if (activeCellIndex.value >= 0 && activeColName.value) {
        setCellValue(activeCellIndex.value, activeColName.value, '')
      }
      return
    }
    pushUndoSnapshot()
    for (let r = bounds.rowMin; r <= bounds.rowMax; r++) {
      for (let c = bounds.colMin; c <= bounds.colMax; c++) {
        const row = rows.value[r]
        if (!row) continue
        const colName = dataColumns.value[c].name
        const rowKey = getRowKey(row)
        const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined
        const newVal = ''
        const origIsNull = origVal === null || origVal === undefined
        const isChanged = !(newVal === origVal || (newVal === '' && origIsNull))
        if (isChanged) {
          if (!changedRows.value[rowKey]) changedRows.value[rowKey] = {}
          changedRows.value[rowKey][colName] = newVal
        } else {
          if (changedRows.value[rowKey]) {
            delete changedRows.value[rowKey][colName]
            if (Object.keys(changedRows.value[rowKey]).length === 0) {
              delete changedRows.value[rowKey]
            }
          }
        }
      }
    }
  }

  function fillDown() {
    const bounds = selectionBounds.value
    if (!bounds || bounds.rowMin === bounds.rowMax) return
    pushUndoSnapshot()
    for (let c = bounds.colMin; c <= bounds.colMax; c++) {
      const colName = dataColumns.value[c].name
      const sourceRow = rows.value[bounds.rowMin]
      if (!sourceRow) continue
      const sourceKey = getRowKey(sourceRow)
      const sourceChanged = changedRows.value[sourceKey]
      const sourceVal = sourceChanged && sourceChanged[colName] !== undefined ? sourceChanged[colName] : sourceRow[colName]
      for (let r = bounds.rowMin + 1; r <= bounds.rowMax; r++) {
        const row = rows.value[r]
        if (!row) continue
        const rowKey = getRowKey(row)
        const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined
        const newVal = sourceVal ?? ''
        const origIsNull = origVal === null || origVal === undefined
        const newIsNull = newVal === null || newVal === undefined
        const isChanged = !(newVal === origVal || (newIsNull && origIsNull) || (newIsNull && origVal === '') || (newVal === '' && origIsNull))
        if (isChanged) {
          if (!changedRows.value[rowKey]) changedRows.value[rowKey] = {}
          changedRows.value[rowKey][colName] = newVal
        } else {
          if (changedRows.value[rowKey]) {
            delete changedRows.value[rowKey][colName]
            if (Object.keys(changedRows.value[rowKey]).length === 0) {
              delete changedRows.value[rowKey]
            }
          }
        }
      }
    }
    ElMessage({ message: '已向下填充', type: 'success' })
  }

  // ===== Paste grid =====
  function parsePasteGrid(text: string): string[][] {
    const lines = text.split(/\r?\n/)
    if (lines.length > 0 && lines[lines.length - 1] === '') {
      lines.pop()
    }
    return lines.map((line) => line.split('\t'))
  }

  function applyPasteGrid(grid: string[][], startRowIdx: number, startColIdx: number) {
    if (startRowIdx < 0 || startColIdx < 0) return
    pushUndoSnapshot()
    for (let ri = 0; ri < grid.length; ri++) {
      let targetRowIdx = startRowIdx + ri
      if (targetRowIdx >= rows.value.length) {
        const blank: Record<string, any> = { _rowUid: nextRowUid++, _autoExpanded: true }
        dataColumns.value.forEach((col) => {
          blank[col.name] = ''
        })
        rows.value.push(blank)
        newRowUids.value = new Set([...newRowUids.value, blank._rowUid])
        const key = getRowKey(blank)
        if (!originalRows.value[key]) {
          originalRows.value[key] = {}
        }
      }
      const targetRow = rows.value[targetRowIdx]
      const rowKey = getRowKey(targetRow)
      for (let ci = 0; ci < grid[ri].length; ci++) {
        const targetColIdx = startColIdx + ci
        if (targetColIdx >= dataColumns.value.length) break
        const colName = dataColumns.value[targetColIdx].name
        const newVal: any = grid[ri][ci]
        const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined
        const newIsNull = newVal === null || newVal === undefined
        const origIsNull = origVal === null || origVal === undefined
        const isChanged = !(
          newVal === origVal ||
          (newIsNull && origIsNull) ||
          (newIsNull && origVal === '') ||
          (newVal === '' && origIsNull)
        )
        if (isChanged) {
          if (!changedRows.value[rowKey]) changedRows.value[rowKey] = {}
          changedRows.value[rowKey][colName] = newVal
        } else {
          if (changedRows.value[rowKey]) {
            delete changedRows.value[rowKey][colName]
            if (Object.keys(changedRows.value[rowKey]).length === 0) {
              delete changedRows.value[rowKey]
            }
          }
        }
      }
    }
    activeCellIndex.value = -1
    activeColName.value = ''
  }

  function handlePaste(event: ClipboardEvent) {
    // If a cell is being inline-edited, the paste is handled by the input's capture handler
    if (editingCell.value) return
    event.preventDefault()
    const text = event.clipboardData?.getData('text/plain')
    if (!text) return

    let startRowIdx = -1
    let startColIdx = -1
    let targetRowCount = -1
    let targetColCount = -1

    const bounds = selectionBounds.value
    if (bounds) {
      startRowIdx = bounds.rowMin
      startColIdx = bounds.colMin
      targetRowCount = bounds.rowMax - bounds.rowMin + 1
      targetColCount = bounds.colMax - bounds.colMin + 1
    } else if (activeCellIndex.value >= 0 && activeColName.value) {
      startRowIdx = activeCellIndex.value
      startColIdx = dataColumns.value.findIndex((c) => c.name === activeColName.value)
    }

    const grid = parsePasteGrid(text)
    if (grid.length === 0) return

    // When the selection is larger than 1x1 and the paste content doesn't match,
    // tile the paste content to fill the selection.
    if (targetRowCount > 1 || targetColCount > 1) {
      const pasteRows = grid.length
      const pasteCols = Math.max(...grid.map((r) => r.length))
      if (pasteRows !== targetRowCount || pasteCols !== targetColCount) {
        const tiledGrid: string[][] = []
        for (let r = 0; r < targetRowCount; r++) {
          const row: string[] = []
          const srcRow = grid[r % pasteRows]
          for (let c = 0; c < targetColCount; c++) {
            row.push(srcRow[c % pasteCols] ?? '')
          }
          tiledGrid.push(row)
        }
        applyPasteGrid(tiledGrid, startRowIdx, startColIdx)
        return
      }
    }
    applyPasteGrid(grid, startRowIdx, startColIdx)
  }

  // ===== Mouse / keyboard interaction =====
  function onCellMouseDown(rowIdx: number, colName: string, e: MouseEvent) {
    if (editingCell.value) {
      const editingRow = editingCell.value.rowKey
      const editingCol = editingCell.value.colName
      const clickedKey = getRowKey(rows.value[rowIdx])
      if (editingRow !== clickedKey || editingCol !== colName) {
        commitInlineEdit()
      } else {
        return
      }
    }
    if (e.button !== 0) return
    const colIdx = colNameToIndex(colName)
    if (colIdx < 0) return
    activeCellIndex.value = rowIdx
    activeColName.value = colName
    if (e.altKey) {
      clearRangeSelection()
      return
    }
    if (e.shiftKey && selAnchor.value.row >= 0) {
      selEnd.value = { row: rowIdx, col: colIdx }
      return
    }
    pendingStart.value = { row: rowIdx, col: colIdx }
    pendingMoved.value = false
    selAnchor.value = { row: rowIdx, col: colIdx }
  }

  function onCellMouseMove(rowIdx: number, colName: string) {
    if (!pendingStart.value) return
    const colIdx = colNameToIndex(colName)
    if (colIdx < 0) return
    if (rowIdx === pendingStart.value.row && colIdx === pendingStart.value.col) return
    if (!pendingMoved.value) {
      pendingMoved.value = true
      selStart.value = { ...pendingStart.value }
      selEnd.value = { row: rowIdx, col: colIdx }
      const sel = window.getSelection()
      if (sel) sel.removeAllRanges()
      return
    }
    selEnd.value = { row: rowIdx, col: colIdx }
  }

  function onCellMouseEnter(rowIdx: number, colName: string) {
    if (pendingStart.value && pendingMoved.value) {
      const colIdx = colNameToIndex(colName)
      if (colIdx < 0) return
      selEnd.value = { row: rowIdx, col: colIdx }
    }
  }

  function onTableMouseUp() {
    if (pendingStart.value && !pendingMoved.value) {
      selStart.value = { ...pendingStart.value }
      selEnd.value = { ...pendingStart.value }
    }
    pendingStart.value = null
    pendingMoved.value = false
  }

  function copySelectedRange() {
    const bounds = selectionBounds.value
    if (!bounds) return
    const lines: string[] = []
    const cols = dataColumns.value.slice(bounds.colMin, bounds.colMax + 1)
    for (let r = bounds.rowMin; r <= bounds.rowMax; r++) {
      const row = rows.value[r]
      if (!row) continue
      const line = cols
        .map((c) => {
          const val = row[c.name]
          return val != null ? String(val) : '\\N'
        })
        .join('\t')
      lines.push(line)
    }
    if (lines.length > 0) {
      navigator.clipboard.writeText(lines.join('\n')).catch(() => {})
      ElMessage({ message: `已复制 ${lines.length} 行`, type: 'success' })
    }
  }

  function navigateFocus(fromRow: number, fromCol: number, dRow: number, dCol: number) {
    let newRow = fromRow + dRow
    let newCol = fromCol + dCol
    if (newCol >= dataColumns.value.length) {
      newCol = 0
      newRow++
    } else if (newCol < 0) {
      newCol = dataColumns.value.length - 1
      newRow--
    }
    newRow = Math.max(0, Math.min(rows.value.length - 1, newRow))
    newCol = Math.max(0, Math.min(dataColumns.value.length - 1, newCol))
    activeCellIndex.value = newRow
    activeColName.value = dataColumns.value[newCol].name
    selStart.value = { row: newRow, col: newCol }
    selEnd.value = { row: newRow, col: newCol }
    selAnchor.value = { row: newRow, col: newCol }
  }

  function onTableKeydown(event: KeyboardEvent) {
    // Ctrl+Z / Ctrl+Y: undo/redo
    if ((event.ctrlKey || event.metaKey) && !event.shiftKey && event.key === 'z') {
      event.preventDefault()
      undo()
      return
    }
    if (
      (event.ctrlKey || event.metaKey) &&
      (event.key === 'y' || (event.shiftKey && event.key === 'Z') || (event.shiftKey && event.key === 'z'))
    ) {
      event.preventDefault()
      redo()
      return
    }

    // If inline editing, handle Tab/Enter navigation within the editor
    if (editingCell.value) {
      if (event.key === 'Tab') {
        event.preventDefault()
        const rowIdx = rows.value.findIndex((r) => getRowKey(r) === editingCell.value?.rowKey)
        const colIdx = dataColumns.value.findIndex((c) => c.name === editingCell.value?.colName)
        commitInlineEdit()
        if (event.shiftKey) navigateFocus(rowIdx, colIdx, 0, -1)
        else navigateFocus(rowIdx, colIdx, 0, 1)
        return
      }
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault()
        const rowIdx = rows.value.findIndex((r) => getRowKey(r) === editingCell.value?.rowKey)
        const colIdx = dataColumns.value.findIndex((c) => c.name === editingCell.value?.colName)
        commitInlineEdit()
        navigateFocus(rowIdx, colIdx, 1, 0)
        return
      }
      if (event.key === 'Escape') {
        cancelInlineEdit()
        return
      }
      return
    }

    // Keyboard navigation when not editing
    const bounds = selectionBounds.value
    if (!bounds && activeCellIndex.value < 0) return
    const currentRow = bounds ? bounds.rowMin : activeCellIndex.value
    const currentCol = bounds ? bounds.colMin : colNameToIndex(activeColName.value)

    if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) {
      event.preventDefault()
      let dRow = 0
      let dCol = 0
      if (event.key === 'ArrowUp') dRow = -1
      else if (event.key === 'ArrowDown') dRow = 1
      else if (event.key === 'ArrowLeft') dCol = -1
      else if (event.key === 'ArrowRight') dCol = 1

      if (event.shiftKey) {
        const endRow = (selEnd.value.row >= 0 ? selEnd.value.row : currentRow) + dRow
        const endCol = (selEnd.value.col >= 0 ? selEnd.value.col : currentCol) + dCol
        const clampedRow = Math.max(0, Math.min(rows.value.length - 1, endRow))
        const clampedCol = Math.max(0, Math.min(dataColumns.value.length - 1, endCol))
        if (selStart.value.row < 0) {
          selStart.value = { row: currentRow, col: currentCol }
          selAnchor.value = { row: currentRow, col: currentCol }
        }
        selEnd.value = { row: clampedRow, col: clampedCol }
      } else {
        navigateFocus(currentRow, currentCol, dRow, dCol)
      }
      return
    }

    if (event.key === 'Tab') {
      event.preventDefault()
      if (event.shiftKey) navigateFocus(currentRow, currentCol, 0, -1)
      else navigateFocus(currentRow, currentCol, 0, 1)
      return
    }

    if (event.key === 'Enter') {
      event.preventDefault()
      if (event.shiftKey) navigateFocus(currentRow, currentCol, -1, 0)
      else navigateFocus(currentRow, currentCol, 1, 0)
      return
    }

    if (event.key === 'F2') {
      event.preventDefault()
      const row = rows.value[currentRow]
      if (row && currentCol >= 0) {
        startInlineEdit(row, dataColumns.value[currentCol].name)
      }
      return
    }

    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault()
      clearSelectedCells()
      return
    }

    if ((event.ctrlKey || event.metaKey) && event.key === 'd') {
      event.preventDefault()
      fillDown()
      return
    }

    if ((event.ctrlKey || event.metaKey) && event.key === 'c') {
      const sel = window.getSelection()
      if (sel && sel.toString().length > 0) return
      event.preventDefault()
      copySelectedRange()
      return
    }

    // Direct typing → enter edit mode with the typed character (replace mode)
    if (!event.ctrlKey && !event.metaKey && !event.altKey && event.key.length === 1) {
      const row = rows.value[currentRow]
      if (row && currentCol >= 0) {
        const colName = dataColumns.value[currentCol].name
        startInlineEditReplace(row, colName, event.key)
        event.preventDefault()
      }
      return
    }
  }

  // ===== Context menu =====
  function onTableContextMenu(event: MouseEvent) {
    contextMenuPos.value = { x: event.clientX, y: event.clientY }
    contextMenuVisible.value = true
    setTimeout(() => {
      document.addEventListener('click', closeContextMenu)
      document.addEventListener('contextmenu', closeContextMenu)
    }, 0)
  }

  function onCellContextMenu(rowIdx: number, colName: string, event: MouseEvent) {
    const colIdx = colNameToIndex(colName)
    const bounds = selectionBounds.value
    const inSelection =
      bounds &&
      rowIdx >= bounds.rowMin &&
      rowIdx <= bounds.rowMax &&
      colIdx >= bounds.colMin &&
      colIdx <= bounds.colMax
    if (!inSelection) {
      activeCellIndex.value = rowIdx
      activeColName.value = colName
      selStart.value = { row: rowIdx, col: colIdx }
      selEnd.value = { row: rowIdx, col: colIdx }
      selAnchor.value = { row: rowIdx, col: colIdx }
    }
    contextMenuPos.value = { x: event.clientX, y: event.clientY }
    contextMenuVisible.value = true
    setTimeout(() => {
      document.addEventListener('click', closeContextMenu)
      document.addEventListener('contextmenu', closeContextMenu)
    }, 0)
  }

  function closeContextMenu() {
    contextMenuVisible.value = false
    document.removeEventListener('click', closeContextMenu)
    document.removeEventListener('contextmenu', closeContextMenu)
  }

  // ===== Row operations =====
  function insertBlankRowAt(idx: number) {
    const blank: Record<string, any> = { _rowUid: nextRowUid++ }
    dataColumns.value.forEach((col) => {
      blank[col.name] = ''
    })
    rows.value.splice(idx, 0, blank)
    const key = getRowKey(blank)
    newRowUids.value = new Set([...newRowUids.value, blank._rowUid])
    originalRows.value[key] = {}
  }

  function removeNewRow(row: Record<string, any>) {
    if (!row._rowUid) return
    const uid = row._rowUid
    const idx = rows.value.findIndex((r) => r._rowUid === uid)
    if (idx >= 0) rows.value.splice(idx, 1)
    const nextSet = new Set(newRowUids.value)
    nextSet.delete(uid)
    newRowUids.value = nextSet
    const key = getRowKey(row)
    delete changedRows.value[key]
    delete originalRows.value[key]
  }

  function copyRow(row: Record<string, any>) {
    const copied: Record<string, any> = { _rowUid: nextRowUid++ }
    dataColumns.value.forEach((col) => {
      if (pkColumns.value.includes(col.name)) {
        copied[col.name] = ''
      } else {
        copied[col.name] = row[col.name] !== undefined ? row[col.name] : ''
      }
    })
    rows.value.push(copied)
    const key = getRowKey(copied)
    newRowUids.value = new Set([...newRowUids.value, copied._rowUid])
    originalRows.value[key] = {}
    const changed: Record<string, any> = {}
    dataColumns.value.forEach((col) => {
      const val = copied[col.name]
      if (val !== '' && val !== null && val !== undefined && !pkColumns.value.includes(col.name)) {
        changed[col.name] = val
      }
    })
    if (Object.keys(changed).length > 0) {
      changedRows.value[key] = changed
    }
  }

  // ===== Context menu action handlers =====
  function ctxCopy() {
    closeContextMenu()
    copySelectedRange()
  }

  function ctxEditDetail() {
    closeContextMenu()
    const bounds = selectionBounds.value
    const rowIdx = bounds ? bounds.rowMin : activeCellIndex.value
    const row = rows.value[rowIdx]
    if (row && !isNewRow(row)) {
      params.onEditRow?.(row)
    }
  }

  async function ctxPaste() {
    closeContextMenu()
    try {
      const text = await navigator.clipboard.readText()
      if (!text) return
      const bounds = selectionBounds.value
      let startRowIdx = -1
      let startColIdx = -1
      if (bounds) {
        startRowIdx = bounds.rowMin
        startColIdx = bounds.colMin
      } else if (activeCellIndex.value >= 0 && activeColName.value) {
        startRowIdx = activeCellIndex.value
        startColIdx = dataColumns.value.findIndex((c) => c.name === activeColName.value)
      }
      if (startRowIdx < 0 || startColIdx < 0) return
      const grid = parsePasteGrid(text)
      if (grid.length > 0) {
        applyPasteGrid(grid, startRowIdx, startColIdx)
      }
    } catch {
      /* ignore */
    }
  }

  function ctxClearCells() {
    closeContextMenu()
    clearSelectedCells()
  }

  function ctxSetNull() {
    closeContextMenu()
    const bounds = selectionBounds.value
    if (!bounds) {
      if (activeCellIndex.value >= 0 && activeColName.value) {
        setCellValue(activeCellIndex.value, activeColName.value, null)
      }
      return
    }
    pushUndoSnapshot()
    for (let r = bounds.rowMin; r <= bounds.rowMax; r++) {
      for (let c = bounds.colMin; c <= bounds.colMax; c++) {
        const row = rows.value[r]
        if (!row) continue
        const colName = dataColumns.value[c].name
        const rowKey = getRowKey(row)
        const origVal = originalRows.value[rowKey] ? originalRows.value[rowKey][colName] : undefined
        const origIsNull = origVal === null || origVal === undefined
        if (!origIsNull) {
          if (!changedRows.value[rowKey]) changedRows.value[rowKey] = {}
          changedRows.value[rowKey][colName] = null
        } else {
          if (changedRows.value[rowKey]) {
            delete changedRows.value[rowKey][colName]
            if (Object.keys(changedRows.value[rowKey]).length === 0) {
              delete changedRows.value[rowKey]
            }
          }
        }
      }
    }
  }

  function ctxInsertRowAbove() {
    closeContextMenu()
    const bounds = selectionBounds.value
    const idx = bounds ? bounds.rowMin : activeCellIndex.value >= 0 ? activeCellIndex.value : rows.value.length
    insertBlankRowAt(idx)
  }

  function ctxInsertRowBelow() {
    closeContextMenu()
    const bounds = selectionBounds.value
    const idx = bounds ? bounds.rowMax + 1 : activeCellIndex.value >= 0 ? activeCellIndex.value + 1 : rows.value.length
    insertBlankRowAt(idx)
  }

  function ctxDeleteRows() {
    closeContextMenu()
    const bounds = selectionBounds.value
    if (!bounds) {
      if (activeCellIndex.value >= 0) {
        const row = rows.value[activeCellIndex.value]
        if (row && isNewRow(row)) {
          removeNewRow(row)
        } else if (row) {
          pushUndoSnapshot()
          pendingDeleteKeys.value = new Set([...pendingDeleteKeys.value, getRowKey(row)])
        }
      }
      return
    }
    pushUndoSnapshot()
    for (let r = bounds.rowMax; r >= bounds.rowMin; r--) {
      const row = rows.value[r]
      if (!row) continue
      if (isNewRow(row)) {
        removeNewRow(row)
      } else {
        pendingDeleteKeys.value = new Set([...pendingDeleteKeys.value, getRowKey(row)])
      }
    }
  }

  function ctxFillDown() {
    closeContextMenu()
    fillDown()
  }

  function ctxCopyRow() {
    closeContextMenu()
    const bounds = selectionBounds.value
    const rowIdx = bounds ? bounds.rowMin : activeCellIndex.value
    const row = rows.value[rowIdx]
    if (row) copyRow(row)
  }

  function ctxUndo() {
    closeContextMenu()
    undo()
  }

  function ctxRedo() {
    closeContextMenu()
    redo()
  }

  // ===== Save / Discard =====
  async function saveInlineChanges(): Promise<void> {
    const rowKeys = Object.keys(changedRows.value)
    const newKeys = rowKeys.filter((k) => k.startsWith('_new_'))
    const existingKeys = rowKeys.filter((k) => !k.startsWith('_new_'))
    if (rowKeys.length === 0 && newRowUids.value.size === 0 && pendingDeleteKeys.value.size === 0) return

    savingInline.value = true
    try {
      const sqlStatements: string[] = []

      for (const rowKey of newKeys) {
        const changed = changedRows.value[rowKey]
        const row = rows.value.find((r) => getRowKey(r) === rowKey)
        if (!row) continue
        const merged: Record<string, any> = {}
        if (changed) {
          Object.keys(changed).forEach((k) => {
            merged[k] = changed[k]
          })
        }
        dataColumns.value.forEach((col) => {
          if (merged[col.name] === undefined && row[col.name] !== '' && row[col.name] !== null && row[col.name] !== undefined) {
            merged[col.name] = row[col.name]
          }
        })
        const insertCols = dataColumns.value.filter((col) => {
          const val = merged[col.name]
          return val !== null && val !== undefined && val !== ''
        })
        if (insertCols.length === 0) continue
        const colList = insertCols.map((c) => quoteId(c.name, params.effectiveDbType())).join(', ')
        const valList = insertCols.map((c) => fmtVal(merged[c.name], params.effectiveDbType())).join(', ')
        sqlStatements.push(
          'INSERT INTO ' + quoteId(params.tableName() || '', params.effectiveDbType()) + ' (' + colList + ') VALUES (' + valList + ')',
        )
      }

      for (const rowKey of existingKeys) {
        const changed = changedRows.value[rowKey]
        const orig = originalRows.value[rowKey]
        if (!orig) continue
        const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(orig).slice(0, 1)
        const setClauses = Object.keys(changed)
          .map((k) => quoteId(k, params.effectiveDbType()) + ' = ' + fmtVal(changed[k], params.effectiveDbType()))
          .join(', ')
        const allWhereCols = [...pkCols, ...Object.keys(changed).filter((k) => !pkCols.includes(k))]
        const whereClauses = allWhereCols
          .map((k) => buildWhereCondition(k, orig[k], params.effectiveDbType()))
          .join(' AND ')
        sqlStatements.push(
          'UPDATE ' + quoteId(params.tableName() || '', params.effectiveDbType()) + ' SET ' + setClauses + ' WHERE ' + whereClauses,
        )
      }

      for (const rowKey of pendingDeleteKeys.value) {
        const orig = originalRows.value[rowKey]
        if (!orig) continue
        const pkCols = pkColumns.value.length > 0 ? pkColumns.value : Object.keys(orig).slice(0, 1)
        const whereClauses = pkCols
          .map((k) => buildWhereCondition(k, orig[k], params.effectiveDbType()))
          .join(' AND ')
        sqlStatements.push(
          'DELETE FROM ' + quoteId(params.tableName() || '', params.effectiveDbType()) + ' WHERE ' + whereClauses,
        )
      }

      if (sqlStatements.length === 0) {
        ElMessage({ message: '没有需要保存的更改', type: 'warning' })
        return
      }

      const batchSize = 100
      let totalSuccess = 0
      let totalFailed = 0
      let lastError = ''

      for (let i = 0; i < sqlStatements.length; i += batchSize) {
        const batch = sqlStatements.slice(i, i + batchSize)
        const batchSql = batch.join('; ')
        const urlParams = new URLSearchParams()
        urlParams.append('connId', params.connId() || '')
        urlParams.append('schema', params.schema() || '')
        urlParams.append('sql', batchSql)
        try {
          const resp = await http.post('/execSQL', urlParams)
          const respData = resp.data.data
          if (respData && respData.msg) {
            totalFailed += batch.length
            lastError = respData.msg
          } else {
            totalSuccess += batch.length
          }
        } catch (err: any) {
          totalFailed += batch.length
          lastError = err?.message || '请求失败'
        }
      }

      if (totalFailed === 0) {
        ElMessage({ message: `成功保存 ${totalSuccess} 条记录`, type: 'success' })
      } else if (totalSuccess > 0) {
        ElMessage({
          message: `部分保存: ${totalSuccess} 成功, ${totalFailed} 失败 (${lastError})`,
          type: 'warning',
          duration: 5000,
        })
      } else {
        ElMessage({ message: `保存失败: ${lastError}`, type: 'error', duration: 5000 })
      }
      await loadData()
    } catch (err) {
      console.error('[DataBrowser] 保存失败:', err)
      ElMessage({ message: '保存失败', type: 'error' })
    } finally {
      savingInline.value = false
    }
  }

  function discardInlineChanges() {
    const uidSet = newRowUids.value
    const filtered = rows.value.filter((r) => !r._rowUid || !uidSet.has(r._rowUid))
    // Mutate in place — see applySnapshot comment.
    rows.value.splice(0, rows.value.length, ...filtered)
    newRowUids.value = new Set()
    pendingDeleteKeys.value = new Set()
    changedRows.value = {}
    rows.value.forEach((row) => {
      if (!row._rowUid) {
        const key = getRowKey(row)
        if (originalRows.value[key]) {
          Object.assign(row, originalRows.value[key])
        }
      }
    })
    ElMessage({ message: '已放弃更改', type: 'info' })
  }

  // ===== Reset state when parent reloads rows (reassignment, not mutation) =====
  watch(
    () => rows.value,
    () => {
      // Only reset when the array reference changes (i.e. loadData reassigns rows.value).
      // Inline-edit mutations (push/splice) operate on the same array reference
      // and therefore do NOT trigger this watcher.
      changedRows.value = {}
      newRowUids.value = new Set()
      pendingDeleteKeys.value = new Set()
      undoStack.value = []
      redoStack.value = []
      editingCell.value = null
      originalRows.value = {}
      rows.value.forEach((row) => {
        originalRows.value[getRowKey(row)] = { ...row }
      })
    },
  )

  return {
    // inline edit state
    editingCell,
    editingValue,
    editingOriginalValue,
    changedRows,
    originalRows,
    savingInline,
    newRowUids,
    pendingDeleteKeys,
    inlineChangeCount,
    hasInlineChanges,
    // selection state
    activeCellIndex,
    activeColName,
    selStart,
    selEnd,
    selAnchor,
    selectionBounds,
    canFillDown,
    // context menu state
    contextMenuVisible,
    contextMenuPos,
    // undo/redo state
    undoStack,
    redoStack,
    // helpers
    colNameToIndex,
    isCellInSelection,
    isCellFocused,
    cellClassFn,
    rowClassName,
    isNewRow,
    getRowValue,
    isEditingCell,
    isCellChanged,
    formatCellTitle,
    // inline edit methods
    setEditInputRef,
    startInlineEdit,
    commitInlineEdit,
    cancelInlineEdit,
    // cell manipulation
    setCellValue,
    clearSelectedCells,
    fillDown,
    // paste
    parsePasteGrid,
    applyPasteGrid,
    handlePaste,
    // mouse/keyboard
    onCellMouseDown,
    onCellMouseMove,
    onCellMouseEnter,
    onTableMouseUp,
    copySelectedRange,
    navigateFocus,
    onTableKeydown,
    // context menu
    onTableContextMenu,
    onCellContextMenu,
    closeContextMenu,
    ctxCopy,
    ctxEditDetail,
    ctxPaste,
    ctxClearCells,
    ctxSetNull,
    ctxInsertRowAbove,
    ctxInsertRowBelow,
    ctxDeleteRows,
    ctxFillDown,
    ctxCopyRow,
    ctxUndo,
    ctxRedo,
    // row operations
    insertBlankRowAt,
    removeNewRow,
    copyRow,
    // save/discard
    saveInlineChanges,
    discardInlineChanges,
  }
}
