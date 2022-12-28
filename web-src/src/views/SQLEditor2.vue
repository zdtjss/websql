<template>
    <el-container>
        <el-header height="30px" class="toolbar">
            <el-button @click="exec">执行</el-button>
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
import { ref, watch, onMounted } from 'vue';
import { ElButton, ElTag, TableV2FixedDir, TableV2SortOrder } from 'element-plus'

let editorView = ref<EditorView>();
const codemirror = ref(null);

const longText =
    'Quaerat ipsam necessitatibus eum quibusdam est id voluptatem cumque mollitia.'
const midText = 'Corrupti doloremque a quos vero delectus consequatur.'
const shortText = 'Eius optio fugiat.'
const textList = [shortText, midText, longText]

let id = 0
const dataGenerator = () => ({
    id: `random:${++id}`,
    name: 'Tom',
    date: '2016-05-03',
})

const columns = [
    {
        key: 'id',
        title: 'Id',
        dataKey: 'id',
        width: 150,
        fixed: TableV2FixedDir.LEFT,
    },
    {
        key: 'name',
        title: 'Name',
        dataKey: 'name',
        width: 150,
        align: 'center'
    }]

const result = ref(
    Array.from({ length: 200 })
        .map(dataGenerator)
        .sort((a, b) => (a.name > b.name ? 1 : -1))
)

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
    alert(getSelection())
}
</script>
<style>
.cm-editor {
    height: 100%;
    width: 100%;
    font-size: 18px;
}

.sql_area {
    height: calc(100vh * 0.5);
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
    text-align: right;
}
</style>
  