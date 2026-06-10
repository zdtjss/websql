<template>
    <div class="sql-editor-panel" @keyup.f9="exec" @keyup.ctrl.shift.f="formatSql">
        <el-splitter layout="vertical" @resize="onResultDivResize">

            <div class="sql-toolbar">
                <div class="toolbar-left">
                    <el-button :type="exectingSql ? 'danger' : 'primary'" @click="exectingSql ? stopExec() : exec()" :title="exectingSql ? '终止执行' : 'F9'">
                        <el-icon style="margin-right: 4px;">
                            <Loading v-if="exectingSql" />
                            <VideoPlay v-else />
                        </el-icon>{{ exectingSql ? '终止' : '执行' }}
                    </el-button>
                    <el-divider direction="vertical" />
                    <el-button @click="formatSql" title="Ctrl + Shift + F">美化</el-button>
                    <el-button type="success" @click="toggleOptimizePanel" title="AI SQL优化建议">优化</el-button>
                    <el-divider direction="vertical" />
                    <el-dropdown @command="handleExportResult">
                        <el-button :disabled="result.length === 0">
                            导出结果<el-icon class="el-icon--right">
                                <ArrowDown />
                            </el-icon>
                        </el-button>
                        <template #dropdown>
                            <el-dropdown-menu>
                                <el-dropdown-item :disabled="result.length === 0" command="insert">SQL  新增</el-dropdown-item>
                                <el-dropdown-item :disabled="result.length === 0" command="update">SQL  修改</el-dropdown-item>
                                <el-dropdown-item :disabled="result.length === 0" command="xlsx" divided>Excel (.xlsx)</el-dropdown-item>
                                <el-dropdown-item :disabled="result.length === 0" command="csv">CSV</el-dropdown-item>
                                <el-dropdown-item :disabled="result.length === 0" command="json">JSON</el-dropdown-item>
                            </el-dropdown-menu>
                        </template>
                    </el-dropdown>
                    <el-divider direction="vertical" />
                    <el-button @click="openTableManager">表管理</el-button>
                    <el-button @click="showSqlHistory">历史</el-button>
                    <el-button @click="snippetVisible = true" title="SQL 收藏夹">收藏</el-button>
                    <el-upload :show-file-list="false" accept=".sql" :http-request="handleSqlFile">
                        <el-button title="执行 SQL 文件">执行文件</el-button>
                    </el-upload>
                </div>
                <div class="toolbar-right">
                    <span v-if="executionTime !== null" class="exec-time">{{ executionTime }}ms</span>
                    <span v-if="canInlineEdit && result.length > 0" class="inline-edit-badge"
                        title="当前结果集有主键，支持双击单元格内联编辑">✎ 可编辑</span>
                    <el-tooltip :content="roleForbidModify ? '当前角色禁止修改数据' : (canModify ? '当前允许修改数据，点击切换为只读' : '当前为只读模式，点击允许修改数据')" placement="bottom"
                        :show-after="400">
                        <label class="modify-toggle">
                            <el-switch v-model="canModify" size="small" :disabled="roleForbidModify" />
                            <span class="modify-label">{{ canModify ? '可写' : '只读' }}</span>
                        </label>
                    </el-tooltip>
                    <el-divider direction="vertical" />
                    <span class="max-rows-label">行数上限</span>
                    <el-input v-model="maxLine" style="width: 56px;" size="small" />
                </div>
            </div>

            <el-splitter-panel size="55%">
                <div id="sqlArea" ref="sqlAreaRef" class="sql-area">
                    <div ref="codemirror" class="codemirror" :class="{ 'table-link-cursor': tableNameUnderCursor }"
                        @keyup="onKeyup" @keydown="onEditorKeydown" @mousemove="onEditorMousemove"
                        @click="onEditorClick"></div>
                </div>
            </el-splitter-panel>
            <el-splitter-panel size="45%">
                <div style="height: 100%; display: flex; flex-direction: column;">
                    <el-tabs v-if="isBatchMode && batchDisplayTabs.length > 0" v-model="activeResultTab"
                        type="border-card" class="batch-tabs" @tab-change="onBatchTabChange">
                        <el-tab-pane v-for="tab in batchDisplayTabs" :key="tab.name" :name="tab.name">
                            <template #label>
                                <span v-if="tab.type === 'modify-summary'" class="batch-tab-label batch-tab-modify">
                                    <span class="batch-tab-index">M</span>
                                    <span class="batch-tab-sql">修改汇总</span>
                                    <el-tag v-if="tab.hasError" type="danger" size="small">{{ tab.allFailed ? '全部失败' : '部分失败' }}</el-tag>
                                    <el-tag v-else-if="tab.hasRollback" type="info" size="small">已回滚</el-tag>
                                    <el-tag v-else type="warning" size="small">{{ tab.modifyCount }}条</el-tag>
                                </span>
                                <el-tooltip v-else :content="tab.item?.sql || ''" placement="bottom" :show-after="400" popper-class="sql-history-tooltip">
                                    <span class="batch-tab-label" :class="{'batch-tab-error': tab.item?.status === 'error'}">
                                        <span class="batch-tab-sql">结果集 {{ tab.queryNum }}</span>
                                        <el-tag v-if="tab.item?.status === 'error'" type="danger" size="small">错误</el-tag>
                                        <el-tag v-if="tab.item?.status === 'success'" type="success" size="small">{{ (tab.item?.data || []).length }}行</el-tag>
                                    </span>
                                </el-tooltip>
                            </template>
                        </el-tab-pane>
                    </el-tabs>
                    <div v-if="sqlError" class="sql-exec-error">
                        <div class="sql-exec-error-title">SQL 执行异常</div>
                        <div class="sql-exec-error-body">{{ sqlError }}</div>
                    </div>
                    <div v-else style="flex: 1; overflow: hidden;">
                        <el-auto-resizer>
                            <template #default="{ height: autoHeight, width: autoWidth }">
                                <div ref="resultScrollRef" :style="{ height: autoHeight + 'px', overflowX: 'auto', overflowY: 'hidden' }"
                                    @paste="handlePaste2" @keydown="onTableKeydown2" @scroll="onResultScroll">
                                    <el-table-v2 :columns="columns" :data="result" :width="Math.max(totalColumnWidth, autoWidth)"
                                        :height="autoHeight" scrollbar-always-on />
                                </div>
                            </template>
                        </el-auto-resizer>
                    </div>
                    <div v-if="canInlineEdit && inlineChangeCount > 0" class="db-inline-bar">
                        <el-button type="warning" size="small" @click="saveInlineChanges" :loading="savingInline">
                            <span>保存更改</span>
                        </el-button>
                        <el-button size="small" @click="exec(true); inlineChanges.clear()">
                            <span>放弃更改</span>
                        </el-button>
                        <span class="inline-count">{{ inlineChangeCount }} 处更改</span>
                    </div>
                </div>
            </el-splitter-panel>
        </el-splitter>
    </div>
    <el-dialog v-model="exportDialogVisible" title="导表" width="60%" center :draggable="true" :destroyOnClose="true">
        <DBExport :connId="connId" :schema="schema" opt="insert" :canImport="canModify" :dbType="dbType" />
    </el-dialog>
    <el-drawer v-model="backupDataDrawerShow">
        <template #header>
            <h3>备份的数据</h3>
            <a @click="exportBackupData" style="margin-right: 10px;cursor: pointer;">下载</a>
        </template>
        <template #default>
            <pre style="white-space: pre;">{{ backupData }}</pre>
        </template>
    </el-drawer>
    <el-drawer v-model="sqlHistoryDrawerShow" title="SQL 执行历史" :size="sqlDrawerWidth + 'px'">
        <div v-if="sqlHistoryDrawerShow" class="drawer-drag-handle" :style="{ right: sqlDrawerWidth + 'px' }"
            @mousedown="onDrawerDragStart"></div>
        <div style="margin-bottom: 12px;">
            <el-input v-model="sqlHistorySearch" placeholder="搜索 SQL..." clearable size="small" />
        </div>
        <el-table :data="filteredSqlHistory" stripe size="small" style="width: 100%;" max-height="calc(100vh - 240px)">
            <el-table-column prop="exec_time" label="时间" width="160" resizable />
            <el-table-column prop="operation_type" label="类型" width="80" resizable>
                <template #default="scope">
                    <el-tag v-if="scope.row.operation_type === 'select'" type="info" size="small">SELECT</el-tag>
                    <el-tag v-else-if="scope.row.operation_type === 'update'" type="warning"
                        size="small">UPDATE</el-tag>
                    <el-tag v-else type="danger" size="small">DELETE</el-tag>
                </template>
            </el-table-column>
            <el-table-column prop="exec_sql" label="SQL" resizable>
                <template #default="scope">
                    <el-tooltip :content="scope.row.exec_sql" placement="top" popper-class="sql-history-tooltip"
                        :show-after="400">
                        <span class="sql-history-text" @click="applySqlFromHistory(scope.row.exec_sql)">
                            {{ scope.row.exec_sql }}
                        </span>
                    </el-tooltip>
                </template>
            </el-table-column>
            <el-table-column label="操作" width="50" resizable>
                <template #default="scope">
                    <el-icon v-if="scope.row.operation_type !== 'select'" style="cursor: pointer;"
                        @click="showBackupData(scope.row.id)" title="查看备份数据">
                        <View />
                    </el-icon>
                </template>
            </el-table-column>
        </el-table>
        <div style="position: absolute;right: 10px;bottom: 5px;">
            <el-pagination layout="prev, pager, next" v-model:total="sqlHistoryTotal"
                v-model:page-size="sqlHistoryPageSize" v-model:current-page="sqlHistoryCurrent"
                @current-change="showSqlHistory" />
        </div>
    </el-drawer>
    <el-dialog v-model="dataDetailsDialogVisible" :draggable="true" :title="currentSelectTable" width="1000px"
        class="data-details-dialog">
        <div class="dialog-scroll-body">
            <el-form :model="rowData" label-width="auto" style="margin-right: 10px;">
                <el-form-item v-for="col in columns.slice(1)" :label="col.dataKey" :title="col.comment">
                    <el-date-picker v-if="col.dataType === 'DATETIME'" v-model="rowData[col.dataKey]" type="datetime"
                        value-format="YYYY-MM-DD HH:mm:ss" />
                    <el-switch v-if="col.dataType === 'BIT'" v-model="rowData[col.dataKey]" active-value="b'1'"
                        inactive-value="b'0'" />
                    <el-input v-if="col.dataKey !== 'col-idx' && col.dataType !== 'DATETIME' && col.dataType !== 'BIT'"
                        v-model="rowData[col.dataKey]" type="textarea" autosize
                        :disabled="tableKeys.includes(col.dataKey)" />
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

    <SqlSnippetManager v-model="snippetVisible" :current-sql="getEditorDoc()" @apply="onApplySnippet" />

    <SQLOptimizePanel v-model:visible="optimizePanelVisible" :conn-id="connId" :schema="schema" :sql="optimizeSql"
        :db-type="dbType" />
</template>

<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view'
import { oneDarkHighlightStyle } from "@codemirror/theme-one-dark"
import { EditorState, Compartment } from '@codemirror/state'
import { standardKeymap, insertTab, history, redo, undo } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql';
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { autocompletion } from '@codemirror/autocomplete'
import { tags } from '@lezer/highlight'
import { ref, onMounted, onBeforeUnmount, watch, h, nextTick, computed } from 'vue'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()
import { ElMessage } from 'element-plus'
import { ArrowDown, VideoPlay, View, Loading } from '@element-plus/icons-vue'
import { format, type SqlLanguage } from 'sql-formatter'
import DBExport from './DBExport.vue'
import SqlSnippetManager from '@/components/sql-editor/SqlSnippetManager.vue'
import SQLOptimizePanel from '@/components/sql-editor/SQLOptimizePanel.vue'

import axios from 'axios'
import http from '@/utils/httpProxy.js'
import excel from '@/utils/excel.js'
import copyToClipboard from '@/utils/copy-to-clipboard.js'
import { fmtVal, getSqlDialect } from '@/utils/sqlHelper.ts'
import { exportToCsv, exportToJson } from '@/utils/exportHelper.ts'
import { useTheme } from '@/utils/useTheme.ts'

const { currentTheme } = useTheme()

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

const { tabId, connId, schema, schemaPath, tableName, dbType } = defineProps<{
    tabId: string,
    connId: string,
    schema: string,
    schemaPath: string,
    tableName?: string,
    dbType?: string,
}>()

const emit = defineEmits(['openTableManager', 'openDataBrowser', 'viewTableInfo'])

const sqlAreaRef: any = ref(null)

const maxLine = ref("15")
const columns: any = ref([])
const result: any = ref([])
const editorView = ref<EditorView>()
const codemirror = ref()
const exportDialogVisible = ref(false)

const exectingSql = ref(false)
let abortController: AbortController | null = null
const executionTime = ref<number | null>(null)
const sqlError = ref('')
const snippetVisible = ref(false)
const optimizePanelVisible = ref(false)
const optimizeSql = ref('')
const currentSelectTable = ref("")

const canEdit = ref(false)
const tableKeys = ref([] as string[])
const rowData: any = ref({})
// 原始的数据 
let originRowData: any = {}
const dataDetailsDialogVisible = ref(false)
const onDataSaving = ref(false)

const canModify = ref(false)
const roleForbidModify = ref(false)

const editingCellRow = ref(-1)
const editingCellCol = ref('')
const editingCellValue = ref('')
const inlineChanges = ref(new Map<string, any>())
const savingInline = ref(false)

const batchResults = ref<any[]>([])
const activeResultTab = ref('0')
const isBatchMode = ref(false)

const batchDisplayTabs = computed(() => {
    if (!isBatchMode.value || batchResults.value.length === 0) return []
    const tabs: { name: string; type: string; modifyCount?: number; queryNum?: number; idx?: number; item?: any; hasError?: boolean; hasRollback?: boolean; allFailed?: boolean }[] = []
    const modifyItems = batchResults.value.filter((r: any) => r.type === 'modify')
    if (modifyItems.length > 0) {
        const hasError = modifyItems.some((r: any) => r.status === 'error')
        const hasRollback = modifyItems.some((r: any) => r.status === 'rolled_back')
        const allFailed = modifyItems.every((r: any) => r.status === 'error')
        tabs.push({
            name: 'modify-summary',
            type: 'modify-summary',
            modifyCount: modifyItems.length,
            hasError,
            hasRollback,
            allFailed
        })
    }
    let queryNum = 0
    batchResults.value.forEach((item: any, idx: number) => {
        if (item.type === 'query') {
            queryNum++
            tabs.push({
                name: String(idx),
                type: 'query',
                queryNum,
                idx,
                item
            })
        }
    })
    return tabs
})

const activeCellRow2 = ref(-1)
const activeCellCol2 = ref('')
const pasteSnapshot2 = ref<any>(null)

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

const tableList = computed(() => {
    try {
        return dbSchemaProxy.getTable(schema).map((t: any) => t.label)
    } catch {
        return []
    }
})

const ctrlHeld = ref(false)
const tableNameUnderCursor = ref('')
const lastMousePos = ref({ x: -1, y: -1 })

// 计算所有列的总宽度
const totalColumnWidth = computed(() => {
    if (!columns.value || columns.value.length === 0) {
        return 800
    }
    return columns.value.reduce((sum: number, col: any) => {
        return sum + (col.width || 150)
    }, 0)
})

const backupData = ref("")
const backupDataDrawerShow = ref(false)

// SQL 执行历史
const sqlHistoryDrawerShow = ref(false)
const sqlHistoryList = ref([])
const sqlHistorySearch = ref('')
const sqlHistoryTotal = ref(0)
const sqlHistoryCurrent = ref(1)
const sqlHistoryPageSize = ref(12)
const sqlDrawerWidth = ref(600)
const isDraggingDrawer = ref(false)

const filteredSqlHistory = computed(() => {
    const kw = sqlHistorySearch.value.trim().toLowerCase()
    if (!kw) return sqlHistoryList.value
    return sqlHistoryList.value.filter((item: any) =>
        (item.exec_sql || '').toLowerCase().includes(kw)
    )
})

function showSqlHistory() {
    http.get("/listBackupData", { params: { connId: connId, schema: schema, current: sqlHistoryCurrent.value, pageSize: sqlHistoryPageSize.value } })
        .then((resp: any) => {
            sqlHistoryList.value = resp.data.data.data || []
            sqlHistoryTotal.value = resp.data.data.total || 0
            sqlHistoryDrawerShow.value = true
        })
}

function onDrawerDragStart(e: MouseEvent) {
    isDraggingDrawer.value = true
    document.addEventListener('mousemove', onDrawerDragMove)
    document.addEventListener('mouseup', onDrawerDragEnd)
    e.preventDefault()
}

function onDrawerDragMove(e: MouseEvent) {
    if (!isDraggingDrawer.value) return
    const newWidth = window.innerWidth - e.clientX
    if (newWidth >= 300 && newWidth <= 1200) {
        sqlDrawerWidth.value = newWidth
    }
}

function onDrawerDragEnd() {
    isDraggingDrawer.value = false
    document.removeEventListener('mousemove', onDrawerDragMove)
    document.removeEventListener('mouseup', onDrawerDragEnd)
}

async function handleSqlFile(options: any) {
    const file = options.file
    const text = await file.text()
    const statements = text
        .split(/;\s*\n|;\s*$/)
        .map((s: string) => s.trim())
        .filter((s: string) => s.length > 0 && !s.startsWith('--') && !s.startsWith('/*'))

    if (statements.length === 0) {
        ElMessage({ message: '文件中没有可执行的 SQL', type: 'warning' })
        return
    }

    ElMessage({ message: `开始执行 ${statements.length} 条语句...`, type: 'info' })
    let successCount = 0
    let errorCount = 0

    for (const stmt of statements) {
        const params = new URLSearchParams()
        params.append("connId", connId)
        params.append("schema", schema)
        params.append("sql", stmt)
        params.append("maxLine", maxLine.value)
        try {
            await http.post("/execSQL", params)
            successCount++
        } catch {
            errorCount++
        }
    }

    if (errorCount === 0) {
        ElMessage({ message: `全部 ${successCount} 条执行成功`, type: 'success' })
    } else {
        ElMessage({ message: `${successCount} 成功, ${errorCount} 失败`, type: 'warning' })
    }
}

function applySqlFromHistory(sql: string) {
    if (!sql) return
    const editorState = editorView.value?.state as EditorState
    if (editorState) {
        const doc = editorState.doc.toString()
        const insertPos = doc.length
        editorView.value?.dispatch({
            changes: { from: insertPos, insert: '\n' + sql }
        })
    }
    sqlHistoryDrawerShow.value = false
    ElMessage({ message: '已填入编辑器', type: 'success' })
}

function onApplySnippet(sql: string) {
    if (!sql) return
    const editorState = editorView.value?.state as EditorState
    if (editorState) {
        const doc = editorState.doc.toString()
        const insertPos = doc.length
        editorView.value?.dispatch({
            changes: { from: insertPos, insert: '\n' + sql }
        })
    }
    ElMessage({ message: '已填入编辑器', type: 'success' })
}

onMounted(() => {
    window.addEventListener('keyup', onGlobalKeyup)
    dbSchemaProxy.registLsn((schema: any) => {
        if (schema === schema) {
            reconfigureSql()
        }
    })
    const doc = localStorage.getItem(getSqlKey()) || "\n\n\n\n\n"
    createEditor(codemirror, doc);
    const schemaPathLower = schemaPath.toLowerCase()
    const schemaCanModify = schemaPathLower.indexOf("_test") != -1 || schemaPathLower.indexOf("_uat") != -1 || schemaPathLower.indexOf("_dev") != -1 || schemaPathLower.indexOf("_read") != -1
    http.get('/canModifyData').then(resp => {
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

watch([result, totalColumnWidth], () => {
    nextTick(() => onResultScroll())
})

watch(canModify, (can) => {
    if (roleForbidModify.value && can) {
        canModify.value = false
        ElMessage({ message: "当前角色禁止修改数据，请联系管理员开通", type: "warning" })
        return
    }
    const schemaPathLower = schemaPath.toLowerCase()
    if (can && !(schemaPathLower.indexOf("_test") != -1 || schemaPathLower.indexOf("_uat") != -1 || schemaPathLower.indexOf("_dev") != -1 || schemaPathLower.indexOf("_read") != -1)) {
        ElMessage({ message: "当前可能为生产库，请谨慎修改。", type: "error" })
    }
})

function createEditor(editorContainer: any, doc: any) {
    if (editorView.value) {
        editorView.value.destroy();
    }
    if (editorContainer.value) {
        editorContainer.value.innerHTML = '';
    }
    const isDark = currentTheme.value === 'dark'
    const cleanKeymap = standardKeymap.filter(
        k => k.key !== "Mod-z" && k.key !== "Mod-Shift-z" && k.key !== "Mod-y"
    )
    const extensions = [
        keymap.of([
            ...cleanKeymap,
            { key: "Mod-z", run: undo, preventDefault: true },
            { key: "Mod-y", run: redo, preventDefault: true },
            { key: "Mod-Shift-z", run: redo, preventDefault: true },
            { key: 'Tab', run: insertTab, preventDefault: true },
        ]),
        sqlCompartment.of(sql({
            dialect: dbSchemaProxy.getDialect(schema),
            schema: <any>dbSchemaProxy.getAll(schema),
        })),
        history(),
        lineNumbers(),
        highlightActiveLineGutter(),
        autocompletion(),
        EditorView.editable.of(true),
        EditorView.domEventHandlers({
            paste() {
                setTimeout(() => {
                    if (editorView.value) {
                        editorView.value.dispatch({
                            effects: sqlCompartment.reconfigure(sql({
                                dialect: dbSchemaProxy.getDialect(schema),
                                schema: <any>dbSchemaProxy.getAll(schema),
                            }))
                        })
                    }
                }, 50)
            }
        }),
        themeCompartment.of(getEditorTheme()),
        highlightCompartment.of(syntaxHighlighting(isDark ? oneDarkHighlightStyle : lightHighlightStyle)),
    ]
    const startState = EditorState.create({
        doc: doc,
        extensions: extensions,
    });
    editorView.value = new EditorView({
        state: startState,
        parent: editorContainer.value,
    });
}

function reconfigureSql() {
    if (!editorView.value) return
    editorView.value.dispatch({
        effects: sqlCompartment.reconfigure(sql({
            dialect: dbSchemaProxy.getDialect(schema),
            schema: <any>dbSchemaProxy.getAll(schema),
        }))
    })
}

function reconfigureTheme() {
    if (!editorView.value) return
    const isDark = currentTheme.value === 'dark'
    editorView.value.dispatch({
        effects: [
            themeCompartment.reconfigure(getEditorTheme()),
            highlightCompartment.reconfigure(syntaxHighlighting(isDark ? oneDarkHighlightStyle : lightHighlightStyle)),
        ]
    })
}
//获取编辑器里的文本内容
const getEditorDoc = (): string => {
    try {
        return (editorView.value as EditorView)?.state?.doc?.toString() || ""
    } catch {
        return ""
    }
};

function getSqlKey() {
    return "go-web-sql-" + tabId
}

function formatSql() {
    const sql = getSelection()?.toString()
    if (!sql) {
        ElMessage({ message: "请先选择SQL", type: "error" })
        return
    }
    const editorState = <EditorState>editorView.value?.state
    editorView.value?.dispatch(editorState.replaceSelection(format(sql || "", { language: getSqlLang() }) + "\n"))
}

function toggleOptimizePanel() {
    const sqlExec = getSelection()?.toString()
    if (!sqlExec?.trim()) {
        ElMessage({ message: '请先选择要优化的 SQL', type: 'warning' })
        return
    }
    optimizeSql.value = sqlExec
    optimizePanelVisible.value = true
}

function showBackupData(backupId: any) {
    http.get("/showBackupData", { params: { backupId: backupId } })
        .then((resp) => {
            backupData.value = JSON.stringify(JSON.parse(resp.data.data), null, 4)
            backupDataDrawerShow.value = true
        })
}

function exportBackupData() {

    let header: any = {}
    let keys: any = []

    const jsonObj = JSON.parse(backupData.value)
    if (Array.isArray(jsonObj)) {
        keys = Object.keys(jsonObj[0])
    } else {
        keys = Object.keys(jsonObj)
    }

    keys.forEach((key: any) => {
        header[key] = key
    })

    const obj = {
        header: header,
        title: '',
        key: keys,
        data: Array.isArray(jsonObj) ? jsonObj : [jsonObj],
        filename: "导出的备份数据",
        autoWidth: false
    }
    excel.exportJsonToExcel(obj)
}
function extractSqlStatements(sql: string): string[] {
    let cleanSql = sql.replace(/\/\*[\s\S]*?\*\//g, '')
    const lines = cleanSql.split("\n")
    const cleanedLines: string[] = []
    for (const line of lines) {
        const trimmed = line.trim()
        if (trimmed === "" || trimmed.startsWith("--") || trimmed.startsWith("//")) {
            continue
        }
        cleanedLines.push(line.trimEnd())
    }
    cleanSql = cleanedLines.join("\n").trim()
    return cleanSql.split(";")
        .map((s: string) => s.trim())
        .filter((s: string) => s.length > 0)
}

function buildColumnDef(col: any): any {
    return {
        key: col.name,
        title: col.name,
        dataKey: col.name,
        comment: col.comment,
        dataType: col.type,
        width: 150,
        minWidth: 150,
        headerCellRenderer: ({ column }: { column: any }) => {
            return h('div', {
                class: "header-box",
                onDragenter: (e: any) => e.preventDefault(),
                onMouseenter: (e: any) => {
                    const dragLine = e.target.querySelector('.drag-line')
                    if (dragLine) dragLine.style.opacity = '1'
                },
                onMouseleave: (e: any) => {
                    const dragLine = e.target.querySelector('.drag-line')
                    if (dragLine) dragLine.style.opacity = '0'
                }
            }, [
                h('div', {
                    class: "header-text",
                    title: col.comment
                }, col.name),
                h('div', {
                    class: "drag-line",
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
                        transition: 'opacity 0.2s'
                    },
                    onDragstart: (e: any) => dragStart(e),
                    onDragend: (e: any) => dragEnd(e),
                    onMouseenter: (e: any) => {
                        e.target.style.opacity = '1'
                    },
                    onMouseleave: (e: any) => {
                        e.target.style.opacity = '0'
                    }
                })
            ])
        },
        cellRenderer: ({ cellData, rowData, column, rowIndex }: { cellData: any, rowData: any, column: any, rowIndex: number }) => {
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
                            width: '100%', height: '28px', border: '1px solid #409eff',
                            borderRadius: '4px', padding: '0 4px', fontSize: '12px',
                            background: 'var(--bg-secondary)', color: 'var(--text-primary)',
                            boxSizing: 'border-box'
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
                            // Directly set DOM value (bypasses Vue reactivity timing)
                            input.value = newVal
                            // Sync reactive state
                            editingCellValue.value = newVal
                            const newPos = start + text.length
                            input.setSelectionRange(newPos, newPos)
                        },
                        onBlur: () => commitInlineEdit()
                    })
                }
                return h('input', {
                    value: editingCellValue.value,
                    style: {
                        width: '100%', height: '28px', border: '1px solid #409eff',
                        borderRadius: '4px', padding: '0 4px', fontSize: '12px',
                        background: 'var(--bg-secondary)', color: 'var(--text-primary)',
                        boxSizing: 'border-box'
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
                        // Directly set DOM value (bypasses Vue reactivity timing)
                        input.value = newVal
                        // Sync reactive state
                        editingCellValue.value = newVal
                        const newPos = start + text.length
                        input.setSelectionRange(newPos, newPos)
                    },
                    onBlur: () => commitInlineEdit()
                })
            }

            const displayVal = cellData != null ? String(cellData) : ''
            const changedStyle = isChanged ? {
                backgroundColor: 'var(--bg-row-changed, #fff7e6)', padding: '2px 4px',
                borderRadius: '3px', borderBottom: '1px dashed var(--warning-color, #faad14)',
                cursor: 'pointer'
            } : { cursor: 'pointer' }

            return h('span', {
                title: displayVal,
                style: changedStyle,
                onDblclick: (e: MouseEvent) => startInlineEdit(rowIndex, colKey, e)
            }, displayVal)
        }
    }
}

function buildRowNumberColumn(): any {
    return {
        dataKey: "col-idx",
        width: 60,
        fixed: true,
        cellRenderer: ({ cellData, rowIndex }: { cellData: any, rowIndex: number }) => {
            return h('div', {},
                [h('div', { class: "el-table-v2__cell-text", title: cellData }, cellData), h('div', { class: "data-view", onClick: () => openDataDetails(rowIndex) })]
            )
        }
    }
}

function applyResultToUI(data: any) {
    canEdit.value = data.canEdit || false
    tableKeys.value = data.keys || []
    columns.value = (data.columns || []).map((col: any) => buildColumnDef(col))
    columns.value.unshift(buildRowNumberColumn())
    result.value = data.data || []
    result.value.forEach((row: any, idx: number) => {
        row["col-idx"] = idx + 1
    })
}

function displayBatchResult(idx: number) {
    const item = batchResults.value[idx]
    if (!item) return

    if (item.status === 'error') {
        sqlError.value = item.error
        columns.value = []
        result.value = []
        canEdit.value = false
        tableKeys.value = []
        return
    }

    sqlError.value = ''
    currentSelectTable.value = extractTableName(item.sql || '')
    applyResultToUI(item)
    if (item.status === 'rolled_back') {
        canEdit.value = false
    }
}

function displayModifySummary() {
    sqlError.value = ''
    currentSelectTable.value = ''
    const modifyItems = batchResults.value.filter((item: any) => item.type === 'modify')
    if (modifyItems.length === 0) return

    const summaryColumns: any[] = [
        { name: 'SQL', type: 'VARCHAR', comment: '' },
        { name: '状态', type: 'VARCHAR', comment: '' },
        { name: '受影响行数', type: 'BIGINT', comment: '' },
        { name: '备注', type: 'VARCHAR', comment: '' },
    ]

    const summaryData = modifyItems.map((item: any) => {
        const row: any = {
            'SQL': item.sql,
            '状态': item.status === 'success' ? '成功' : item.status === 'rolled_back' ? '已回滚' : '失败',
            '受影响行数': item.affected || 0,
            '备注': item.error || (item.status === 'rolled_back' ? '事务回滚' : ''),
        }
        return row
    })

    canEdit.value = false
    tableKeys.value = []
    columns.value = summaryColumns.map((col: any) => buildColumnDef(col))
    const sqlCol = columns.value.find((c: any) => c.dataKey === 'SQL')
    if (sqlCol) sqlCol.width = 300
    const remarkCol = columns.value.find((c: any) => c.dataKey === '备注')
    if (remarkCol) remarkCol.width = 200
    columns.value.unshift(buildRowNumberColumn())
    result.value = summaryData
    result.value.forEach((row: any, idx: number) => {
        row['col-idx'] = idx + 1
    })
}

function onBatchTabChange(tabName: string | number) {
    const name = String(tabName)
    if (name === 'modify-summary') {
        displayModifySummary()
        return
    }
    const idx = parseInt(name)
    if (!isNaN(idx)) {
        displayBatchResult(idx)
    }
}

function getSqlPreview(sql: string): string {
    if (!sql) return ''
    const firstLine = sql.split('\n')[0].trim()
    if (firstLine.length > 30) {
        return firstLine.substring(0, 30) + '...'
    }
    return firstLine
}

function execBatch(statements: string[]) {
    for (const stmt of statements) {
        if (checkSql(stmt)) {
            return
        }
    }

    isBatchMode.value = true
    exectingSql.value = true
    executionTime.value = null
    sqlError.value = ''
    batchResults.value = []
    activeResultTab.value = '0'
    columns.value = []
    result.value = []

    const startTime = performance.now()
    const fullSql = statements.join(";")

    const params = new URLSearchParams()
    params.append("connId", connId)
    params.append("schema", schema)
    params.append("sql", fullSql)
    params.append("maxLine", maxLine.value)
    params.append("batch", "true")

    abortController = new AbortController()
    http.post("/execSQL", params, { signal: abortController.signal })
        .then((resp) => {
            executionTime.value = Math.round(performance.now() - startTime)
            batchResults.value = resp.data.data.results || []
            if (batchDisplayTabs.value.length > 0) {
                const firstTab = batchDisplayTabs.value[0]
                activeResultTab.value = firstTab.name
                if (firstTab.type === 'modify-summary') {
                    displayModifySummary()
                } else if (firstTab.idx !== undefined) {
                    displayBatchResult(firstTab.idx)
                }
            }
            exectingSql.value = false
            abortController = null
        }).catch((error) => {
            if (axios.isCancel(error)) {
                sqlError.value = '执行已终止'
            } else {
                sqlError.value = error.message || '执行失败'
            }
            columns.value = []
            result.value = []
            exectingSql.value = false
            abortController = null
        })
}

function exec(silent = false) {
    const sqlExec = getSelection()?.toString()
    if (!sqlExec) {
        if (!silent) ElMessage({ message: "请先选择SQL", type: "error" })
        return
    }

    const statements = extractSqlStatements(sqlExec)
    if (statements.length > 1) {
        execBatch(statements)
        return
    }

    const effiectiveSql = extractEffectiveSql(sqlExec)
    if (checkSql(effiectiveSql)) {
        return
    }
    currentSelectTable.value = extractTableName(sqlExec)
    isBatchMode.value = false
    batchResults.value = []
    exectingSql.value = true
    executionTime.value = null
    sqlError.value = ''
    const startTime = performance.now()
    const params = new URLSearchParams()
    params.append("connId", connId)
    params.append("schema", schema)
    params.append("tableName", currentSelectTable.value)
    params.append("sql", effiectiveSql)
    params.append("maxLine", maxLine.value)
    abortController = new AbortController()
    http.post("/execSQL", params, { signal: abortController.signal })
        .then((resp) => {
            executionTime.value = Math.round(performance.now() - startTime)
            applyResultToUI(resp.data.data)
            exectingSql.value = false
            abortController = null
        }).catch((error) => {
            if (axios.isCancel(error)) {
                sqlError.value = '执行已终止'
            } else {
                sqlError.value = error.message || '执行失败'
            }
            columns.value = []
            result.value = []
            exectingSql.value = false
            abortController = null
        })
}

function stopExec() {
    if (abortController) {
        abortController.abort()
        abortController = null
    }
    exectingSql.value = false
    sqlError.value = '执行已终止'
    columns.value = []
    result.value = []
}

function isDateType(colType: string | undefined): boolean {
    if (!colType) return false
    const upper = colType.toUpperCase()
    return upper === 'DATETIME' || upper === 'DATE' || upper === 'TIMESTAMP'
        || upper === 'TIMESTAMP(6)' || upper.includes('TIMESTAMP')
        || upper === 'TIMESTAMPTZ' || upper === 'TIMESTAMPLTZ'
}

function startInlineEdit(rowIndex: number, colKey: string, event: MouseEvent) {
    if (!canInlineEdit.value) return
    if (tableKeys.value.length === 0) return
    const target = event.target as HTMLElement
    if (target.tagName === 'INPUT') return
    editingCellRow.value = rowIndex
    editingCellCol.value = colKey
    const rawVal = result.value[rowIndex]?.[colKey]
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
    const oldVal = result.value[rowIdx]?.[colKey]
    if (String(oldVal ?? '') !== newVal) {
        const changeKey = rowIdx + '::' + colKey
        const newMap = new Map(inlineChanges.value)
        newMap.set(changeKey, newVal)
        inlineChanges.value = newMap
        result.value[rowIdx][colKey] = newVal
    }
    cancelInlineEdit()
}

function cancelInlineEdit() {
    editingCellRow.value = -1
    editingCellCol.value = ''
    editingCellValue.value = ''
}

function handlePaste2(event: ClipboardEvent) {
    const text = event.clipboardData?.getData('text/plain')
    if (!text) return

    let startRowIdx = -1
    let startColIdx = -1

    const colKeys = columns.value.map((c: any) => c.dataKey)

    if (editingCellRow.value >= 0 && editingCellCol.value) {
        startRowIdx = editingCellRow.value
        startColIdx = colKeys.indexOf(editingCellCol.value)
    } else if (activeCellRow2.value >= 0 && activeCellCol2.value) {
        startRowIdx = activeCellRow2.value
        startColIdx = colKeys.indexOf(activeCellCol2.value)
    }

    if (startRowIdx < 0 || startColIdx < 0) return

    const lines = text.split('\n')
    const grid: string[][] = []
    for (const line of lines) {
        const trimmed = line.trim()
        if (trimmed) {
            grid.push(trimmed.split('\t'))
        }
    }
    if (grid.length === 0) return

    event.preventDefault()

    // Save snapshot for Ctrl+Z undo
    const snapshot: any = {
        inlineChanges: new Map(inlineChanges.value),
        restoredCells: [] as { rowIdx: number; colKey: string; oldVal: any }[]
    }

    cancelInlineEdit()

    for (let ri = 0; ri < grid.length; ri++) {
        const targetRowIdx = startRowIdx + ri
        if (targetRowIdx >= result.value.length) break
        const targetRow = result.value[targetRowIdx]

        for (let ci = 0; ci < grid[ri].length; ci++) {
            const targetColIdx = startColIdx + ci
            if (targetColIdx >= colKeys.length) break
            const colKey = colKeys[targetColIdx]
            if (tableKeys.value.includes(colKey)) continue

            const newVal = grid[ri][ci].trim()

            // Record old value for undo
            const changeKey = targetRowIdx + '::' + colKey
            const oldChanged = inlineChanges.value.get(changeKey)
            const oldVal = oldChanged !== undefined ? oldChanged : targetRow[colKey]
            snapshot.restoredCells.push({ rowIdx: targetRowIdx, colKey, oldVal })

            if (String(targetRow[colKey] ?? '') !== newVal) {
                const newMap = new Map(inlineChanges.value)
                newMap.set(changeKey, newVal)
                inlineChanges.value = newMap
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
}

function undoPaste2() {
    const snapshot = pasteSnapshot2.value
    if (!snapshot) return

    inlineChanges.value = new Map(snapshot.inlineChanges)

    for (const cell of snapshot.restoredCells) {
        const { rowIdx, colKey, oldVal } = cell
        if (rowIdx < result.value.length) {
            result.value[rowIdx][colKey] = oldVal
        }
    }

    pasteSnapshot2.value = null
}

function isEditingCell(rowIndex: number, colKey: string) {
    return editingCellRow.value === rowIndex && editingCellCol.value === colKey
}

function isCellChanged(rowIndex: number, colKey: string) {
    const changeKey = rowIndex + '::' + colKey
    return inlineChanges.value.has(changeKey)
}

function discardInlineChanges() {
    const changes = inlineChanges.value
    changes.forEach((newVal, key) => {
        const [rowStr, colKey] = key.split('::')
        const rowIdx = parseInt(rowStr)
        if (result.value[rowIdx]) {
            // Restore use original value; we don't have it stored separately so keep the edit
        }
    })
    inlineChanges.value = new Map()
    result.value = [...result.value]
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
        const row = result.value[rowIdx]
        if (!row) return

        const pkConditions = tableKeys.value
            .filter(k => k in row)
            .map(k => k + ' = ' + fmtVal(row[k]))

        const setClauses: string[] = []
        colMap.forEach((newVal, colKey) => {
            setClauses.push(colKey + ' = ' + fmtVal(newVal))
        })

        if (setClauses.length === 0) return

        let sql: string
        if (pkConditions.length > 0 && canEdit.value) {
            sql = 'update ' + currentSelectTable.value + ' set ' + setClauses.join(', ') + ' where ' + pkConditions.join(' and ')
        } else {
            const allWhereCols = Object.keys(row)
                .filter((k: string) => k !== 'col-idx' && colMap.has(k))
            const whereConditions = allWhereCols.map((k: string) => k + ' = ' + fmtVal(row[k]))
            sql = 'update ' + currentSelectTable.value + ' set ' + setClauses.join(', ') + ' where ' + whereConditions.join(' and ')
        }

        const params = new URLSearchParams()
        params.append('connId', connId)
        params.append('schema', schema)
        params.append('tableName', currentSelectTable.value)
        params.append('sql', sql)
        promises.push(http.post('/execSQL', params))
    })

    Promise.all(promises)
        .then(() => {
            ElMessage.success('已保存成功')
            inlineChanges.value = new Map()
            exec(true)
        })
        .catch((err) => {
            console.error(err)
            ElMessage.error('保存失败')
        })
        .finally(() => {
            savingInline.value = false
        })
}

const inlineChangeCount = computed(() => inlineChanges.value.size)

const canInlineEdit = computed(() => canModify.value && canEdit.value && tableKeys.value.length > 0)

function openDataDetails(rowIndex: number) {
    dataDetailsDialogVisible.value = true
    rowData.value = result.value[rowIndex]
    originRowData = JSON.parse(JSON.stringify(result.value[rowIndex]))
}

function saveData(rowData: any) {
    const changedKeys = Object.keys(originRowData).filter((key) => originRowData[key] != rowData[key])

    if (changedKeys.length === 0 && canEdit.value) {
        ElMessage({ message: "数据未修改", type: "warning" })
        return
    }

    const updateColumnSets = changedKeys.map((key) => key + " = " + fmtVal(rowData[key]))

    const allWhereCols = [
        ...tableKeys.value,
        ...changedKeys.filter((k: string) => !tableKeys.value.includes(k))
    ]
    const whereColumns = allWhereCols.map((key: string) => key + " = " + fmtVal(originRowData[key]))

    let effiectiveSql = "update " + currentSelectTable.value + " set "
    effiectiveSql += updateColumnSets.join(", ") + " where "
    effiectiveSql += whereColumns.join(" and ")

    onDataSaving.value = true

    const params = new URLSearchParams()
    params.append("connId", connId)
    params.append("schema", schema)
    params.append("tableName", currentSelectTable.value)
    params.append("sql", effiectiveSql)

    http.post("/execSQL", params)
        .then((resp) => {
            onDataSaving.value = false
            if (!resp.data.data.msg) {
                dataDetailsDialogVisible.value = false
            }
            const respConlumn = resp.data.data.columns[0].name
            const respData = resp.data.data.data[0]
            ElMessage({ message: resp.data.data.msg ? "操作失败，请检查 SQL 语句" : "修改了 " + respData[respConlumn] + " 条数据", type: resp.data.data.msg ? "error" : "success" })
        }).catch((error) => {
            console.log(error);
        });
}

function extractEffectiveSql(sql: string) {
    let relSql = sql.trimStart()
    const sqlArr = relSql.split("\n")
    const nsql: string[] = []
    for (let i = 0; i < sqlArr.length; i++) {
        let row = sqlArr[i].trim()
        if (row === "" || row.startsWith("--") || row.startsWith("//") || row.startsWith("/*")) {
            continue
        }
        row = row.trimEnd()
        if (row.endsWith(";")) {
            nsql.push(row.substring(0, row.length - 1))
        } else {
            nsql.push(row)
        }
    }
    relSql = nsql.join("\n")

    relSql = relSql.trimEnd()
    if (relSql.endsWith(";")) {
        return relSql.substring(0, relSql.length - 1)
    }

    return relSql
}

function fillSchema(relSql: string, sqlResult: string, schema: string, searchStart: number, concatStart: number): string {
    if (searchStart >= relSql.length) {
        return sqlResult
    }
    const idxTableNameBegin = relSql.substring(searchStart).search(/\s*\w+/)
    const idxTableNameEnd = relSql.substring(searchStart + idxTableNameBegin).search(/\w+\s*/)
    let tableName_ = idxTableNameBegin === idxTableNameEnd ? relSql.substring(searchStart) : relSql.substring(idxTableNameEnd, idxTableNameEnd - idxTableNameBegin)

    tableName_ = tableName_.split(",").map(name => {
        if (name.includes(".")) {
            return name
        }
        const name_ = name.split(" ")
        name_.splice(name.search(/\w+/), 0, schema, ".")
        return name_.join(" ")
    }).join(",")

    const idxEnd = searchStart + idxTableNameBegin + tableName_.length
    const finalSql = relSql.substring(concatStart, searchStart + idxTableNameBegin) + tableName_ + relSql.substring(idxEnd)
    return fillSchema(relSql, finalSql, schema, idxEnd, idxEnd)
}

function checkSql(sql: string) {
    let hasInvalid = false
    const sqlArr = sql.split(";")
    for (let i = 0; i < sqlArr.length; i++) {
        const sqlLowerCase = sqlArr[i].toLowerCase().trimStart()
        if (!canModify.value && (sqlLowerCase.startsWith("update ") || sqlLowerCase.startsWith("delete ") || sqlLowerCase.startsWith("alter ") || sqlLowerCase.startsWith("insert ") || sqlLowerCase.startsWith("drop ") || sqlLowerCase.startsWith("create ") || sqlLowerCase.startsWith("truncate "))) {
            ElMessage.warning("当前模式不允许修改")
            hasInvalid = true
            break
        }
        if ((sqlLowerCase.startsWith("update ") || sqlLowerCase.startsWith("delete ")) && sqlLowerCase.indexOf(" where ") === -1) {
            hasInvalid = true
            ElMessage.warning("请明确 where 条件")
            break
        }
    }
    return hasInvalid
}

function extractTableName(sqlExec: string) {
    let currentSelectTable = ""
    if (sqlExec.trim().startsWith("select ") || sqlExec.trim().startsWith("select\n")) {
        let fromIdx = sqlExec.toLowerCase().indexOf(" from ") + 6
        if (fromIdx === 5) {
            fromIdx = sqlExec.toLowerCase().indexOf("\nfrom\n") + 6
        }
        const tableNameArr = []
        for (let i = fromIdx; i < sqlExec.length; i++) {
            if (sqlExec.charAt(i) != ' ' && sqlExec.charAt(i) != '\n') {
                tableNameArr.push(sqlExec.charAt(i))
            }
            else if (tableNameArr.length !== 0 && (sqlExec.charAt(i) === ' ' || sqlExec.charAt(i) === '\n')) {
                break
            }
        }
        currentSelectTable = tableNameArr.join("")
    }
    return currentSelectTable
}

function handleExportResult(command: string) {
    if (result.value.length === 0) {
        ElMessage({ message: "请先执行查询", type: "warning" })
        return
    }

    if (command === 'insert') {
        exportCurrentToSqlInsert()
    } else if (command === 'update') {
        exportCurrentToSqlUpdate()
    } else if (command === 'xlsx') {
        exportCurrentToXlsx()
    } else if (command === 'csv') {
        const cols = columns.value.slice(1).map((col: any) => col.title)
        exportToCsv(cols, result.value, currentSelectTable.value || 'query_result')
        ElMessage({ message: '已导出 CSV', type: 'success' })
    } else if (command === 'json') {
        exportToJson(result.value, currentSelectTable.value || 'query_result')
        ElMessage({ message: '已导出 JSON', type: 'success' })
    }
}

function exportCurrentToXlsx() {

    if (result.value.length === 0) {
        ElMessage({ message: "请先执行查询，在导出", type: "warning" })
        return
    }

    let header: any = {}
    let keys: any = []
    columns.value.forEach((col: any, idx: number) => {
        if (idx > 0) {
            keys.push(col["title"])
            header[col["title"]] = col["title"]
        }
    })

    const obj = {
        header: header,
        title: '',
        key: keys,
        data: [...result.value].map((row) => {
            delete row["col-idx"]
            return row
        }),
        filename: currentSelectTable.value,
        autoWidth: false
    }
    excel.exportJsonToExcel(obj)
}

function exportCurrentToSqlInsert() {
    if (result.value.length === 0) {
        ElMessage({ message: "请先执行查询，在导出SQL", type: "warning" })
        return
    }
    let sqlArr = []
    const columnArr: any = []
    let sql = `insert into ${currentSelectTable.value} (`
    for (let i = 1; i < columns.value.length; i++) {
        columnArr.push(columns.value[i]["key"])
    }
    for (let j = 0; j < result.value.length; j++) {
        let rowVal = []
        let valueArr = []
        for (let i = 1; i < columns.value.length; i++) {
            let val = result.value[j][columns.value[i]["key"]]
            rowVal.push(fmtVal(val))
        }
        valueArr.push(rowVal.join(","))
        sqlArr.push(sql + columnArr.join(",") + ") values (" + valueArr.join(",") + ")")
    }

    copyToClipboard(sqlArr.length > 0 ? sqlArr.join(";\n") + ";" : "",
        () => ElMessage({ message: "已复制到粘贴板", type: "success" }),
        () => ElMessage({ message: "导出失败", type: "error" })
    )
}

function exportCurrentToSqlUpdate() {
    if (result.value.length === 0) {
        ElMessage({ message: "请先执行查询，在导出SQL", type: "warning" })
        return
    }
    let sqlArr = []
    let sql = `update ${currentSelectTable.value} set `
    for (let j = 0; j < result.value.length; j++) {
        let rowVal = []
        for (let i = 2; i < columns.value.length; i++) {
            let column = columns.value[i]["key"]
            let val = result.value[j][column]
            rowVal.push(column + " = " + fmtVal(val))
        }

        let conditionVal = []
        for (let i = 0; i < tableKeys.value.length; i++) {
            conditionVal.push(tableKeys.value[i] + " = " + fmtVal(result.value[j][tableKeys.value[i]]))
        }

        sqlArr.push(sql + rowVal.join(", ") + " where " + conditionVal.join(" and "))
    }

    copyToClipboard(sqlArr.length > 0 ? format(sqlArr.join(";\n") + ";", { language: getSqlLang() }) : "",
        () => ElMessage({ message: "已复制到粘贴板", type: "success" }),
        () => ElMessage({ message: "导出失败", type: "error" })
    )
}

function getSqlLang(): SqlLanguage {
    return getSqlDialect(dbSchemaProxy.getDbType(schema) || '')
}

let saveTimer: ReturnType<typeof setTimeout> | null = null

function onKeyup(e: KeyboardEvent) {
    onEditorKeyup(e)
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
        try {
            localStorage.setItem(getSqlKey(), getEditorDoc())
        } catch (e) {
            // localStorage may be full, silently ignore
        }
    }, 500)
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
    while (from > 0 && /[a-zA-Z0-9_$#]/.test(doc.sliceString(from - 1, from))) {
        from--
    }
    while (to < doc.length && /[a-zA-Z0-9_$#]/.test(doc.sliceString(to, to + 1))) {
        to++
    }
    const wordText = state.sliceDoc(from, to)
    const tables = tableList.value
    if (tables.some((t: string) => t.toLowerCase() === wordText.toLowerCase())) {
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
        if (tableNameUnderCursor.value) {
            tableNameUnderCursor.value = ''
        }
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
    emit('openDataBrowser', {
        connId: connId,
        schema: schema,
        tableName: tableName,
        dbType: dbType,
    })
}

function onResultDivResize(index: number, sizes: number[]) {
    nextTick(() => {
        if (sqlAreaRef.value) {
            sqlAreaRef.value.style.height = (sizes[index] - 25) + "px"
            sqlAreaRef.value.style.setProperty('height', (sizes[index] - 25) + "px", 'important')
        }
    })

    console.log(index, JSON.stringify(sizes))
}

function openTableManager() {
    emit('openTableManager', {
        connId: connId,
        schema: schema,
        schemaPath: schemaPath
    })
}

// 拖拽改变列宽相关逻辑
let x1: number, currentDraggingColumn: string | null = null
let originalWidth: number = 0
let dragLineElement: HTMLElement | null = null
let columnItemRef: any = null // 直接保存列对象的引用
let lastDragTime = 0 // 节流控制

const dragStart = (e: DragEvent) => {
    x1 = e.clientX
    lastDragTime = 0 // 重置节流计时器
    dragLineElement = e.target as HTMLElement

    // 直接从事件目标获取列信息，避免 DOM 查询延迟
    const headerBox = (e.target as HTMLElement).parentElement as HTMLElement
    if (headerBox && headerBox.classList.contains('header-box')) {
        const headerText = headerBox.querySelector('.header-text')
        if (headerText) {
            const colName = headerText.textContent?.trim()
            const columnItem = columns.value.find((item: any) => item.dataKey === colName)
            if (columnItem) {
                currentDraggingColumn = columnItem.dataKey
                originalWidth = columnItem.width || 150
                columnItemRef = columnItem // 直接保存引用，避免重复查找

                // 设置拖动标识为拖动中状态
                dragLineElement.style.opacity = '1'
                dragLineElement.style.backgroundColor = 'rgba(64, 158, 255, 0.3)'
            }
        }
    }

    // 必须设置 drag effect 才能触发 dragover 事件
    e.dataTransfer!.effectAllowed = 'move'
    e.dataTransfer!.setData('text/plain', '')

    // 添加全局拖动监听
    document.addEventListener('dragover', handleGlobalDragOver, { passive: false })
    document.addEventListener('dragend', handleGlobalDragEnd)
}

const handleGlobalDragOver = (e: DragEvent) => {
    if (!currentDraggingColumn || !columnItemRef) return
    e.preventDefault()

    // 节流：限制更新频率（每 16ms 约 60fps）
    const now = Date.now()
    if (now - lastDragTime < 8) return // 限制为 125fps，平衡性能和流畅度
    lastDragTime = now

    const deltaX = e.clientX - x1
    const newWidth = Math.max(50, originalWidth + deltaX)

    // 直接修改引用对象的属性
    columnItemRef.width = newWidth
}

const handleGlobalDragEnd = (e: DragEvent) => {
    if (dragLineElement) {
        dragLineElement.style.opacity = '0'
        dragLineElement.style.backgroundColor = 'transparent'
        dragLineElement = null
    }
    currentDraggingColumn = null
    columnItemRef = null

    // 移除全局监听
    document.removeEventListener('dragover', handleGlobalDragOver)
    document.removeEventListener('dragend', handleGlobalDragEnd)
}

const dragEnd = (e: DragEvent) => {
    // 清理工作由 handleGlobalDragEnd 处理
}

</script>
<style>
.sql-editor-panel {
    height: calc(100vh - 38px);
}

/* ── Toolbar ── */
.sql-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    background: var(--bg-toolbar);
    border-bottom: 1px solid var(--border-primary);
    gap: 4px;
}

.sql-toolbar .toolbar-left {
    display: flex;
    align-items: center;
    gap: 4px;
}

.sql-toolbar .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
}

.sql-toolbar .el-button {
    height: 28px;
    padding: 0 10px;
    font-size: 13px;
    border-radius: 6px;
}

.sql-toolbar .el-divider--vertical {
    margin: 0 4px;
    height: 16px;
}

.modify-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
}

.modify-label {
    font-size: 12px;
    color: var(--text-secondary);
    user-select: none;
}

.max-rows-label {
    font-size: 12px;
    color: var(--text-tertiary);
    white-space: nowrap;
}

.exec-time {
    font-size: 12px;
    color: #67c23a;
    font-weight: 500;
    white-space: nowrap;
    margin-right: 4px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
}

.sql-exec-error {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding: 40px 20px;
    text-align: center;
}

.sql-exec-error-title {
    font-size: 18px;
    font-weight: 600;
    color: #f56c6c;
    margin-bottom: 16px;
}

.sql-exec-error-body {
    font-size: 13px;
    color: var(--text-secondary, #909399);
    max-width: 600px;
    word-break: break-all;
    line-height: 1.6;
    white-space: pre-wrap;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
}

[data-theme="dark"] .sql-exec-error-title {
    color: #f89898;
}

[data-theme="dark"] .sql-exec-error-body {
    color: #b0b0b0;
}

.drawer-drag-handle {
    position: fixed;
    top: 0;
    bottom: 0;
    width: 6px;
    cursor: ew-resize;
    z-index: 3000;
    background: transparent;
    transition: background 0.2s;
}

.drawer-drag-handle:hover {
    background: rgba(64, 158, 255, 0.3);
}

.sql-history-text {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
    color: #409eff;
}

.sql-history-tooltip {
    max-width: 500px !important;
    word-break: break-all;
    white-space: pre-wrap;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    line-height: 1.5;
}

.inline-edit-badge {
    font-size: 12px;
    color: #409eff;
    font-weight: 500;
    white-space: nowrap;
    margin-right: 4px;
    cursor: default;
}

/* ── SQL Editor Area ── */
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

/* ── Result Table ── */
.el-table-v2__header-cell-text {
    user-select: text;
}

.el-table-v2__main {
    overflow: visible !important;
}

[data-theme="dark"] .el-scrollbar__thumb {
    background-color: rgba(121, 121, 121, 0.4) !important;
}

[data-theme="dark"] .el-scrollbar__thumb:hover {
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

    .header-text {
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

    .drag-line {
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

        &:hover {
            opacity: 1;
            border-right-color: var(--accent-color);
            background: rgba(0, 122, 204, 0.08);
        }
    }
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

.el-table-v2 {
    overflow: visible !important;
}

.header-box {
    overflow: visible !important;
}

.el-drawer__header {
    margin-bottom: -20px;
}

/* ── Data Details Dialog ── */
.el-dialog .el-form-item {
    margin-bottom: 12px;
}

/* ── Inline Edit Bar ── */
.db-inline-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    background: var(--bg-row-changed);
    border-top: 1px solid var(--border-primary);
    border-bottom: 1px solid var(--border-primary);
    margin-top: -1px;
}

.db-inline-bar .el-button {
    height: 26px;
    padding: 0 8px;
    font-size: 12px;
}

.inline-count {
    font-size: 12px;
    color: var(--warning-color);
    margin-left: 4px;
}

[data-theme="dark"] .db-inline-bar {
    background: var(--bg-row-changed);
    border-top: 1px solid var(--border-secondary);
    border-bottom: 1px solid var(--border-secondary);
}

[data-theme="dark"] .inline-count {
    color: var(--warning-color);
}

/* ── Table Structure Dialog ── */
.table-structure-dialog {
    height: 750px !important;
}

.table-structure-dialog .el-dialog__body {
    display: flex;
    flex-direction: column;
    height: calc(100% - 54px);
    overflow: hidden;
    padding: 0 20px 20px;
}

.table-structure-dialog .dialog-toolbar {
    flex-shrink: 0;
    padding: 12px 0 8px;
}

.table-structure-dialog .dialog-scroll-body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
}

/* ── Data Details Dialog ── */
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

.batch-tabs {
    flex-shrink: 0;
    border-bottom: none;
}

.batch-tabs .el-tabs__header {
    margin-bottom: 0;
}

.batch-tabs .el-tabs__content {
    display: none;
}

.batch-tabs .el-tabs__item {
    padding: 0 12px;
    height: 32px;
    line-height: 32px;
    font-size: 12px;
}

.batch-tab-label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
}

.batch-tab-index {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--el-color-primary-light-8);
    color: var(--el-color-primary);
    font-size: 11px;
    font-weight: 600;
    flex-shrink: 0;
}

.batch-tab-error .batch-tab-index {
    background: var(--el-color-danger-light-8);
    color: var(--el-color-danger);
}

.batch-tab-sql {
    white-space: nowrap;
    font-size: 12px;
    flex-shrink: 0;
}

.batch-tab-error .batch-tab-sql {
    color: var(--el-color-danger);
}

.batch-tab-rolled-back .batch-tab-index {
    background: var(--el-color-info-light-8);
    color: var(--el-color-info);
}

.batch-tab-rolled-back .batch-tab-sql {
    color: var(--el-color-info);
    text-decoration: line-through;
}

.batch-tab-modify .batch-tab-index {
    background: var(--el-color-warning-light-8);
    color: var(--el-color-warning);
    font-size: 10px;
    font-weight: 700;
}
</style>
<style lang="less" scoped></style>
