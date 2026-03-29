<template>
    <el-container>
        <el-main class="sql_area">
            <el-table :data="tableData" :stripe="true" :highlight-current-row="true" width="100%" height="650">
                <el-table-column prop="name" label="表名" />
                <el-table-column prop="comment" label="注释" :show-overflow-tooltip="true" />
                <el-table-column v-if="canImport" label="操作" align="center" width="260">
                    <template #default="scope">
                        <el-row :gutter="10">
                            <el-col :span="6">
                                <el-button size="small" @click="exportXlsx(scope.row)"
                                    :loading="scope.row.exporting">导出</el-button>
                            </el-col>
                            <el-col :span="9" v-if="props.canImport">
                                <el-upload :file-list="fileListInsert" :http-request="handleFileSelect"
                                    :data="{ row: scope.row, table: scope.row.name, optType: 'insert' }"
                                    :show-file-list="false" :limit="1" accept=".xlsx,.xls">
                                    <el-button size="small" :loading="scope.row.inserting">导入/新增</el-button>
                                </el-upload>
                            </el-col>
                            <el-col :span="9" v-if="props.canImport">
                                <el-upload :file-list="fileListUpdate" :http-request="handleFileSelect"
                                    :data="{ row: scope.row, table: scope.row.name, optType: 'update' }"
                                    :show-file-list="false" :limit="1" accept=".xlsx,.xls">
                                    <el-button size="small" :loading="scope.row.updateing">导入/修改</el-button>
                                </el-upload>
                            </el-col>
                        </el-row>
                    </template>
                </el-table-column>
                <el-table-column v-else label="操作" align="center" width="100">
                    <template #default="scope">
                        <el-button size="small" @click="exportXlsx(scope.row)" :loading="scope.row.exporting">导出</el-button>
                    </template>
                </el-table-column>
            </el-table>
        </el-main>
    </el-container>

    <!-- Import preview dialog -->
    <ImportPreviewDialog
        v-model="importPreviewVisible"
        :conn-id="connId"
        :schema="schema"
        :table-name="currentRow?.name"
        :db-columns="dbColumns"
        :on-import-success="queryData"
        ref="importDialogRef"
    />
</template>

<script setup>

import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../js/utils/httpProxy.js'
import * as XLSX from 'xlsx'
import ImportPreviewDialog from '../components/ImportPreviewDialog.vue'

const props = defineProps({
    connId: String,
    schema: String,
    opt: String,
    canImport: Boolean,
})

const fileListInsert = ref([])
const fileListUpdate = ref([])

const tableData = ref([])

// Import preview state
const importPreviewVisible = ref(false)
const currentRow = ref(null)
const currentOptType = ref('insert')
const dbColumns = ref([])
const importDialogRef = ref(null)

onMounted(() => {
    queryData()
})

function queryData() {
    http.get("/listTable?connId=" + props.connId + "&schema=" + props.schema)
        .then((resp) => {
            tableData.value = resp.data
        })
        .catch((error) => {
            console.log(error);
        });
}

function exportXlsx(row) {
    row.exporting = true
    http.get("/exportXlsx?connId=" + props.connId + "&schema=" + props.schema + "&table=" + row.name, { responseType: 'blob' }).then((res) => {
        if (!res) {
            ElMessage.error("下载失败")
            return;
        }
        const blob = new Blob([res.data], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;charset=utf-8' });
        const downloadElement = document.createElement('a');
        const href = window.URL.createObjectURL(blob);
        downloadElement.style.display = 'none';
        downloadElement.href = href;
        downloadElement.download = (row.comment ? row.comment + "-" : "") + row.name + ".xlsx";
        document.body.appendChild(downloadElement);
        downloadElement.click();
        document.body.removeChild(downloadElement);
        window.URL.revokeObjectURL(href);
    }).finally(() => row.exporting = false)
}

function handleFileSelect(options) {
    currentRow.value = options.data.row
    currentOptType.value = options.data.optType
    
    const file = options.file
    const reader = new FileReader()
    reader.onload = (e) => {
        try {
            const data = new Uint8Array(e.target.result)
            const workbook = XLSX.read(data, { type: 'array' })
            const firstSheetName = workbook.SheetNames[0]
            const worksheet = workbook.Sheets[firstSheetName]
            const jsonData = XLSX.utils.sheet_to_json(worksheet, { header: 1 })
            
            if (jsonData.length === 0) {
                ElMessage({ message: 'Excel 文件为空', type: 'warning' })
                return
            }
            
            const headers = jsonData[0] || []
            const dataRows = jsonData.slice(1)
            
            // 获取数据库字段
            fetchDbColumns(options.data.table).then(() => {
                // 设置文件数据并打开对话框
                importDialogRef.value?.setFileData(file, headers, dataRows)
                importDialogRef.value?.initMapping()
                importDialogRef.value?.previewData()
                // 设置导入模式
                if (importDialogRef.value?.setImportMode) {
                    importDialogRef.value.setImportMode(currentOptType.value)
                }
                importPreviewVisible.value = true
            })
        } catch (err) {
            ElMessage({ message: '读取 Excel 文件失败：' + err.message, type: 'error' })
        }
    }
    reader.readAsArrayBuffer(options.file)
}

function fetchDbColumns(tableName) {
    return new Promise((resolve) => {
        const sql = `SELECT * FROM \`${tableName}\` LIMIT 0`
        const params = new URLSearchParams()
        params.append('connId', props.connId)
        params.append('schema', props.schema)
        params.append('sql', sql)
        
        http.post('/execSQL', params).then((resp) => {
            const data = resp.data.data
            if (data && data.columns) {
                dbColumns.value = data.columns.map(col => col.name)
            } else {
                dbColumns.value = []
            }
            resolve()
        }).catch((err) => {
            console.warn('获取数据库字段失败:', err)
            dbColumns.value = []
            resolve()
        })
    })
}

</script>
