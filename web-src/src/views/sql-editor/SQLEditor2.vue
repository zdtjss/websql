<template>
    <div class="sql-editor-panel" @keyup.f9="execSmart" @keyup.ctrl.shift.f="formatSql">
        <el-splitter layout="vertical" @resize="onResultDivResize">
            <SqlEditorToolbar
                :execting-sql="exectingSql"
                :execution-time="executionTime"
                :result-length="result.length"
                :can-inline-edit="canInlineEdit"
                v-model:can-modify="canModify"
                :role-forbid-modify="roleForbidModify"
                v-model:max-line="maxLine"
                :refreshing-schema="refreshingSchema"
                @exec="execSmart"
                @format="formatSql"
                @optimize="toggleOptimizePanel"
                @export="handleExportResult"
                @open-table-manager="openTableManager"
                @show-history="historyPanelVisible = true"
                @show-snippet="snippetVisible = true"
                @exec-file="execFile"
                @refresh-schema="refreshSchema"
            />
            <el-splitter-panel size="55%">
                <div id="sqlArea" ref="sqlAreaRef" class="sql-area">
                    <div ref="codemirror" class="codemirror" :class="{ 'table-link-cursor': tableNameUnderCursor }"
                        @keyup="onKeyup" @keydown="onEditorKeydown" @mousemove="onEditorMousemove"
                        @click="onEditorClick"></div>
                </div>
            </el-splitter-panel>
            <el-splitter-panel size="45%">
                <SqlResultPanel
                    :result="result"
                    :raw-columns="rawColumns"
                    :sql-error="sqlError"
                    :is-batch-mode="isBatchMode"
                    :batch-display-tabs="batchDisplayTabs"
                    :active-result-tab="activeResultTab"
                    :can-inline-edit="canInlineEdit"
                    :can-modify="canModify"
                    :can-edit="canEdit"
                    :table-keys="tableKeys"
                    :conn-id="connId"
                    :schema="schema"
                    :max-line="maxLine"
                    :current-select-table="currentSelectTable"
                    :effective-db-type="effectiveDbType"
                    @update:active-result-tab="activeResultTab = $event"
                    @batch-tab-change="onBatchTabChange"
                    @open-data-details="openDataDetails"
                    @exec-refresh="exec(true)"
                />
            </el-splitter-panel>
        </el-splitter>
    </div>

    <el-dialog v-model="exportDialogVisible" title="导表" width="60%" center :draggable="true" :destroyOnClose="true">
        <DBExport :connId="connId" :schema="schema" opt="insert" :canImport="canModify" :dbType="dbType" />
    </el-dialog>

    <SqlBackupDialog v-model="backupDataDrawerShow" :backup-data="backupData" @export="exportBackupData" />

    <SqlHistoryPanel v-model="historyPanelVisible" :conn-id="connId" :schema="schema"
        @apply-sql="applySqlFromHistory" @show-backup="showBackupData" />

    <el-dialog v-model="dataDetailsDialogVisible" :draggable="true" :title="currentSelectTable" width="1000px"
        class="data-details-dialog">
        <div class="dialog-scroll-body">
            <el-form :model="rowData" label-width="auto" style="margin-right: 10px;">
                <el-form-item v-for="col in rawColumns" :key="col.name" :label="col.name" :title="col.comment">
                    <div style="display: flex; align-items: flex-start; gap: 6px; width: 100%;">
                        <el-date-picker v-if="col.type === 'DATETIME'" v-model="rowData[col.name]" type="datetime"
                            value-format="YYYY-MM-DD HH:mm:ss" :placeholder="rowData[col.name] === null ? 'NULL' : ''"
                            style="flex: 1;" />
                        <el-switch v-if="col.type === 'BIT'" v-model="rowData[col.name]" active-value="b'1'"
                            inactive-value="b'0'" />
                        <el-input v-if="col.type !== 'DATETIME' && col.type !== 'BIT'"
                            v-model="rowData[col.name]" type="textarea" autosize
                            :disabled="tableKeys.includes(col.name)"
                            :placeholder="rowData[col.name] === null ? 'NULL' : ''" style="flex: 1;" />
                        <el-button
                            v-if="col.type !== 'DATETIME' && col.type !== 'BIT' && !tableKeys.includes(col.name)"
                            size="small"
                            :type="rowData[col.name] === null ? 'warning' : 'default'"
                            link
                            @click="rowData[col.name] = rowData[col.name] === null ? '' : null"
                            :title="rowData[col.name] === null ? '当前为 NULL，点击设为空字符串' : '点击设为 NULL'"
                        >∅</el-button>
                    </div>
                </el-form-item>
            </el-form>
        </div>
        <template #footer>
            <div class="dialog-footer">
                <el-button v-if="canModify && canEdit" type="primary" :loading="onDataSaving"
                    @click="saveData(rowData)">
                    保存
                </el-button>
                <el-button v-if="!(canModify && canEdit)" type="primary" @click="dataDetailsDialogVisible = false">
                    关闭
                </el-button>
            </div>
        </template>
    </el-dialog>

    <SqlSnippetManager v-model="snippetVisible" :current-sql="getEditorDoc()" :conn-id="connId" :schema="schema"
        :db-type="dbType" :schema-path="schemaPath" @apply="onApplySnippet" />

    <SQLOptimizePanel v-model:visible="optimizePanelVisible" :conn-id="connId" :schema="schema" :sql="optimizeSql"
        :db-type="dbType" />
</template>

<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view'
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark'
import { EditorState, Compartment } from '@codemirror/state'
import { standardKeymap, insertTab, history, redo, undo } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { autocompletion } from '@codemirror/autocomplete'
import { tags } from '@lezer/highlight'
import { ref, shallowRef, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import { useDbSchemaStore } from '@/stores/dbSchema'
import { ElMessage } from 'element-plus'
import type { SqlLanguage } from 'sql-formatter'
import DBExport from './DBExport.vue'
import SqlSnippetManager from '@/components/sql-editor/SqlSnippetManager.vue'
import SQLOptimizePanel from '@/components/sql-editor/SQLOptimizePanel.vue'
import SqlEditorToolbar from './components/SqlEditorToolbar.vue'
import SqlResultPanel from './components/SqlResultPanel.vue'
import SqlBackupDialog from './components/SqlBackupDialog.vue'
import SqlHistoryPanel from './components/SqlHistoryPanel.vue'
import { canModifyData } from '@/api/auth'
import { showTree } from '@/api/conn'
import { execSQL, type SQLResult } from '@/api/sql'
import { useStorage } from '@/composables/useStorage'
import { buildWhereCondition, fmtVal, getSqlDialect, quoteId } from '@/utils/sqlHelper'
import { useTheme } from '@/utils/useTheme'
import { useSqlExecution } from './composables/useSqlExecution'
import { useSqlFormat } from './composables/useSqlFormat'
import { useSqlHistory } from './composables/useSqlHistory'

const { currentTheme } = useTheme()
const dbSchemaProxy = useDbSchemaStore()
const storage = useStorage()

// ── 编辑器主题与高亮 ──
const lightEditorTheme = EditorView.theme({
    '&': { backgroundColor: '#ffffff', color: '#303133' },
    '.cm-gutters': { backgroundColor: '#fafbfc', color: '#909399', borderRight: '1px solid #ebeef5' },
    '.cm-activeLineGutter': { backgroundColor: '#ecf5ff' },
    '.cm-activeLine': { backgroundColor: '#f5f7fa33' },
    '.cm-cursor': { borderLeftColor: '#303133' },
    '.cm-selectionBackground': { backgroundColor: '#b3d8ff66' },
}, { dark: false })

const darkEditorTheme = EditorView.theme({
    '&': { backgroundColor: '#1e1e2e', color: '#cdd6f4' },
    '.cm-gutters': { backgroundColor: '#1e1e2e', color: '#6c7086', borderRight: '1px solid #313244' },
    '.cm-activeLineGutter': { backgroundColor: '#313147' },
    '.cm-activeLine': { backgroundColor: '#2a2a3d33' },
    '.cm-cursor': { borderLeftColor: '#cdd6f4' },
    '.cm-selectionBackground': { backgroundColor: '#45475a66' },
}, { dark: true })

const lightHighlightStyle = HighlightStyle.define([
    { tag: tags.keyword, color: '#0550ae', fontWeight: '600' },
    { tag: tags.string, color: '#0a3069' },
    { tag: tags.number, color: '#0550ae' },
    { tag: tags.comment, color: '#6e7781', fontStyle: 'italic' },
    { tag: tags.typeName, color: '#0550ae' },
    { tag: tags.operator, color: '#0550ae' },
    { tag: tags.bracket, color: '#0550ae' },
    { tag: tags.function(tags.variableName), color: '#8250df' },
    { tag: tags.variableName, color: '#0550ae' },
    { tag: tags.bool, color: '#0550ae' },
    { tag: tags.null, color: '#0550ae' },
])

function getEditorTheme() {
    return currentTheme.value === 'dark' ? darkEditorTheme : lightEditorTheme
}

const sqlCompartment = new Compartment()
const themeCompartment = new Compartment()
const highlightCompartment = new Compartment()

// ── Props / Emits ──
const { tabId, connId, schema, schemaPath, dbType } = defineProps<{
    tabId: string
    connId: string
    schema: string
    schemaPath: string
    tableName?: string
    dbType?: string
}>()

const emit = defineEmits(['openTableManager', 'openDataBrowser', 'viewTableInfo'])

// ── 主容器状态 ──
const editorView = shallowRef<EditorView>()
const sqlAreaRef: any = ref(null)
const codemirror = ref()
const maxLine = ref('15')
const canModify = ref(false)
const roleForbidModify = ref(false)
const refreshingSchema = ref(false)
const snippetVisible = ref(false)
const exportDialogVisible = ref(false)
const historyPanelVisible = ref(false)
const dataDetailsDialogVisible = ref(false)
const onDataSaving = ref(false)
const rowData: any = ref({})
let originRowData: any = {}
const ctrlHeld = ref(false)
const tableNameUnderCursor = ref('')
const lastMousePos = ref({ x: -1, y: -1 })

const effectiveDbType = computed(() => dbType || dbSchemaProxy.getDbType(schema) || '')

const tableList = computed(() => {
    try {
        return dbSchemaProxy.getTable(schema).map((t: any) => t.label)
    } catch {
        return []
    }
})

// ── 辅助函数（composable 依赖） ──
function getEditorView() {
    return editorView.value
}

function getSqlKey() {
    return 'go-web-sql-' + tabId
}

function getSqlLang(): SqlLanguage {
    return getSqlDialect(dbSchemaProxy.getDbType(schema) || '')
}

const getEditorDoc = (): string => {
    try {
        return (editorView.value as EditorView)?.state?.doc?.toString() || ''
    } catch {
        return ''
    }
}

// ── Composables ──
const {
    result, rawColumns, tableKeys, canEdit, sqlError, executionTime, exectingSql,
    isBatchMode, activeResultTab, currentSelectTable, batchDisplayTabs, canInlineEdit,
    exec, execSmart, execFile, onBatchTabChange, handleExportResult,
} = useSqlExecution({
    connId, schema, maxLine, getEditorView, canModify, effectiveDbType,
})

const { optimizePanelVisible, optimizeSql, formatSql, toggleOptimizePanel } = useSqlFormat({
    getEditorView, getSqlLang,
})

const { backupData, backupDataDrawerShow, showBackupData, exportBackupData } = useSqlHistory({
    connId, schema,
})

// ── 编辑器实例管理 ──
function createEditor(editorContainer: any, doc: any) {
    if (editorView.value) {
        editorView.value.destroy()
    }
    if (editorContainer.value) {
        editorContainer.value.innerHTML = ''
    }
    const isDark = currentTheme.value === 'dark'
    const cleanKeymap = standardKeymap.filter(
        k => k.key !== 'Mod-z' && k.key !== 'Mod-Shift-z' && k.key !== 'Mod-y'
    )
    const extensions = [
        keymap.of([
            ...cleanKeymap,
            { key: 'Mod-z', run: undo, preventDefault: true },
            { key: 'Mod-y', run: redo, preventDefault: true },
            { key: 'Mod-Shift-z', run: redo, preventDefault: true },
            { key: 'Tab', run: insertTab, preventDefault: true },
        ]),
        sqlCompartment.of(sql({
            dialect: dbSchemaProxy.getDialect(schema),
            schema: JSON.parse(JSON.stringify(dbSchemaProxy.getAll(schema) || {})),
        })),
        history(),
        lineNumbers(),
        highlightActiveLineGutter(),
        autocompletion(),
        EditorView.editable.of(true),
        themeCompartment.of(getEditorTheme()),
        highlightCompartment.of(syntaxHighlighting(isDark ? oneDarkHighlightStyle : lightHighlightStyle, { fallback: true })),
    ]
    editorView.value = new EditorView({
        state: EditorState.create({ doc, extensions }),
        parent: editorContainer.value,
    })
}

function reconfigureSql() {
    if (!editorView.value) return
    const schemaData = dbSchemaProxy.getAll(schema)
    const plainSchema = schemaData ? JSON.parse(JSON.stringify(schemaData)) : {}
    editorView.value.dispatch({
        effects: sqlCompartment.reconfigure(sql({
            dialect: dbSchemaProxy.getDialect(schema),
            schema: plainSchema,
        }))
    })
}

function reconfigureTheme() {
    if (!editorView.value) return
    const isDark = currentTheme.value === 'dark'
    editorView.value.dispatch({
        effects: [
            themeCompartment.reconfigure(getEditorTheme()),
            highlightCompartment.reconfigure(syntaxHighlighting(isDark ? oneDarkHighlightStyle : lightHighlightStyle, { fallback: true })),
        ]
    })
}

function refreshSchema() {
    refreshingSchema.value = true
    showTree({ connId, key: schema, type: 'schema', level: 2, schema })
        .then((resp) => {
            if (resp.data.data) {
                dbSchemaProxy.addTable(schema, effectiveDbType.value, resp.data.data, connId)
            }
            ElMessage({ message: '补全信息已刷新', type: 'success' })
        })
        .catch((err) => {
            console.error('[SQLEditor] refreshSchema failed:', err)
            ElMessage({ message: '刷新失败', type: 'error' })
        })
        .finally(() => {
            refreshingSchema.value = false
        })
}

// ── 编辑器交互 ──
let saveTimer: ReturnType<typeof setTimeout> | null = null

function onKeyup(e: KeyboardEvent) {
    onEditorKeyup(e)
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
        try {
            storage.setItem(getSqlKey(), getEditorDoc())
        } catch {
            // localStorage may be full
        }
    }, 1000)
}

function onEditorKeydown(e: KeyboardEvent) {
    if (e.key === 'Control' || e.key === 'Meta') {
        ctrlHeld.value = true
        detectTableAtMouse()
    }
}

function onEditorKeyup(e: KeyboardEvent) {
    if (e.key === 'Control' || e.key === 'Meta') {
        ctrlHeld.value = false
        tableNameUnderCursor.value = ''
    }
}

function onGlobalKeyup(e: KeyboardEvent) {
    if (e.key === 'Control' || e.key === 'Meta') {
        ctrlHeld.value = false
        tableNameUnderCursor.value = ''
    }
}

function extractTablesFromSql(sql: string): string[] {
    const tables: string[] = []
    const normalized = sql.replace(/--.*$/gm, '').replace(/\/\*[\s\S]*?\*\//g, '')
    const pattern = /(?:^|\s)(?:from|join|inner\s+join|left\s+(?:outer\s+)?join|right\s+(?:outer\s+)?join|full\s+(?:outer\s+)?join|cross\s+join)\s+([a-zA-Z_][a-zA-Z0-9_$#]*)/gi
    let match: RegExpExecArray | null
    while ((match = pattern.exec(normalized)) !== null) {
        tables.push(match[1])
    }
    return tables
}

function detectTableAtPosition(clientX: number, clientY: number) {
    if (!editorView.value) return
    const pos = editorView.value.posAtCoords({ x: clientX, y: clientY })
    if (pos === null) {
        tableNameUnderCursor.value = ''
        return
    }
    const word = editorView.value.state.wordAt(pos)
    if (!word) {
        tableNameUnderCursor.value = ''
        return
    }
    const state = editorView.value.state
    let from = word.from
    let to = word.to
    const doc = state.doc
    while (from > 0 && /[a-zA-Z0-9_$#]/.test(doc.sliceString(from - 1, from))) from--
    while (to < doc.length && /[a-zA-Z0-9_$#]/.test(doc.sliceString(to, to + 1))) to++
    const wordText = state.sliceDoc(from, to)
    if (tableList.value.some((t: string) => t.toLowerCase() === wordText.toLowerCase())) {
        tableNameUnderCursor.value = wordText
    } else {
        const sqlTables = extractTablesFromSql(state.doc.toString())
        if (sqlTables.some(t => t.toLowerCase() === wordText.toLowerCase())) {
            tableNameUnderCursor.value = wordText
        } else {
            tableNameUnderCursor.value = ''
        }
    }
}

function detectTableAtMouse() {
    if (lastMousePos.value.x < 0) return
    detectTableAtPosition(lastMousePos.value.x, lastMousePos.value.y)
}

function onEditorMousemove(e: MouseEvent) {
    lastMousePos.value = { x: e.clientX, y: e.clientY }
    if (!ctrlHeld.value) {
        if (tableNameUnderCursor.value) tableNameUnderCursor.value = ''
        return
    }
    detectTableAtPosition(e.clientX, e.clientY)
}

function onEditorClick(e: MouseEvent) {
    if (!ctrlHeld.value || !tableNameUnderCursor.value) return
    e.preventDefault()
    e.stopPropagation()
    const tableName = tableNameUnderCursor.value
    tableNameUnderCursor.value = ''
    ctrlHeld.value = false
    emit('openDataBrowser', { connId, schema, tableName, dbType })
}

// ── 数据详情弹窗 ──
function openDataDetails(rowIndex: number) {
    dataDetailsDialogVisible.value = true
    rowData.value = result.value[rowIndex]
    originRowData = JSON.parse(JSON.stringify(result.value[rowIndex]))
}

function saveData(rowData: any) {
    const changedKeys = Object.keys(originRowData).filter((key) => originRowData[key] != rowData[key])
    if (changedKeys.length === 0 && canEdit.value) {
        ElMessage({ message: '数据未修改', type: 'warning' })
        return
    }
    const updateColumnSets = changedKeys.map((key) => quoteId(key, effectiveDbType.value) + ' = ' + fmtVal(rowData[key], effectiveDbType.value))
    const allWhereCols = [...tableKeys.value, ...changedKeys.filter((k: string) => !tableKeys.value.includes(k))]
    const whereColumns = allWhereCols.map((key: string) => buildWhereCondition(key, originRowData[key], effectiveDbType.value))
    let effiectiveSql = 'update ' + currentSelectTable.value + ' set '
    effiectiveSql += updateColumnSets.join(', ') + ' where '
    effiectiveSql += whereColumns.join(' and ')
    onDataSaving.value = true
    execSQL({ connId, schema, sql: effiectiveSql, maxLine: maxLine.value, tableName: currentSelectTable.value })
        .then((resp) => {
            onDataSaving.value = false
            const sqlResult = resp.data.data as SQLResult
            if (!sqlResult.msg) {
                dataDetailsDialogVisible.value = false
            }
            const respConlumn = sqlResult.columns![0].name
            const respData = (sqlResult.data as Record<string, unknown>[])[0]
            ElMessage({ message: sqlResult.msg ? '操作失败，请检查 SQL 语句' : '修改了 ' + respData[respConlumn] + ' 条数据', type: sqlResult.msg ? 'error' : 'success' })
        }).catch((error) => {
            console.error(error)
        })
}

// ── Snippet / History ──
function applySqlFromHistory(sql: string) {
    if (!sql) return
    const editorState = editorView.value?.state as EditorState
    if (editorState) {
        const doc = editorState.doc.toString()
        editorView.value?.dispatch({
            changes: { from: doc.length, insert: '\n' + sql }
        })
    }
    historyPanelVisible.value = false
    ElMessage({ message: '已填入编辑器', type: 'success' })
}

function onApplySnippet(sql: string) {
    if (!sql) return
    const editorState = editorView.value?.state as EditorState
    if (editorState) {
        const doc = editorState.doc.toString()
        editorView.value?.dispatch({
            changes: { from: doc.length, insert: '\n' + sql }
        })
    }
    ElMessage({ message: '已填入编辑器', type: 'success' })
}

// ── 布局 ──
function onResultDivResize(index: number, sizes: number[]) {
    nextTick(() => {
        if (sqlAreaRef.value) {
            sqlAreaRef.value.style.height = (sizes[index] - 25) + 'px'
            sqlAreaRef.value.style.setProperty('height', (sizes[index] - 25) + 'px', 'important')
        }
    })
}

function openTableManager() {
    emit('openTableManager', { connId, schema, schemaPath })
}

// ── 生命周期 ──
onMounted(() => {
    window.addEventListener('keyup', onGlobalKeyup)
    dbSchemaProxy.registLsn((changedSchema: any) => {
        if (changedSchema === schema) {
            reconfigureSql()
        }
    })
    const doc = localStorage.getItem(getSqlKey()) || '\n\n\n\n\n'
    createEditor(codemirror, doc)
    const schemaPathLower = schemaPath.toLowerCase()
    const schemaCanModify = schemaPathLower.indexOf('_test') != -1 || schemaPathLower.indexOf('_uat') != -1 || schemaPathLower.indexOf('_dev') != -1 || schemaPathLower.indexOf('_read') != -1
    canModifyData().then(resp => {
        const allowed = resp.data.data?.allowed !== false
        if (!allowed) {
            roleForbidModify.value = true
            canModify.value = false
        } else {
            canModify.value = schemaCanModify
        }
    }).catch(() => {
        canModify.value = schemaCanModify
    })
})

onBeforeUnmount(() => {
    window.removeEventListener('keyup', onGlobalKeyup)
})

watch(currentTheme, () => {
    reconfigureTheme()
})

watch(canModify, (can) => {
    if (roleForbidModify.value && can) {
        canModify.value = false
        ElMessage({ message: '当前角色禁止修改数据，请联系管理员开通', type: 'warning' })
        return
    }
    const schemaPathLower = schemaPath.toLowerCase()
    if (can && !(schemaPathLower.indexOf('_test') != -1 || schemaPathLower.indexOf('_uat') != -1 || schemaPathLower.indexOf('_dev') != -1 || schemaPathLower.indexOf('_read') != -1)) {
        ElMessage({ message: '当前可能为生产库，请谨慎修改。', type: 'error' })
    }
})
</script>

<style>
.sql-editor-panel {
    height: calc(100vh - 38px);
}

.sql-area {
    padding: 0;
    margin-top: 2px;
    border-top: 1px solid var(--border-primary);
    height: calc(100vh * 0.55 - 50px);
}

.cm-editor {
    height: 100%;
    width: 100%;
    font-size: 15px;
    font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
}

.codemirror {
    height: 100%;
}

.table-link-cursor .cm-editor {
    cursor: pointer;
}

/* ── Data Details Dialog ── */
.el-dialog .el-form-item {
    margin-bottom: 12px;
}

.data-details-dialog {
    height: 750px !important;
}

.data-details-dialog .el-dialog__body {
    height: calc(100% - 140px);
    overflow-y: auto;
}

.data-details-dialog .dialog-scroll-body {
    min-height: 100%;
}

.el-drawer__header {
    margin-bottom: -20px;
}
</style>
