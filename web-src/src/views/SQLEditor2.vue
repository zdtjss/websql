<template>
    <el-container>
        <el-header height="30px" class="toolbar">
            <el-button @click="exec">执行</el-button>
            <el-button @click="exportDb">导表</el-button>
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
        <el-dialog v-model="exportDialogVisible" title="导表" width="60%" center :draggable="true" >
            <DBExport :connId="props.connId" :schema="props.schema" start="3" opt="insert" />
        </el-dialog>
    </el-container>
</template>
  
<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { standardKeymap, insertTab, history } from '@codemirror/commands';
import { sql, MySQL } from '@codemirror/lang-sql';
import { autocompletion } from '@codemirror/autocomplete';
import { ref, onMounted } from 'vue';
import { useDBStore } from '../stores/sql'

import DBExport from './DBExport.vue'

import http from '../js/utils/httpProxy.js'

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

const dbStore = useDBStore()
let schemaDD:any = {}
let tablesDD:any = []

/* let count = 0
dbStore.$subscribe((mutation, state) => {
    console.log(count++)
    schemaDD = dbStore.getAll()
    tablesDD = dbStore.getTable(props.schema)
    let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
    createEditor(codemirror, doc);
}) */

/* watch(schemaDD, () => {
    let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
    createEditor(codemirror, doc);
}) */

onMounted(() => {
    createEditor(codemirror, "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n");
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
                },
            ]),
            sql({
                dialect: MySQL,
                schema: dbStore.getAll(),
                tables: dbStore.getTable(props.schema)
            }),
            history(),
            lineNumbers(),
            highlightActiveLineGutter(),
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
    http.get("/execSQL", { params: { connId: props.connId, schema: props.schema, sql: getSelection(), maxLine: maxLine.value } })
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
        })
        .catch(function (error) {
            console.log(error);
        });
}

function exportDb() {
    exportDialogVisible.value = true
    /*  router.push({
         path: "/export", query: {
             connId: props.connId, schema: props.schema, start: 3, opt: "insert"
         }
     }) */
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
    border-top: solid 1px gray;
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
  