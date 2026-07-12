<template>
    <div style="height: 100%; display: flex; flex-direction: column;">
        <SqlBatchResultTabs v-if="isBatchMode && batchDisplayTabs.length > 0" :tabs="batchDisplayTabs"
            :model-value="activeResultTab"
            @update:model-value="emit('update:activeResultTab', $event)"
            @change="emit('batchTabChange', $event)" />
        <SqlErrorPanel v-if="sqlError" :error="sqlError" />
        <div v-else style="flex: 1; overflow: hidden;">
            <el-auto-resizer>
                <template #default="{ height: autoHeight, width: autoWidth }">
                    <div ref="resultScrollRef"
                        :style="{ height: autoHeight + 'px', overflowX: 'auto', overflowY: 'hidden' }"
                        @paste="handlePaste2" @keydown="onTableKeydown2" @scroll="onResultScroll"
                        @mouseup="onTableMouseUp2" @mouseleave="onTableMouseUp2">
                        <el-table-v2 :columns="displayColumns" :data="result"
                            :width="Math.max(totalColumnWidth, autoWidth)" :height="autoHeight"
                            scrollbar-always-on />
                    </div>
                </template>
            </el-auto-resizer>
        </div>
        <SqlInlineEditor v-if="canInlineEdit && inlineChangeCount > 0" :change-count="inlineChangeCount"
            :saving="savingInline" @save="saveInlineChanges" @discard="discardInlineChanges" />
    </div>
</template>

<script lang="ts" setup>
import { ref, computed, watch, h, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { execSQL } from '@/api/sql'
import { buildWhereCondition, fmtVal, quoteId } from '@/utils/sqlHelper'
import SqlBatchResultTabs from './SqlBatchResultTabs.vue'
import SqlErrorPanel from './SqlErrorPanel.vue'
import SqlInlineEditor from './SqlInlineEditor.vue'

const props = defineProps<{
    result: any[]
    rawColumns: any[]
    sqlError: string
    isBatchMode: boolean
    batchDisplayTabs: any[]
    activeResultTab: string
    canInlineEdit: boolean
    canModify: boolean
    canEdit: boolean
    tableKeys: string[]
    connId: string
    schema: string
    maxLine: string
    currentSelectTable: string
    effectiveDbType: string
}>()

const emit = defineEmits<{
    'update:activeResultTab': [value: string]
    batchTabChange: [name: string | number]
    openDataDetails: [rowIndex: number]
    execRefresh: [silent: boolean]
}>()

// ── 显示列（含 cellRenderer），从 rawColumns 构建 ──
const displayColumns = ref<any[]>([])

const totalColumnWidth = computed(() => {
    if (!displayColumns.value || displayColumns.value.length === 0) return 800
    return displayColumns.value.reduce((sum: number, col: any) => sum + (col.width || 150), 0)
})

// ── 内联编辑状态 ──
const editingCellRow = ref(-1)
const editingCellCol = ref('')
const editingCellValue = ref('')
const editingCellOriginalValue = ref<any>(null)
const inlineChanges = ref(new Map<string, any>())
const savingInline = ref(false)

const inlineChangeCount = computed(() => inlineChanges.value.size)

// ── 范围选择状态 ──
const activeCellRow2 = ref(-1)
const activeCellCol2 = ref('')
const pasteSnapshot2 = ref<any>(null)

const selStart2 = ref({ row: -1, col: -1 })
const selEnd2 = ref({ row: -1, col: -1 })
const selAnchor2 = ref({ row: -1, col: -1 })
const pendingStart2 = ref<{ row: number; col: number } | null>(null)
const pendingMoved2 = ref(false)

const selectionBounds2 = computed(() => {
    if (selStart2.value.row < 0 || selEnd2.value.row < 0) return null
    return {
        rowMin: Math.min(selStart2.value.row, selEnd2.value.row),
        rowMax: Math.max(selStart2.value.row, selEnd2.value.row),
        colMin: Math.min(selStart2.value.col, selEnd2.value.col),
        colMax: Math.max(selStart2.value.col, selEnd2.value.col),
    }
})

// ── 滚动条偏移 ──
const resultScrollRef = ref<HTMLElement | null>(null)
let cachedVScrollbar: HTMLElement | null = null
let rafId: number | null = null

function getVScrollbar(): HTMLElement | null {
    if (cachedVScrollbar && cachedVScrollbar.isConnected) return cachedVScrollbar
    const container = resultScrollRef.value
    if (!container) return null
    cachedVScrollbar = container.querySelector('.el-vl__vertical') as HTMLElement
    return cachedVScrollbar
}

function applyVScrollbarOffset() {
    const container = resultScrollRef.value
    if (!container) return
    const scrollLeft = container.scrollLeft
    const containerWidth = container.clientWidth
    const tableWidth = Math.max(totalColumnWidth.value, containerWidth)
    const vScrollbar = getVScrollbar()
    if (vScrollbar) {
        vScrollbar.style.transform = `translateX(${containerWidth - tableWidth + scrollLeft}px)`
    }
    rafId = null
}

function onResultScroll() {
    if (rafId !== null) return
    rafId = requestAnimationFrame(applyVScrollbarOffset)
}

// ── 列辅助方法 ──

function dataColKeys(): string[] {
    return displayColumns.value.slice(1).map((c: any) => c.dataKey)
}

function dataColIndex(colKey: string): number {
    return dataColKeys().indexOf(colKey)
}

function isCellInSelection2(rowIndex: number, colKey: string): boolean {
    const bounds = selectionBounds2.value
    if (!bounds) return false
    const colIdx = dataColIndex(colKey)
    return (
        rowIndex >= bounds.rowMin &&
        rowIndex <= bounds.rowMax &&
        colIdx >= bounds.colMin &&
        colIdx <= bounds.colMax
    )
}

// ── 范围选择事件 ──

function onCellMouseDown2(rowIndex: number, colKey: string, e: MouseEvent) {
    if (editingCellRow.value >= 0) return
    if (e.button !== 0) return
    const colIdx = dataColIndex(colKey)
    if (colIdx < 0) return

    if (e.altKey) {
        clearRangeSelection2()
        return
    }

    if (e.shiftKey && selAnchor2.value.row >= 0) {
        selEnd2.value = { row: rowIndex, col: colIdx }
        return
    }

    pendingStart2.value = { row: rowIndex, col: colIdx }
    pendingMoved2.value = false
    selAnchor2.value = { row: rowIndex, col: colIdx }
    activeCellRow2.value = rowIndex
    activeCellCol2.value = colKey
}

function onCellMouseMove2(rowIndex: number, colKey: string) {
    if (!pendingStart2.value) return
    const colIdx = dataColIndex(colKey)
    if (colIdx < 0) return
    if (rowIndex === pendingStart2.value.row && colIdx === pendingStart2.value.col) return
    if (!pendingMoved2.value) {
        pendingMoved2.value = true
        selStart2.value = { ...pendingStart2.value }
        selEnd2.value = { row: rowIndex, col: colIdx }
        const sel = window.getSelection()
        if (sel) sel.removeAllRanges()
        return
    }
    selEnd2.value = { row: rowIndex, col: colIdx }
}

function onCellMouseEnter2(rowIndex: number, colKey: string) {
    if (pendingStart2.value && pendingMoved2.value) {
        const colIdx = dataColIndex(colKey)
        if (colIdx < 0) return
        selEnd2.value = { row: rowIndex, col: colIdx }
    }
}

function onTableMouseUp2() {
    if (pendingStart2.value && !pendingMoved2.value) {
        selStart2.value = { ...pendingStart2.value }
        selEnd2.value = { ...pendingStart2.value }
    }
    pendingStart2.value = null
    pendingMoved2.value = false
}

function clearRangeSelection2() {
    selStart2.value = { row: -1, col: -1 }
    selEnd2.value = { row: -1, col: -1 }
    selAnchor2.value = { row: -1, col: -1 }
    pendingStart2.value = null
    pendingMoved2.value = false
}

function copySelectedRange2() {
    const bounds = selectionBounds2.value
    if (!bounds) return

    const colKeys = dataColKeys()
    const lines: string[] = []
    const cols = colKeys.slice(bounds.colMin, bounds.colMax + 1)
    for (let r = bounds.rowMin; r <= bounds.rowMax; r++) {
        const row = props.result[r]
        if (!row) continue
        const line = cols
            .map((k) => {
                const val = row[k]
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

// ── 内联编辑 ──

function isDateType(colType: string | undefined): boolean {
    if (!colType) return false
    const upper = colType.toUpperCase()
    return (
        upper === 'DATETIME' ||
        upper === 'DATE' ||
        upper === 'TIMESTAMP' ||
        upper === 'TIMESTAMP(6)' ||
        upper.includes('TIMESTAMP') ||
        upper === 'TIMESTAMPTZ' ||
        upper === 'TIMESTAMPLTZ'
    )
}

function isEditingCell(rowIndex: number, colKey: string) {
    return editingCellRow.value === rowIndex && editingCellCol.value === colKey
}

function isCellChanged(rowIndex: number, colKey: string) {
    const changeKey = rowIndex + '::' + colKey
    return inlineChanges.value.has(changeKey)
}

function startInlineEdit(rowIndex: number, colKey: string, event: MouseEvent) {
    if (!props.canInlineEdit) return
    if (props.tableKeys.length === 0) return
    const target = event.target as HTMLElement
    if (target.tagName === 'INPUT') return
    clearRangeSelection2()
    editingCellRow.value = rowIndex
    editingCellCol.value = colKey
    const rawVal = props.result[rowIndex]?.[colKey]
    editingCellOriginalValue.value = rawVal
    editingCellValue.value = rawVal != null ? String(rawVal) : ''
}

function commitInlineEdit() {
    if (editingCellRow.value < 0 || !editingCellCol.value) {
        cancelInlineEdit()
        return
    }
    const rowIdx = editingCellRow.value
    const colKey = editingCellCol.value
    const newVal = editingCellValue.value
    const origVal = editingCellOriginalValue.value
    if (origVal === null && newVal === '') {
        cancelInlineEdit()
        return
    }
    if (String(origVal ?? '') !== newVal) {
        const changeKey = rowIdx + '::' + colKey
        inlineChanges.value.set(changeKey, newVal)
        props.result[rowIdx][colKey] = newVal
    }
    cancelInlineEdit()
}

function cancelInlineEdit() {
    editingCellRow.value = -1
    editingCellCol.value = ''
    editingCellValue.value = ''
}

function saveInlineChanges() {
    if (inlineChanges.value.size === 0) return

    savingInline.value = true

    const groupedByRow = new Map<number, Map<string, string>>()
    inlineChanges.value.forEach((newVal, key) => {
        const [rowStr, colKey] = key.split('::')
        const rowIdx = parseInt(rowStr)
        if (!groupedByRow.has(rowIdx)) {
            groupedByRow.set(rowIdx, new Map())
        }
        groupedByRow.get(rowIdx)!.set(colKey, newVal)
    })

    const promises: Promise<any>[] = []
    groupedByRow.forEach((colMap, rowIdx) => {
        const row = props.result[rowIdx]
        if (!row) return

        const pkConditions = props.tableKeys
            .filter((k) => k in row)
            .map((k) => buildWhereCondition(k, row[k], props.effectiveDbType))

        const setClauses: string[] = []
        colMap.forEach((newVal, colKey) => {
            setClauses.push(
                quoteId(colKey, props.effectiveDbType) + ' = ' + fmtVal(newVal, props.effectiveDbType)
            )
        })

        if (setClauses.length === 0) return

        let sql: string
        if (pkConditions.length > 0 && props.canEdit) {
            sql =
                'update ' +
                props.currentSelectTable +
                ' set ' +
                setClauses.join(', ') +
                ' where ' +
                pkConditions.join(' and ')
        } else {
            const allWhereCols = Object.keys(row).filter(
                (k: string) => k !== 'col-idx' && colMap.has(k)
            )
            const whereConditions = allWhereCols.map((k: string) =>
                buildWhereCondition(k, row[k], props.effectiveDbType)
            )
            sql =
                'update ' +
                props.currentSelectTable +
                ' set ' +
                setClauses.join(', ') +
                ' where ' +
                whereConditions.join(' and ')
        }

        promises.push(
            execSQL({
                connId: props.connId,
                schema: props.schema,
                sql,
                maxLine: props.maxLine,
                tableName: props.currentSelectTable,
            })
        )
    })

    Promise.all(promises)
        .then(() => {
            ElMessage.success('已保存成功')
            inlineChanges.value = new Map()
            emit('execRefresh', true)
        })
        .catch((err) => {
            console.error(err)
            ElMessage.error('保存失败')
        })
        .finally(() => {
            savingInline.value = false
        })
}

function discardInlineChanges() {
    inlineChanges.value = new Map()
    emit('execRefresh', true)
}

// ── 粘贴与键盘 ──

function handlePaste2(event: ClipboardEvent) {
    const text = event.clipboardData?.getData('text/plain')
    if (!text) return

    let startRowIdx = -1
    let startColIdx = -1
    let targetRowCount = -1
    let targetColCount = -1

    const colKeys = displayColumns.value.map((c: any) => c.dataKey)

    const bounds = selectionBounds2.value
    if (bounds) {
        startRowIdx = bounds.rowMin
        startColIdx = bounds.colMin + 1
        targetRowCount = bounds.rowMax - bounds.rowMin + 1
        targetColCount = bounds.colMax - bounds.colMin + 1
    } else if (editingCellRow.value >= 0 && editingCellCol.value) {
        startRowIdx = editingCellRow.value
        startColIdx = colKeys.indexOf(editingCellCol.value)
    } else if (activeCellRow2.value >= 0 && activeCellCol2.value) {
        startRowIdx = activeCellRow2.value
        startColIdx = colKeys.indexOf(activeCellCol2.value)
    }

    if (startRowIdx < 0 || startColIdx < 0) return

    const lines = text.split('\n')
    let grid: string[][] = []
    for (const line of lines) {
        const trimmed = line.trim()
        if (trimmed) {
            grid.push(trimmed.split('\t'))
        }
    }
    if (grid.length === 0) return

    event.preventDefault()

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
            grid = tiledGrid
        }
    }

    const snapshot: any = {
        inlineChanges: new Map(inlineChanges.value),
        restoredCells: [] as { rowIdx: number; colKey: string; oldVal: any }[],
    }

    cancelInlineEdit()

    for (let ri = 0; ri < grid.length; ri++) {
        const targetRowIdx = startRowIdx + ri
        if (targetRowIdx >= props.result.length) break
        const targetRow = props.result[targetRowIdx]

        for (let ci = 0; ci < grid[ri].length; ci++) {
            const targetColIdx = startColIdx + ci
            if (targetColIdx >= colKeys.length) break
            const colKey = colKeys[targetColIdx]
            if (colKey === 'col-idx' || props.tableKeys.includes(colKey)) continue

            const newVal = grid[ri][ci].trim()

            const changeKey = targetRowIdx + '::' + colKey
            const oldChanged = inlineChanges.value.get(changeKey)
            const oldVal = oldChanged !== undefined ? oldChanged : targetRow[colKey]
            snapshot.restoredCells.push({ rowIdx: targetRowIdx, colKey, oldVal })

            if (
                String(targetRow[colKey] ?? '') !== newVal &&
                !(targetRow[colKey] === null && newVal === '') &&
                !(targetRow[colKey] === '' && newVal === '\\N')
            ) {
                inlineChanges.value.set(changeKey, newVal)
                targetRow[colKey] = newVal
            }
        }
    }

    pasteSnapshot2.value = snapshot
    activeCellRow2.value = -1
    activeCellCol2.value = ''
}

function onTableKeydown2(event: KeyboardEvent) {
    if ((event.ctrlKey || event.metaKey) && event.key === 'z') {
        if (pasteSnapshot2.value) {
            event.preventDefault()
            undoPaste2()
        }
    }
    if (editingCellRow.value >= 0) return
    if ((event.ctrlKey || event.metaKey) && event.key === 'c') {
        const sel = window.getSelection()
        if (sel && sel.toString().length > 0) return
        if (selectionBounds2.value) {
            event.preventDefault()
            copySelectedRange2()
        }
    }
}

function undoPaste2() {
    const snapshot = pasteSnapshot2.value
    if (!snapshot) return

    inlineChanges.value.clear()
    snapshot.inlineChanges.forEach((v: any, k: string) => inlineChanges.value.set(k, v))

    for (const cell of snapshot.restoredCells) {
        const { rowIdx, colKey, oldVal } = cell
        if (rowIdx < props.result.length) {
            props.result[rowIdx][colKey] = oldVal
        }
    }

    pasteSnapshot2.value = null
}

// ── 列宽拖拽 ──
let x1: number
let currentDraggingColumn: string | null = null
let originalWidth: number = 0
let dragLineElement: HTMLElement | null = null
let columnItemRef: any = null
let lastDragTime = 0

const dragStart = (e: DragEvent) => {
    x1 = e.clientX
    lastDragTime = 0
    dragLineElement = e.target as HTMLElement

    const headerBox = (e.target as HTMLElement).parentElement as HTMLElement
    if (headerBox && headerBox.classList.contains('header-box')) {
        const headerText = headerBox.querySelector('.header-text')
        if (headerText) {
            const colName = headerText.textContent?.trim()
            const columnItem = displayColumns.value.find((item: any) => item.dataKey === colName)
            if (columnItem) {
                currentDraggingColumn = columnItem.dataKey
                originalWidth = columnItem.width || 150
                columnItemRef = columnItem
                dragLineElement.style.opacity = '1'
                dragLineElement.style.backgroundColor = 'rgba(64, 158, 255, 0.3)'
            }
        }
    }

    e.dataTransfer!.effectAllowed = 'move'
    e.dataTransfer!.setData('text/plain', '')

    document.addEventListener('dragover', handleGlobalDragOver, { passive: false })
    document.addEventListener('dragend', handleGlobalDragEnd)
}

const handleGlobalDragOver = (e: DragEvent) => {
    if (!currentDraggingColumn || !columnItemRef) return
    e.preventDefault()

    const now = Date.now()
    if (now - lastDragTime < 8) return
    lastDragTime = now

    const deltaX = e.clientX - x1
    const newWidth = Math.max(50, originalWidth + deltaX)
    columnItemRef.width = newWidth
}

const handleGlobalDragEnd = () => {
    if (dragLineElement) {
        dragLineElement.style.opacity = '0'
        dragLineElement.style.backgroundColor = 'transparent'
        dragLineElement = null
    }
    currentDraggingColumn = null
    columnItemRef = null
    document.removeEventListener('dragover', handleGlobalDragOver)
    document.removeEventListener('dragend', handleGlobalDragEnd)
}

// ── 列构建 ──

function buildColumnDef(col: any): any {
    return {
        key: col.name,
        title: col.name,
        dataKey: col.name,
        comment: col.comment,
        dataType: col.type,
        width: col.width || 150,
        minWidth: 150,
        headerCellRenderer: ({ column }: { column: any }) => {
            return h(
                'div',
                {
                    class: 'header-box',
                    onDragenter: (e: any) => e.preventDefault(),
                    onMouseenter: (e: any) => {
                        const dragLine = e.target.querySelector('.drag-line')
                        if (dragLine) dragLine.style.opacity = '1'
                    },
                    onMouseleave: (e: any) => {
                        const dragLine = e.target.querySelector('.drag-line')
                        if (dragLine) dragLine.style.opacity = '0'
                    },
                },
                [
                    h(
                        'div',
                        {
                            class: 'header-text',
                            title: col.comment,
                        },
                        col.name
                    ),
                    h('div', {
                        class: 'drag-line',
                        draggable: true,
                        style: {
                            position: 'absolute',
                            right: '-4px',
                            top: 0,
                            bottom: 0,
                            width: '8px',
                            cursor: 'ew-resize',
                            backgroundColor: 'transparent',
                            borderRight: '1px solid #409eff',
                            zIndex: 999,
                            transform: 'translateZ(999px)',
                            opacity: 0,
                            transition: 'opacity 0.2s',
                        },
                        onDragstart: (e: any) => dragStart(e),
                        onDragend: () => handleGlobalDragEnd(),
                        onMouseenter: (e: any) => {
                            e.target.style.opacity = '1'
                        },
                        onMouseleave: (e: any) => {
                            e.target.style.opacity = '0'
                        },
                    }),
                ]
            )
        },
        cellRenderer: ({
            cellData,
            rowData: _rowData,
            column,
            rowIndex,
        }: {
            cellData: any
            rowData: any
            column: any
            rowIndex: number
        }) => {
            const colKey = column.dataKey as string
            const isEditing = isEditingCell(rowIndex, colKey)
            const isChanged = isCellChanged(rowIndex, colKey)
            const colType = col.type

            if (isEditing) {
                if (isDateType(colType)) {
                    return h('input', {
                        type: 'datetime-local',
                        value: editingCellValue.value ? editingCellValue.value.substring(0, 16) : '',
                        style: {
                            width: '100%',
                            height: '28px',
                            border: '1px solid #409eff',
                            borderRadius: '4px',
                            padding: '0 4px',
                            fontSize: '12px',
                            background: 'var(--bg-secondary)',
                            color: 'var(--text-primary)',
                            boxSizing: 'border-box',
                        },
                        onInput: (e: Event) => {
                            const target = e.target as HTMLInputElement
                            editingCellValue.value = target.value + ':00'
                        },
                        onKeyup: (e: KeyboardEvent) => {
                            if (e.key === 'Enter') commitInlineEdit()
                            if (e.key === 'Escape') cancelInlineEdit()
                        },
                        onPaste: (e: ClipboardEvent) => {
                            const text = e.clipboardData?.getData('text/plain')
                            if (!text) return
                            e.preventDefault()
                            e.stopPropagation()
                            const input = e.target as HTMLInputElement
                            const start = input.selectionStart ?? editingCellValue.value.length
                            const end = input.selectionEnd ?? start
                            const val = editingCellValue.value
                            const newVal = val.substring(0, start) + text + val.substring(end)
                            input.value = newVal
                            editingCellValue.value = newVal
                            const newPos = start + text.length
                            input.setSelectionRange(newPos, newPos)
                        },
                        onBlur: () => commitInlineEdit(),
                    })
                }
                return h('input', {
                    value: editingCellValue.value,
                    style: {
                        width: '100%',
                        height: '28px',
                        border: '1px solid #409eff',
                        borderRadius: '4px',
                        padding: '0 4px',
                        fontSize: '12px',
                        background: 'var(--bg-secondary)',
                        color: 'var(--text-primary)',
                        boxSizing: 'border-box',
                    },
                    onInput: (e: Event) => {
                        const target = e.target as HTMLInputElement
                        editingCellValue.value = target.value
                    },
                    onKeyup: (e: KeyboardEvent) => {
                        if (e.key === 'Enter') commitInlineEdit()
                        if (e.key === 'Escape') cancelInlineEdit()
                    },
                    onPaste: (e: ClipboardEvent) => {
                        const text = e.clipboardData?.getData('text/plain')
                        if (!text) return
                        e.preventDefault()
                        e.stopPropagation()
                        const input = e.target as HTMLInputElement
                        const start = input.selectionStart ?? editingCellValue.value.length
                        const end = input.selectionEnd ?? start
                        const val = editingCellValue.value
                        const newVal = val.substring(0, start) + text + val.substring(end)
                        input.value = newVal
                        editingCellValue.value = newVal
                        const newPos = start + text.length
                        input.setSelectionRange(newPos, newPos)
                    },
                    onBlur: () => commitInlineEdit(),
                })
            }

            const isNull = cellData === null || cellData === undefined
            const displayVal = isNull ? 'NULL' : String(cellData)
            const nullStyle = isNull
                ? { color: 'var(--text-tertiary, #bbb)', fontStyle: 'italic', fontSize: '0.85em' }
                : {}
            const changedStyle = isChanged
                ? {
                      backgroundColor: 'var(--bg-row-changed, #fff7e6)',
                      padding: '2px 4px',
                      borderRadius: '3px',
                      borderBottom: '1px dashed var(--warning-color, #faad14)',
                      cursor: 'pointer',
                  }
                : { cursor: 'pointer' }
            const isSelected = isCellInSelection2(rowIndex, colKey)
            const selectedStyle = isSelected ? { backgroundColor: '#68a6eb', outline: 'none' } : {}

            return h(
                'span',
                {
                    title: displayVal,
                    style: { ...changedStyle, ...nullStyle, ...selectedStyle },
                    onDblclick: (e: MouseEvent) => startInlineEdit(rowIndex, colKey, e),
                    onMousedown: (e: MouseEvent) => onCellMouseDown2(rowIndex, colKey, e),
                    onMousemove: () => onCellMouseMove2(rowIndex, colKey),
                    onMouseenter: () => onCellMouseEnter2(rowIndex, colKey),
                },
                displayVal
            )
        },
    }
}

function buildRowNumberColumn(): any {
    return {
        dataKey: 'col-idx',
        width: 60,
        fixed: true,
        cellRenderer: ({ cellData, rowIndex }: { cellData: any; rowIndex: number }) => {
            return h('div', {}, [
                h('div', { class: 'el-table-v2__cell-text', title: cellData }, cellData),
                h('div', { class: 'data-view', onClick: () => emit('openDataDetails', rowIndex) }),
            ])
        },
    }
}

function rebuildColumns() {
    if (props.rawColumns.length === 0) {
        displayColumns.value = []
        return
    }
    displayColumns.value = [
        buildRowNumberColumn(),
        ...props.rawColumns.map((col: any) => buildColumnDef(col)),
    ]
    clearRangeSelection2()
    nextTick(() => onResultScroll())
}

watch(
    () => props.rawColumns,
    () => rebuildColumns(),
    { immediate: true }
)

watch([() => props.result, totalColumnWidth], () => {
    nextTick(() => onResultScroll())
})
</script>

<style scoped>
.data-view {
    cursor: pointer;
}
</style>

<style>
/* ── Result Table (element-plus 全局覆盖) ── */
.el-table-v2__header-cell-text {
    user-select: text;
}

.el-table-v2__row-cell,
.el-table-v2__row-cell > span {
    user-select: text;
}

.el-table-v2__main {
    overflow: visible !important;
}

[data-theme='dark'] .el-scrollbar__thumb {
    background-color: rgba(121, 121, 121, 0.4) !important;
}

[data-theme='dark'] .el-scrollbar__thumb:hover {
    background-color: rgba(121, 121, 121, 0.6) !important;
}

.el-table-v2__row-cell {
    overflow: hidden !important;
}

.el-table-v2__row-cell > span {
    white-space: nowrap !important;
    overflow: hidden !important;
    text-overflow: ellipsis !important;
    display: block !important;
}

.el-table-v2__header-row,
.el-table-v2__header-wrapper {
    height: 35px !important;
}

.header-box {
    position: relative;
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    overflow: visible;
}

.header-box .header-text {
    flex: 1;
    max-width: calc(100% - 12px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    user-select: text;
    box-sizing: border-box;
    line-height: 35px;
    font-weight: 600;
    font-size: 13px;
    color: var(--text-secondary);
}

.header-box .drag-line {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    cursor: ew-resize;
    width: 12px;
    border-right: 2px solid transparent;
    z-index: 100;
    transition: all 0.15s;
    flex-shrink: 0;
    opacity: 0;
}

.header-box .drag-line:hover {
    opacity: 1;
    border-right-color: var(--accent-color);
    background: rgba(0, 122, 204, 0.08);
}

.el-table-v2__header-cell,
.el-table-v2__header-cell-content {
    overflow: visible !important;
    padding-right: 12px !important;
}

.el-table-v2__header,
.el-table-v2__header-wrapper,
.el-table-v2__header-row {
    overflow: visible !important;
}

.el-table-v2__row-cell > span {
    padding: 3px;
    border-radius: 3px;
}

.el-table-v2 {
    overflow: visible !important;
}

.el-drawer__header {
    margin-bottom: -20px;
}
</style>
