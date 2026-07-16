<template>
    <el-tabs v-model="activeName" type="card" class="table-editor-tabs" @tab-click="loadData">
        <el-tab-pane label="字段" name="colums">
            <div style="margin-bottom: 8px; display: flex; gap: 8px;">
                <el-button size="small" @click="addColAtEnd">添加字段</el-button>
                <el-button size="small" type="danger" :disabled="selectedColumns.length === 0" @click="batchDelCols">删除选中</el-button>
            </div>
            <el-table :data="columnList" style="width: 100%" class="col-table" height="100%" @selection-change="onColSelectionChange">
                <el-table-column type="selection" width="40" />
                <el-table-column label="名称" width="240" resizable>
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnName" size="small" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnNameEdit" class="column_name" @dblclick="scope.row.onColumnNameEdit = true">
                            <span>{{ scope.row.columnName }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改名称"
                                    @click="scope.row.onColumnNameEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnNameEdit" style="display:flex;align-items:center;gap:3px;">
                            <el-input v-model="scope.row.columnName" style="flex:1;min-width:100px;" size="small" />
                            <el-icon class="edit-action edit-save" title="保存" :size="14" @click="modifyColumnName(scope.row.idx, scope.row.columnName)"><Check /></el-icon>
                            <el-icon class="edit-action edit-cancel" title="取消" :size="14" @click="scope.row.onColumnNameEdit = false; scope.row.columnName = columnListOrigin[scope.row.idx].columnName"><Refresh /></el-icon>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="类型" width="210" resizable>
                    <template #default="scope">
                        <el-autocomplete
                            v-if="scope.row.isNew"
                            v-model="scope.row.columnType"
                            :fetch-suggestions="queryTypeOptions"
                            style="width:100%;"
                            size="small"
                            clearable
                            placeholder="类型"
                        />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnTypeEdit" class="column_type" @dblclick="scope.row.onColumnTypeEdit = true">
                            <span>{{ scope.row.columnType }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改类型"
                                    @click="scope.row.onColumnTypeEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnTypeEdit" style="display:flex;align-items:center;gap:3px;">
                            <el-autocomplete
                                v-model="scope.row.columnType"
                                :fetch-suggestions="queryTypeOptions"
                                style="flex:1;min-width:100px;"
                                size="small"
                                clearable
                            />
                            <el-icon class="edit-action edit-save" title="保存" :size="14" @click="modifyColumnType(scope.row.idx, scope.row.columnType)"><Check /></el-icon>
                            <el-icon class="edit-action edit-cancel" title="取消" :size="14" @click="scope.row.onColumnTypeEdit = false; scope.row.columnType = columnListOrigin[scope.row.idx].columnType"><Refresh /></el-icon>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="默认值" width="140" resizable>
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnDefault" size="small" placeholder="NULL" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnDefaultEdit" class="column_default" @dblclick="scope.row.onColumnDefaultEdit = true">
                            <span>{{ scope.row.columnDefault ?? 'NULL' }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改默认值"
                                    @click="scope.row.onColumnDefaultEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnDefaultEdit" style="display:flex;align-items:center;gap:3px;">
                            <el-input v-model="scope.row.columnDefault" style="flex:1;min-width:60px;" size="small" placeholder="NULL" />
                            <el-icon class="edit-action edit-save" title="保存" :size="14" @click="modifyColumnDefault(scope.row.idx, scope.row.columnDefault)"><Check /></el-icon>
                            <el-icon class="edit-action edit-cancel" title="取消" :size="14" @click="scope.row.onColumnDefaultEdit = false; scope.row.columnDefault = columnListOrigin[scope.row.idx].columnDefault"><Refresh /></el-icon>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="可空" width="80" resizable>
                    <template #default="scope">
                        <el-switch v-model="scope.row.isNullable" class="ml-2" :disabled="scope.row.columnKey === 'PRI'"
                            style="margin-left: 10px; --el-switch-on-color: #13ce66;" active-value="YES"
                            inactive-value="NO" />
                    </template>
                </el-table-column>
                <el-table-column label="注释" resizable min-width="200">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnComment" size="small" type="textarea" autosize />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnCommentEdit" class="column_comment" @dblclick="scope.row.onColumnCommentEdit = true">
                            <span>{{ scope.row.columnComment }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改注释"
                                    @click="scope.row.onColumnCommentEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnCommentEdit" style="display:flex;align-items:center;gap:3px;">
                            <el-input v-model="scope.row.columnComment" style="flex:1;min-width:100px;" size="small"
                                type="textarea" autosize />
                            <el-icon class="edit-action edit-save" title="保存" :size="14" @click="modifyColumnComment(scope.row.idx, scope.row.columnComment)"><Check /></el-icon>
                            <el-icon class="edit-action edit-cancel" title="取消" :size="14" @click="scope.row.onColumnCommentEdit = false; scope.row.columnComment = columnListOrigin[scope.row.idx].columnComment"><Refresh /></el-icon>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="操作" width="100" resizable>
                    <template #default="scope">
                        <div v-if="scope.row.isNew" style="display:flex;align-items:center;gap:4px;">
                            <el-button size="small" type="primary" @click="doColAdd(scope.row)">保存</el-button>
                            <el-button size="small" @click="cancelAdd(scope.row.idx)">取消</el-button>
                        </div>
                    </template>
                </el-table-column>
            </el-table>
        </el-tab-pane>
        <el-tab-pane label="索引" name="indexes">
            <div style="margin-bottom: 8px;">
                <el-button size="small" @click="showAddIndexDialog">新建索引</el-button>
            </div>
            <el-table :data="indexList" style="width: 100%" class="col-table" row-key="rowKey" height="100%">
                <el-table-column label="索引名" width="200" resizable>
                    <template #default="scope">
                        <div v-if="!scope.row.onNameEdit" class="column_comment" @dblclick="scope.row.onNameEdit = true; scope.row._editName = scope.row.indexName">
                            <span>{{ scope.row.indexName }}</span>
                        </div>
                        <div v-else>
                            <el-input v-model="scope.row._editName" size="small" style="width: 150px;" />
                            <el-icon title="保存" style="cursor: pointer;margin-left:4px;margin-right:4px;" :size="12"
                                @click="renameIndex(scope.row)"><Check /></el-icon>
                            <el-icon title="取消" style="cursor: pointer;" :size="12"
                                @click="scope.row.onNameEdit = false"><Refresh /></el-icon>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column prop="columnName" label="字段" width="200" resizable />
                <el-table-column label="唯一" width="80" resizable>
                    <template #default="scope">
                        <span>{{ scope.row.nonUnique == 0 ? '是' : '否' }}</span>
                    </template>
                </el-table-column>
                <el-table-column prop="seqInIndex" label="序号" width="60" resizable />
                <el-table-column prop="indexType" label="类型" width="120" resizable />
                <el-table-column label="注释" resizable>
                    <template #default="scope">
                        <div v-if="!scope.row.onCommentEdit" class="column_comment" @dblclick="scope.row.onCommentEdit = true; scope.row._editComment = scope.row.indexComment">
                            <span>{{ scope.row.indexComment || '-' }}</span>
                            <span class="modify_icon">
                                <el-icon :size="12" style="cursor: pointer;" title="修改注释"
                                    @click="scope.row.onCommentEdit = true; scope.row._editComment = scope.row.indexComment">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-else>
                            <el-input v-model="scope.row._editComment" size="small" style="width: 140px;" />
                            <el-icon title="保存" style="cursor: pointer;margin-left:4px;margin-right:4px;" :size="12"
                                @click="saveIndexComment(scope.row)"><Check /></el-icon>
                            <el-icon title="取消" style="cursor: pointer;" :size="12"
                                @click="scope.row.onCommentEdit = false"><Refresh /></el-icon>
                        </div>
                    </template>
                </el-table-column>
                <el-table-column label="操作" width="70" resizable>
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

        <el-tab-pane label="外键" name="foreignKeys">
            <div style="margin-bottom: 8px;">
                <el-button size="small" @click="showAddFkDialog">新建外键</el-button>
            </div>
            <el-table :data="foreignKeyList" style="width: 100%" class="col-table" v-loading="fkLoading">
                <el-table-column prop="constraintName" label="约束名" min-width="180" show-overflow-tooltip resizable />
                <el-table-column prop="columnName" label="本表字段" width="160" resizable />
                <el-table-column prop="referencedTable" label="引用表" width="160" resizable />
                <el-table-column prop="referencedColumn" label="引用字段" width="160" resizable />
                <el-table-column prop="updateRule" label="ON UPDATE" width="120" resizable>
                    <template #default="scope">
                        <el-tag size="small" :type="ruleTagType(scope.row.updateRule)">{{ scope.row.updateRule || '-' }}</el-tag>
                    </template>
                </el-table-column>
                <el-table-column prop="deleteRule" label="ON DELETE" width="120" resizable>
                    <template #default="scope">
                        <el-tag size="small" :type="ruleTagType(scope.row.deleteRule)">{{ scope.row.deleteRule || '-' }}</el-tag>
                    </template>
                </el-table-column>
                <el-table-column label="操作" width="70" resizable>
                    <template #default="scope">
                        <el-popconfirm :title="'删除外键 ' + scope.row.constraintName + '？'" hide-after="0" @confirm="dropForeignKey(scope.row.constraintName)">
                            <template #reference>
                                <el-icon title="删除外键" style="cursor: pointer;" :size="14">
                                    <Delete />
                                </el-icon>
                            </template>
                        </el-popconfirm>
                    </template>
                </el-table-column>
            </el-table>
            <el-empty v-if="!fkLoading && foreignKeyList.length === 0" description="该表没有外键" />
        </el-tab-pane>

        <el-tab-pane label="选项" name="option">
            <div v-if="editableOptions.length > 0" style="padding: 10px;">
                <el-table :data="editableOptions" border size="small" style="width: 100%;">
                    <el-table-column label="选项" width="160" resizable>
                        <template #default="scope">{{ optionLabel(scope.row.key) }}</template>
                    </el-table-column>
                    <el-table-column label="值" resizable>
                        <template #default="scope">
                            <div v-if="!scope.row.editing" class="column_comment" @dblclick="scope.row.editing = true; scope.row._edit = scope.row.value">
                                <span>{{ scope.row.value ?? '-' }}</span>
                                <span v-if="EDITABLE_OPTIONS.has(scope.row.key)" class="modify_icon">
                                    <el-icon :size="12" style="cursor: pointer;" title="修改"
                                        @click="scope.row.editing = true; scope.row._edit = scope.row.value">
                                        <Edit />
                                    </el-icon>
                                </span>
                            </div>
                            <div v-else style="display:flex;align-items:center;gap:4px;">
                                <el-select v-if="scope.row.key === 'ENGINE'" v-model="scope.row._edit" size="small" style="width:160px;">
                                    <el-option v-for="e in ENGINE_OPTIONS" :key="e" :label="e" :value="e" />
                                </el-select>
                                <el-select v-else-if="scope.row.key === 'TABLE_COLLATION'" v-model="scope.row._edit" size="small" filterable style="width:220px;">
                                    <el-option v-for="c in COLLATION_OPTIONS" :key="c" :label="c" :value="c" />
                                </el-select>
                                <el-select v-else-if="scope.row.key === 'CHARACTER_SET_NAME'" v-model="scope.row._edit" size="small" filterable style="width:180px;">
                                    <el-option v-for="c in CHARSET_OPTIONS" :key="c" :label="c" :value="c" />
                                </el-select>
                                <el-input v-else v-model="scope.row._edit" size="small" style="width:200px;" />
                                <el-icon title="保存" style="cursor:pointer;" :size="12" @click="saveOption(scope.row)"><Check /></el-icon>
                                <el-icon title="取消" style="cursor:pointer;" :size="12" @click="scope.row.editing = false"><Refresh /></el-icon>
                            </div>
                        </template>
                    </el-table-column>
                </el-table>
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
            <el-icon style="margin-top: 0px;position: absolute;right: 18px;cursor: pointer;z-index: 9999999;" size="16" @click="copyCreateScript">
                <CopyDocument />
            </el-icon>
            <div style="font-size: 18px;width: 100%; height: 100%;overflow-y: auto;overflow-x: hidden;">
                <pre><code class="language-sql" v-bind:innerHTML="tableCreateDdl" ref="tableCreateDdlRef"></code></pre>
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

    <!-- 新建外键对话框 -->
    <el-dialog v-model="addFkDialogVisible" title="新建外键" width="550px" :draggable="true" destroy-on-close>
        <el-form label-width="100px">
            <el-form-item label="约束名">
                <el-input v-model="newFk.constraintName" placeholder="fk_xxx" />
            </el-form-item>
            <el-form-item label="本表字段">
                <el-select v-model="newFk.columnName" style="width: 100%;" placeholder="选择字段">
                    <el-option v-for="col in columnListOrigin" :key="col.columnName" :label="col.columnName" :value="col.columnName" />
                </el-select>
            </el-form-item>
            <el-form-item label="引用表">
                <el-input v-model="newFk.referencedTable" placeholder="引用的表名" />
            </el-form-item>
            <el-form-item label="引用字段">
                <el-input v-model="newFk.referencedColumn" placeholder="引用的字段名" />
            </el-form-item>
            <el-form-item label="ON UPDATE">
                <el-select v-model="newFk.updateRule" style="width: 100%;">
                    <el-option label="RESTRICT" value="RESTRICT" />
                    <el-option label="CASCADE" value="CASCADE" />
                    <el-option label="SET NULL" value="SET NULL" />
                    <el-option label="NO ACTION" value="NO ACTION" />
                </el-select>
            </el-form-item>
            <el-form-item label="ON DELETE">
                <el-select v-model="newFk.deleteRule" style="width: 100%;">
                    <el-option label="RESTRICT" value="RESTRICT" />
                    <el-option label="CASCADE" value="CASCADE" />
                    <el-option label="SET NULL" value="SET NULL" />
                    <el-option label="NO ACTION" value="NO ACTION" />
                </el-select>
            </el-form-item>
        </el-form>
        <template #footer>
            <el-button @click="addFkDialogVisible = false">取消</el-button>
            <el-button type="primary" @click="createForeignKey">创建</el-button>
        </template>
    </el-dialog>
</template>
<script setup>
import { ref, useTemplateRef, watch } from 'vue'
import { Check, CopyDocument, Delete, Edit, Refresh } from '@element-plus/icons-vue'
import http from '@/api/index'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()
import { format } from 'sql-formatter'
import { highlightSql } from '@/utils/lazyDeps'
import copyToClipboard from '@/utils/copy-to-clipboard'
import { getSqlDialect } from '@/utils/sqlHelper'

const activeName = ref("colums")

let columnListOrigin = []
const columnList = ref([])
const tableCreateDdl = ref("")
const tableCreateDdlRef = useTemplateRef('tableCreateDdlRef')

const indexList = ref([])
const tableOptionsData = ref({})
const tableStatsData = ref({})

const foreignKeyList = ref([])
const fkLoading = ref(false)

const selectedColumns = ref([])

const addIndexDialogVisible = ref(false)
const newIndex = ref({ name: "", type: "INDEX", columns: [] })

const addFkDialogVisible = ref(false)
const newFk = ref({ constraintName: "", columnName: "", referencedTable: "", referencedColumn: "", updateRule: "RESTRICT", deleteRule: "RESTRICT" })

// computed list for editable options table
const editableOptions = ref([])

// keys that can be modified via ALTER TABLE
const EDITABLE_OPTIONS = new Set(['TABLE_COMMENT', 'ENGINE', 'TABLE_COLLATION', 'CHARACTER_SET_NAME', 'AUTO_INCREMENT'])

const ENGINE_OPTIONS = ['InnoDB', 'MyISAM', 'MEMORY', 'CSV', 'ARCHIVE', 'BLACKHOLE', 'NDB']

const CHARSET_OPTIONS = [
    'utf8mb4', 'utf8', 'latin1', 'gbk', 'gb2312', 'ascii',
    'binary', 'utf16', 'utf32', 'ucs2', 'cp1251', 'cp1256',
]

const COLLATION_OPTIONS = [
    'utf8mb4_general_ci', 'utf8mb4_unicode_ci', 'utf8mb4_bin',
    'utf8mb4_0900_ai_ci', 'utf8mb4_0900_as_cs',
    'utf8_general_ci', 'utf8_unicode_ci', 'utf8_bin',
    'latin1_swedish_ci', 'latin1_general_ci', 'latin1_bin',
    'gbk_chinese_ci', 'gbk_bin',
    'gb2312_chinese_ci', 'ascii_general_ci', 'ascii_bin',
]

const COL_TYPE_OPTIONS = [
    'VARCHAR', 'CHAR', 'INT', 'BIGINT', 'TINYINT', 'SMALLINT',
    'FLOAT', 'DOUBLE', 'DECIMAL', 'TEXT', 'LONGTEXT', 'MEDIUMTEXT',
    'DATETIME', 'DATE', 'TIME', 'TIMESTAMP',
    'BOOLEAN', 'BLOB', 'LONGBLOB', 'MEDIUMBLOB',
    'ENUM', 'SET', 'JSON', 'YEAR',
    'VARCHAR2', 'NUMBER', 'CLOB', 'NCLOB', 'RAW',
    'NVARCHAR2', 'LONG', 'BFILE', 'ROWID', 'UROWID',
    'INTERVAL YEAR TO MONTH', 'INTERVAL DAY TO SECOND',
    'TIMESTAMP WITH TIME ZONE', 'TIMESTAMP WITH LOCAL TIME ZONE',
]

function getDbType() {
    return (tableMeta.dbType || dbSchemaProxy.getDbType(tableMeta.schema) || '').toLowerCase()
}

function isOracle() { return getDbType() === 'oracle' }
function isMysql() { return getDbType() === 'mysql' }

function quoteIdent(name) {
    if (isOracle()) return '"' + (name || '').toUpperCase() + '"'
    return '`' + name + '`'
}

function escapeStr(str) {
    return (str || '').replace(/'/g, "''")
}

function queryTypeOptions(queryString, cb) {
    const results = COL_TYPE_OPTIONS
        .filter(t => t.toLowerCase().includes((queryString || '').toLowerCase()))
        .map(t => ({ value: t }))
    cb(results)
}

const { tableMeta } = defineProps({
    tableMeta: Object,
})

const emit = defineEmits(['tableDrop'])

watch(() => tableMeta, (newVal, oldVal) => {
    if (newVal && newVal !== oldVal) {
        tableCreateDdl.value = ""
        activeName.value = 'colums'
        loadData('colums')
    }
}, { deep: true, immediate: true });

function getPostBody() {
    return { connId: tableMeta.connId, schema: tableMeta.schema, tableName: tableMeta.tableName }
}

function loadData(pane) {
    if (!tableMeta || !tableMeta.connId) return
    // el-tabs @tab-click passes a TabsPaneContext; paneName is the tab's name value
    const name = pane?.paneName ?? pane?.props?.name ?? pane
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
    } else if (name === "foreignKeys") {
        loadForeignKeys()
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

function loadForeignKeys() {
    const dbType = getDbType()
    let sql = ''
    if (dbType === 'mysql') {
        sql = `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME, r.UPDATE_RULE, r.DELETE_RULE FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON k.CONSTRAINT_NAME = r.CONSTRAINT_NAME AND k.CONSTRAINT_SCHEMA = r.CONSTRAINT_SCHEMA WHERE k.TABLE_SCHEMA = '${tableMeta.schema}' AND k.TABLE_NAME = '${tableMeta.tableName}' AND k.REFERENCED_TABLE_NAME IS NOT NULL`
    } else if (dbType === 'sqlite') {
        sql = `PRAGMA foreign_key_list('${tableMeta.tableName}')`
    } else if (dbType === 'oracle') {
        sql = `SELECT a.constraint_name, a.column_name, c_pk.table_name AS referenced_table_name, c_pk.column_name AS referenced_column_name, c.update_rule, c.delete_rule FROM all_cons_columns a JOIN all_constraints c ON a.constraint_name = c.constraint_name AND a.owner = c.owner JOIN all_cons_columns c_pk ON c.r_constraint_name = c_pk.constraint_name AND c.r_owner = c_pk.owner WHERE c.constraint_type = 'R' AND c.table_name = '${(tableMeta.tableName || '').toUpperCase()}' AND c.owner = '${(tableMeta.schema || '').toUpperCase()}'`
    }
    if (!sql) {
        foreignKeyList.value = []
        return
    }
    fkLoading.value = true
    const params = new URLSearchParams()
    params.append('connId', tableMeta.connId)
    params.append('schema', tableMeta.schema)
    params.append('sql', sql)
    params.append('maxLine', '100')
    http.post('/execSQL', params)
        .then((resp) => {
            const data = resp.data.data?.data || []
            if (dbType === 'sqlite') {
                foreignKeyList.value = data.map(r => ({
                    constraintName: 'FK_' + r.id,
                    columnName: r.from,
                    referencedTable: r.table,
                    referencedColumn: r.to,
                    updateRule: r.on_update || 'NO ACTION',
                    deleteRule: r.on_delete || 'NO ACTION',
                }))
            } else if (dbType === 'oracle') {
                foreignKeyList.value = data.map(r => ({
                    constraintName: r.CONSTRAINT_NAME || r.constraint_name,
                    columnName: r.COLUMN_NAME || r.column_name,
                    referencedTable: r.REFERENCED_TABLE_NAME || r.referenced_table_name,
                    referencedColumn: r.REFERENCED_COLUMN_NAME || r.referenced_column_name,
                    updateRule: r.UPDATE_RULE || r.update_rule || 'NO ACTION',
                    deleteRule: r.DELETE_RULE || r.delete_rule || 'NO ACTION',
                }))
            } else {
                foreignKeyList.value = data.map(r => ({
                    constraintName: r.CONSTRAINT_NAME || r.constraint_name || r.CONSTRAINT_NAME,
                    columnName: r.COLUMN_NAME || r.column_name || r.COLUMN_NAME,
                    referencedTable: r.REFERENCED_TABLE_NAME || r.referenced_table_name || r.REFERENCED_TABLE_NAME,
                    referencedColumn: r.REFERENCED_COLUMN_NAME || r.referenced_column_name || r.REFERENCED_COLUMN_NAME,
                    updateRule: r.UPDATE_RULE || r.update_rule || r.UPDATE_RULE || '-',
                    deleteRule: r.DELETE_RULE || r.delete_rule || r.DELETE_RULE || '-',
                }))
            }
        })
        .catch(() => { foreignKeyList.value = [] })
        .finally(() => { fkLoading.value = false })
}

function ruleTagType(rule) {
    if (!rule) return 'info'
    const r = rule.toUpperCase()
    if (r === 'CASCADE') return 'danger'
    if (r === 'SET NULL') return 'warning'
    if (r === 'RESTRICT' || r === 'NO ACTION') return ''
    return 'info'
}

function loadOptions() {
    http.post("/tableOptions", getPostBody())
        .then((resp) => {
            tableOptionsData.value = resp.data.data || {}
            const options = Object.entries(tableOptionsData.value).map(([key, value]) => ({
                key, value, editing: false, _edit: value
            }))
            // Extract CHARACTER_SET_NAME from TABLE_COLLATION
            const collation = tableOptionsData.value['TABLE_COLLATION']
            if (collation) {
                const charset = collation.split('_')[0]
                options.push({
                    key: 'CHARACTER_SET_NAME',
                    value: charset,
                    editing: false,
                    _edit: charset
                })
            }
            editableOptions.value = options
        })
}

function loadStatistics() {
    http.post("/tableStatistics", getPostBody())
        .then((resp) => { tableStatsData.value = resp.data.data || {} })
}

function loadCreateDdl() {
    let sqlStr = ""
    if (isOracle()) {
        sqlStr = "select dbms_metadata.get_ddl('TABLE','" + tableMeta.tableName.toUpperCase() + "') from dual"
    } else if (isMysql()) {
        sqlStr = "show create table `" + tableMeta.tableName + "`"
    } else {
        tableCreateDdl.value = "暂不支持"
        return
    }
    const params = new URLSearchParams()
    params.append("connId", tableMeta.connId)
    params.append("schema", tableMeta.schema)
    params.append("sql", sqlStr)
    params.append("maxLine", 1)
    http.post("/execSQL", params)
        .then((resp) => {
            const data = resp.data.data.data[0]
            const sql = format(data[Object.keys(data)[0].trim()] || "", { language: getSqlLang() })
            highlightSql(sql).then(html => { tableCreateDdl.value = html })
        }).catch((error) => { console.log(error) });
}

function saveOption(row) {
    const val = row._edit
    let sql = ""
    if (row.key === 'TABLE_COMMENT') {
        if (isOracle()) {
            sql = "COMMENT ON TABLE " + quoteIdent(tableMeta.tableName) + " IS '" + escapeStr(val) + "'"
        } else {
            sql = "ALTER TABLE `" + tableMeta.tableName + "` COMMENT = '" + val + "'"
        }
    } else if (row.key === 'ENGINE') {
        if (isOracle()) {
            ElMessage({ message: 'Oracle 不支持修改存储引擎', type: 'warning' })
            row.editing = false
            return
        }
        sql = "ALTER TABLE `" + tableMeta.tableName + "` ENGINE = " + val
    } else if (row.key === 'TABLE_COLLATION') {
        if (isOracle()) {
            ElMessage({ message: 'Oracle 不支持修改排序规则', type: 'warning' })
            row.editing = false
            return
        }
        sql = "ALTER TABLE `" + tableMeta.tableName + "` COLLATE = " + val
    } else if (row.key === 'CHARACTER_SET_NAME') {
        if (isOracle()) {
            ElMessage({ message: 'Oracle 不支持修改字符集', type: 'warning' })
            row.editing = false
            return
        }
        const collation = val + '_general_ci'
        sql = "ALTER TABLE `" + tableMeta.tableName + "` COLLATE = " + collation
    } else if (row.key === 'AUTO_INCREMENT') {
        if (isOracle()) {
            ElMessage({ message: 'Oracle 不支持 AUTO_INCREMENT', type: 'warning' })
            row.editing = false
            return
        }
        sql = "ALTER TABLE `" + tableMeta.tableName + "` AUTO_INCREMENT = " + val
    }
    if (!sql) return
    execSql(sql, () => {
        row.value = val
        row.editing = false
        ElMessage({ message: '修改成功', type: 'success' })
    })
}

function saveIndexComment(row) {
    if (isOracle()) {
        ElMessage({ message: 'Oracle 不支持修改索引注释（需 19c+）', type: 'warning' })
        row.onCommentEdit = false
        return
    }
    if (isMysql()) {
        execSql("DROP INDEX `" + row.indexName + "` ON `" + tableMeta.tableName + "`", () => {
            const unique = row.nonUnique == 0 ? 'UNIQUE ' : ''
            execSql("CREATE " + unique + "INDEX `" + row.indexName + "` ON `" + tableMeta.tableName + "` (`" + row.columnName + "`) COMMENT '" + escapeStr(row._editComment) + "'", () => {
                row.indexComment = row._editComment
                row.onCommentEdit = false
                ElMessage({ message: '索引注释已更新', type: 'success' })
            })
        })
        return
    }
    ElMessage({ message: '当前数据库不支持修改索引注释', type: 'warning' })
    row.onCommentEdit = false
}

function renameIndex(row) {
    if (isOracle()) {
        execSql("ALTER INDEX " + quoteIdent(row.indexName) + " RENAME TO " + quoteIdent(row._editName), () => {
            row.indexName = row._editName
            row.onNameEdit = false
            ElMessage({ message: '索引已重命名', type: 'success' })
        })
        return
    }
    if (isMysql()) {
        execSql("DROP INDEX `" + row.indexName + "` ON `" + tableMeta.tableName + "`", () => {
            const unique = row.nonUnique == 0 ? 'UNIQUE ' : ''
            execSql("CREATE " + unique + "INDEX `" + row._editName + "` ON `" + tableMeta.tableName + "` (`" + row.columnName + "`) COMMENT '" + (row.indexComment || '') + "'", () => {
                row.indexName = row._editName
                row.onNameEdit = false
                ElMessage({ message: '索引已重命名', type: 'success' })
            })
        })
        return
    }
    ElMessage({ message: '当前数据库不支持重命名索引', type: 'warning' })
    row.onNameEdit = false
}

function getSqlLang() {
    return getSqlDialect(getDbType())
}

const OPTION_LABELS = {
    ENGINE: '存储引擎', TABLE_COLLATION: '排序规则', CHARACTER_SET_NAME: '字符集', TABLE_COMMENT: '表注释',
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
function formatDefaultValue(defaultVal) {
    if (defaultVal == null || defaultVal === '') return 'NULL'
    return "'" + defaultVal.replace(/'/g, "''") + "'"
}

function modifyColumnName(seq, newName) {
    const col = columnListOrigin[seq]
    if (isOracle()) {
        const sql = 'ALTER TABLE ' + quoteIdent(tableMeta.tableName) + ' RENAME COLUMN ' + quoteIdent(col.columnName) + ' TO ' + quoteIdent(newName)
        execSql(sql, () => {
            columnListOrigin[seq].columnName = newName
            columnList.value[seq].onColumnNameEdit = false
        })
        return
    }
    const sql = "alter table `" + tableMeta.tableName + "` change `" + col.columnName + "` `" + newName + "` " + col.columnType + " DEFAULT " + formatDefaultValue(col.columnDefault) + " comment '" + escapeStr(col.columnComment || '') + "'"
    execSql(sql, () => {
        columnListOrigin[seq].columnName = newName
        columnList.value[seq].onColumnNameEdit = false
    })
}

function modifyColumnType(seq, newType) {
    const col = columnListOrigin[seq]
    if (isOracle()) {
        const sql = 'ALTER TABLE ' + quoteIdent(tableMeta.tableName) + ' MODIFY (' + quoteIdent(col.columnName) + ' ' + newType + ')'
        execSql(sql, () => {
            columnListOrigin[seq].columnType = newType
            columnList.value[seq].onColumnTypeEdit = false
        })
        return
    }
    const sql = "alter table `" + tableMeta.tableName + "` modify `" + col.columnName + "` " + newType + " DEFAULT " + formatDefaultValue(col.columnDefault) + " comment '" + escapeStr(col.columnComment || '') + "'"
    execSql(sql, () => {
        columnListOrigin[seq].columnType = newType
        columnList.value[seq].onColumnTypeEdit = false
    })
}

function modifyColumnDefault(seq, newDefault) {
    const col = columnListOrigin[seq]
    const defaultVal = (newDefault === null || newDefault === undefined || newDefault === '') ? 'NULL' : "'" + newDefault.replace(/'/g, "''") + "'"
    if (isOracle()) {
        const sql = 'ALTER TABLE ' + quoteIdent(tableMeta.tableName) + ' MODIFY (' + quoteIdent(col.columnName) + ' DEFAULT ' + defaultVal + ')'
        execSql(sql, () => {
            columnListOrigin[seq].columnDefault = newDefault
            columnList.value[seq].onColumnDefaultEdit = false
        })
        return
    }
    const sql = "alter table `" + tableMeta.tableName + "` modify `" + col.columnName + "` " + col.columnType + " DEFAULT " + defaultVal + " comment '" + escapeStr(col.columnComment || '') + "'"
    execSql(sql, () => {
        columnListOrigin[seq].columnDefault = newDefault
        columnList.value[seq].onColumnDefaultEdit = false
    })
}

function modifyColumnComment(seq, newComment) {
    const col = columnListOrigin[seq]
    if (isOracle()) {
        const sql = "COMMENT ON COLUMN " + quoteIdent(tableMeta.tableName) + "." + quoteIdent(col.columnName) + " IS '" + escapeStr(newComment || '') + "'"
        execSql(sql, () => {
            columnListOrigin[seq].columnComment = newComment
            columnList.value[seq].onColumnCommentEdit = false
        })
        return
    }
    const sql = "alter table `" + tableMeta.tableName + "` modify `" + col.columnName + "` " + col.columnType + " DEFAULT " + formatDefaultValue(col.columnDefault) + " comment '" + escapeStr(newComment || '') + "'"
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
    let sql = ""
    const tbl = quoteIdent(tableMeta.tableName)
    const col = quoteIdent(column.columnName)
    if (isOracle()) {
        sql = "ALTER TABLE " + tbl + " ADD (" + col + " " + column.columnType
        if ("YES" !== column.isNullable) sql += " NOT NULL"
        if (column.columnDefault) sql += " DEFAULT '" + column.columnDefault.replace(/'/g, "''") + "'"
        sql += ")"
        if (column.columnComment) {
            sql += ";\nCOMMENT ON COLUMN " + tbl + "." + col + " IS '" + escapeStr(column.columnComment) + "'"
        }
    } else {
        sql = "alter table `" + tableMeta.tableName + "` add `" + column.columnName + "` " + column.columnType
        if ("YES" === column.isNullable) {
            sql += " default null "
        } else {
            sql += " not null "
        }
        if (column.columnDefault) {
            sql += " default '" + column.columnDefault.replace(/'/g, "''") + "' "
        }
        sql += " comment '" + escapeStr(column.columnComment || '') + "' after `" + column.after + "`"
    }
    execSql(sql, () => loadData('colums'))
}

function delCol(seq) {
    const tbl = quoteIdent(tableMeta.tableName)
    const col = quoteIdent(columnListOrigin[seq].columnName)
    if (isOracle()) {
        execSql("ALTER TABLE " + tbl + " DROP COLUMN " + col, () => loadData('colums'))
        return
    }
    execSql("alter table " + tbl + " drop column " + col, () => loadData('colums'))
}

function onColSelectionChange(selection) {
    selectedColumns.value = selection
}

function addColAtEnd() {
    const newIdx = columnList.value.length
    for (let i = 0; i < columnList.value.length; i++) {
        if (columnList.value[i].isNew) {
            ElMessage({ message: '请先完成当前新增', type: 'warning' })
            return
        }
    }
    const lastCol = columnList.value.length > 0 ? columnList.value[columnList.value.length - 1] : null
    columnList.value.push({ isNew: true, idx: newIdx, columnName: "", columnType: "", isNullable: "YES", columnComment: "", columnDefault: "", after: lastCol ? lastCol.columnName : "" })
}

function batchDelCols() {
    const names = selectedColumns.value.map(c => c.columnName)
    if (names.length === 0) return
    ElMessageBox.confirm(
        `确定要删除选中的 ${names.length} 个字段吗？`,
        '批量删除字段',
        { confirmButtonText: '确定删除', cancelButtonText: '取消', type: 'warning' }
    ).then(() => {
        const seqs = selectedColumns.value.map(c => c.idx).sort((a, b) => b - a)
        const tbl = quoteIdent(tableMeta.tableName)
        const sqls = seqs.map(s => {
            const col = quoteIdent(columnListOrigin[s].columnName)
            if (isOracle()) return "ALTER TABLE " + tbl + " DROP COLUMN " + col
            return "alter table " + tbl + " drop column " + col
        })
        const sep = isOracle() ? '\n/\n' : ';'
        execSql(sqls.join(sep), () => {
            selectedColumns.value = []
            loadData('colums')
            ElMessage({ message: `已删除 ${names.length} 个字段`, type: 'success' })
        })
    }).catch(() => {})
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
    const tbl = quoteIdent(tableMeta.tableName)
    const idx = quoteIdent(newIndex.value.name)
    const cols = newIndex.value.columns.map(c => quoteIdent(c)).join(", ")
    const sql = prefix + " " + idx + " ON " + tbl + " (" + cols + ")"
    execSql(sql, () => {
        addIndexDialogVisible.value = false
        loadIndexes()
        ElMessage({ message: "索引创建成功", type: "success" })
    })
}

function dropIndex(indexName) {
    const dbType = getDbType()
    let sql = ""
    if (dbType === "mysql") {
        sql = "DROP INDEX `" + indexName + "` ON `" + tableMeta.tableName + "`"
    } else if (dbType === "oracle") {
        sql = "DROP INDEX \"" + indexName.toUpperCase() + "\""
    } else {
        sql = "DROP INDEX `" + indexName + "`"
    }
    execSql(sql, () => {
        loadIndexes()
        ElMessage({ message: "索引已删除", type: "success" })
    })
}

// ========== 外键操作 ==========
function showAddFkDialog() {
    newFk.value = { constraintName: "", columnName: "", referencedTable: "", referencedColumn: "", updateRule: "RESTRICT", deleteRule: "RESTRICT" }
    addFkDialogVisible.value = true
}

function createForeignKey() {
    const fk = newFk.value
    if (!fk.constraintName || !fk.columnName || !fk.referencedTable || !fk.referencedColumn) {
        ElMessage({ message: "请填写完整的外键信息", type: "warning" })
        return
    }
    let sql = ""
    const tbl = quoteIdent(tableMeta.tableName)
    const con = quoteIdent(fk.constraintName)
    const col = quoteIdent(fk.columnName)
    const refTbl = quoteIdent(fk.referencedTable)
    const refCol = quoteIdent(fk.referencedColumn)
    if (isOracle()) {
        sql = "ALTER TABLE " + tbl + " ADD CONSTRAINT " + con + " FOREIGN KEY (" + col + ") REFERENCES " + refTbl + " (" + refCol + ")"
        if (fk.deleteRule === 'CASCADE') sql += " ON DELETE CASCADE"
        else if (fk.deleteRule === 'SET NULL') sql += " ON DELETE SET NULL"
        if (fk.updateRule && fk.updateRule !== 'RESTRICT' && fk.updateRule !== 'NO ACTION') {
            ElMessage({ message: 'Oracle 不支持 ON UPDATE，该规则将被忽略', type: 'warning' })
        }
    } else {
        sql = "ALTER TABLE `" + tableMeta.tableName + "` ADD CONSTRAINT `" + fk.constraintName + "` FOREIGN KEY (`" + fk.columnName + "`) REFERENCES `" + fk.referencedTable + "` (`" + fk.referencedColumn + "`) ON UPDATE " + fk.updateRule + " ON DELETE " + fk.deleteRule
    }
    execSql(sql, () => {
        addFkDialogVisible.value = false
        loadForeignKeys()
        ElMessage({ message: "外键创建成功", type: "success" })
    })
}

function dropForeignKey(constraintName) {
    const tbl = quoteIdent(tableMeta.tableName)
    const con = quoteIdent(constraintName)
    if (isOracle()) {
        execSql("ALTER TABLE " + tbl + " DROP CONSTRAINT " + con, () => {
            loadForeignKeys()
            ElMessage({ message: "外键已删除", type: "success" })
        })
        return
    }
    execSql("ALTER TABLE `" + tableMeta.tableName + "` DROP FOREIGN KEY `" + constraintName + "`", () => {
        loadForeignKeys()
        ElMessage({ message: "外键已删除", type: "success" })
    })
}

// ========== 通用 ==========
function execSql(sql, succ) {
    const params = new URLSearchParams()
    params.append("connId", tableMeta.connId)
    params.append("schema", tableMeta.schema)
    params.append("tableName", tableMeta.tableName)
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
.table-editor-tabs {
    display: flex;
    flex-direction: column;
    max-height: calc(65vh - 54px);

    :deep(.el-tabs__header) {
        margin-bottom: 0;
        background: var(--bg-toolbar);
        border-bottom: 1px solid var(--border-primary);
        padding: 0 8px;
    }

    :deep(.el-tabs__item) {
        font-size: 13px;
        height: 34px;
        line-height: 34px;
        border-radius: 6px 6px 0 0;
    }

    :deep(.el-tabs__item.is-active) {
        background: var(--bg-primary);
        font-weight: 500;
    }

    :deep(.el-tabs__content) {
        flex: 1;
        overflow: auto;
        padding: 8px;
    }

    :deep(.el-tab-pane) {
        height: 100%;
    }
}

/* 在 TableManager 页面内使用时，撑满剩余高度 */

.col-table {
    width: 100%;
    height: 100%;
    overflow-y: auto;
    overflow-x: hidden;
}

.modify_icon {
    width: 16px;
    height: 16px;
    position: relative;
    right: -5px;
    top: 50%;
    transform: translateY(-50%);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s ease;
}

.column_name:hover .modify_icon,
.column_type:hover .modify_icon,
.column_default:hover .modify_icon,
.column_comment:hover .modify_icon {
    opacity: 1;
}

.column_name,
.column_type,
.column_default,
.column_comment {
    cursor: pointer;
}

.modify_icon:hover {
    opacity: 0.8 !important;
}

.edit-action {
    cursor: pointer;
    padding: 2px;
    border-radius: 3px;
    transition: background .15s;
}
.edit-save:hover {
    background: #e1f3d8;
    color: #67c23a;
}
.edit-cancel:hover {
    background: #fef0f0;
    color: #f56c6c;
}
</style>

<style>
.el-table .cell {
    padding: 0 5px !important;
}
</style>
