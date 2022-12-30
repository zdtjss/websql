<template>
    <el-container>
        <el-header height="30px" class="toolbar">
            <el-button @click="toSql">返回</el-button>
        </el-header>
        <el-main class="sql_area">
            <el-table :data="tableData" stripe="true" highlight-current-row="true" width="100%">
                <el-table-column prop="name" label="表名" width="180" />
                <el-table-column prop="comment" label="注释" width="180" />
                <el-table-column label="操作" style="text-align: center; " width="250">
                    <template #default="scope">
                        <el-row :gutter="10">
                            <el-col :span="6">
                                <el-button size="small" @click="exportCsv(scope.row.name)">导出</el-button>
                            </el-col>
                            <el-col :span="9">
                                <el-upload v-model="fileList" action="/importCsv" :limit="1" :data="dataInsert">
                                    <el-button size="small" @click="dataInsert.table = scope.row.name">导入/新增</el-button>
                                </el-upload>
                            </el-col>
                            <el-col :span="9">
                                <el-upload v-model="fileList" action="/importCsv" :limit="1" :data="dataUpdate">
                                    <el-button size="small" @click="dataUpdate.table = scope.row.name">导入/修改</el-button>
                                </el-upload>
                            </el-col>
                        </el-row>
                    </template>
                </el-table-column>
            </el-table>
        </el-main>
    </el-container>
</template>
  
<script setup>

import { onMounted, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'

import axios from 'axios'

const router = useRouter()

const fileList = ref([])
const tableData = ref([])
const upload = ref()
const dataInsert = ref({
    start: 2,
    connId: "test",
    schema: "mat",
    opt: "insert",
    table: "undo_log"
})
const dataUpdate = ref({
    start: 2,
    connId: "test",
    schema: "mat",
    opt: "update",
    table: "undo_log"
})

const route = useRoute()

onMounted(() => {

    queryData()

    dataInsert.value.start = route.query.start
    dataInsert.value.connId = route.query.connId
    dataInsert.value.schema = route.query.schema

    dataUpdate.value.start = route.query.start
    dataUpdate.value.connId = route.query.connId
    dataUpdate.value.schema = route.query.schema
})

function queryData() {
    debugger
    axios.get("/listTable?connId=" + route.query.connId + "&schema=" + route.query.schema)
        .then((resp) => {
            tableData.value = resp.data
        })
        .catch(function (error) {
            console.log(error);
        });
}

function exportCsv(table) {
    location.href = "/exportCsv?env=" + route.query.env + "&schema=" + route.query.schema + "&table=" + table
}

function toSql() {
    router.push("/")
}
</script>
  