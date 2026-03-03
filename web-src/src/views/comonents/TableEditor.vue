<template>
    <el-tabs v-model="activeName" type="card" style="height:500px;" @tab-click="loadData">
        <el-tab-pane label="字段" name="colums">
            <el-table :data="columnList" style="width: 100%" height="470">
                <el-table-column label="名称" width="250">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnName" style="margin-bottom: 10px;" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnNameEdit" class="column_name">
                            <span>{{ scope.row.columnName }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改名称"
                                    @click="scope.row.onColumnNameEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnNameEdit">
                            <el-input v-model="scope.row.columnName" style="margin-bottom: 10px;width: 200px;" />
                            <div style="display: inline-block;margin-left: 3px;">
                                <el-icon title="保存" style="cursor: pointer;margin-right: 5px;" :size="12"
                                    @click="modifyColumnName(scope.row.idx, scope.row.columnName)">
                                    <Check />
                                </el-icon>
                                <el-icon title="取消" style="cursor: pointer;" :size="12"
                                    @click="scope.row.onColumnNameEdit = false; scope.row.columnName = columnListOrigin[scope.row.idx].columnName">
                                    <Close />
                                </el-icon>
                            </div>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="类型" width="180">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnType" style="margin-bottom: 10px;" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnTypeEdit" class="column_type">
                            <span>{{ scope.row.columnType }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改类型"
                                    @click="scope.row.onColumnTypeEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnTypeEdit">
                            <el-input v-model="scope.row.columnType" style="margin-bottom: 10px;width: 135px;" />
                            <div style="display: inline-block;margin-left: 3px;">
                                <el-icon title="保存" style="cursor: pointer;margin-right: 5px;" :size="12"
                                    @click="modifyColumnType(scope.row.idx, scope.row.columnType)">
                                    <Check />
                                </el-icon>
                                <el-icon title="取消" style="cursor: pointer;" :size="12"
                                    @click="scope.row.onColumnTypeEdit = false; scope.row.columnType = columnListOrigin[scope.row.idx].columnType">
                                    <Close />
                                </el-icon>
                            </div>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="默认值" width="140">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnDefault" style="margin-bottom: 10px;" placeholder="NULL" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnDefaultEdit" class="column_default">
                            <span>{{ scope.row.columnDefault ?? 'NULL' }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改默认值"
                                    @click="scope.row.onColumnDefaultEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnDefaultEdit">
                            <el-input v-model="scope.row.columnDefault" style="margin-bottom: 10px;width: 100px;" placeholder="NULL" />
                            <div style="display: inline-block;margin-left: 3px;">
                                <el-icon title="保存" style="cursor: pointer;margin-right: 5px;" :size="12"
                                    @click="modifyColumnDefault(scope.row.idx, scope.row.columnDefault)">
                                    <Check />
                                </el-icon>
                                <el-icon title="取消" style="cursor: pointer;" :size="12"
                                    @click="scope.row.onColumnDefaultEdit = false; scope.row.columnDefault = columnListOrigin[scope.row.idx].columnDefault">
                                    <Close />
                                </el-icon>
                            </div>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="可空" width="80">
                    <template #default="scope">
                        <el-switch v-model="scope.row.isNullable" class="ml-2" :disabled="scope.row.columnKey === 'PRI'"
                            style="margin-left: 10px; --el-switch-on-color: #13ce66;" active-value="YES"
                            inactive-value="NO" />
                    </template>
                </el-table-column>
                <el-table-column label="注释">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnComment" style="margin-bottom: 10px;"
                            type="textarea" autosize />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnCommentEdit" class="column_comment">
                            <span>{{ scope.row.columnComment }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改注释"
                                    @click="scope.row.onColumnCommentEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnCommentEdit">
                            <el-input v-model="scope.row.columnComment" style="margin-bottom: 10px;min-width: 76%;"
                                type="textarea" autosize />
                            <div style="display: inline-block;margin-left: 3px;">
                                <el-icon title="保存" style="cursor: pointer;margin-right: 5px;" :size="12"
                                    @click="modifyColumnComment(scope.row.idx, scope.row.columnComment)">
                                    <Check />
                                </el-icon>
                                <el-icon title="取消" style="cursor: pointer;" :size="12"
                                    @click="scope.row.onColumnCommentEdit = false; scope.row.columnComment = columnListOrigin[scope.row.idx].columnComment">
                                    <Close />
                                </el-icon>
                            </div>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="操作" width="70">
                    <template #default="scope">
                        <div v-if="scope.row.isNew">
                            <el-icon title="保存新增" style="cursor: pointer;margin-right: 5px;" :size="12"
                                @click="doColAdd(scope.row)">
                                <Check />
                            </el-icon>
                            <el-icon title="取消新增" style="cursor: pointer;" :size="12" @click="cancelAdd(scope.row.idx)">
                                <Close />
                            </el-icon>
                        </div>
                        <div v-if="!scope.row.isNew">
                            <el-icon title="新增字段" style="cursor: pointer;margin-right: 5px;" :size="12"
                                @click="addCol(scope.row.idx)">
                                <Plus />
                            </el-icon>
                            <el-popconfirm title="删除此字段？" hide-after="0" @confirm="delCol(scope.row.idx)">
                                <template #reference>
                                    <el-icon title="删除字段" style="cursor: pointer;" :size="12">
                                        <Minus />
                                    </el-icon>
                                </template>
                            </el-popconfirm>
                        </div>
                    </template>
                </el-table-column>
            </el-table>
        </el-tab-pane>
        <el-tab-pane label="索引" name="indexes">
            <div style="margin-bottom: 8px;">
                <el-button size="small" @click="showAddIndexDialog">新建索引</el-button>
            </div>
            <el-table :data="indexList" style="width: 100%" height="430" row-key="rowKey">
                <el-table-column prop="indexName" label="索引名" width="200" />
                <el-table-column prop="columnName" label="字段" width="200" />
                <el-table-column label="唯一" width="80">
                    <template #default="scope">
                        <span>{{ scope.row.nonUnique == 0 ? '是' : '否' }}</span>
                    </template>
                </el-table-column>
                <el-table-column prop="seqInIndex" label="序号" width="60" />
                <el-table-column prop="indexType" label="类型" width="120" />
                <el-table-column prop="indexComment" label="注释" />
                <el-table-column label="操作" width="70">
                    <template #default="scope">
                        <el-popconfirm :title="'删除索引 ' + scope.row.indexName + '？'" hide-after="0" @confirm="dropIndex(scope.row.indexName)">
                            <template #reference>
                                <el-icon title="删除索引" style="cursor: pointer;" :size="14">
                                    <Delete />
                                </el-icon>
                            </template>
                        </el-popconfirm>
                    </template>
                </el-table-column>
            </el-table>
        </el-tab-pane>

        <el-tab-pane label="选项" name="option">
            <div v-if="Object.keys(tableOptionsData).length > 0" style="padding: 10px;">
                <el-descriptions :column="2" border>
                    <el-descriptions-item v-for="(val, key) in tableOptionsData" :key="key" :label="optionLabel(key)">
                        {{ val ?? '-' }}
                    </el-descriptions-item>
                </el-descriptions>
            </div>
            <el-empty v-else description="暂不支持" />
        </el-tab-pane>

        <el-tab-pane label="统计" name="statistics">
            <div v-if="Object.keys(tableStatsData).length > 0" style="padding: 10px;">
                <el-descriptions :column="2" border>
                    <el-descriptions-item v-for="(val, key) in tableStatsData" :key="key" :label="statsLabel(key)">
                        {{ formatStatsValue(key, val) }}
                    </el-descriptions-item>
                </el-descriptions>
            </div>
            <el-empty v-else description="暂不支持" />
        </el-tab-pane>

        <el-tab-pane label="DDL" name="showCreate">
            <el-icon style="margin-top: 5px;position: absolute;right: 0px;cursor: pointer;z-index: 9999999;" size="16" @click="copyCreateScript">
                <CopyDocument />
            </el-icon>
            <el-scrollbar style="font-size: 18px;width: 100%;height: 470px;">
                <pre><code class="language-sql" v-bind:innerHTML="tableCreateDdl" ref="tableCreateDdlRef"></code></pre>
            </el-scrollbar>
        </el-tab-pane>

        <el-tab-pane label="操作" name="tableOps">
            <div style="padding: 20px;">
                <div style="margin-bottom: 20px;">
                    <span style="margin-right: 10px;">重命名表：</span>
                    <el-input v-model="newTableName" style="width: 300px;" :placeholder="props.tableMeta?.tableName" />
                    <el-button style="margin-left: 10px;" @click="renameTable">执行</el-button>
                </div>
                <div style="margin-bottom: 20px;">
                    <span style="margin-right: 10px;">修改注释：</span>
                    <el-input v-model="newTableComment" style="width: 300px;" placeholder="输入新的表注释" />
                    <el-button style="margin-left: 10px;" @click="modifyTableComment">执行</el-button>
                </div>
                <el-divider />
            </div>
        </el-tab-pane>
    </el-tabs>

    <!-- 新建索引对话框 -->
    <el-dialog v-model="addIndexDialogVisible" title="新建索引" width="500px" :draggable="true" destroy-on-close>
        <el-form label-width="80px">
            <el-form-item label="索引名">
                <el-input v-model="newIndex.name" placeholder="idx_xxx" />
            </el-form-item>
            <el-form-item label="类型">
                <el-select v-model="newIndex.type" style="width: 100%;">
                    <el-option label="普通索引" value="INDEX" />
                    <el-option label="唯一索引" value="UNIQUE" />
                </el-select>
            </el-form-item>
            <el-form-item label="字段">
                <el-select v-model="newIndex.columns" multiple style="width: 100%;" placeholder="选择字段">
                    <el-option v-for="col in columnListOrigin" :key="col.columnName" :label="col.columnName" :value="col.columnName" />
                </el-select>
            </el-form-item>
        </el-form>
        <template #footer>
            <el-button @click="addIndexDialogVisible = false">取消</el-button>
            <el-button type="primary" @click="createIndex">创建</el-button>
        </template>
    </el-dialog>
</template>
<script setup>
import { ref, onMounted, watch } from 'vue'
import http from '@/js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'
import { format } from 'sql-formatter'
import hljs from 'highlight.js/lib/core'
import * as highlightSql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/stackoverflow-light.css'
import copyToClipboard from '@/js/utils/copy-to-clipboard.js'

hljs.registerLanguage('sql', highlightSql.default);

const activeName = ref("colums")

let columnListOrigin = []
const columnList = ref([])
const tableCreateDdl = ref("")
const tableCreateDdlRef = ref()

const indexList = ref([])
const tableOptionsData = ref({})
const tableStatsData = ref({})

const newTableName = ref("")
const newTableComment = ref("")

const addIndexDialogVisible = ref(false)
const newIndex = ref({ name: "", type: "INDEX", columns: [] })

const props = defineProps({
    tableMeta: Object,
})

const emit = defineEmits(['tableDrop'])

onMounted(() => {
    loadData({ props: { name: 'colums' } })
})

watch(() => props.tableMeta, (newVal, oldVal) => {
    if (newVal && newVal !== oldVal) {
        loadData({ props: { name: 'colums' } });
    }
}, { deep: true, immediate: true });

function getPostBody() {
    return { connId: props.tableMeta.connId, schema: props.tableMeta.schema, tableName: props.tableMeta.tableName }
}

function loadData(pane) {
    if (!props.tableMeta || !props.tableMeta.connId) return
    const name = pane.props?.name ?? pane.paneName
    if (name === "colums") {
        http.post("/listTableColumns", getPostBody())
            .then((resp) => {
                columnList.value = resp.data.data
                for (let i = 0; i < columnList.value.length; i++) {
                    columnList.value[i]['idx'] = i
                }
                columnListOrigin = JSON.parse(JSON.stringify(columnList.value))
            })
    } else if (name === "indexes") {
        loadIndexes()
    } else if (name === "option") {
        loadOptions()
    } else if (name === "statistics") {
        loadStatistics()
    } else if (name === "showCreate") {
        loadCreateDdl()
    }
}

function loadIndexes() {
    http.post("/listIndexes", getPostBody())
        .then((resp) => {
            const raw = resp.data.data || []
            indexList.value = raw.map((r, i) => ({
                rowKey: i,
                indexName: r.INDEX_NAME || r.index_name || r.indexName,
                columnName: r.COLUMN_NAME || r.column_name || r.columnName,
                nonUnique: r.NON_UNIQUE ?? r.non_unique ?? r.nonUnique,
                seqInIndex: r.SEQ_IN_INDEX || r.seq_in_index || r.seqInIndex,
                indexType: r.INDEX_TYPE || r.index_type || r.indexType,
                indexComment: r.INDEX_COMMENT || r.index_comment || r.indexComment || '',
            }))
        })
}

function loadOptions() {
    http.post("/tableOptions", getPostBody())
        .then((resp) => { tableOptionsData.value = resp.data.data || {} })
}

function loadStatistics() {
    http.post("/tableStatistics", getPostBody())
        .then((resp) => { tableStatsData.value = resp.data.data || {} })
}

function loadCreateDdl() {
    let sqlStr = ""
    const dbType = dbSchemaProxy.getDbType(props.tableMeta.schema)
    if (dbType === 'mysql') {
        sqlStr = "show create table " + props.tableMeta.tableName
    } else if (dbType === 'oracle') {
        sqlStr = "select dbms_metadata.get_ddl('TABLE','" + props.tableMeta.tableName.toUpperCase() + "') from dual"
    } else {
        tableCreateDdl.value = "暂不支持"
        return
    }
    const params = new URLSearchParams()
    params.append("connId", props.tableMeta.connId)
    params.append("schema", props.tableMeta.schema)
    params.append("sql", sqlStr)
    params.append("maxLine", 1)
    http.post("/execSQL", params)
        .then((resp) => {
            const data = resp.data.data.data[0]
            const sql = format(data[Object.keys(data)[0].trim()] || "", { language: getSqlLang(props.tableMeta.schema) })
            tableCreateDdl.value = hljs.highlight(sql, { language: 'sql' }).value
        }).catch((error) => { console.log(error) });
}

function getSqlLang(schema) {
    const dbType = dbSchemaProxy.getDbType(schema).toLowerCase()
    if (dbType === "oracle") return "plsql"
    if (dbType === "mysql") return "mysql"
    return "sql"
}

const OPTION_LABELS = {
    ENGINE: '存储引擎', TABLE_COLLATION: '排序规则', TABLE_COMMENT: '表注释',
    ROW_FORMAT: '行格式', AUTO_INCREMENT: '自增值', CREATE_OPTIONS: '创建选项',
    TABLESPACE_NAME: '表空间', PCT_FREE: 'PCT_FREE', INI_TRANS: 'INI_TRANS',
    LOGGING: '日志', COMPRESSION: '压缩', TABLE_NAME: '表名',
}
function optionLabel(key) { return OPTION_LABELS[key] || key }

const STATS_LABELS = {
    TABLE_ROWS: '行数(估算)', DATA_LENGTH: '数据大小', INDEX_LENGTH: '索引大小',
    DATA_FREE: '碎片空间', AVG_ROW_LENGTH: '平均行长', CREATE_TIME: '创建时间',
    UPDATE_TIME: '更新时间', NUM_ROWS: '行数(估算)', BLOCKS: '数据块数',
    AVG_ROW_LEN: '平均行长', LAST_ANALYZED: '最后分析时间',
}
function statsLabel(key) { return STATS_LABELS[key] || key }

function formatStatsValue(key, val) {
    if (val === null || val === undefined) return '-'
    if (['DATA_LENGTH', 'INDEX_LENGTH', 'DATA_FREE'].includes(key)) {
        const num = Number(val)
        if (isNaN(num)) return val
        if (num >= 1073741824) return (num / 1073741824).toFixed(2) + ' GB'
        if (num >= 1048576) return (num / 1048576).toFixed(2) + ' MB'
        if (num >= 1024) return (num / 1024).toFixed(2) + ' KB'
        return num + ' B'
    }
    return val
}

// ========== 字段操作 ==========
function modifyColumnName(seq, newName) {
    const sql = "alter table " + props.tableMeta.tableName + " change " + columnListOrigin[seq].columnName + " " + newName + " " + columnListOrigin[seq].columnType + " DEFAULT " + columnListOrigin[seq].columnDefault + " comment '" + columnListOrigin[seq].columnComment + "'";
    execSql(sql, () => {
        columnListOrigin[seq].columnName = newName
        columnList.value[seq].onColumnNameEdit = false
    })
}

function modifyColumnType(seq, newType) {
    const sql = "alter table " + props.tableMeta.tableName + " modify " + columnListOrigin[seq].columnName + " " + newType + " DEFAULT " + columnListOrigin[seq].columnDefault + " comment '" + columnListOrigin[seq].columnComment + "'";
    execSql(sql, () => {
        columnListOrigin[seq].columnType = newType
        columnList.value[seq].onColumnTypeEdit = false
    })
}

function modifyColumnDefault(seq, newDefault) {
    const defaultVal = (newDefault === null || newDefault === undefined || newDefault === '') ? 'NULL' : "'" + newDefault + "'"
    const sql = "alter table " + props.tableMeta.tableName + " modify " + columnListOrigin[seq].columnName + " " + columnListOrigin[seq].columnType + " DEFAULT " + defaultVal + " comment '" + columnListOrigin[seq].columnComment + "'";
    execSql(sql, () => {
        columnListOrigin[seq].columnDefault = newDefault
        columnList.value[seq].onColumnDefaultEdit = false
    })
}

function modifyColumnComment(seq, newComment) {
    const sql = "alter table " + props.tableMeta.tableName + " modify " + columnListOrigin[seq].columnName + " " + columnListOrigin[seq].columnType + " DEFAULT " + columnListOrigin[seq].columnDefault + " comment '" + newComment + "'";
    execSql(sql, () => {
        columnListOrigin[seq].columnComment = newComment
        columnList.value[seq].onColumnCommentEdit = false
    })
}

function addCol(targetIdx) {
    const index = columnList.value.findIndex(item => item.idx === targetIdx);
    if (index !== -1) {
        for (let i = index + 1; i < columnList.value.length; i++) {
            columnList.value[i].idx += 1;
        }
        columnList.value.splice(index + 1, 0, { isNew: true, columnName: "", columnType: "", isNullable: "YES", columnComment: "", columnDefault: "", after: columnList.value[index].columnName });
    }
}

function cancelAdd(targetIdx) {
    const index = columnList.value.findIndex(item => item.idx === targetIdx);
    if (index === -1) return
    for (let i = index + 1; i < columnList.value.length; i++) {
        columnList.value[i].idx -= 1;
    }
    columnList.value.splice(index, 1);
}

function doColAdd(column) {
    let sql = "alter table " + props.tableMeta.tableName + " add " + column.columnName + " " + column.columnType;
    if ("YES" === column.isNullable) {
        sql += " default null "
    } else {
        sql += " not null "
    }
    if (column.columnDefault) {
        sql += " default '" + column.columnDefault + "' "
    }
    sql += " comment '" + column.columnComment + "' after " + column.after;
    execSql(sql, () => loadData({ props: { name: 'colums' } }))
}

function delCol(seq) {
    const sql = "alter table " + props.tableMeta.tableName + " drop " + columnListOrigin[seq].columnName;
    execSql(sql, () => loadData({ props: { name: 'colums' } }))
}

// ========== 索引操作 ==========
function showAddIndexDialog() {
    newIndex.value = { name: "", type: "INDEX", columns: [] }
    addIndexDialogVisible.value = true
}

function createIndex() {
    if (!newIndex.value.name || newIndex.value.columns.length === 0) {
        ElMessage({ message: "请填写索引名和选择字段", type: "warning" })
        return
    }
    const prefix = newIndex.value.type === "UNIQUE" ? "CREATE UNIQUE INDEX" : "CREATE INDEX"
    const sql = prefix + " " + newIndex.value.name + " ON " + props.tableMeta.tableName + " (" + newIndex.value.columns.join(", ") + ")"
    execSql(sql, () => {
        addIndexDialogVisible.value = false
        loadIndexes()
        ElMessage({ message: "索引创建成功", type: "success" })
    })
}

function dropIndex(indexName) {
    const dbType = dbSchemaProxy.getDbType(props.tableMeta.schema)
    let sql = ""
    if (dbType === "mysql") {
        sql = "DROP INDEX " + indexName + " ON " + props.tableMeta.tableName
    } else {
        sql = "DROP INDEX " + indexName
    }
    execSql(sql, () => {
        loadIndexes()
        ElMessage({ message: "索引已删除", type: "success" })
    })
}

// ========== 表级操作 ==========
function renameTable() {
    if (!newTableName.value) {
        ElMessage({ message: "请输入新表名", type: "warning" })
        return
    }
    const dbType = dbSchemaProxy.getDbType(props.tableMeta.schema)
    let sql = ""
    if (dbType === "oracle") {
        sql = "ALTER TABLE " + props.tableMeta.tableName + " RENAME TO " + newTableName.value
    } else {
        sql = "RENAME TABLE " + props.tableMeta.tableName + " TO " + newTableName.value
    }
    execSql(sql, () => {
        ElMessage({ message: "表已重命名为 " + newTableName.value, type: "success" })
        props.tableMeta.tableName = newTableName.value
    })
}

function modifyTableComment() {
    if (newTableComment.value === "") {
        ElMessage({ message: "请输入表注释", type: "warning" })
        return
    }
    const dbType = dbSchemaProxy.getDbType(props.tableMeta.schema)
    let sql = ""
    if (dbType === "oracle") {
        sql = "COMMENT ON TABLE " + props.tableMeta.tableName + " IS '" + newTableComment.value + "'"
    } else {
        sql = "ALTER TABLE " + props.tableMeta.tableName + " COMMENT = '" + newTableComment.value + "'"
    }
    execSql(sql, () => {
        ElMessage({ message: "表注释已修改", type: "success" })
    })
}

function truncateTable() {
    execSql("TRUNCATE TABLE " + props.tableMeta.tableName, () => {
        ElMessage({ message: "表已清空", type: "success" })
    })
}

function dropTable() {
    execSql("DROP TABLE " + props.tableMeta.tableName, () => {
        ElMessage({ message: "表已删除", type: "success" })
        emit('tableDrop')
    })
}

// ========== 通用 ==========
function execSql(sql, succ) {
    const params = new URLSearchParams()
    params.append("connId", props.tableMeta.connId)
    params.append("schema", props.tableMeta.schema)
    params.append("tableName", props.tableMeta.tableName)
    params.append("sql", sql)
    params.append("maxLine", 1)
    http.post("/execSQL", params)
        .then((resp) => { if (succ) succ(resp) })
        .catch((error) => { console.log(error) });
}

function copyCreateScript() {
    copyToClipboard(tableCreateDdlRef.value.innerText,
        () => ElMessage({ message: "已复制到粘贴板", type: "success" }),
        () => ElMessage({ message: "复制失败", type: "error" })
    )
}
</script>

<style lang="less" scoped>
.modify_icon {
    width: 16px;
    height: 16px;
    position: relative;
    right: -5px;
    top: 50%;
    transform: translateY(-50%);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.2s ease-in-out;
}

.column_name:hover .modify_icon,
.column_type:hover .modify_icon,
.column_default:hover .modify_icon,
.column_comment:hover .modify_icon {
    opacity: 1;
}

.modify_icon:hover {
    opacity: 0.8 !important;
}
</style>

<style>
.el-table .cell {
    padding: 0 5px !important;
}
</style>
