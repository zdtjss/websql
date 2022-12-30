<template>
    <el-container>
        <el-header height="30px" class="toolbar">
            <el-button @click="exec">执行</el-button>
            <el-button @click="exportDb">导表</el-button>
        </el-header>
        <el-main class="sql_area">
            <div ref="codemirror" class="codemirror"></div>
        </el-main>
        <el-footer class="result">
            <el-table-v2 :columns="columns" :data="result" :width="700" :height="400" fixed />
        </el-footer>
    </el-container>
</template>
  
<script lang="ts" setup>
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { standardKeymap, insertTab, history } from '@codemirror/commands';
import { sql, MySQL } from '@codemirror/lang-sql';
import { autocompletion } from '@codemirror/autocomplete';
import { ref, watch, onMounted, defineProps } from 'vue';
import { useRouter } from 'vue-router'

import http from '../js/utils/httpProxy.js'

const props = defineProps<{
    connId: string,
    schema: string
}>()

const router = useRouter()

const columns = ref([])
const result = ref([])
let editorView = ref<EditorView>();
const codemirror = ref(null);

//监听对应sql的代码补全信息,如果更新,则重置editor
watch(
    () => ['user', 'app_user', 'app_user_user'],
    () => {
        let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
        const editorContainer = codemirror.value;
        createEditor(editorContainer, doc);
    }
)

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
                schema: {
                    apom: ['user', 'app_user', 'app_user_user'],
                },
                tables: [
                    { label: "user1" },
                    { label: "app_user2" }
                ]
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
    http.get("/execSQL", { params: { connId: props.connId, schema: props.schema, sql: getSelection() } })
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
    router.push({
        path: "/export", query: {
            connId: props.connId, schema: props.schema, start: 3, opt: "insert"
        }
    })
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
</style>
<style lang="less" scoped>
.codemirror {}

.toolbar {
    padding: 0px;
}
</style>
  