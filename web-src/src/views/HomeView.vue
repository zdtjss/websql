
<script setup>
import { ref, onMounted } from 'vue'
import axios from 'axios'
import dayjs from 'dayjs'

const tableData = ref()

onMounted(() => queryData())

const queryData = () => {
  axios.get("/api/sqlite")
    .then((response) => {
      tableData.value = response.data
    })
    .catch(function (error) {
      console.log(error);
    });
}

function formatterDateTime(row, column, cellValue, index) {
  return dayjs(cellValue).format("YYYY-MM-DD")
}
</script>

<template>
  <el-table :data="tableData" style="width: 100%">
    <el-table-column prop="username" label="名字" width="180" />
    <el-table-column prop="uepartment" label="部门" width="180" />
    <el-table-column prop="created" label="创建时间" :formatter="formatterDateTime" />
  </el-table>
</template>
