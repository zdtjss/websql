<template>
    <el-tabs v-model="activeName" type="card" style="height:500px;" @tab-click="loadData">
        <el-tab-pane label="字段" name="colums">
            <el-table :data="columnList" style="width: 100%" height="470">
                <el-table-column label="名称" width="250">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnName" style="margin-bottom: 10px;" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnNameEdit" class="column_name">
                            <span>{{ scope.row.columnName }}</span>
                            <span class="modify_column_name">
                                <el-icon :size="12" style="cursor: pointer;" title="修改名称"
                                    @click="scope.row.onColumnNameEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnNameEdit">
                            <el-input v-model="scope.row.columnName" style="margin-bottom: 10px;width: 85%;" />
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
                <el-table-column label="类型" width="160">
                    <template #default="scope">
                        <el-input v-if="scope.row.isNew" v-model="scope.row.columnType" style="margin-bottom: 10px;" />
                        <div v-if="!scope.row.isNew && !scope.row.onColumnTypeEdit" class="column_type">
                            <span>{{ scope.row.columnType }}</span>
                            <span class="modify_column_type">
                                <el-icon :size="12" style="cursor: pointer;" title="修改类型"
                                    @click="scope.row.onColumnTypeEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnTypeEdit">
                            <el-input v-model="scope.row.columnType" style="margin-bottom: 10px;width: 76%;" />
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
                <el-table-column label="可空" width="100">
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
                            <span class="modify_column_comment">
                                <el-icon :size="12" style="cursor: pointer;" title="修改注释"
                                    @click="scope.row.onColumnCommentEdit = true">
                                    <Edit />
                                </el-icon>
                            </span>
                        </div>
                        <div v-if="scope.row.onColumnCommentEdit">
                            <el-input v-model="scope.row.columnComment" style="margin-bottom: 10px;width: 76%;"
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
        <el-tab-pane label="选项" name="option">

        </el-tab-pane>
        <el-tab-pane label="统计" name="statistics">

        </el-tab-pane>
        <el-tab-pane label="建表语句" name="showCreate">
            <el-scrollbar style="font-size: 18px;width: 100%;height: 470px;">
                <pre><code class="language-sql" v-bind:innerHTML="tableCreateDdl" ref="tableCreateDdlRef"></code></pre>
            </el-scrollbar>
        </el-tab-pane>
    </el-tabs>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import http from '@/js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'
import { format } from 'sql-formatter'
import hljs from 'highlight.js/lib/core'
import * as highlightSql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/stackoverflow-light.css'

hljs.registerLanguage('sql', highlightSql.default);

const activeName = ref("colums")

let columnListOrigin = []
const columnList = ref([])
const tableCreateDdl = ref("")

const props = defineProps({
    tableMeta: Object,
})

onMounted(() => {
    loadData({ props: { name: 'columns' } })
})


function loadData(pane) {
    if (pane.props.name === "columns") {
        http.post("/listTableColumns", props.tableMeta)
            .then((resp) => {
                columnList.value = resp.data.data
                for (let i = 0; i < columnList.value.length; i++) {
                    columnList.value[i]['idx'] = i
                }
                columnListOrigin = JSON.parse(JSON.stringify(columnList.value))
            })
    } else if (pane.props.name === "showCreate") {
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
                tableCreateDdl.value =  hljs.highlight(sql, { language: 'sql' }).value
            }).catch((error) => {
                console.log(error);
            });
    }
}

function getSqlLang(schma) {
    let sqlLang = "sql"
    const dbType = dbSchemaProxy.getDbType(schema).toLowerCase()
    if (dbType === "oracle") {
        sqlLang = "plsql"
    } else if (dbType === "mysql") {
        sqlLang = "mysql"
    }
    return sqlLang
}

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
        columnList.value.splice(index + 1, 0, { isNew: true, columnName: "", columnType: "", isNullable: "YES", columnComment: "", after: columnList.value[index].columnName });
    }
}

function cancelAdd(targetIdx) {
    // 1. 找到要删除元素的索引
    const index = columnList.value.findIndex(item => item.idx === targetIdx);
    if (index === -1) {
        console.warn(`未找到 idx 为 ${targetIdx} 的元素`);
        return;
    }

    // 2. 先将其后所有元素的 idx 减 1
    for (let i = index + 1; i < columnList.value.length; i++) {
        columnList.value[i].idx -= 1;
    }

    // 3. 删除该元素
    columnList.value.splice(index, 1);
}

function doColAdd(column) {
    let sql = "alter table " + props.tableMeta.tableName + " add " + column.columnName + " " + column.columnType;
    if ("YES" === column.isNullable) {
        sql += " default null "
    } else {
        sql += " not null "
    }
    sql += " comment '" + column.columnComment + "' after " + column.after;
    execSql(sql, () => loadData({ props: { name: 'columns' } }))
}

function delCol(seq) {
    const sql = "alter table " + props.tableMeta.tableName + " drop " + columnListOrigin[seq].columnName;
    execSql(sql, () => loadData({ props: { name: 'columns' } }))
}

function execSql(sql, succ) {
    const params = new URLSearchParams()
    params.append("connId", props.tableMeta.connId)
    params.append("schema", props.tableMeta.schema)
    params.append("tableName", props.tableMeta.tableName)
    params.append("sql", sql)
    params.append("maxLine", 1)
    http.post("/execSQL", params)
        .then((resp) => {
            if (succ) {
                succ(resp)
            }
        }).catch((error) => {
            console.log(error);
        });
}

</script>

<style lang="less" scoped>
.modify_column_name {
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

.column_name:hover .modify_column_name {
    opacity: 1;
}

.modify_column_name:hover {
    opacity: 0.8 !important;
}

.modify_column_name:hover {
    opacity: 0.8;
}



.modify_column_type {
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

.column_type:hover .modify_column_type {
    opacity: 1;
}

.modify_column_type:hover {
    opacity: 0.8 !important;
}

.modify_column_type:hover {
    opacity: 0.8;
}


.modify_column_comment {
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

.column_comment:hover .modify_column_comment {
    opacity: 1;
}

.modify_column_comment:hover {
    opacity: 0.8 !important;
}

.modify_column_comment:hover {
    opacity: 0.8;
}
</style>