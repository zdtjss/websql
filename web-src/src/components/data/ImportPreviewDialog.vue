<template>
  <el-dialog
    v-model="visible"
    :title="importMode === 'insert' ? '导入预览 - 新增' : '导入预览 - 修改'"
    width="1000px"
    :draggable="true"
    destroy-on-close
  >
    <div style="margin-bottom: 15px; display: flex; align-items: center; gap: 12px; flex-wrap: wrap;">
      <el-form :inline="true" size="small">
        <el-form-item label="导入模式">
          <el-radio-group v-model="importMode" size="small">
            <el-radio-button value="insert">
              <el-icon><Upload /></el-icon>新增
            </el-radio-button>
            <el-radio-button value="update">
              <el-icon><Refresh /></el-icon>修改
            </el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="数据起始行">
          <el-input-number v-model="dataStartRow" :min="1" :step="1" style="width: 100px;" @change="previewData" />
        </el-form-item>
        <el-form-item label="预览行数">
          <el-input-number v-model="previewRows" :min="1" :max="100" :step="10" style="width: 100px;" @change="previewData" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="small" @click="previewData">
            <el-icon><Refresh /></el-icon>刷新
          </el-button>
        </el-form-item>
      </el-form>
    </div>

    <div style="margin-bottom: 10px; overflow-x: auto;">
      <el-table 
        :data="previewDataList" 
        border 
        max-height="400" 
        stripe 
        :header-cell-style="{background:'#f5f7fa'}"
        :resizable="true"
      >
        <el-table-column 
          v-for="(col, idx) in previewColumns" 
          :key="col.excelCol + '_' + idx"
          min-width="120"
          :width="150"
          resizable
        >
          <template #header>
            <div style="display: flex; flex-direction: column; gap: 6px; padding: 8px 0;">
              <div style="display: flex; flex-direction: column; gap: 6px; width: 100%;">
                <!-- 未匹配字段：显示红色 Excel 列名 + 下拉框 -->
                <template v-if="!col.dbCol">
                  <div 
                    style="font-weight: 600; font-size: 13px; color: #f56c6c; white-space: nowrap;" 
                    :title="col.excelCol"
                  >
                    {{ col.excelCol }}
                  </div>
                  <el-select 
                    v-model="col.dbCol" 
                    size="small" 
                    placeholder="选择字段"
                    filterable
                    allow-create
                    clearable
                    style="width: 100%;"
                    @change="onMappingChange"
                  >
                    <el-option
                      v-for="dbCol in getAvailableColumns(col)"
                      :key="dbCol"
                      :label="dbCol"
                      :value="dbCol"
                    />
                  </el-select>
                </template>
                <!-- 已匹配字段：显示绿色数据库字段名 + 重置按钮（仅自定义匹配） -->
                <template v-else>
                  <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;">
                    <div style="font-size: 13px; color: #67c23a; font-weight: 600; white-space: nowrap; flex: 1; overflow: hidden; text-overflow: ellipsis;">
                      <el-icon style="vertical-align: middle;"><CircleCheck /></el-icon>
                      {{ col.dbCol }}
                    </div>
                    <el-button 
                      v-if="!col.isAutoMatched"
                      size="small" 
                      type="warning" 
                      link
                      style="flex-shrink: 0; padding: 4px;"
                      @click="resetColumnMapping(col)"
                    >
                      <el-icon size="14"><RefreshLeft /></el-icon>
                    </el-button>
                  </div>
                </template>
              </div>
            </div>
          </template>
          <template #default="scope">
            <span :title="scope.row[col.excelCol]" style="font-size: 13px;">{{ scope.row[col.excelCol] }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
    
    <template #footer>
      <div style="display: flex; justify-content: space-between; align-items: center;">
        <div style="display: flex; gap: 12px;">
          <el-tag type="success" size="small">
            <el-icon><CircleCheck /></el-icon>
            已匹配 {{ mappingStatus.matched > 0 ? mappingStatus.matched : '0' }}
          </el-tag>
          <el-tag type="danger" size="small">
            <el-icon><Warning /></el-icon>
            未匹配 {{ mappingStatus.unmatchedExcel.length > 0 ? mappingStatus.unmatchedExcel.length : '0' }}
          </el-tag>
        </div>
        <div style="display: flex; gap: 10px;">
          <el-button @click="handleClose">取消</el-button>
          <el-button type="primary" :loading="importing" @click="confirmImport">
            <el-icon><Upload /></el-icon>导入
          </el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import { CircleCheck, Refresh, RefreshLeft, Upload, Warning } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { ref, watch } from 'vue'
import http from '@/api/index'

const visible = defineModel({ default: false })
const { connId, schema, tableName, dbColumns, onImportSuccess, importFormat } = defineProps({
  connId: String,
  schema: String,
  tableName: String,
  dbColumns: Array,
  onImportSuccess: Function,
  importFormat: { type: String, default: 'xlsx' }
})
const emit = defineEmits(['success', 'confirmImportData'])

// 状态管理
const importMode = ref('insert') // 'insert' 或 'update'
const dataStartRow = ref(1)
const previewRows = ref(10)
const previewDataList = ref([])
const previewColumns = ref([])
const excelData = ref([])
const excelHeaders = ref([])
const selectedFile = ref(null)
const importing = ref(false)

const mappingStatus = ref({
  matched: 0,
  unmatchedExcel: [],
  unmatchedDb: []
})

// 监听 dbColumns 变化
watch(() => dbColumns, (newVal) => {
  if (newVal && newVal.length > 0 && excelHeaders.value.length > 0) {
    initMapping()
    previewData()
  }
}, { immediate: true })

// 初始化映射
function initMapping() {
  const dbCols = dbColumns || []
  previewColumns.value = excelHeaders.value.map((excelCol) => {
    const matchedDbCol = dbCols.find(dbCol => dbCol.toLowerCase() === excelCol.toLowerCase())
    return {
      excelCol: excelCol,
      dbCol: matchedDbCol || '',
      isAutoMatched: !!matchedDbCol
    }
  })
  updateMappingStatus()
}

// 映射变化
function onMappingChange() {
  updateMappingStatus()
}

// 获取可用字段
function getAvailableColumns(currentCol) {
  const dbCols = dbColumns || []
  const usedColumns = previewColumns.value
    .filter(col => col.dbCol && col.excelCol !== currentCol.excelCol)
    .map(col => col.dbCol)
  
  return dbCols.filter(dbCol => !usedColumns.includes(dbCol))
}

// 重置列映射
function resetColumnMapping(col) {
  col.dbCol = ''
  updateMappingStatus()
  ElMessage({ message: '已重置该列的映射关系', type: 'info' })
}

// 更新映射状态
function updateMappingStatus() {
  const matched = previewColumns.value.filter(col => col.dbCol).length
  const unmatchedExcel = previewColumns.value.filter(col => !col.dbCol).map(col => col.excelCol)
  const matchedDbCols = previewColumns.value.filter(col => col.dbCol).map(col => col.dbCol)
  const dbCols = dbColumns || []
  const unmatchedDb = dbCols.filter(dbCol => !matchedDbCols.includes(dbCol))
  
  mappingStatus.value = {
    matched,
    unmatchedExcel,
    unmatchedDb
  }
}

// 预览数据
function previewData() {
  const startIdx = dataStartRow.value - 1
  const endIdx = startIdx + previewRows.value
  const slicedData = excelData.value.slice(startIdx, endIdx)
  
  previewDataList.value = slicedData.map((row) => {
    const rowData = {}
    excelHeaders.value.forEach((header, idx) => {
      rowData[header] = row[idx] !== undefined ? row[idx] : null
    })
    return rowData
  })
}

// 确认导入
function confirmImport() {
  const validMapping = previewColumns.value.filter(col => col.dbCol)
  if (validMapping.length === 0) {
    ElMessage({ message: '请至少映射一个字段', type: 'warning' })
    return
  }

  if (importFormat === 'csv' || importFormat === 'json') {
    const mapping = {}
    previewColumns.value.forEach(col => {
      if (col.dbCol) {
        mapping[col.excelCol] = col.dbCol
      }
    })
    const mappedData = excelData.value.map(row => {
      const obj = {}
      excelHeaders.value.forEach((header, idx) => {
        const dbCol = mapping[header]
        if (dbCol) {
          obj[dbCol] = row[idx] !== undefined ? row[idx] : null
        }
      })
      return obj
    })
    emit('confirmImportData', { data: mappedData, mapping, mode: importMode.value })
    return
  }

  executeImport()
}

// 执行导入
function executeImport() {
  const mapping = {}
  previewColumns.value.forEach(col => {
    if (col.dbCol) {
      mapping[col.excelCol] = col.dbCol
    }
  })
  
  let param = new FormData()
  param.append('file', selectedFile.value)
  param.append('connId', connId)
  param.append('schema', schema)
  param.append('table', tableName)
  param.append('optType', importMode.value) // 添加导入模式参数
  param.append('startRow', dataStartRow.value.toString())
  param.append('mapping', JSON.stringify(mapping))
  
  importing.value = true
  
  http.post('/importXlsx', param, {
    headers: { 'content-type': 'multipart/form-data' }
  }).then((res) => {
    if (res && res.status === 200) {
      const modeText = importMode.value === 'insert' ? '新增' : '修改'
      ElMessage({ message: `导入${modeText}成功`, type: 'success' })
      visible.value = false
      emit('success')
      if (onImportSuccess) {
        onImportSuccess()
      }
    } else {
      if (res && res.data) {
        ElMessage({ message: '导入失败，请检查数据格式', type: 'error', duration: 5000 })
      } else {
        ElMessage({ message: '导入失败', type: 'error', duration: 5000 })
      }
    }
  }).catch((err) => {
    console.error('导入错误:', err)
    ElMessage({ 
      message: '导入失败，请检查数据格式或联系管理员', 
      type: 'error',
      duration: 5000,
      showClose: true
    })
  }).finally(() => {
    importing.value = false
  })
}

// 关闭对话框
function handleClose() {
  visible.value = false
}

// 暴露方法供外部调用
function setFileData(file, headers, data) {
  selectedFile.value = file
  excelHeaders.value = headers
  excelData.value = data
}

function setImportMode(mode) {
  importMode.value = mode
}

defineExpose({
  setFileData,
  initMapping,
  previewData,
  setImportMode
})
</script>
