import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { format, type SqlLanguage } from 'sql-formatter'
import type { EditorView } from '@codemirror/view'
import type { EditorState } from '@codemirror/state'

export interface UseSqlFormatOptions {
    getEditorView: () => EditorView | undefined
    /** 返回当前 schema 对应的 sql-formatter 方言 */
    getSqlLang: () => SqlLanguage
}

/**
 * SQL 格式化与优化面板相关逻辑：
 * - formatSql: 格式化编辑器中选中的 SQL
 * - toggleOptimizePanel: 打开 AI 优化面板并传入选中的 SQL
 *
 * 优化面板组件（SQLOptimizePanel）本身已存在，本 composable 仅管理
 * 面板可见性与待优化的 SQL 文本。
 */
export function useSqlFormat(options: UseSqlFormatOptions) {
    const { getEditorView, getSqlLang } = options

    const optimizePanelVisible = ref(false)
    const optimizeSql = ref('')

    function formatSql() {
        const sql = getSelection()?.toString()
        if (!sql) {
            ElMessage({ message: '请先选择SQL', type: 'error' })
            return
        }
        const editorState = <EditorState>getEditorView()?.state
        getEditorView()?.dispatch(
            editorState.replaceSelection(format(sql || '', { language: getSqlLang() }) + '\n')
        )
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

    return {
        optimizePanelVisible,
        optimizeSql,
        formatSql,
        toggleOptimizePanel,
    }
}
