<template>
    <el-container>
        <el-header height="30px" class="toolbar">
            <el-button @click="exec" :loading="exectingSql">执行</el-button>
            <el-button @click="exportDb">导表</el-button>
            <el-button @click="exportCurrentToXlsx">excel</el-button>
            <el-button @click="exportCurrentToSqlInsert">SQL-insert</el-button>
            <el-button @click="exportCurrentToSqlUpdate">SQL-update</el-button>
            <span style="float:right;">最大行数：<el-input v-model="maxLine" style="width:50px;" size="small" /></span>
        </el-header>
        <el-main class="sql_area">
            <div ref="codemirror" class="codemirror"></div>
        </el-main>
        <el-footer class="result">
            <el-auto-resizer>
                <template #default="{ height, width }">
                    <el-table-v2 :columns="columns" :data="result" :width="width" :height="height" fixed />
                </template>
            </el-auto-resizer>
        </el-footer>
        <el-dialog v-model="exportDialogVisible" title="导表" width="60%" center :draggable="true">
            <DBExport :connId="props.connId" :schema="props.schema" opt="insert" />
        </el-dialog>
    </el-container>
</template>

<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view';
import { oneDarkHighlightStyle } from "@codemirror/theme-one-dark";
import { EditorState } from '@codemirror/state';
import { standardKeymap, insertTab, history, redo, undo } from '@codemirror/commands';
import { sql, MySQL } from '@codemirror/lang-sql';
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language';
import { autocompletion } from '@codemirror/autocomplete';
import { ref, onMounted } from 'vue';
import { dbSchemaProxy } from '../stores/sql'
import { ElMessage } from 'element-plus'

import DBExport from './DBExport.vue'

import http from '../js/utils/httpProxy.js'
import excel from '../js/utils/excel.js'

const props = defineProps<{
    connId: string,
    schema: string
}>()

const maxLine = ref(10)
const columns = ref([])
const result = ref([])
let editorView = ref<EditorView>();
const codemirror = ref(null);
const exportDialogVisible = ref(false)

const exectingSql = ref(false)
let currentSelectTable = ""

onMounted(() => {
    dbSchemaProxy.registLsn((schema: any) => {
        if (schema === props.schema) {
            let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
            createEditor(codemirror, doc);
        }
    })
    createEditor(codemirror, "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n");
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
                dialect: MySQL,
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
const getEditorDoc = (): string | null => {
    return (editorView.value as EditorView).state.doc.toString();
};

function exec() {
    const sqlExec = getSelection()?.toString()
    if (!sqlExec) {
        ElMessage({ message: "请先选择SQL", type: "error" })
        return
    }
    if (sqlExec.trim().startsWith("select ") || sqlExec.trim().startsWith("select ")) {
        const fromIdx = sqlExec.toLowerCase().indexOf(" from ") + 6
        const subSql = sqlExec.substring(fromIdx)
        const blankLengh = subSql.length - subSql.trim().length
        const tableNameLength = subSql.trim().indexOf(" ") === -1 ? subSql.trim().length : subSql.trim().indexOf(" ")
        currentSelectTable = sqlExec.substring(fromIdx, fromIdx + blankLengh + tableNameLength)
    }
    exectingSql.value = true
    http.get("/execSQL", { params: { connId: props.connId, schema: props.schema, sql: sqlExec, maxLine: maxLine.value } })
        .then((resp) => {
            columns.value = resp.data.data.columns.map((col: any) => {
                return {
                    key: col.name,
                    title: col.name,
                    dataKey: col.name,
                    width: 150
                }
            })
            result.value = resp.data.data.data
            exectingSql.value = false
        })
        .catch(function (error) {
            console.log(error);
            exectingSql.value = false
        });
}

function exportDb() {
    exportDialogVisible.value = true
}

function exportCurrentToXlsx() {

    if (result.value.length === 0) {
        ElMessage({ message: "请先执行查询，在导出", type: "warning" })
        return
    }

    let header = {}
    columns.value.forEach(col => header[col["title"]] = col["title"])

    const obj = {
        header: header,
        data: result.value,
        key: columns.value.map(col => col["title"]),
        title: '',
        filename: currentSelectTable,
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
    let sql = `insert into ${currentSelectTable} (`
    for (const i in columns.value) {
        columnArr.push(columns.value[i]["key"])
    }
    for (const j in result.value) {
        let rowVal = []
        let valueArr = []
        for (const i in columns.value) {
            let val = result.value[j][columns.value[i]["key"]]
            rowVal.push(fmtValForInsert(val))
        }
        valueArr.push(rowVal.join(","))
        sqlArr.push(sql + columnArr.join(",") + ") values (" + valueArr.join(",") + ")")
    }

    console.log(sql)
    const blob = new Blob(["\uFEFF" + sqlArr.join(";\n")], {
        type: "text/plain;charset=utf-8",
    });
    const url = window.URL.createObjectURL(blob);
    const downloadLink = document.createElement("a");
    downloadLink.href = url;
    downloadLink.download = currentSelectTable + "-insert.sql";
    downloadLink.click();
    window.URL.revokeObjectURL(url);
}

function exportCurrentToSqlUpdate() {
    if (result.value.length === 0) {
        ElMessage({ message: "请先执行查询，在导出SQL", type: "warning" })
        return
    }
    let sqlArr = []
    let sql = `update ${currentSelectTable} set `
    for (let j = 0; j < result.value.length; j++) {
        let rowVal = []
        for (let i = 1; i < columns.value.length; i++) {
            let column = columns.value[i]["key"]
            let val = result.value[j][column]
            if (i === columns.value.length - 1) {
                rowVal.push((column + fmtValForUpdate(val)) + " where " + columns.value[0]["key"] + fmtValForUpdate(result.value[j][columns.value[0]["key"]]))
            } else {
                rowVal.push(column + fmtValForUpdate(val))
            }
        }
        sqlArr.push(sql + rowVal.join(", "))
    }
    console.log(sql)
    const blob = new Blob(["\uFEFF" + sqlArr.join(";\n")], {
        type: "text/plain;charset=utf-8",
    });
    const url = window.URL.createObjectURL(blob);
    const downloadLink = document.createElement("a");
    downloadLink.href = url;
    downloadLink.download = currentSelectTable + "-update.sql";
    downloadLink.click();
    window.URL.revokeObjectURL(url);
}

function fmtValForInsert(val: any) {
    if (val === null) {
        return "null"
    } else if (typeof val === "string") {
        return "'" + val + "'"
    }
    return val
}

function fmtValForUpdate(val: any) {
    if (val === null) {
        return " is null"
    } else if (typeof val === "string") {
        return " = '" + val + "'"
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
    height: calc(100vh * 0.4);
    padding: 0px;
    margin-top: 10px;
    border-top: dashed 1px gray;
}

.result {
    height: calc(100vh * 0.4);
}

/** 表头可选择复制 */
.el-table-v2__header-cell-text {
    user-select: text;
}
</style>
<style lang="less" scoped>
.toolbar {
    padding: 0px;
}
</style>
