<template>
    <el-table :data="tableData" stripe="true" highlight-current-row="true" style="width: 100%">
        <el-table-column prop="name" label="表名" />
        <el-table-column prop="comment" label="注释" />
        <el-table-column label="操作" style="text-align: center;">
            <template #default="scope">
                <el-button size="small" @click="exportCsv(scope.row.name)">导出</el-button>
            </template>
        </el-table-column>
    </el-table>
</template>
  
<script setup>

import { onMounted, ref } from 'vue'
import axios from 'axios'

defineProps(['env', 'db'])

const tableData = ref([])

const params = parsUrlVar()

onMounted(() => queryData())

function queryData() {
    axios.get("/api/listTable?env=" + params.get("env") + "&db=" + params.get("db"))
        .then((resp) => {
            tableData.value = resp.data
        })
        .catch(function (error) {
            console.log(error);
        });
}

function exportCsv(table) {
    location.href = "/api/exportCsv?env=" + params.get("env") + "&db=" + params.get("db") + "&table=" + table
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
  