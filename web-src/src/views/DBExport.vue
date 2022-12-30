<template>
    <el-container>
        <el-main class="sql_area">
            <el-table :data="tableData" stripe="true" highlight-current-row="true" width="100%">
                <el-table-column prop="name" label="表名" />
                <el-table-column prop="comment" label="注释" />
                <el-table-column label="操作" style="text-align: center; ">
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

import http from '../js/utils/httpProxy.js'

const props = defineProps({
    connId: String,
    schema: String,
    start: Number,
    opt: String
})

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

onMounted(() => {

    queryData()

    dataInsert.value.start = props.start
    dataInsert.value.connId = props.connId
    dataInsert.value.schema = props.schema

    dataUpdate.value.start = props.start
    dataUpdate.value.connId = props.connId
    dataUpdate.value.schema = props.schema
})

function queryData() {
    http.get("/listTable?connId=" + props.connId + "&schema=" + props.schema)
        .then((resp) => {
            tableData.value = resp.data.data
        })
        .catch(function (error) {
            console.log(error);
        });
}

function exportCsv(table) {
    http.get("/exportCsv?connId=" + props.connId + "&schema=" + props.schema + "&table=" + table, { responseType: 'blob' }).then((res) => {
        if (!res) {
            this.$message.error("下载模板文件失败");
            return false;
        }
        const blob = new Blob([res.data], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;charset=utf-8' });
        const downloadElement = document.createElement('a');
        const href = window.URL.createObjectURL(blob);
        let contentDisposition = res.headers['content-disposition'];  //从response的headers中获取filename, 后端response.setHeader("Content-disposition", "attachment; filename=xxxx.docx") 设置的文件名;
        let patt = new RegExp("filename=([^;]+\\.[^\\.;]+);*");
        let result = patt.exec(contentDisposition);
        let filename = decodeURI(result[1]);
        downloadElement.style.display = 'none';
        downloadElement.href = href;
        downloadElement.download = filename; //下载后文件名
        document.body.appendChild(downloadElement);
        downloadElement.click(); //点击下载
        document.body.removeChild(downloadElement); //下载完成移除元素
        window.URL.revokeObjectURL(href); //释放掉blob对象
    })
}
</script>
  