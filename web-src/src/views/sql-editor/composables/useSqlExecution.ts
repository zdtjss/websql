import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { ElMessage, ElLoading } from 'element-plus'
import { execSQL } from '@/api/sql'
import { isCancel } from '@/api'
import { exportToCsv, exportToJson } from '@/utils/exportHelper'
import excel from '@/utils/excel'
import copyToClipboard from '@/utils/copy-to-clipboard'
import { buildWhereCondition, fmtVal, quoteId } from '@/utils/sqlHelper'
import type { EditorView } from '@codemirror/view'

export interface UseSqlExecutionOptions {
    connId: string
    schema: string
    maxLine: Ref<string>
    getEditorView: () => EditorView | undefined
    canModify: Ref<boolean>
    effectiveDbType: ComputedRef<string>
}

// SQL 语句起始关键字（不区分大小写）
const SQL_STATEMENT_KEYWORDS = new Set([
    'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'ALTER', 'DROP',
    'TRUNCATE', 'GRANT', 'REVOKE', 'COMMIT', 'ROLLBACK', 'SAVEPOINT',
    'SET', 'SHOW', 'DESC', 'DESCRIBE', 'EXPLAIN', 'USE', 'WITH',
    'MERGE', 'CALL', 'EXEC', 'EXECUTE', 'PREPARE', 'DEALLOCATE',
    'LOCK', 'UNLOCK', 'OPTIMIZE', 'ANALYZE', 'CHECK', 'REFRESH',
    'REPLACE', 'COMMENT', 'HANDLER', 'LOAD', 'FLUSH', 'RESET', 'DO',
    'VALUES', 'START', 'RELEASE'
])

/**
 * SQL 执行相关状态与逻辑：
 * - 管理结果集、列定义（原始元数据）、批量结果、错误、执行耗时
 * - 提供 exec / execSmart / execBatch / execFile / stopExec 等方法
 * - 提供结果导出（xlsx / csv / json / insert / update）
 *
 * 注意：列的渲染（cellRenderer）和内联编辑由 SqlResultPanel 组件负责，
 * 本 composable 只维护原始列元数据（rawColumns）。
 */
export function useSqlExecution(options: UseSqlExecutionOptions) {
    const {
        connId,
        schema,
        maxLine,
        getEditorView,
        canModify,
        effectiveDbType,
    } = options

    // ── 响应式状态 ──
    const result = ref<any[]>([])
    /** 原始列元数据（{name, type, comment, width?}），不含渲染函数 */
    const rawColumns = ref<any[]>([])
    const tableKeys = ref<string[]>([])
    const canEdit = ref(false)
    const batchResults = ref<any[]>([])
    const sqlError = ref('')
    const executionTime = ref<number | null>(null)
    const exectingSql = ref(false)
    const isBatchMode = ref(false)
    const activeResultTab = ref('0')
    const currentSelectTable = ref('')

    let abortController: AbortController | null = null

    // ── 计算属性 ──
    const batchDisplayTabs = computed(() => {
        if (!isBatchMode.value || batchResults.value.length === 0) return []
        const tabs: {
            name: string
            type: string
            modifyCount?: number
            queryNum?: number
            idx?: number
            item?: any
            hasError?: boolean
            hasRollback?: boolean
            allFailed?: boolean
        }[] = []
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
                allFailed,
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
                    item,
                })
            }
        })
        return tabs
    })

    const canInlineEdit = computed(
        () => canModify.value && canEdit.value && tableKeys.value.length > 0
    )

    // ── 辅助方法 ──

    function extractSqlStatements(sql: string): string[] {
        let cleanSql = sql.replace(/\/\*[\s\S]*?\*\//g, '')
        const lines = cleanSql.split('\n')
        const cleanedLines: string[] = []
        for (const line of lines) {
            const trimmed = line.trim()
            if (trimmed === '' || trimmed.startsWith('--') || trimmed.startsWith('//')) {
                continue
            }
            cleanedLines.push(line.trimEnd())
        }
        cleanSql = cleanedLines.join('\n').trim()
        return cleanSql
            .split(';')
            .map((s: string) => s.trim())
            .filter((s: string) => s.length > 0)
    }

    function extractEffectiveSql(sql: string) {
        let relSql = sql.trimStart()
        const sqlArr = relSql.split('\n')
        const nsql: string[] = []
        for (let i = 0; i < sqlArr.length; i++) {
            let row = sqlArr[i].trim()
            if (row === '' || row.startsWith('--') || row.startsWith('//') || row.startsWith('/*')) {
                continue
            }
            row = row.trimEnd()
            if (row.endsWith(';')) {
                nsql.push(row.substring(0, row.length - 1))
            } else {
                nsql.push(row)
            }
        }
        relSql = nsql.join('\n')
        relSql = relSql.trimEnd()
        if (relSql.endsWith(';')) {
            return relSql.substring(0, relSql.length - 1)
        }
        return relSql
    }

    function extractTableName(sqlExec: string) {
        let table = ''
        if (sqlExec.trim().startsWith('select ') || sqlExec.trim().startsWith('select\n')) {
            let fromIdx = sqlExec.toLowerCase().indexOf(' from ') + 6
            if (fromIdx === 5) {
                fromIdx = sqlExec.toLowerCase().indexOf('\nfrom\n') + 6
            }
            const tableNameArr: string[] = []
            for (let i = fromIdx; i < sqlExec.length; i++) {
                if (sqlExec.charAt(i) !== ' ' && sqlExec.charAt(i) !== '\n') {
                    tableNameArr.push(sqlExec.charAt(i))
                } else if (tableNameArr.length !== 0 && (sqlExec.charAt(i) === ' ' || sqlExec.charAt(i) === '\n')) {
                    break
                }
            }
            table = tableNameArr.join('')
        }
        return table
    }

    function checkSql(sql: string) {
        let hasInvalid = false
        const sqlArr = sql.split(';')
        for (let i = 0; i < sqlArr.length; i++) {
            const sqlLowerCase = sqlArr[i].toLowerCase().trimStart()
            if (
                !canModify.value &&
                (sqlLowerCase.startsWith('update ') ||
                    sqlLowerCase.startsWith('delete ') ||
                    sqlLowerCase.startsWith('alter ') ||
                    sqlLowerCase.startsWith('insert ') ||
                    sqlLowerCase.startsWith('drop ') ||
                    sqlLowerCase.startsWith('create ') ||
                    sqlLowerCase.startsWith('truncate '))
            ) {
                ElMessage.warning('当前模式不允许修改')
                hasInvalid = true
                break
            }
            if (
                (sqlLowerCase.startsWith('update ') || sqlLowerCase.startsWith('delete ')) &&
                !hasWhereClause(sqlLowerCase)
            ) {
                hasInvalid = true
                ElMessage.warning('请明确 where 条件')
                break
            }
        }
        return hasInvalid
    }

    /**
     * 检测 SQL 是否包含 WHERE 子句。
     * 支持多行 SQL，WHERE 关键字可能出现在行首、行尾或被空白/换行符包围。
     * 使用正则匹配 WHERE 作为独立单词（前后为空白或行边界），
     * 并排除字符串字面量中的 WHERE。
     */
    function hasWhereClause(sqlLowerCase: string): boolean {
        // 去除字符串字面量内容，避免误匹配引号中的 'where'
        const sqlWithoutStrings = sqlLowerCase
            .replace(/'[^']*'/g, "''")
            .replace(/"[^"]*"/g, '""')
        // 匹配 WHERE 作为独立关键字：前后是空白字符（含换行）或字符串边界
        return /(?:^|\s)where(?:\s|$)/m.test(sqlWithoutStrings)
    }

    function formatDuration(ms: number): string {
        if (ms < 1000) return `${ms}ms`
        if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
        const minutes = Math.floor(ms / 60000)
        const seconds = Math.round((ms % 60000) / 1000)
        return `${minutes}m ${seconds}s`
    }

    function getSqlPreview(sql: string): string {
        if (!sql) return ''
        const firstLine = sql.split('\n')[0].trim()
        if (firstLine.length > 30) {
            return firstLine.substring(0, 30) + '...'
        }
        return firstLine
    }

    function isStatementKeywordAt(doc: string, pos: number): boolean {
        let end = pos
        while (end < doc.length && /[A-Za-z0-9_]/.test(doc[end])) {
            end++
        }
        if (end === pos) return false
        return SQL_STATEMENT_KEYWORDS.has(doc.substring(pos, end).toUpperCase())
    }

    /** 获取光标所在位置的 SQL 语句（通过状态机扫描文档，正确处理字符串和注释） */
    function getCurrentStatement(): { text: string; from: number; to: number } | null {
        const view = getEditorView()
        if (!view) return null
        const doc = view.state.doc.toString()
        if (!doc) return null
        const cursorPos = view.state.selection.main.head

        let state: 'normal' | 'single' | 'double' | 'line' | 'block' = 'normal'
        const stmtStarts: number[] = []
        const semicolons: number[] = []
        let atLineStart = true

        for (let i = 0; i < doc.length; i++) {
            const ch = doc[i]
            const next = doc[i + 1]

            if (ch === '\n') {
                if (state === 'line') state = 'normal'
                atLineStart = true
                continue
            }

            if (state === 'normal') {
                if (atLineStart) {
                    if (ch === ' ' || ch === '\t' || ch === '\r') {
                        // 行首空白
                    } else {
                        if (isStatementKeywordAt(doc, i)) {
                            stmtStarts.push(i)
                        }
                        atLineStart = false
                    }
                }
                if (ch === ';') semicolons.push(i)
                if (ch === "'") state = 'single'
                else if (ch === '"') state = 'double'
                else if (ch === '-' && next === '-') state = 'line'
                else if (ch === '/' && next === '/') state = 'line'
                else if (ch === '/' && next === '*') state = 'block'
            } else if (state === 'single') {
                if (ch === "'") {
                    if (next === "'") i++
                    else state = 'normal'
                }
            } else if (state === 'double') {
                if (ch === '"') {
                    if (next === '"') i++
                    else state = 'normal'
                }
            } else if (state === 'block') {
                if (ch === '*' && next === '/') {
                    state = 'normal'
                    i++
                }
            }
        }

        if (stmtStarts.length === 0) {
            const text = doc.trim()
            if (!text) return null
            return { text, from: 0, to: doc.length }
        }

        const ranges: { from: number; to: number }[] = []
        for (let i = 0; i < stmtStarts.length; i++) {
            const from = stmtStarts[i]
            let to = doc.length
            if (i + 1 < stmtStarts.length) {
                to = Math.min(to, stmtStarts[i + 1])
            }
            for (const sc of semicolons) {
                if (sc >= from) {
                    to = Math.min(to, sc + 1)
                    break
                }
            }
            ranges.push({ from, to })
        }

        for (const r of ranges) {
            if (cursorPos >= r.from && cursorPos < r.to) {
                const text = doc.substring(r.from, r.to).trim()
                if (text) return { text, from: r.from, to: r.to }
            }
        }

        if (cursorPos < ranges[0].from) {
            const r = ranges[0]
            const text = doc.substring(r.from, r.to).trim()
            if (text) return { text, from: r.from, to: r.to }
        }

        const last = ranges[ranges.length - 1]
        if (cursorPos >= last.to) {
            const text = doc.substring(last.from, last.to).trim()
            if (text) return { text, from: last.from, to: last.to }
        }

        for (const r of ranges) {
            if (r.from > cursorPos) {
                const text = doc.substring(r.from, r.to).trim()
                if (text) return { text, from: r.from, to: r.to }
            }
        }

        return null
    }

    // ── 结果应用到 UI（仅设置原始数据，列渲染由组件处理） ──

    function applyResultToUI(data: any) {
        canEdit.value = data.canEdit || false
        tableKeys.value = data.keys || []
        rawColumns.value = data.columns || []
        result.value = data.data || []
        result.value.forEach((row: any, idx: number) => {
            row['col-idx'] = idx + 1
        })
    }

    function displayBatchResult(idx: number) {
        const item = batchResults.value[idx]
        if (!item) return

        if (item.status === 'error') {
            sqlError.value = item.error
            rawColumns.value = []
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
            { name: 'SQL', type: 'VARCHAR', comment: '', width: 300 },
            { name: '状态', type: 'VARCHAR', comment: '' },
            { name: '受影响行数', type: 'BIGINT', comment: '' },
            { name: '备注', type: 'VARCHAR', comment: '', width: 200 },
        ]

        const summaryData = modifyItems.map((item: any) => ({
            SQL: item.sql,
            状态: item.status === 'success' ? '成功' : item.status === 'rolled_back' ? '已回滚' : '失败',
            受影响行数: item.affected || 0,
            备注: item.error || (item.status === 'rolled_back' ? '事务回滚' : ''),
        }))

        canEdit.value = false
        tableKeys.value = []
        rawColumns.value = summaryColumns
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

    // ── 执行方法 ──

    function exec(silent = false, overrideSql?: string) {
        const sqlExec = overrideSql ?? getSelection()?.toString()
        if (!sqlExec) {
            if (!silent) ElMessage({ message: '请先选择SQL', type: 'error' })
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
        abortController = new AbortController()
        execSQL(
            { connId, schema, sql: effiectiveSql, maxLine: maxLine.value, tableName: currentSelectTable.value },
            abortController.signal
        )
            .then((resp) => {
                executionTime.value = Math.round(performance.now() - startTime)
                applyResultToUI(resp.data.data)
                exectingSql.value = false
                abortController = null
            })
            .catch((error) => {
                if (isCancel(error)) {
                    sqlError.value = '执行已终止'
                } else {
                    sqlError.value = error.message || '执行失败'
                }
                rawColumns.value = []
                result.value = []
                exectingSql.value = false
                abortController = null
            })
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
        rawColumns.value = []
        result.value = []

        const startTime = performance.now()
        const fullSql = statements.join(';')

        abortController = new AbortController()
        execSQL({ connId, schema, sql: fullSql, maxLine: maxLine.value, batch: 'true' }, abortController.signal)
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
            })
            .catch((error) => {
                if (isCancel(error)) {
                    sqlError.value = '执行已终止'
                } else {
                    sqlError.value = error.message || '执行失败'
                }
                rawColumns.value = []
                result.value = []
                exectingSql.value = false
                abortController = null
            })
    }

    function executeCurrentStatement() {
        if (exectingSql.value) return
        const stmt = getCurrentStatement()
        if (!stmt) {
            ElMessage({ message: '光标处未找到可执行的 SQL 语句', type: 'warning' })
            return
        }
        exec(false, stmt.text)
    }

    /** 统一执行入口：有选中内容时执行选中内容，否则执行光标所在语句 */
    function execSmart() {
        if (exectingSql.value) {
            stopExec()
            return
        }
        const view = getEditorView()
        if (view) {
            const { from, to } = view.state.selection.main
            if (from !== to) {
                exec(false, view.state.sliceDoc(from, to))
                return
            }
        }
        executeCurrentStatement()
    }

    function stopExec() {
        if (abortController) {
            abortController.abort()
            abortController = null
        }
        exectingSql.value = false
        sqlError.value = '执行已终止'
        rawColumns.value = []
        result.value = []
    }

    async function execFile(options: any) {
        const file = options.file
        const text = await file.text()

        if (!text.trim()) {
            ElMessage({ message: '文件中没有可执行的 SQL', type: 'warning' })
            return
        }

        ElMessage({ message: `正在执行文件: ${file.name}...`, type: 'info' })
        exectingSql.value = true
        executionTime.value = null
        sqlError.value = ''
        rawColumns.value = []
        result.value = []

        const startTime = performance.now()
        abortController = new AbortController()
        execSQL(
            { connId, schema, sql: text, maxLine: maxLine.value, batch: 'true', isFile: 'true' },
            abortController.signal
        )
            .then((resp) => {
                executionTime.value = Math.round(performance.now() - startTime)
                const data = resp.data.data as any
                const results = data.results || []
                const errorItems = results.filter((r: any) => r.status === 'error')
                const successItems = results.filter((r: any) => r.status === 'success')
                const rolledBackItems = results.filter((r: any) => r.status === 'rolled_back')

                if (errorItems.length === 0) {
                    ElMessage({ message: `全部 ${successItems.length} 条执行成功`, type: 'success' })
                    const lastQuery = [...results].reverse().find((r: any) => r.type === 'query' && r.status === 'success')
                    if (lastQuery) {
                        applyResultToUI(lastQuery)
                    } else {
                        const lastModify = [...results]
                            .reverse()
                            .find((r: any) => r.type === 'modify' && r.status === 'success')
                        if (lastModify) {
                            applyResultToUI(lastModify)
                        }
                    }
                } else {
                    const firstError = errorItems[0]
                    const errorDetail =
                        errorItems.length === 1
                            ? firstError.error
                            : `第 ${results.indexOf(firstError) + 1} 条起，共 ${errorItems.length} 条失败。首条错误: ${firstError.error}`
                    sqlError.value = errorDetail
                    ElMessage({
                        message: `${successItems.length} 成功, ${errorItems.length} 失败${
                            rolledBackItems.length > 0 ? ', ' + rolledBackItems.length + ' 条已回滚' : ''
                        }`,
                        type: 'warning',
                        duration: 6000,
                    })
                }
                exectingSql.value = false
                abortController = null
            })
            .catch((error) => {
                if (isCancel(error)) {
                    sqlError.value = '执行已终止'
                } else {
                    sqlError.value = error.message || '文件执行失败'
                }
                exectingSql.value = false
                abortController = null
            })
    }

    // ── 导出 ──

    function handleExportResult(command: string) {
        if (result.value.length === 0) {
            ElMessage({ message: '请先执行查询', type: 'warning' })
            return
        }

        if (command === 'insert') {
            exportCurrentToSqlInsert()
        } else if (command === 'update') {
            exportCurrentToSqlUpdate()
        } else if (command === 'xlsx') {
            exportCurrentToXlsx()
        } else if (command === 'csv') {
            const cols = rawColumns.value.map((col: any) => col.name)
            exportToCsv(cols, result.value, currentSelectTable.value || 'query_result')
            ElMessage({ message: '已导出 CSV', type: 'success' })
        } else if (command === 'json') {
            exportToJson(result.value, currentSelectTable.value || 'query_result')
            ElMessage({ message: '已导出 JSON', type: 'success' })
        }
    }

    function exportCurrentToXlsx() {
        if (result.value.length === 0) {
            ElMessage({ message: '请先执行查询，在导出', type: 'warning' })
            return
        }

        const rowCount = result.value.length
        const loading = ElLoading.service({
            lock: true,
            text: `正在导出 ${rowCount} 行数据到 Excel...`,
            background: 'rgba(0, 0, 0, 0.7)',
        })

        setTimeout(() => {
            try {
                const header: any = {}
                const keys: any = []
                rawColumns.value.forEach((col: any) => {
                    keys.push(col.name)
                    header[col.name] = col.name
                })

                const obj = {
                    header,
                    title: '',
                    key: keys,
                    data: [...result.value].map((row) => {
                        const copy = { ...row }
                        delete copy['col-idx']
                        return copy
                    }),
                    filename: currentSelectTable.value,
                    autoWidth: false,
                }
                excel.exportJsonToExcel(obj)
            } finally {
                loading.close()
            }
        }, 50)
    }

    function exportCurrentToSqlInsert() {
        if (result.value.length === 0) {
            ElMessage({ message: '请先执行查询，在导出SQL', type: 'warning' })
            return
        }
        const sqlArr: string[] = []
        const columnArr: any[] = []
        let sql = `insert into ${currentSelectTable.value} (`
        for (let i = 0; i < rawColumns.value.length; i++) {
            columnArr.push(rawColumns.value[i].name)
        }
        for (let j = 0; j < result.value.length; j++) {
            const rowVal: string[] = []
            const valueArr: string[] = []
            for (let i = 0; i < rawColumns.value.length; i++) {
                const val = result.value[j][rawColumns.value[i].name]
                rowVal.push(fmtVal(val, effectiveDbType.value))
            }
            valueArr.push(rowVal.join(','))
            sqlArr.push(sql + columnArr.join(',') + ') values (' + valueArr.join(',') + ')')
        }

        copyToClipboard(
            sqlArr.length > 0 ? sqlArr.join(';\n') + ';' : '',
            () => ElMessage({ message: '已复制到粘贴板', type: 'success' }),
            () => ElMessage({ message: '导出失败', type: 'error' })
        )
    }

    function exportCurrentToSqlUpdate() {
        if (result.value.length === 0) {
            ElMessage({ message: '请先执行查询，在导出SQL', type: 'warning' })
            return
        }
        const sqlArr: string[] = []
        let sql = `update ${currentSelectTable.value} set `
        for (let j = 0; j < result.value.length; j++) {
            const rowVal: string[] = []
            // 跳过第一个列（通常是主键，作为 where 条件）
            for (let i = 1; i < rawColumns.value.length; i++) {
                const column = rawColumns.value[i].name
                const val = result.value[j][column]
                rowVal.push(quoteId(column, effectiveDbType.value) + ' = ' + fmtVal(val, effectiveDbType.value))
            }

            const conditionVal: string[] = []
            for (let i = 0; i < tableKeys.value.length; i++) {
                conditionVal.push(
                    buildWhereCondition(tableKeys.value[i], result.value[j][tableKeys.value[i]], effectiveDbType.value)
                )
            }

            sqlArr.push(sql + rowVal.join(', ') + ' where ' + conditionVal.join(' and '))
        }

        copyToClipboard(
            sqlArr.length > 0 ? sqlArr.join(';\n') + ';' : '',
            () => ElMessage({ message: '已复制到粘贴板', type: 'success' }),
            () => ElMessage({ message: '导出失败', type: 'error' })
        )
    }

    // ── 内联编辑保存由 SqlResultPanel 组件处理 ──

    return {
        // state
        result,
        rawColumns,
        tableKeys,
        canEdit,
        batchResults,
        sqlError,
        executionTime,
        exectingSql,
        isBatchMode,
        activeResultTab,
        currentSelectTable,
        // computed
        batchDisplayTabs,
        canInlineEdit,
        // methods
        exec,
        execSmart,
        execBatch,
        executeCurrentStatement,
        stopExec,
        execFile,
        applyResultToUI,
        displayBatchResult,
        displayModifySummary,
        onBatchTabChange,
        handleExportResult,
        // helpers
        extractSqlStatements,
        extractEffectiveSql,
        extractTableName,
        checkSql,
        formatDuration,
        getSqlPreview,
    }
}
