<template>
    <el-tabs v-model="activeName" type="card" class="view-dialog-tabs" @tab-click="loadData">
        <el-tab-pane label="字段" name="colums">
            <el-table :data="columnList" style="width: 100%" height="470" size="small" stripe>
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
            <el-scrollbar style="font-size: 15px; width: 100%; height: 470px;">
                <pre style="margin: 0; padding: 12px;"><code class="language-sql" v-bind:innerHTML="tableCreateDdl" ref="tableCreateDdlRef"></code></pre>
            </el-scrollbar>
        </el-tab-pane>
    </el-tabs>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import http from '@/js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'
import { getSqlDialect } from '@/js/utils/sqlHelper.ts'
import hljs from 'highlight.js/lib/core'
import { format } from 'sql-formatter'
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
            sqlStr = "select dbms_metadata.get_ddl('VIEW','" + props.tableMeta.tableName.toUpperCase() + "') from dual"
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

function getSqlLang(schema) {
    return getSqlDialect(dbSchemaProxy.getDbType(schema))
}

</script>

<style lang="less" scoped>
.view-dialog-tabs {
    height: 500px;

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
}
</style>