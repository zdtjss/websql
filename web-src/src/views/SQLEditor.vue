
<template>
    <Codemirror v-model="code" :options="cmOption" :extensions="extensions" />
</template>
  
<script setup>
import { ref, reactive } from 'vue'
import { EditorView, keymap } from '@codemirror/view';
import { Codemirror } from 'vue-codemirror'
import { standardKeymap, insertTab } from '@codemirror/commands';
import { sql, MySQL } from '@codemirror/lang-sql';
import { autocompletion } from '@codemirror/autocomplete';
import { oneDark } from '@codemirror/theme-one-dark'

const code = ref("")
const cmOption = ref({
    tabSize: 4,
    styleActiveLine: true,
    lineNumbers: true,
    line: true,
    mode: 'text/x-mysql'
})

const extensions = ref(
    oneDark,
    keymap.of([
        ...standardKeymap,
        {
            key: 'Tab',
            run: insertTab,
        },
    ]),
    sql({
        dialect: MySQL,
    }),
    autocompletion({ activateOnTyping: true })
    // EditorView.editable.of(true),
)

</script>
  