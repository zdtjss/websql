<template>
    <el-container>
        <el-header height="30px" class="toolbar">
            <el-button @click="exec" :loading="exectingSql" title="F9">执行</el-button>
            <el-button @click="exportDb">导表</el-button>
            <el-button @click="exportCurrentToXlsx">excel</el-button>
            <el-dropdown @command="handleDdlCommand" style="margin-left: 12px;">
                <el-button>
                    SQL<el-icon class="el-icon--right"><arrow-down /></el-icon>
                </el-button>
                <template #dropdown>
                    <el-dropdown-menu>
                        <el-dropdown-item command="insert">insert</el-dropdown-item>
                        <el-dropdown-item command="update">update</el-dropdown-item>
                        <el-dropdown-item command="create">create</el-dropdown-item>
                    </el-dropdown-menu>
                </template>
            </el-dropdown>
            <el-button @click="formatSql" style="margin-left: 12px;" title="Ctrl + Shift + F">美化</el-button>
            <el-button @click="listBackupData" style="margin-left: 12px;">备份</el-button>
            <span style="float:right;">最大行数：<el-input v-model="maxLine" style="width:50px;" size="small" /></span>
        </el-header>
        <el-main id="sqlArea" class="sql_area" :style="{ height: sqlDivHeight }" @keyup.f9="exec"
            @keyup.ctrl.shift.f="formatSql">
            <div ref="codemirror" class="codemirror" @keyup="onKeyup"></div>
        </el-main>
        <div style="width: 100%; border: 1px solid #9e9e9e30; cursor: row-resize;" @mousedown="resizeResultArea"></div>
        <el-footer id="result" v-if="showResultArea" class="result" :style="{ height: resultDivHeight }">
            <el-icon @click="toggleResult" style="right: 0px;position: absolute;cursor: pointer;"
                :title="toggleResultTitle" :size="22">
                <ArrowDown v-if="showResult" style="margin-top:15px;" />
                <ArrowUp v-if="resultHide" />
            </el-icon>
            <el-auto-resizer>
                <template #default="{ height, width }">
                    <el-table-v2 :columns="columns" :data="result" :width="width" :height="height" :row-height="35"
                        fixed />
                </template>
            </el-auto-resizer>
        </el-footer>
        <el-dialog v-model="exportDialogVisible" title="导表" width="60%" center :draggable="true" :destroyOnClose="true">
            <DBExport :connId="props.connId" :schema="props.schema" opt="insert" />
        </el-dialog>
        <el-dialog v-model="tableCreateDialogVisible" @close="tableCreateDialogVisible = false" :draggable="true"
            width="1000px" style="height:650px;overflow-y: auto;">
            <el-row>
                <el-form-item label="表名">
                    <el-input v-model="tableName" @keyup.enter="showCreateScript" style="width: 300px;" />
                </el-form-item>
                <el-form-item>
                    <el-button @click="showCreateScript" style="margin-left:12px;" size="small">查看</el-button>
                    <el-button @click="copyCreateScript" style="margin-left:12px;" size="small">复制</el-button>
                </el-form-item>
            </el-row>
            <el-row>
                <el-scrollbar style="font-size: 18px;width: 100%;height: 470px;">
                    <pre><code class="language-sql" v-bind:innerHTML="tableCreateDdl" ref="tableCreateDdlRef"></code></pre>
                </el-scrollbar>
            </el-row>
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
                <el-pagination layout="prev, pager, next" v-model:total="backupDataTotal"
                    v-model:page-size="backupDataSize" v-model:current-page="backupDataCurrent"
                    @current-change="listBackupData" />
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
        <el-dialog v-model="dataDetailsDialogVisible" :draggable="true" :title="currentSelectTable"
            width="1000px" style="height:650px;overflow-y: auto;">
            <div style="height: 530px;overflow-y: auto;">
                <el-form :model="rowData" label-width="auto" style="margin-right: 10px;">
                    <el-form-item v-for="col in columns.slice(1)" :label="col.dataKey" :title="col.comment">
                        <el-date-picker v-if="col.dataType === 'DATETIME'" v-model="rowData[col.dataKey]"
                            type="datetime" value-format="YYYY-MM-DD HH:mm:ss" />
                        <el-switch v-if="col.dataType === 'BIT'" v-model="rowData[col.dataKey]" active-value="b'1'" inactive-value="b'0'"/>
                        <el-input
                            v-if="col.dataKey !== 'col-idx' && col.dataType !== 'DATETIME' && col.dataType !== 'BIT'"
                            v-model="rowData[col.dataKey]" type="textarea" autosize :disabled="tableKeys.includes(col.dataKey)"/>
                    </el-form-item>
                </el-form>
            </div>
            <template #footer>
                <div class="dialog-footer">
                    <el-button v-if="canEdit" type="primary" :loading="onDataSaving" @click="saveData(rowData)">
                        保存
                    </el-button>
                    <el-button  v-if="!canEdit" type="primary" @click="dataDetailsDialogVisible = false">
                        关闭
                    </el-button>
                </div>
            </template>
        </el-dialog>
    </el-container>
</template>

<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view'
import { oneDarkHighlightStyle } from "@codemirror/theme-one-dark"
import { EditorState } from '@codemirror/state'
import { standardKeymap, insertTab, history, redo, undo } from '@codemirror/commands'
import { sql } from '@codemirror/lang-sql';
import { syntaxHighlighting } from '@codemirror/language'
import { autocompletion } from '@codemirror/autocomplete'
import { ref, onMounted, h } from 'vue'
import { dbSchemaProxy } from '../stores/sql'
import { ElMessage } from 'element-plus'
import { format, type SqlLanguage } from 'sql-formatter'
import DBExport from './DBExport.vue'

import hljs from 'highlight.js/lib/core'
import * as highlightSql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/stackoverflow-light.css'

import http from '../js/utils/httpProxy.js'
import excel from '../js/utils/excel.js'
import copyToClipboard from '../js/utils/copy-to-clipboard.js'

hljs.registerLanguage('sql', highlightSql.default);

const props = defineProps<{
    tabId: string,
    connId: string,
    schema: string
}>()

const maxLine = ref(15)
const columns: any = ref([])
const result: any = ref([])
const editorView = ref<EditorView>()
const codemirror = ref()
const exportDialogVisible = ref(false)

const showResult = ref(false)
const showResultArea = ref(false)
const resultHide = ref(false)
const sqlDivHeight = ref("")
let defaultSqlDivHeight: string
const resultDivHeight = ref("")
const toggleResultTitle = ref("")

const exectingSql = ref(false)
const currentSelectTable = ref("")

const tableName = ref("")
const tableCreateDdl = ref("")
const tableCreateDdlRef = ref()
const tableCreateDialogVisible = ref(false)

const backupDataList = ref([])
const backupDataTotal = ref(0)
const backupDataCurrent = ref(0)
const backupDataSize = ref(12)
const backupDataDialogVisible = ref(false)

const canEdit = ref(false)
const tableKeys = ref([] as string[])
const rowData:any = ref({})
// 原始的数据 
let originRowData: any = {}
const dataDetailsDialogVisible = ref(false)
const onDataSaving = ref(false)

const backupData = ref("")
const backupDataDrawerShow = ref(false)

onMounted(() => {
    // 默认高度
    defaultSqlDivHeight = (calHeight() - 3) + "px"
    sqlDivHeight.value = defaultSqlDivHeight
    dbSchemaProxy.registLsn((schema: any) => {
        if (schema === props.schema) {
            let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
            createEditor(codemirror, doc);
        }
    })
    const doc = localStorage.getItem(getSqlKey()) || "\n\n\n\n\n"
    createEditor(codemirror, doc);
    window.addEventListener('resize', () => {
        if (showResult.value) {
            sqlDivHeight.value = (calHeight() * 0.3)  + "px"
            resultDivHeight.value = (calHeight() * 0.7) + "px"
        } else {
            sqlDivHeight.value = (calHeight() - 15) + "px"
            defaultSqlDivHeight = sqlDivHeight.value
            resultDivHeight.value = "15px"
        }
    })
})

function createEditor(editorContainer: any, doc: any) {
    if (typeof editorView.value !== 'undefined') {
        editorView.value.destroy();
    }
    const startState = EditorState.create({
        //doc为编辑器内容
        doc: doc,
        extensions: [
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
                schema: <any>dbSchemaProxy.getAll(),
                tables: dbSchemaProxy.getTable(props.schema)
            }),
            history(),
            lineNumbers(),
            highlightActiveLineGutter(),
            syntaxHighlighting(oneDarkHighlightStyle),
            autocompletion(),
            EditorView.editable.of(true),
        ],
    });
    editorView.value = new EditorView({
        state: startState,
        parent: editorContainer.value,
    });
}
//获取编辑器里的文本内容
const getEditorDoc = (): string => {
    return (editorView.value as EditorView).state.doc.toString() || "";
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
    http.get("/execSQL", { params: { connId: props.connId, schema: props.schema, tableName: currentSelectTable.value, sql: effiectiveSql, maxLine: maxLine.value } })
        .then((resp) => {
            canEdit.value = resp.data.data.canEdit
            tableKeys.value = resp.data.data.keys || []
            columns.value = resp.data.data.columns.map((col: any) => {
                const colDef = {
                    key: col.name,
                    title: col.name,
                    dataKey: col.name,
                    comment: col.comment,
                    dataType: col.type,
                    width: 150,
                    minWidth: "150px",
                    headerCellRenderer: () => {
                        return h('div', { class: "el-table-v2__header-cell-text", title: col.comment }, col.name)
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
            // showResult.value = false
            if (defaultSqlDivHeight === sqlDivHeight.value || resultHide.value) {
                toggleResult()
            }
            if (!showResultArea.value) {
                showResultArea.value = true
            }
        }).catch((error) => {
            console.log(error);
            exectingSql.value = false
        });
}

function openDataDetails(rowIndex: number) {
    dataDetailsDialogVisible.value = true
    rowData.value = result.value[rowIndex]
    originRowData = JSON.parse(JSON.stringify(result.value[rowIndex]))
}

function saveData(rowData: any) {

    let effiectiveSql = "update " + currentSelectTable.value + " set "

    const updateColumnSets = Object.keys(originRowData).filter((key) => originRowData[key] != rowData[key]).map((key) => key + " = " + fmtVal(rowData[key]))

    if (updateColumnSets.length === 0 && canEdit.value) {
        ElMessage({ message: "数据未修改", type: "warning" })
        return
    }

    effiectiveSql += updateColumnSets.join(", ") + " where "
    effiectiveSql += tableKeys.value.map((key: string) => key + " = " + fmtVal(originRowData[key])).join(" and ")

    onDataSaving.value = true

    http.get("/execSQL", { params: { connId: props.connId, schema: props.schema, tableName: currentSelectTable.value, sql: effiectiveSql } })
        .then((resp) => {
            onDataSaving.value = false
            if (!resp.data.data.msg) {
                dataDetailsDialogVisible.value = false
            }
            const respConlumn = resp.data.data.columns[0].name
            const respData = resp.data.data.data[0]
            ElMessage({ message: resp.data.data.msg ? resp.data.data.msg : "修改了 " + respData[respConlumn] + " 条数据", type: resp.data.data.msg ? "error" : "success" })
        }).catch((error) => {
            console.log(error);
        });
}

function extractEffectiveSql(sql: string) {
    let relSql = sql.trimStart()
    // 忽略注释的语句
    if (relSql == "" || relSql.startsWith("--") || relSql.startsWith("//") || relSql.startsWith("/*")) {
        const nsql = []
        const sqlArr = relSql.split("\n")
        for (let i = 0; i < sqlArr.length; i++) {
            const row = sqlArr[i]
            if (row == "" || row.startsWith("--") || row.startsWith("//") || row.startsWith("/*")) {
                continue
            }
            nsql.push(row)
        }
        relSql = nsql.join("\n")
    }

    // 补充schema
    /* const sqlLower = relSql.toLowerCase()
    const idxFromEnd = sqlLower.indexOf(" from ") + 6
    if (idxFromEnd !== 5) {
        relSql = fillSchema(relSql, "", props.schema, idxFromEnd, 0)
    } */

    return relSql
}

function fillSchema(relSql: string, sqlResult: string, schema: string, searchStart: number, concatStart: number) :string {
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
        debugger
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
        if ((sqlArr[i].trimStart().startsWith("update ") || sqlArr[i].trimStart().startsWith("delete ")) && sqlArr[i].indexOf(" where ") === -1) {
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

function exportDb() {
    exportDialogVisible.value = true
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

function handleDdlCommand(command: string) {
    switch (command) {
        case "insert":
            exportCurrentToSqlInsert()
            break
        case "update":
            exportCurrentToSqlUpdate()
            break
        case "create":
            tableCreateDialogVisible.value = true
            break
        default:
            ElMessage({ message: "无效操作", type: "error" })
    }
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
    let sqlLang: SqlLanguage = "sql"
    const dbType = dbSchemaProxy.getDbType(props.schema).toLowerCase()
    if (dbType === "oracle") {
        sqlLang = "plsql"
    } else if (dbType === "mysql") {
        sqlLang = "mysql"
    }
    return sqlLang
}

function showCreateScript() {
    let sqlStr = ""
    const dbType = dbSchemaProxy.getDbType(props.schema)
    if (dbType === 'mysql') {
        sqlStr = "show create table " + tableName.value
    } else if (dbType === 'oracle') {
        sqlStr = "select dbms_metadata.get_ddl('TABLE','" + tableName.value.toUpperCase() + "') from dual"
    } else {
        ElMessage({ message: "暂不支持", type: "error" })
        return
    }
    http.get("/execSQL", { params: { connId: props.connId, schema: props.schema, sql: sqlStr, maxLine: maxLine.value } })
        .then((resp) => {
            const data = resp.data.data.data[0]
            tableCreateDdl.value = hljs.highlight(data[Object.keys(data)[0].trim()], { language: 'sql' }).value
        }).catch((error) => {
            console.log(error);
        });
}

function copyCreateScript() {
    copyToClipboard(tableCreateDdlRef.value.innerText,
        () => ElMessage({ message: "已复制到粘贴板", type: "success" }),
        () => ElMessage({ message: "复制失败", type: "error" })
    )
}

function onKeyup() {
    localStorage.setItem(getSqlKey(), getEditorDoc())
}

function resizeResultArea(event: MouseEvent) {
    const startY = event.clientY
    const ogiHeight = sqlDivHeight.value === "" ? startY : Number.parseFloat(sqlDivHeight.value.substring(0, sqlDivHeight.value.length - 2))
    const resultHeight = resultDivHeight.value === "" ? startY : Number.parseFloat(resultDivHeight.value.substring(0, resultDivHeight.value.length - 2))
    document.onmousemove = (e) => {
        sqlDivHeight.value = (ogiHeight + e.clientY - startY) + "px"
        resultDivHeight.value = (resultHeight - (e.clientY - startY)) + "px"
    }
    document.onmouseup = () => {
        document.onmouseup = null
        document.onmousemove = null
    }
}

function toggleResult() {
    if (showResult.value) {
        resultHide.value = true
        showResult.value = false
        sqlDivHeight.value = (calHeight() - 15) + "px"
        resultDivHeight.value = "15px"
        toggleResultTitle.value = "显示结果"
    } else {
        resultHide.value = false
        showResult.value = true
        sqlDivHeight.value = (calHeight() * 0.3) + "px"
        resultDivHeight.value = (calHeight() * 0.7) + "px"
        toggleResultTitle.value = "隐藏结果"
    }
}

function calHeight() {
    return document.body.scrollHeight - 75
}

function fmtVal(val: any) {
    if (val === null) {
        return "null"
    } else if (typeof val === "string" && val.length > 2 && val.startsWith("b'") && val.charAt(val.length - 1) === "'") {
        return val
    } else if (typeof val === "string" && val.length > 2 && val.startsWith("s:") && new Number(val.substring(2)).toString() !== "NaN") {
        return val.substring(2, val.length)
    } else if (typeof val === "string") {
        return "'" + val + "'"
    }
    return val
}

</script>
<style>
.cm-editor {
    height: 100%;
    width: 100%;
    font-size: 18px;
}

.sql_area {
    padding: 0px;
    margin-top: 5px;
    border-top: dashed 1px gray;
}

.codemirror {
    height: 100%;
}

/** 表头可选择复制 */
.el-table-v2__header-cell-text {
    user-select: text;
}

.el-table-v2__header-row,
.el-table-v2__header-wrapper {
    height: 35px !important;
}

.el-drawer__header {
    margin-bottom: -20px
}

.el-button {
    height: 25px;
    padding: 8px 10px;
}
</style>
<style lang="less" scoped>
.toolbar {
    padding: 0px;
}
</style>
