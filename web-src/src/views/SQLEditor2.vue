<template>
    <div class="sql-editor-panel" @keyup.f9="exec" @keyup.ctrl.shift.f="formatSql">
        <el-splitter layout="vertical" @resize="onResultDivResize">

            <div class="sql-toolbar">
                <div class="toolbar-left">
                    <el-button type="primary" @click="exec" :loading="exectingSql" title="F9">
                        <el-icon style="margin-right: 4px;"><VideoPlay /></el-icon>执行
                    </el-button>
                    <el-divider direction="vertical" />
                    <el-button @click="formatSql" title="Ctrl + Shift + F">美化</el-button>
                    <el-button @click="explainSql" title="查看执行计划">EXPLAIN</el-button>
                    <el-divider direction="vertical" />
                    <el-dropdown @command="handleExportResult">
                      <el-button :disabled="result.length === 0">
                        导出结果<el-icon class="el-icon--right"><ArrowDown /></el-icon>
                      </el-button>
                      <template #dropdown>
                        <el-dropdown-menu>
                          <el-dropdown-item command="insert">INSERT SQL</el-dropdown-item>
                          <el-dropdown-item command="update">UPDATE SQL</el-dropdown-item>
                          <el-dropdown-item command="xlsx" divided>Excel (.xlsx)</el-dropdown-item>
                          <el-dropdown-item command="csv">CSV</el-dropdown-item>
                          <el-dropdown-item command="json">JSON</el-dropdown-item>
                        </el-dropdown-menu>
                      </template>
                    </el-dropdown>
                    <el-button @click="tableCreateDialogVisible = true">建表语句</el-button>
                    <el-divider direction="vertical" />
                    <el-button @click="listBackupData">备份</el-button>
                    <el-button @click="openTableManager">表管理</el-button>
                    <el-button @click="showSqlHistory">历史</el-button>
                    <el-button @click="snippetVisible = true" title="SQL 收藏夹">收藏</el-button>
                    <el-button @click="queryBuilderVisible = true" title="可视化查询构建器">查询构建</el-button>
                    <el-upload
                      :show-file-list="false"
                      accept=".sql"
                      :http-request="handleSqlFile"
                    >
                      <el-button title="执行 SQL 文件">执行文件</el-button>
                    </el-upload>
                    <el-divider v-if="canModify" direction="vertical" />
                    <template v-if="canModify">
                      <el-button size="small" @click="execTransaction('BEGIN')" :disabled="exectingSql" style="color: #67c23a;">BEGIN</el-button>
                      <el-button size="small" @click="execTransaction('COMMIT')" :disabled="exectingSql" style="color: #409eff;">COMMIT</el-button>
                      <el-button size="small" @click="execTransaction('ROLLBACK')" :disabled="exectingSql" style="color: #f56c6c;">ROLLBACK</el-button>
                    </template>
                </div>
                <div class="toolbar-right">
                    <span v-if="executionTime !== null" class="exec-time">{{ executionTime }}ms</span>
                    <span v-if="canInlineEdit && result.length > 0" class="inline-edit-badge" title="当前结果集有主键，支持双击单元格内联编辑">✎ 可编辑</span>
                    <el-tooltip :content="canModify ? '当前允许修改数据，点击切换为只读' : '当前为只读模式，点击允许修改数据'" placement="bottom" :show-after="400">
                        <label class="modify-toggle">
                            <el-switch v-model="canModify" size="small" />
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
                    <div ref="codemirror" class="codemirror" @keyup="onKeyup"></div>
                </div>
            </el-splitter-panel>
            <el-splitter-panel size="45%">
                <el-auto-resizer>
                    <template #default="{ height: autoHeight, width: autoWidth }">
                        <div :style="{ height: autoHeight + 'px', overflowX: 'auto', overflowY: 'hidden' }">
                            <el-table-v2 
                    :columns="columns" 
                    :data="result" 
                    :width="totalColumnWidth" 
                    :height="autoHeight" 
                    :row-height="35" />
                <div v-if="canInlineEdit && inlineChangeCount > 0" class="db-inline-bar">
                    <el-button type="warning" size="small" @click="saveInlineChanges" :loading="savingInline">
                        <span>保存更改</span>
                    </el-button>
                    <el-button size="small" @click="exec(); inlineChanges.clear()">
                        <span>放弃更改</span>
                    </el-button>
                    <span class="inline-count">{{ inlineChangeCount }} 处更改</span>
                </div>
                        </div>
                    </template>
                </el-auto-resizer>
            </el-splitter-panel>
        </el-splitter>
    </div>
    <el-dialog v-model="exportDialogVisible" title="导表" width="60%" center :draggable="true" :destroyOnClose="true">
        <DBExport :connId="props.connId" :schema="props.schema" opt="insert" :canImport="canModify"/>
    </el-dialog>
    <el-dialog v-model="tableCreateDialogVisible" @close="tableCreateDialogVisible = false" :draggable="true"
        destroy-on-close width="1000px" style="height:650px;overflow-y: auto;">
        <div>
            <el-switch v-model="isTable" class="ml-2" inline-prompt size="large"
                style="--el-switch-on-color: #13ce66; --el-switch-off-color: #409eff;margin-right: 10px;"
                active-text="表" inactive-text="视图" />
            <el-input v-model="tableName" @keyup.enter="showCreateScript" style="width: 300px;" />
            <el-button @click="showCreateScript" style="margin-left:12px;" size="small">查看</el-button>
        </div>
        <div>
            <TableEditor v-if="isTable" :tableMeta="tableMeta" />
            <ViewDialog v-else :tableMeta="tableMeta" />
        </div>
    </el-dialog>
    <el-dialog v-model="backupDataDialogVisible" :draggable="true" title="自动备份的数据" width="1000px"
        style="height:650px;overflow-y: auto;">
        <el-table :data="backupDataList" stripe style="width: 100%;" :max-height="520">
            <el-table-column type="index" width="50" />
            <el-table-column prop="exec_time" label="操作时间" width="170" />
            <el-table-column prop="exec_sql" label="SQL" show-overflow-tooltip />
            <el-table-column label="" width="38">
                <template #default="scope">
                    <el-icon style="cursor: pointer;" @click="showBackupData(scope.row.id)">
                        <View />
                    </el-icon>
                </template>
            </el-table-column>
        </el-table>
        <div style="position: absolute;right: 10px;bottom: 5px;">
            <el-pagination layout="prev, pager, next" v-model:total="backupDataTotal" v-model:page-size="backupDataSize"
                v-model:current-page="backupDataCurrent" @current-change="listBackupData" />
        </div>
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
    <el-drawer v-model="sqlHistoryDrawerShow" title="SQL 执行历史" size="520px">
        <div style="margin-bottom: 12px;">
            <el-input v-model="sqlHistorySearch" placeholder="搜索 SQL..." clearable size="small" />
        </div>
        <el-table :data="filteredSqlHistory" stripe size="small" style="width: 100%;" max-height="calc(100vh - 180px)">
            <el-table-column prop="exec_time" label="时间" width="160" />
            <el-table-column prop="exec_sql" label="SQL" show-overflow-tooltip>
                <template #default="scope">
                    <span style="cursor: pointer; color: #409eff;" @click="applySqlFromHistory(scope.row.exec_sql)" title="点击填入编辑器">
                        {{ scope.row.exec_sql }}
                    </span>
                </template>
            </el-table-column>
        </el-table>
    </el-drawer>
    <el-dialog v-model="dataDetailsDialogVisible" :draggable="true" :title="currentSelectTable" width="1000px"
        style="height:650px;overflow-y: auto;">
        <div style="height: 530px;overflow-y: auto;">
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
                <el-button v-if="canModify && canEdit" type="primary" :loading="onDataSaving" @click="saveData(rowData)">
                    保存
                </el-button>
                <el-button v-if="!(canModify && canEdit)" type="primary" @click="dataDetailsDialogVisible = false">
                    关闭
                </el-button>
            </div>
        </template>
    </el-dialog>

  <SqlSnippetManager
    v-model="snippetVisible"
    :current-sql="getEditorDoc()"
    @apply="onApplySnippet"
  />

  <QueryBuilderDialog
    v-model="queryBuilderVisible"
    :conn-id="props.connId"
    :schema="props.schema"
    @execute="onQueryBuilderExecute"
    @insert="onQueryBuilderInsert"
  />
</template>

<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view'
import { oneDarkHighlightStyle } from "@codemirror/theme-one-dark"
import { EditorState } from '@codemirror/state'
import { standardKeymap, insertTab, history, redo, undo } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql';
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { autocompletion } from '@codemirror/autocomplete'
import { tags } from '@lezer/highlight'
import { ref, onMounted, watch, h, nextTick, computed } from 'vue'
import { dbSchemaProxy } from '../stores/sql'
import { ElMessage } from 'element-plus'
import { format, type SqlLanguage } from 'sql-formatter'
import DBExport from './DBExport.vue'
import TableEditor from './comonents/TableEditor.vue'
import ViewDialog from './comonents/ViewDialog.vue'
import SqlSnippetManager from '../components/SqlSnippetManager.vue'
import QueryBuilderDialog from '../components/QueryBuilderDialog.vue'

import hljs from 'highlight.js/lib/core'
import * as highlightSql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/stackoverflow-light.css'

import http from '../js/utils/httpProxy.js'
import excel from '../js/utils/excel.js'
import copyToClipboard from '../js/utils/copy-to-clipboard.js'
import { fmtVal, getSqlDialect } from '../js/utils/sqlHelper.ts'
import { exportToCsv, exportToJson } from '../js/utils/exportHelper.ts'
import { useTheme } from '../js/utils/useTheme.ts'

hljs.registerLanguage('sql', highlightSql.default);

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

const props = defineProps<{
    tabId: string,
    connId: string,
    schema: string,
    schemaPath: string,
}>()

const emit = defineEmits(['openTableManager', 'openDataBrowser'])

const sqlAreaRef:any = ref(null)

const maxLine = ref("15")
const columns: any = ref([])
const result: any = ref([])
const editorView = ref<EditorView>()
const codemirror = ref()
const exportDialogVisible = ref(false)

const exectingSql = ref(false)
const executionTime = ref<number | null>(null)
const snippetVisible = ref(false)
const queryBuilderVisible = ref(false)
const currentSelectTable = ref("")

const tableName = ref("")
const tableCreateDdlRef = ref()
const tableCreateDialogVisible = ref(false)

const isTable = ref(true)
const tableMeta = ref({})

const backupDataList = ref([])
const backupDataTotal = ref(0)
const backupDataCurrent = ref(0)
const backupDataSize = ref(12)
const backupDataDialogVisible = ref(false)

const canEdit = ref(false)
const tableKeys = ref([] as string[])
const rowData: any = ref({})
// 原始的数据 
let originRowData: any = {}
const dataDetailsDialogVisible = ref(false)
const onDataSaving = ref(false)

const canModify = ref(false)

const editingCellRow = ref(-1)
const editingCellCol = ref('')
const editingCellValue = ref('')
const inlineChanges = ref(new Map<string, any>())
const savingInline = ref(false)

const tableList = computed(() => {
    try {
        return dbSchemaProxy.getTable(props.schema).map((t: any) => t.label)
    } catch {
        return []
    }
})

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

const filteredSqlHistory = computed(() => {
    const kw = sqlHistorySearch.value.trim().toLowerCase()
    if (!kw) return sqlHistoryList.value
    return sqlHistoryList.value.filter((item: any) => 
        (item.exec_sql || '').toLowerCase().includes(kw)
    )
})

function showSqlHistory() {
    http.get("/listBackupData", { params: { connId: props.connId, schema: props.schema, current: 1, pageSize: 200 } })
        .then((resp: any) => {
            sqlHistoryList.value = resp.data.data.data || []
            sqlHistoryDrawerShow.value = true
        })
}

function execTransaction(command: string) {
    const params = new URLSearchParams()
    params.append("connId", props.connId)
    params.append("schema", props.schema)
    params.append("sql", command)
    http.post("/execSQL", params)
        .then(() => {
            ElMessage({ message: command + ' 执行成功', type: 'success' })
        })
        .catch(() => {
            ElMessage({ message: command + ' 执行失败', type: 'error' })
        })
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
        params.append("connId", props.connId)
        params.append("schema", props.schema)
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

function onQueryBuilderExecute(sql: string) {
    const editorState = editorView.value?.state as EditorState
    if (editorState) {
        const doc = editorState.doc.toString()
        const insertPos = doc.length
        editorView.value?.dispatch({
            changes: { from: 0, to: doc.length, insert: sql }
        })
    }
    nextTick(() => {
        exec()
    })
}

function onQueryBuilderInsert(sql: string) {
    const editorState = editorView.value?.state as EditorState
    if (editorState) {
        const doc = editorState.doc.toString()
        const insertPos = doc.length
        editorView.value?.dispatch({
            changes: { from: insertPos, insert: '\n' + sql }
        })
    }
    ElMessage({ message: 'SQL 已插入编辑器', type: 'success' })
}

onMounted(() => {
    dbSchemaProxy.registLsn((schema: any) => {
        if (schema === props.schema) {
            let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
            createEditor(codemirror, doc);
        }
    })
    const doc = localStorage.getItem(getSqlKey()) || "\n\n\n\n\n"
    createEditor(codemirror, doc);
    const schemaPathLower = props.schemaPath.toLowerCase()
    canModify.value = schemaPathLower.indexOf("_test") != -1 || schemaPathLower.indexOf("_uat")  != -1 || schemaPathLower.indexOf("_dev") != -1 || schemaPathLower.indexOf("_read") != -1
})

watch(currentTheme, () => {
    const doc = getEditorDoc()
    createEditor(codemirror, doc)
})

watch(canModify, (can) => {
     const schemaPathLower = props.schemaPath.toLowerCase()
    if (can && !(schemaPathLower.indexOf("_test") != -1 || schemaPathLower.indexOf("_uat") != -1 || schemaPathLower.indexOf("_read") != -1)) {
        ElMessage({ message: "当前可能为生产库，请谨慎修改。", type: "error" })
    }
})

function createEditor(editorContainer: any, doc: any) {
    if (typeof editorView.value !== 'undefined') {
        editorView.value.destroy();
    }
    const isDark = currentTheme.value === 'dark'
    const extensions = [
        keymap.of([
            ...standardKeymap,
            {
                key: 'Tab',
                run: insertTab,
            }, {
                key: "ctrl-y",
                run: redo
            }, {
                key: "ctrl-z",
                run: undo
            }
        ]),
        sql({
            dialect: dbSchemaProxy.getDialect(props.schema),
            schema: <any>dbSchemaProxy.getAll(props.schema),
        }),
        history(),
        lineNumbers(),
        highlightActiveLineGutter(),
        autocompletion(),
        EditorView.editable.of(true),
        getEditorTheme(),
        syntaxHighlighting(isDark ? oneDarkHighlightStyle : lightHighlightStyle),
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
//获取编辑器里的文本内容
const getEditorDoc = (): string => {
    try {
        return (editorView.value as EditorView)?.state?.doc?.toString() || ""
    } catch {
        return ""
    }
};

function getSqlKey() {
    return "go-web-sql-" + props.tabId
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

function explainSql() {
    const sqlExec = getSelection()?.toString()
    if (!sqlExec) {
        ElMessage({ message: "请先选择SQL", type: "error" })
        return
    }
    const effectiveSql = extractEffectiveSql(sqlExec)
    if (effectiveSql.toLowerCase().startsWith('explain ')) {
        ElMessage({ message: "语句已包含 EXPLAIN", type: "warning" })
        return
    }
    const explainStmt = 'EXPLAIN ' + effectiveSql
    currentSelectTable.value = extractTableName(sqlExec)
    exectingSql.value = true
    executionTime.value = null
    const startTime = performance.now()
    const params = new URLSearchParams()
    params.append("connId", props.connId)
    params.append("schema", props.schema)
    params.append("tableName", currentSelectTable.value)
    params.append("sql", explainStmt)
    params.append("maxLine", maxLine.value)
    http.post("/execSQL", params)
        .then((resp) => {
            executionTime.value = Math.round(performance.now() - startTime)
            canEdit.value = false
            tableKeys.value = resp.data.data.keys || []
            columns.value = resp.data.data.columns.map((col: any) => {
                const colDef: any = {
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
                                onBlur: () => commitInlineEdit()
                            })
                        }
                        
                        const displayVal = cellData != null ? String(cellData) : ''
                        const changedStyle = isChanged ? {
                            backgroundColor: '#fff7e6', padding: '2px 4px', 
                            borderRadius: '3px', borderBottom: '1px dashed #faad14',
                            cursor: 'pointer'
                        } : { cursor: 'pointer' }
                        
                        return h('span', {
                            title: displayVal,
                            style: changedStyle,
                            onDblclick: (e: MouseEvent) => startInlineEdit(rowIndex, colKey, e)
                        }, displayVal)
                    }
                }
                return colDef
            })
            result.value = resp.data.data.data
            inlineChanges.value = new Map()
            exectingSql.value = false
        })
        .catch((error) => {
            console.log(error);
            exectingSql.value = false
        });
}

function listBackupData() {
    http.get("/listBackupData", { params: { connId: props.connId, schema: props.schema, current: backupDataCurrent.value, pageSize: backupDataSize.value } })
        .then((resp) => {
            backupDataList.value = resp.data.data.data
            backupDataTotal.value = resp.data.data.total
            backupDataDialogVisible.value = true
        })
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
function exec() {
    const sqlExec = getSelection()?.toString()
    if (!sqlExec) {
        ElMessage({ message: "请先选择SQL", type: "error" })
        return
    }
    const effiectiveSql = extractEffectiveSql(sqlExec)
    if (checkSql(effiectiveSql)) {
        return
    }
    currentSelectTable.value = extractTableName(sqlExec)
    exectingSql.value = true
    executionTime.value = null
    const startTime = performance.now()
    const params = new URLSearchParams()
    params.append("connId", props.connId)
    params.append("schema", props.schema)
    params.append("tableName", currentSelectTable.value)
    params.append("sql", effiectiveSql)
    params.append("maxLine", maxLine.value)
    http.post("/execSQL", params)
        .then((resp) => {
            executionTime.value = Math.round(performance.now() - startTime)
            canEdit.value = resp.data.data.canEdit
            tableKeys.value = resp.data.data.keys || []
            columns.value = resp.data.data.columns.map((col: any) => {
                const colDef: any = {
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
                                onBlur: () => commitInlineEdit()
                            })
                        }
                        
                        const displayVal = cellData != null ? String(cellData) : ''
                        const changedStyle = isChanged ? {
                            backgroundColor: '#fff7e6', padding: '2px 4px', 
                            borderRadius: '3px', borderBottom: '1px dashed #faad14',
                            cursor: 'pointer'
                        } : { cursor: 'pointer' }
                        
                        return h('span', {
                            title: displayVal,
                            style: changedStyle,
                            onDblclick: (e: MouseEvent) => startInlineEdit(rowIndex, colKey, e)
                        }, displayVal)
                    }
                }
                return colDef
            })
            columns.value.unshift({
                dataKey: "col-idx",
                width: 60,
                fixed: true,
                cellRenderer: ({ cellData, rowIndex }: { cellData: any, rowIndex: number }) => {
                    return h('div', {},
                        [h('div', { class: "el-table-v2__cell-text", title: cellData }, cellData), h('div', { class: "data-view", onClick: () => openDataDetails(rowIndex) })]
                    )
                }
            })
            result.value = resp.data.data.data
            result.value.forEach((row: any, idx: number) => {
                row["col-idx"] = idx + 1
            });
            exectingSql.value = false
        }).catch((error) => {
            console.log(error);
            exectingSql.value = false
        });
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
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('tableName', currentSelectTable.value)
    params.append('sql', sql)
    promises.push(http.post('/execSQL', params))
  })

  Promise.all(promises)
    .then(() => {
      ElMessage.success('内联更改已保存')
      inlineChanges.value = new Map()
      exec()
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

const canInlineEdit = computed(() => canEdit.value && tableKeys.value.length > 0)

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
    params.append("connId", props.connId)
    params.append("schema", props.schema)
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
        if(!canModify.value && (sqlLowerCase.startsWith("update ") || sqlLowerCase.startsWith("delete ") || sqlLowerCase.startsWith("alter "))) {
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
    return getSqlDialect(dbSchemaProxy.getDbType(props.schema))
}

function showCreateScript() {
    tableMeta.value = { connId: props.connId, schema: props.schema, tableName: tableName.value }
}

function copyCreateScript() {
    copyToClipboard(tableCreateDdlRef.value.innerText,
        () => ElMessage({ message: "已复制到粘贴板", type: "success" }),
        () => ElMessage({ message: "复制失败", type: "error" })
    )
}

let saveTimer: ReturnType<typeof setTimeout> | null = null

function onKeyup() {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    try {
      localStorage.setItem(getSqlKey(), getEditorDoc())
    } catch (e) {
      // localStorage may be full, silently ignore
    }
  }, 500)
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
        connId: props.connId,
        schema: props.schema,
        schemaPath: props.schemaPath
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
    height: calc(100vh - 60px);
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

/* ── Result Table ── */
.el-table-v2__header-cell-text {
    user-select: text;
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
            border-right-color: #409eff;
            background: rgba(64, 158, 255, 0.08);
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
    background: #fffbe6;
    border-top: 1px solid #ffe58f;
    border-bottom: 1px solid #ffe58f;
    margin-top: -1px;
}

.db-inline-bar .el-button {
    height: 26px;
    padding: 0 8px;
    font-size: 12px;
}

.inline-count {
    font-size: 12px;
    color: #ad6800;
    margin-left: 4px;
}

[data-theme="dark"] .db-inline-bar {
    background: #3d3a1a;
    border-top: 1px solid #5a5522;
    border-bottom: 1px solid #5a5522;
}

[data-theme="dark"] .inline-count {
    color: #e0c068;
}
</style>
<style lang="less" scoped>
</style>
