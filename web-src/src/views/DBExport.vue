<template>
    <el-table :data="tableData" stripe="true" highlight-current-row="true" style="width: 100%">
        <el-table-column prop="name" label="表名" />
        <el-table-column prop="comment" label="注释" />
        <el-table-column label="操作" style="text-align: center; " width="130px">
            <template #default="scope">
                <el-row :gutter="10">
                    <el-col :span="12">
                        <el-button size="small" @click="exportCsv(scope.row.name)">导出</el-button>
                    </el-col>
                    <el-col :span="12">
                        <el-upload v-model="fileList" action="/importCsv" :limit="1" :data="data" :on-success="onSuccess()">
                            <el-button  size="small" @click="data.table = scope.row.name">导入</el-button>
                        </el-upload>
                    </el-col>
                </el-row>
            </template>
        </el-table-column>
    </el-table>
</template>
  
<script setup>

import { onMounted, ref } from 'vue'
import axios from 'axios'

defineProps(['env', 'db'])

const fileList = ref([])
const tableData = ref([])
const upload = ref()
const data = ref({
    start: 2,
    env: "test",
    db: "mat",
    table: "undo_log"
})

const params = parsUrlVar()

onMounted(() => {

    queryData()

    data.value.start = params.get("start")
    data.value.env = params.get("env")
    data.value.db = params.get("db")
})

function queryData() {
    axios.get("/listTable?env=" + params.get("env") + "&db=" + params.get("db"))
        .then((resp) => {
            tableData.value = resp.data
        })
        .catch(function (error) {
            console.log(error);
        });
}

function exportCsv(table) {
    location.href = "/exportCsv?env=" + params.get("env") + "&db=" + params.get("db") + "&table=" + table
}

const submitUpload = () => {
    upload.value.submit()
}

function onSuccess() {
    fileList.value = []
}

function parsUrlVar() {
    var paramMap = new Map();
    var query = location.search;
    if (query && query.charAt(0) === '?') {
        let str = query.substring(1)
        let params = str.split("&");
        for (let i = 0; i < params.length; i++) {
            paramMap.set(params[i].split("=")[0], decodeURI(params[i].split("=")[1]))
        }
    }
    return paramMap
}
</script>
  