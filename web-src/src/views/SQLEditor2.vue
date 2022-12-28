<template>
    <div>
        <el-row>
            <el-col :offset="21">
                <el-button @click="exec">执行</el-button>
            </el-col>
        </el-row>
        <div ref="codemirror" class="codemirror"></div>
    </div>
</template>
  
<script lang="ts">
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { standardKeymap, insertTab } from '@codemirror/commands';
import { sql, MySQL } from '@codemirror/lang-sql';
import { autocompletion } from '@codemirror/autocomplete';
import { ref, watch } from 'vue';
// 数据库类型, 高度, 重载.(监听stroe,destory后create)
//获取props
export default {
    setup(props) {
        let editorView = ref<EditorView>();
        const codemirror = ref(null);
        let startState;
        const createEditor = (editorContainer, doc) => {
            if (typeof editorView.value !== 'undefined') {
                editorView.value.destroy();
            }
            startState = EditorState.create({
                //doc为编辑器内容
                doc: doc,
                extensions: [
                    keymap.of([
                        ...standardKeymap,
                        // Tab Keymap
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
                    lineNumbers(),
                    autocompletion(),
                    EditorView.editable.of(true),
                ],
            });
            editorView.value = new EditorView({
                state: startState,
                parent: editorContainer,
            });
        };
        //获取编辑器里的文本内容
        const getEditorDoc = (): string | null => {
            return (editorView.value as EditorView).state.doc.toString();
        };
        //监听对应sql的代码补全信息,如果更新,则重置editor
        watch(
            () => ['user', 'app_user', 'app_user_user'],
            () => {
                let doc = (editorView.value as EditorView).state.doc.toString() ?? '';
                const editorContainer = codemirror.value;
                createEditor(editorContainer, doc);
            }
        );
        return {
            createEditor,
            getEditorDoc,
            editorView,
        };
    },
    mounted() {
        let doc = "";
        const editorContainer = (this as any).$refs.codemirror;
        (this as any).createEditor(editorContainer, doc);
    },
    methods: {
        exec() {
            alert(getSelection())
        }
    },
};
</script>
<style>
.cm-editor {
    height: 500px;
    font-size: 18px;
}
</style>
<style lang="less" scoped>
.codemirror {
    margin-top: 10px;
}
</style>
  