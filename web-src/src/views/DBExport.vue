<template>
    <el-container>
        <el-main class="sql_area">
            <el-table :data="tableData" :stripe="true" :highlight-current-row="true" width="100%" height="650">
                <el-table-column prop="name" label="表名" />
                <el-table-column prop="comment" label="注释" :show-overflow-tooltip="true"/>
                <el-table-column label="操作" style="text-align: center; " width="260">
                    <template #default="scope">
                        <el-row :gutter="10">
                            <el-col :span="6">
                                <el-button size="small" @click="exportXlsx(scope.row.name)">导出</el-button>
                            </el-col>
                            <el-col :span="9">
                                <el-upload :file-list="fileListInsert" :http-request="upload" :show-file-list="false" :limit="1">
                                    <el-button size="small"
                                        @click="currentOpt = { table: scope.row.name, optType: 'insert' }">导入/新增</el-button>
                                </el-upload>
                            </el-col>
                            <el-col :span="9">
                                <el-upload :file-list="fileListUpdate" :http-request="upload" :show-file-list="false" :limit="1">
                                    <el-button size="small"
                                        @click="currentOpt = { table: scope.row.name, optType: 'update' }">导入/修改</el-button>
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
    connId: Number,
    schema: String,
    opt: String
})

const fileListInsert = ref([])
const fileListUpdate = ref([])

let currentOpt = {
    table: "",
    optType: ""
}

const tableData = ref([])

onMounted(() => {
    queryData()
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

function exportXlsx(table) {
    http.get("/exportXlsx?connId=" + props.connId + "&schema=" + props.schema + "&table=" + table, { responseType: 'blob' }).then((res) => {
        if (!res) {
            ElMessage.error("下载失败")
            return;
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

function upload(options) {

    let param = new FormData();
    param.append('file', options.file);
    param.append("connId", props.connId)
    param.append("schema", props.schema)

    Object.keys(options.data).forEach(key => {
        param.append(key, options.data[key])
    })

    Object.keys(currentOpt).forEach(key => {
        param.append(key, currentOpt[key])
    })

    http.post("/importXlsx", param, {
        headers: { "content-type": "multipart/form-data" }
    }).then((res) => {
        if (res && res.data.code === 200) {
            ElMessage.success(res.data.data);
        } else {
            if (res && res.data.msg) {
                ElMessage.error(res.data.msg);
            } else {
                ElMessage.error('导入失败');
            }
        }
    }).finally((e) => {
        fileListInsert.value = []
        fileListUpdate.value = []
    })
}

</script>
