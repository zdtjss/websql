<template>
  <el-dialog v-model="visible" title="Schema/数据比较" width="1100px" :close-on-click-modal="false" @opened="onOpen">
    <el-row :gutter="15" style="margin-bottom:15px">
      <el-col :span="11">
        <el-card shadow="never">
          <template #header><span style="color:#409EFF;font-weight:bold">源数据库</span></template>
          <el-select v-model="sourceConn" placeholder="选择连接" style="width:100%;margin-bottom:8px" @change="onSourceConnChange">
            <el-option v-for="c in (connections || []).filter(c => c)" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
          <el-select v-model="sourceSchema" placeholder="选择Schema" style="width:100%">
            <el-option v-for="s in sourceSchemas" :key="s" :label="s" :value="s" />
          </el-select>
        </el-card>
      </el-col>
      <el-col :span="2" style="text-align:center;padding-top:30px">
        <el-icon :size="24" color="#409EFF"><Right /></el-icon>
      </el-col>
      <el-col :span="11">
        <el-card shadow="never">
          <template #header><span style="color:#67c23a;font-weight:bold">目标数据库</span></template>
          <el-select v-model="targetConn" placeholder="选择连接" style="width:100%;margin-bottom:8px" @change="onTargetConnChange">
            <el-option v-for="c in (connections || []).filter(c => c)" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
          <el-select v-model="targetSchema" placeholder="选择Schema" style="width:100%">
            <el-option v-for="s in targetSchemas" :key="s" :label="s" :value="s" />
          </el-select>
        </el-card>
      </el-col>
    </el-row>

    <div style="text-align:center;margin:10px 0;display:flex;justify-content:center;align-items:center;gap:15px">
      <el-radio-group v-model="compareMode" size="small">
        <el-radio-button value="schema">结构比较</el-radio-button>
        <el-radio-button value="data">数据比较</el-radio-button>
      </el-radio-group>
      <el-select v-if="compareMode==='data'" v-model="compareTable" placeholder="选择表" filterable style="width:200px">
        <el-option v-for="t in sourceTables" :key="t" :label="t" :value="t" />
      </el-select>
      <el-button type="primary" @click="startCompare" :loading="comparing" :disabled="!canCompare">开始比较</el-button>
    </div>

    <div v-if="compareMode==='schema' && schemaDiffs.length" style="margin-top:15px">
      <el-alert :title="`差异统计: 新增 ${addCount} 表, 修改 ${modifyCount} 表, 删除 ${dropCount} 表`" type="warning" :closable="false" style="margin-bottom:10px" />

      <el-input v-model="diffTableFilter" placeholder="过滤表名..." size="small" clearable style="margin-bottom:8px;width:250px" />

      <el-table :data="filteredSchemaDiffs" max-height="400" stripe>
        <el-table-column prop="tableName" label="表名" width="180" />
        <el-table-column label="差异" width="80">
          <template #default="{row}">
            <el-tag v-if="row.diffType==='ADD'" type="success" size="small">新增</el-tag>
            <el-tag v-else-if="row.diffType==='DROP'" type="danger" size="small">删除</el-tag>
            <el-tag v-else type="warning" size="small">修改</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="列变更详情">
          <template #default="{row}">
            <div v-for="cd in row.columnDiffs" :key="cd.columnName" style="margin:2px 0;font-size:12px">
              <el-tag :type="cd.diffType==='ADD'?'success':cd.diffType==='DROP'?'danger':'warning'" size="small">
                {{cd.diffType==='ADD'?'+':cd.diffType==='DROP'?'-':'~'}} {{cd.columnName}}
              </el-tag>
              <code style="margin-left:6px;color:#909399;font-size:11px">{{cd.alterStatement?.substring(0,100)}}</code>
            </div>
            <div v-for="id in row.indexDiffs" :key="id.indexName" style="margin:2px 0;font-size:12px">
              <el-tag :type="id.diffType==='ADD'?'success':id.diffType==='DROP'?'danger':'warning'" size="small">
                idx:{{id.diffType}} {{id.indexName}}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{row}">
            <el-button type="primary" size="small" link @click="showDDL(row)">DDL</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-dialog v-model="ddlVisible" title="DDL对比" width="900px" append-to-body>
        <el-row :gutter="10">
          <el-col :span="12">
            <h4 style="color:#409EFF">源表 DDL</h4>
            <pre style="background:#1e1e1e;color:#d4d4d4;padding:10px;border-radius:4px;max-height:400px;overflow:auto;font-size:12px"><code>{{selectedDDL?.sourceDDL || '-- 无'}}</code></pre>
          </el-col>
          <el-col :span="12">
            <h4 style="color:#67c23a">目标表 DDL</h4>
            <pre style="background:#1e1e1e;color:#d4d4d4;padding:10px;border-radius:4px;max-height:400px;overflow:auto;font-size:12px"><code>{{selectedDDL?.targetDDL || '-- 无'}}</code></pre>
          </el-col>
        </el-row>
      </el-dialog>
    </div>

    <div v-if="compareMode==='data' && dataResult" style="margin-top:15px">
      <el-alert :title="`${dataResult.tableName}: 源${dataResult.totalSource}行, 目标${dataResult.totalTarget}行, 新增${dataResult.addCount}行, 修改${dataResult.modifyCount}行, 删除${dataResult.deleteCount}行`" type="warning" :closable="false" style="margin-bottom:10px" />

      <div v-if="dataResult.addedRows && dataResult.addedRows.length">
        <div style="display:flex;align-items:center;margin-bottom:8px">
          <el-tag type="success" size="small">新增行</el-tag>
          <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataResult.addCount}} 行</span>
        </div>
        <el-table :data="dataResult.addedRows" max-height="200" stripe size="small" border>
          <el-table-column v-for="col in dataResult.columns" :key="col.name" :prop="col.name" :label="col.name" width="120" :show-overflow-tooltip="true" />
        </el-table>
      </div>
      <div v-if="dataResult.modifiedRows && dataResult.modifiedRows.length" style="margin-top:10px">
        <div style="display:flex;align-items:center;margin-bottom:8px">
          <el-tag type="warning" size="small">修改行</el-tag>
          <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataResult.modifyCount}} 行</span>
        </div>
        <el-table :data="dataResult.modifiedRows" max-height="200" stripe size="small" border>
          <el-table-column label="键值" width="150">
            <template #default="{row}">
              <span v-for="(v,k) in row.key" :key="k" style="margin-right:4px;font-size:11px">{{k}}={{v}}</span>
            </template>
          </el-table-column>
          <el-table-column label="变更">
            <template #default="{row}">
              <div v-for="ch in row.changes" :key="ch.columnName">
                <span style="color:#f56c6c;text-decoration:line-through;font-size:12px">{{ch.oldValue}}</span>
                <span style="margin:0 4px">&rarr;</span>
                <span style="color:#67c23a;font-size:12px">{{ch.newValue}}</span>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div v-if="dataResult.deletedRows && dataResult.deletedRows.length" style="margin-top:10px">
        <div style="display:flex;align-items:center;margin-bottom:8px">
          <el-tag type="danger" size="small">删除行</el-tag>
          <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataResult.deleteCount}} 行</span>
        </div>
        <el-table :data="dataResult.deletedRows" max-height="200" stripe size="small" border>
          <el-table-column v-for="col in dataResult.columns" :key="col.name" :prop="col.name" :label="col.name" width="120" :show-overflow-tooltip="true" />
        </el-table>
      </div>
    </div>

    <el-empty v-if="!comparing && !schemaDiffs.length && !dataResult" description="选择源和目标后点击比较" :image-size="60" />
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Right } from '@element-plus/icons-vue'
import { listConn, getSyncTargets, compareSyncSchema, compareSyncData } from '@/api/conn'

const visible = defineModel({ default: false })

const { connId, schema } = defineProps({
  connId: String,
  schema: String
})

const connections = ref([])
const sourceConn = ref('')
const targetConn = ref('')
const sourceSchema = ref('')
const targetSchema = ref('')
const sourceSchemas = ref([])
const targetSchemas = ref([])
const sourceTables = ref([])
const compareMode = ref('schema')
const compareTable = ref('')
const comparing = ref(false)
const schemaDiffs = ref([])
const dataResult = ref(null)
const diffTableFilter = ref('')
const ddlVisible = ref(false)
const selectedDDL = ref(null)

const canCompare = computed(() => {
  if (!sourceConn.value || !targetConn.value || !sourceSchema.value || !targetSchema.value) return false
  if (compareMode.value === 'data' && !compareTable.value) return false
  return true
})

const filteredSchemaDiffs = computed(() => {
  if (!diffTableFilter.value) return schemaDiffs.value
  const kw = diffTableFilter.value.toLowerCase()
  return schemaDiffs.value.filter(d => d.tableName.toLowerCase().includes(kw))
})

const addCount = computed(() => schemaDiffs.value.filter(d => d.diffType === 'ADD').length)
const dropCount = computed(() => schemaDiffs.value.filter(d => d.diffType === 'DROP').length)
const modifyCount = computed(() => schemaDiffs.value.filter(d => d.diffType === 'MODIFY').length)

async function onOpen() {
  try {
    const res = await listConn({})
    connections.value = (res.data.data || []).filter(c => c && c.id)
  } catch (e) {}
  if (connId) { sourceConn.value = connId; targetConn.value = connId }
  if (connId) { onSourceConnChange(); onTargetConnChange() }
  if (schema) { sourceSchema.value = schema; targetSchema.value = schema }
}

async function onSourceConnChange() {
  if (!sourceConn.value) return
  try {
    const res = await getSyncTargets(sourceConn.value)
    const result = res.data.data || res.data
    sourceSchemas.value = result.schemas || []
    sourceTables.value = result.tables || []
  } catch (e) {}
}

async function onTargetConnChange() {
  if (!targetConn.value) return
  try {
    const res = await getSyncTargets(targetConn.value)
    const result = res.data.data || res.data
    targetSchemas.value = result.schemas || []
  } catch (e) {}
}

async function startCompare() {
  if (!canCompare.value) {
    ElMessage.warning('请选择源和目标数据库')
    return
  }
  comparing.value = true
  schemaDiffs.value = []
  dataResult.value = null
  try {
    if (compareMode.value === 'schema') {
      const formData = new FormData()
      formData.append('sourceConnId', sourceConn.value)
      formData.append('targetConnId', targetConn.value)
      formData.append('sourceSchema', sourceSchema.value)
      formData.append('targetSchema', targetSchema.value)
      const res = await compareSyncSchema(formData)
      const result = res.data.data || res.data
      if (result.error) {
        ElMessage.error(result.error)
        return
      }
      schemaDiffs.value = result.diffs || []
      ElMessage.success(`比较完成，发现 ${schemaDiffs.value.length} 个差异`)
    } else {
      const formData = new FormData()
      formData.append('sourceConnId', sourceConn.value)
      formData.append('targetConnId', targetConn.value)
      formData.append('sourceSchema', sourceSchema.value)
      formData.append('targetSchema', targetSchema.value)
      formData.append('table', compareTable.value)
      const res = await compareSyncData(formData)
      dataResult.value = res.data.data || res.data
      ElMessage.success('数据比较完成')
    }
  } catch (e) {
    ElMessage.error('比较失败: ' + (e.message || ''))
  } finally {
    comparing.value = false
  }
}

function showDDL(row) {
  selectedDDL.value = row
  ddlVisible.value = true
}
</script>
