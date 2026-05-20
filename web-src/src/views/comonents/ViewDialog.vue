<template>
    <el-tabs v-model="activeName" type="card" class="view-dialog-tabs" @tab-click="loadData">
        <el-tab-pane label="字段" name="colums">
            <el-table :data="columnList" style="width: 100%" height="100%" size="small" stripe>
                <el-table-column prop="columnName" label="名称" width="250" resizable />
                <el-table-column prop="columnType" label="类型" width="160" resizable />
                <el-table-column prop="isNullable" label="可空" width="100" resizable />
                <el-table-column prop="columnComment" label="注释" resizable />
            </el-table>
        </el-tab-pane>
        <el-tab-pane label="选项" name="option">
            <el-empty description="暂无选项信息" />
        </el-tab-pane>
        <el-tab-pane label="统计" name="statistics">
            <el-empty description="暂无统计信息" />
        </el-tab-pane>
        <el-tab-pane label="DDL" name="showCreate">
            <el-scrollbar style="font-size: 15px; width: 100%; height: 100%;">
                <pre style="margin: 0; padding: 12px;"><code class="language-sql" v-bind:innerHTML="tableCreateDdl" ref="tableCreateDdlRef"></code></pre>
            </el-scrollbar>
        </el-tab-pane>
    </el-tabs>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import http from '@/js/utils/httpProxy.js'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()
import { getSqlDialect } from '@/js/utils/sqlHelper.ts'
import { highlightSql } from '@/utils/lazyDeps'
import { format } from 'sql-formatter'

const activeName = ref("colums")

let columnListOrigin = []
const columnList = ref([])
const tableCreateDdl = ref("")

const { tableMeta } = defineProps({
    tableMeta: Object,
})

onMounted(() => {
    loadData({ props: { name: 'columns' } })
})


function loadData(pane) {
    if (pane.props.name === "columns") {
        http.post("/listTableColumns", tableMeta)
            .then((resp) => {
                columnList.value = resp.data.data
                for (let i = 0; i < columnList.value.length; i++) {
                    columnList.value[i]['idx'] = i
                }
                columnListOrigin = JSON.parse(JSON.stringify(columnList.value))
            })
    } else if (pane.props.name === "showCreate") {
        let sqlStr = ""
        const dbType = dbSchemaProxy.getDbType(tableMeta.schema)
        if (dbType === 'mysql') {
            sqlStr = "show create table " + tableMeta.tableName
        } else if (dbType === 'oracle') {
            sqlStr = "select dbms_metadata.get_ddl('VIEW','" + tableMeta.tableName.toUpperCase() + "') from dual"
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
                const sql = format(data[Object.keys(data)[0].trim()] || "", { language: getSqlLang(tableMeta.schema) })
                highlightSql(sql).then(html => { tableCreateDdl.value = html })
            }).catch((error) => {
                console.log(error);
            });
    }
}

function getSqlLang(schema) {
    return getSqlDialect(dbSchemaProxy.getDbType(schema))
}

</script>

<style lang="less" scoped>
.view-dialog-tabs {
    display: flex;
    flex-direction: column;
    max-height: calc(65vh - 54px);

    :deep(.el-tabs__header) {
        margin-bottom: 0;
        background: #fafbfc;
        border-bottom: 1px solid #ebeef5;
        padding: 0 8px;
    }

    :deep(.el-tabs__item) {
        font-size: 13px;
        height: 34px;
        line-height: 34px;
    }

    :deep(.el-tabs__item.is-active) {
        font-weight: 500;
    }

    :deep(.el-tabs__content) {
        flex: 1;
        overflow: auto;
    }

    :deep(.el-tab-pane) {
        height: 100%;
    }
}
</style>