
<script setup>
import { ref, onMounted } from 'vue'
import axios from 'axios'

const tableData = ref()

onMounted(() => queryData())

const queryData = () => {
  axios.get('/api/sqlite')
    .then((response) => {
      tableData.value = response.data
    })
    .catch(function (error) {
      console.log(error);
    });
}

</script>

<template>
  <el-table :data="tableData" style="width: 100%">
    <el-table-column prop="username" label="Date" width="180" />
    <el-table-column prop="uepartment" label="Name" width="180" />
    <el-table-column prop="created" label="Address" />
  </el-table>
</template>
