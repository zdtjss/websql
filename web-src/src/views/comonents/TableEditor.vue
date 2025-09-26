<template>
    <el-tabs v-model="activeName" type="card" style="height:500px;" @tab-click="loadData">
        <el-tab-pane label="字段" name="colums">
            <el-table :data="columnList" style="width: 100%">
                <el-table-column prop="columnName" label="名称" />
                <el-table-column prop="columnType" label="类型" width="180" />
                <el-table-column prop="isNullable" label="可空" width="180" />
                <el-table-column prop="columnComment" label="注释" />
            </el-table>
        </el-tab-pane>
        <el-tab-pane label="选项" name="option">

        </el-tab-pane>
        <el-tab-pane label="统计" name="statistics">

        </el-tab-pane>
    </el-tabs>
</template>
<script setup>
import { ref, onMounted, onActivated } from 'vue'
import http from '@/js/utils/httpProxy.js'

const activeName = ref("colums")

const columnList = ref([])

const props = defineProps({
    tableMeta: Object,
})

onMounted(() => {
    console.log(props.tableMeta)
    loadData({ props: { name: 'columns' } })
})


function loadData(pane) {
    debugger
    if (pane.props.name === "columns") {
        http.post("/listTableColumns", props.tableMeta)
            .then((resp) => {
                columnList.value = resp.data.data
            })
    } else if (pane.props.name === "user") {

    }
}
</script>

<style lang="less" scoped></style>