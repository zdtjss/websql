<template>
  <el-dialog v-model="visible" title="数据同步与结构同步" width="1200px" :close-on-click-modal="false" @opened="onOpen"
    :draggable="!isFullscreen" :fullscreen="isFullscreen" :show-close="false">
    <template #header="{ close }">
      <div style="display:flex;justify-content:space-between;align-items:center">
        <span style="font-weight:bold;font-size:16px">数据同步与结构同步</span>
        <div style="display:flex;gap:4px">
          <el-button text @click="isFullscreen = !isFullscreen" :title="isFullscreen ? '还原' : '全屏'">
            <el-icon><FullScreen /></el-icon>
          </el-button>
          <el-button text @click="close" title="关闭">
            <el-icon><Close /></el-icon>
          </el-button>
        </div>
      </div>
    </template>
    <el-steps :active="activeStep" align-center style="margin-bottom:20px">
      <el-step title="选择源和目标" />
      <el-step title="比较中" />
      <el-step title="预览差异" />
      <el-step title="执行同步" />
    </el-steps>

    <div v-show="activeStep === 0">
      <el-row :gutter="20">
        <el-col :span="11">
          <el-card shadow="never">
            <template #header><span style="color:#409EFF;font-weight:bold">源数据库</span></template>
            <el-form label-position="top">
              <el-form-item label="连接">
                <el-select v-model="sourceConnId" placeholder="选择源连接" style="width:100%" :disabled="!!props.connId" @change="onSourceConnChange">
                  <el-option v-for="c in connections" :key="c.id" :label="c.name" :value="c.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="Schema">
                <el-select v-model="sourceSchema" placeholder="选择源Schema" style="width:100%">
                  <el-option v-for="s in sourceSchemas" :key="s" :label="s" :value="s" />
                </el-select>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>
        <el-col :span="2" style="text-align:center;padding-top:60px">
          <el-icon :size="28" color="#409EFF"><Right /></el-icon>
        </el-col>
        <el-col :span="11">
          <el-card shadow="never">
            <template #header><span style="color:#67c23a;font-weight:bold">目标数据库</span></template>
            <el-form label-position="top">
              <el-form-item label="连接">
                <el-select v-model="targetConnId" filterable placeholder="选择目标连接" style="width:100%" @change="onTargetConnChange">
                  <el-option v-for="c in connections" :key="c.id" :label="c.name" :value="c.id" />
                </el-select>
              </el-form-item>
              <el-form-item label="Schema">
                <el-select v-model="targetSchema" placeholder="选择目标Schema" style="width:100%">
                  <el-option v-for="s in targetSchemas" :key="s" :label="s" :value="s" />
                </el-select>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>
      </el-row>

      <div style="text-align:center;margin:15px 0">
        <el-radio-group v-model="syncMode" size="large">
          <el-radio-button value="structure">结构同步</el-radio-button>
          <el-radio-button value="data">数据同步</el-radio-button>
        </el-radio-group>
      </div>

      <div v-if="syncMode === 'data'" style="margin:0 auto;max-width:500px">
        <el-form label-position="top">
          <el-form-item label="选择要同步的表">
            <el-select v-model="syncTable" placeholder="选择表" style="width:100%" filterable>
              <el-option v-for="t in sourceTables" :key="t" :label="t" :value="t" />
            </el-select>
          </el-form-item>
          <el-form-item label="同步方向">
            <el-radio-group v-model="syncDirection">
              <el-radio value="source_to_target">源 → 目标</el-radio>
              <el-radio value="target_to_source">目标 → 源</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="分块大小">
            <el-input-number v-model="chunkSize" :min="1000" :max="50000" :step="1000" style="width:100%" />
            <div style="font-size:12px;color:#909399;margin-top:4px">大表建议5000，小表可增大到20000以提升速度</div>
          </el-form-item>
        </el-form>
      </div>

      <div v-if="syncMode === 'structure'" style="margin:0 auto;max-width:500px">
        <el-form label-position="top">
          <el-form-item label="过滤表（留空则比较全部）">
            <el-select v-model="tableFilter" multiple filterable allow-create default-first-option placeholder="输入表名" style="width:100%">
              <el-option v-for="t in sourceTables" :key="t" :label="t" :value="t" />
            </el-select>
          </el-form-item>
        </el-form>
      </div>

      <div style="text-align:center;margin-top:20px">
        <el-button type="primary" size="large" @click="startCompare" :loading="comparing" :disabled="!canCompare">
          开始比较
        </el-button>
      </div>
    </div>

    <div v-show="activeStep === 1" style="text-align:center;padding:40px">
      <el-icon class="is-loading" :size="48" color="#409EFF"><Loading /></el-icon>
      <p style="margin-top:15px;color:#606266;font-size:15px">{{compareStatus}}</p>
      <el-progress v-if="compareProgress > 0" :percentage="compareProgress" style="margin-top:15px;max-width:400px;margin-left:auto;margin-right:auto" />
      <div v-if="chunkInfo.totalChunks > 1" style="margin-top:10px;color:#909399;font-size:13px">
        分块 {{chunkInfo.currentChunk}} / {{chunkInfo.totalChunks}}，每块 {{chunkSize}} 行
      </div>
      <div v-if="chunkInfo.accumulatedStats.add || chunkInfo.accumulatedStats.modify || chunkInfo.accumulatedStats.delete" style="margin-top:10px;font-size:13px">
        <el-tag type="success" size="small" style="margin-right:6px">新增 {{chunkInfo.accumulatedStats.add}}</el-tag>
        <el-tag type="warning" size="small" style="margin-right:6px">修改 {{chunkInfo.accumulatedStats.modify}}</el-tag>
        <el-tag type="danger" size="small">删除 {{chunkInfo.accumulatedStats.delete}}</el-tag>
      </div>
      <el-button v-if="comparing" type="danger" size="small" style="margin-top:15px" @click="cancelOperation">取消比较</el-button>
    </div>

    <div v-show="activeStep === 2">
      <div v-if="syncMode === 'structure'">
        <el-alert :title="`发现 ${schemaDiffs.length} 个差异 (新增:${addCount}, 修改:${modifyCount}, 删除:${dropCount})`" type="warning" :closable="false" style="margin-bottom:15px" />

        <el-table :data="schemaDiffs" max-height="400" stripe highlight-current-row @row-click="selectSchemaDiff">
          <el-table-column prop="tableName" label="表名" width="180" />
          <el-table-column prop="diffType" label="差异类型" width="100">
            <template #default="{row}">
              <el-tag v-if="row.diffType==='ADD'" type="success" size="small">新增</el-tag>
              <el-tag v-else-if="row.diffType==='DROP'" type="danger" size="small">删除</el-tag>
              <el-tag v-else type="warning" size="small">修改</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="列变更" min-width="250">
            <template #default="{row}">
              <template v-if="row.columnDiffs && row.columnDiffs.length">
                <div v-for="cd in row.columnDiffs" :key="cd.columnName" style="margin:2px 0">
                  <el-tag v-if="cd.diffType==='ADD'" type="success" size="small">+{{cd.columnName}}</el-tag>
                  <el-tag v-else-if="cd.diffType==='DROP'" type="danger" size="small">-{{cd.columnName}}</el-tag>
                  <el-tag v-else type="warning" size="small">~{{cd.columnName}}</el-tag>
                  <span style="font-size:12px;color:#909399;margin-left:4px">{{cd.sourceDef}}</span>
                </div>
              </template>
              <span v-else style="color:#909399;font-size:12px">{{ row.diffType === 'ADD' ? '整表新增' : row.diffType === 'DROP' ? '整表删除' : '索引变更' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="索引变更" width="200">
            <template #default="{row}">
              <template v-if="row.indexDiffs && row.indexDiffs.length">
                <div v-for="id in row.indexDiffs" :key="id.indexName" style="margin:2px 0">
                  <el-tag :type="id.diffType==='ADD'?'success':id.diffType==='DROP'?'danger':'warning'" size="small">
                    {{id.diffType==='ADD'?'+':id.diffType==='DROP'?'-':'~'}}idx:{{id.indexName}}
                  </el-tag>
                </div>
              </template>
            </template>
          </el-table-column>
        </el-table>

        <div v-if="selectedSchemaDiff" style="margin-top:15px">
          <el-card shadow="never">
            <template #header>
              <div style="display:flex;justify-content:space-between;align-items:center">
                <span style="font-weight:bold">SQL变更脚本</span>
                <el-button type="primary" size="small" @click="copySQL(generatedSQL)">复制SQL</el-button>
              </div>
            </template>
            <pre style="background:#1e1e1e;color:#d4d4d4;padding:15px;border-radius:6px;max-height:300px;overflow:auto;font-size:13px;line-height:1.5"><code>{{generatedSQL}}</code></pre>
          </el-card>
        </div>
      </div>

      <div v-else>
        <el-alert :title="`表 ${syncTable}: 源 ${dataDiff.totalSource||0} 行, 目标 ${dataDiff.totalTarget||0} 行, 新增 ${dataDiff.addCount||0}, 修改 ${dataDiff.modifyCount||0}, 删除 ${dataDiff.deleteCount||0}`" type="warning" :closable="false" style="margin-bottom:15px" />

        <div v-if="dataDiff.addedRows && dataDiff.addedRows.length" style="margin-bottom:15px">
          <div style="display:flex;align-items:center;margin-bottom:8px">
            <el-tag type="success" size="small">新增行</el-tag>
            <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataDiff.addCount}} 行，显示前 {{dataDiff.addedRows.length}} 行</span>
          </div>
          <el-table :data="dataDiff.addedRows" max-height="200" stripe size="small" border>
            <el-table-column v-for="col in dataDiff.columns" :key="col.name" :prop="col.name" :label="col.name" width="120" :show-overflow-tooltip="true" />
          </el-table>
        </div>

        <div v-if="dataDiff.modifiedRows && dataDiff.modifiedRows.length" style="margin-bottom:15px">
          <div style="display:flex;align-items:center;margin-bottom:8px">
            <el-tag type="warning" size="small">修改行</el-tag>
            <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataDiff.modifyCount}} 行，显示前 {{dataDiff.modifiedRows.length}} 行</span>
          </div>
          <el-table :data="dataDiff.modifiedRows" max-height="200" stripe size="small" border>
            <el-table-column label="键值" width="180">
              <template #default="{row}">
                <span v-for="(v,k) in row.key" :key="k" style="margin-right:6px;font-size:12px"><strong>{{k}}</strong>={{v}}</span>
              </template>
            </el-table-column>
            <el-table-column label="变更字段">
              <template #default="{row}">
                <div v-for="ch in row.changes" :key="ch.columnName" style="margin:2px 0;font-size:12px">
                  <strong>{{ch.columnName}}</strong>:
                  <span style="color:#f56c6c;text-decoration:line-through">{{ch.oldValue}}</span>
                  <span style="margin:0 4px">&rarr;</span>
                  <span style="color:#67c23a">{{ch.newValue}}</span>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <div v-if="dataDiff.deletedRows && dataDiff.deletedRows.length" style="margin-bottom:15px">
          <div style="display:flex;align-items:center;margin-bottom:8px">
            <el-tag type="danger" size="small">删除行</el-tag>
            <span style="margin-left:8px;color:#909399;font-size:12px">共 {{dataDiff.deleteCount}} 行，显示前 {{dataDiff.deletedRows.length}} 行</span>
          </div>
          <el-table :data="dataDiff.deletedRows" max-height="200" stripe size="small" border>
            <el-table-column v-for="col in dataDiff.columns" :key="col.name" :prop="col.name" :label="col.name" width="120" :show-overflow-tooltip="true" />
          </el-table>
        </div>

        <el-card shadow="never" style="margin-top:15px">
          <template #header>
            <div style="display:flex;justify-content:space-between;align-items:center">
              <span style="font-weight:bold">同步SQL预览</span>
              <div style="display:flex;gap:8px;align-items:center">
                <span v-if="accumulatedSQL && !syncSQL" style="font-size:12px;color:#909399">已生成 {{sqlStatementCount}} 条SQL</span>
                <el-button v-if="!accumulatedSQL && !syncSQL" type="primary" size="small" @click="loadSyncSQL" :loading="loadingSQL">生成SQL</el-button>
                <el-button type="primary" size="small" @click="copySQL(syncSQL || accumulatedSQL || '')">复制SQL</el-button>
              </div>
            </div>
          </template>
          <pre v-if="syncSQL || accumulatedSQL" style="background:#1e1e1e;color:#d4d4d4;padding:15px;border-radius:6px;max-height:250px;overflow:auto;font-size:13px;line-height:1.5"><code>{{syncSQL || accumulatedSQL}}</code></pre>
          <div v-else style="text-align:center;color:#909399;padding:20px">点击"生成SQL"预览同步语句</div>
        </el-card>
      </div>
    </div>

    <div v-show="activeStep === 3">
      <div v-if="syncing">
        <div style="text-align:center;padding:40px">
          <el-icon class="is-loading" :size="48" color="#409EFF"><Loading /></el-icon>
          <p style="margin-top:15px;color:#606266;font-size:15px">{{syncStatusText}}</p>
          <el-progress :percentage="syncProgress" style="margin-top:15px;max-width:400px;margin-left:auto;margin-right:auto" />
          <div style="margin-top:10px;font-size:13px;color:#909399">
            批次 {{syncBatchInfo.current}} / {{syncBatchInfo.total}}
          </div>
          <div v-if="syncBatchInfo.insertCount || syncBatchInfo.updateCount || syncBatchInfo.deleteCount" style="margin-top:10px;font-size:13px">
            <el-tag type="success" size="small" style="margin-right:6px">插入 {{syncBatchInfo.insertCount}}</el-tag>
            <el-tag type="warning" size="small" style="margin-right:6px">更新 {{syncBatchInfo.updateCount}}</el-tag>
            <el-tag type="danger" size="small">删除 {{syncBatchInfo.deleteCount}}</el-tag>
          </div>
          <el-button type="danger" size="small" style="margin-top:15px" @click="cancelOperation">取消同步</el-button>
        </div>
      </div>
      <div v-else>
        <el-result :icon="syncSuccess ? 'success' : 'warning'" :title="syncSuccess ? '同步完成' : '同步完成（部分错误）'" :sub-title="syncResult">
          <template #extra>
            <el-button @click="resetDialog">重新同步</el-button>
            <el-button type="primary" @click="visible=false">关闭</el-button>
          </template>
        </el-result>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible=false">关闭</el-button>
      <el-button v-if="activeStep===2" @click="activeStep=0">上一步</el-button>
      <el-button v-if="activeStep===2" type="primary" @click="executeSync" :loading="syncing">
        {{syncMode === 'structure' ? '执行结构同步' : '执行数据同步'}}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Right, FullScreen, Close } from '@element-plus/icons-vue'
import http from '@/js/utils/httpProxy.js'

const props = defineProps({
  modelValue: Boolean,
  connId: String,
  schema: String
})
const emit = defineEmits(['update:modelValue'])
const visible = computed({ get: () => props.modelValue, set: v => emit('update:modelValue', v) })

const activeStep = ref(0)
const isFullscreen = ref(false)
const syncMode = ref('structure')
const sourceConnId = ref('')
const targetConnId = ref('')
const sourceSchema = ref('')
const targetSchema = ref('')
const connections = ref([])
const sourceSchemas = ref([])
const targetSchemas = ref([])
const sourceTables = ref([])
const comparing = ref(false)
const syncing = ref(false)
const compareProgress = ref(0)
const compareStatus = ref('')
const schemaDiffs = ref([])
const dataDiff = ref({})
const syncTable = ref('')
const syncDirection = ref('source_to_target')
const tableFilter = ref([])
const selectedSchemaDiff = ref(null)
const syncResult = ref('')
const syncSuccess = ref(true)
const syncSQL = ref('')
const loadingSQL = ref(false)
const chunkSize = ref(5000)
const accumulatedSQL = ref('')
const sqlStatementCount = ref(0)
const cancelled = ref(false)
let abortController = null

const chunkInfo = reactive({
  totalChunks: 0,
  currentChunk: 0,
  accumulatedStats: { add: 0, modify: 0, delete: 0 }
})

const syncProgress = ref(0)
const syncStatusText = ref('')
const syncBatchInfo = reactive({
  current: 0,
  total: 0,
  insertCount: 0,
  updateCount: 0,
  deleteCount: 0
})

const addCount = computed(() => schemaDiffs.value.filter(d => d.diffType === 'ADD').length)
const dropCount = computed(() => schemaDiffs.value.filter(d => d.diffType === 'DROP').length)
const modifyCount = computed(() => schemaDiffs.value.filter(d => d.diffType === 'MODIFY').length)

const canCompare = computed(() => {
  if (!sourceConnId.value || !targetConnId.value || !sourceSchema.value || !targetSchema.value) return false
  if (syncMode.value === 'data' && !syncTable.value) return false
  return true
})

const generatedSQL = computed(() => {
  if (!selectedSchemaDiff.value) return ''
  let sql = ''
  if (selectedSchemaDiff.value.diffType === 'ADD') {
    sql = selectedSchemaDiff.value.sourceDDL ? `CREATE TABLE ... (源表DDL);\n` : ''
  } else if (selectedSchemaDiff.value.diffType === 'DROP') {
    sql = `-- 目标端存在但源端不存在，如需删除: DROP TABLE \`${selectedSchemaDiff.value.tableName}\`;\n`
  }
  if (selectedSchemaDiff.value.columnDiffs) {
    for (const cd of selectedSchemaDiff.value.columnDiffs) {
      sql += cd.alterStatement + '\n'
    }
  }
  if (selectedSchemaDiff.value.indexDiffs) {
    for (const id of selectedSchemaDiff.value.indexDiffs) {
      sql += id.alterStatement + '\n'
    }
  }
  return sql || '-- 无变更'
})

watch(syncMode, () => {
  syncTable.value = ''
  syncSQL.value = ''
  accumulatedSQL.value = ''
})

watch(sourceConnId, () => { sourceSchema.value = '' })
watch(targetConnId, () => { targetSchema.value = '' })

async function onOpen() {
  activeStep.value = 0
  isFullscreen.value = false
  schemaDiffs.value = []
  dataDiff.value = {}
  syncSQL.value = ''
  accumulatedSQL.value = ''
  sqlStatementCount.value = 0
  selectedSchemaDiff.value = null
  sourceConnId.value = ''
  targetConnId.value = ''
  sourceSchema.value = ''
  targetSchema.value = ''
  sourceSchemas.value = []
  targetSchemas.value = []
  sourceTables.value = []
  chunkInfo.totalChunks = 0
  chunkInfo.currentChunk = 0
  chunkInfo.accumulatedStats = { add: 0, modify: 0, delete: 0 }
  syncProgress.value = 0
  syncBatchInfo.current = 0
  syncBatchInfo.total = 0
  syncBatchInfo.insertCount = 0
  syncBatchInfo.updateCount = 0
  syncBatchInfo.deleteCount = 0
  await loadConnections()
  if (props.connId) {
    sourceConnId.value = props.connId
    onSourceConnChange()
  }
  if (props.schema) sourceSchema.value = props.schema
}

async function loadConnections() {
  try {
    const res = await http.get('/listConn2', { params: { pageSize: 9999 } })
    const result = res.data.data || res.data
    connections.value = result.data || []
  } catch (e) {}
}

async function onSourceConnChange() {
  if (!sourceConnId.value) return
  try {
    const res = await http.get('/sync/targets', { params: { connId: sourceConnId.value } })
    const result = res.data.data || res.data
    sourceSchemas.value = result.schemas || []
    sourceTables.value = result.tables || []
  } catch (e) {}
}

async function onTargetConnChange() {
  if (!targetConnId.value) return
  try {
    const res = await http.get('/sync/targets', { params: { connId: targetConnId.value } })
    const result = res.data.data || res.data
    targetSchemas.value = result.schemas || []
  } catch (e) {}
}

function selectSchemaDiff(row) { selectedSchemaDiff.value = row }

function copySQL(sql) {
  if (!sql) { ElMessage.warning('没有可复制的SQL'); return }
  navigator.clipboard.writeText(sql).then(() => ElMessage.success('已复制到剪贴板'))
}

function cancelOperation() {
  cancelled.value = true
  if (abortController) {
    abortController.abort()
    abortController = null
  }
}

async function loadSyncSQL() {
  loadingSQL.value = true
  try {
    const formData = new FormData()
    formData.append('sourceConnId', sourceConnId.value)
    formData.append('sourceSchema', sourceSchema.value)
    formData.append('targetConnId', targetConnId.value)
    formData.append('targetSchema', targetSchema.value)
    formData.append('table', syncTable.value)
    formData.append('direction', syncDirection.value)
    const res = await http.post('/sync/generateSyncSQL', formData)
    const result = res.data.data || res.data
    syncSQL.value = result.sql || '-- 无需同步'
  } catch (e) {
    ElMessage.error('生成SQL失败')
  } finally {
    loadingSQL.value = false
  }
}

async function startCompare() {
  if (!canCompare.value) {
    ElMessage.warning('请选择源和目标数据库')
    return
  }
  comparing.value = true
  cancelled.value = false
  activeStep.value = 1
  compareProgress.value = 10
  compareStatus.value = '正在比较...'
  chunkInfo.accumulatedStats = { add: 0, modify: 0, delete: 0 }
  accumulatedSQL.value = ''
  sqlStatementCount.value = 0

  try {
    if (syncMode.value === 'structure') {
      await startStructureCompare()
    } else {
      await startDataCompareChunked()
    }
  } catch (e) {
    if (cancelled.value) {
      ElMessage.info('比较已取消')
    } else {
      ElMessage.error('比较失败: ' + (e.message || '未知错误'))
    }
    if (chunkInfo.accumulatedStats.add || chunkInfo.accumulatedStats.modify || chunkInfo.accumulatedStats.delete) {
      activeStep.value = 2
    } else {
      activeStep.value = 0
    }
  } finally {
    comparing.value = false
  }
}

async function startStructureCompare() {
  compareStatus.value = '正在比较表结构...'
  compareProgress.value = 50
  const formData = new FormData()
  formData.append('sourceConnId', sourceConnId.value)
  formData.append('targetConnId', targetConnId.value)
  formData.append('sourceSchema', sourceSchema.value)
  formData.append('targetSchema', targetSchema.value)
  if (tableFilter.value.length) formData.append('tables', tableFilter.value.join(','))
  const res = await http.post('/sync/compareSchema', formData)
  const result = res.data.data || res.data
  if (result.error) {
    ElMessage.error(result.error)
    activeStep.value = 0
    return
  }
  schemaDiffs.value = result.diffs || []
  compareProgress.value = 100
  compareStatus.value = '比较完成'
  if (schemaDiffs.value.length) selectedSchemaDiff.value = schemaDiffs.value[0]
  setTimeout(() => activeStep.value = 2, 500)
}

async function startDataCompareChunked() {
  abortController = new AbortController()
  compareStatus.value = `正在比较表 ${syncTable.value} 的数据...`

  const allAddedRows = []
  const allDeletedRows = []
  const allModifiedRows = []
  let totalSource = 0
  let totalTarget = 0
  let columns = []
  let chunkIndex = 0
  let hasMore = true

  while (hasMore && !cancelled.value) {
    chunkInfo.currentChunk = chunkIndex + 1
    compareStatus.value = `正在比较表 ${syncTable.value} 的数据 (分块 ${chunkIndex + 1})...`

    const formData = new FormData()
    formData.append('sourceConnId', sourceConnId.value)
    formData.append('targetConnId', targetConnId.value)
    formData.append('sourceSchema', sourceSchema.value)
    formData.append('targetSchema', targetSchema.value)
    formData.append('table', syncTable.value)
    formData.append('chunkSize', String(chunkSize.value))
    formData.append('chunkIndex', String(chunkIndex))
    formData.append('direction', syncDirection.value)
    formData.append('generateSQL', 'true')

    const res = await http.post('/sync/compareDataChunked', formData, { signal: abortController.signal })
    const result = res.data.data || res.data

    if (result.error) {
      ElMessage.error(result.error)
      if (allAddedRows.length || allDeletedRows.length || allModifiedRows.length) break
      activeStep.value = 0
      return
    }

    totalSource = result.totalSource || 0
    totalTarget = result.totalTarget || 0
    columns = result.columns || columns
    chunkInfo.totalChunks = result.totalChunks || 1

    if (result.addedRows) allAddedRows.push(...result.addedRows)
    if (result.deletedRows) allDeletedRows.push(...result.deletedRows)
    if (result.modifiedRows) allModifiedRows.push(...result.modifiedRows)

    chunkInfo.accumulatedStats.add += result.addCount || 0
    chunkInfo.accumulatedStats.modify += result.modifyCount || 0
    chunkInfo.accumulatedStats.delete += result.deleteCount || 0

    if (result.sql) {
      accumulatedSQL.value += result.sql
      sqlStatementCount.value += (result.sql.match(/;\n/g) || []).length
    }

    hasMore = result.hasMore || false
    chunkIndex++

    compareProgress.value = Math.min(90, Math.round((chunkIndex / (chunkInfo.totalChunks || 1)) * 90))
  }

  const previewLimit = 100
  dataDiff.value = {
    tableName: syncTable.value,
    totalSource,
    totalTarget,
    addedRows: allAddedRows.slice(0, previewLimit),
    deletedRows: allDeletedRows.slice(0, previewLimit),
    modifiedRows: allModifiedRows.slice(0, previewLimit),
    addCount: chunkInfo.accumulatedStats.add,
    deleteCount: chunkInfo.accumulatedStats.delete,
    modifyCount: chunkInfo.accumulatedStats.modify,
    columns
  }

  compareProgress.value = 100
  compareStatus.value = '比较完成'
  setTimeout(() => activeStep.value = 2, 500)
}

async function executeSync() {
  try {
    let sqlCount = 0
    let actionDesc = ''
    if (syncMode.value === 'structure') {
      const sqlToApply = schemaDiffs.value.reduce((acc, d) => {
        if (d.columnDiffs) for (const cd of d.columnDiffs) acc += cd.alterStatement + '\n'
        if (d.indexDiffs) for (const id of d.indexDiffs) acc += id.alterStatement + '\n'
        return acc
      }, '')
      if (!sqlToApply.trim()) { ElMessage.info('没有需要执行的SQL'); return }
      sqlCount = sqlToApply.split(';').filter(s => s.trim()).length
      actionDesc = `即将对目标库执行 ${sqlCount} 条结构变更语句（ALTER/CREATE INDEX/DROP INDEX），是否确认？`
    } else {
      const sqlToExecute = syncSQL.value || accumulatedSQL.value
      if (!sqlToExecute || sqlToExecute === '-- 无需同步') { ElMessage.info('没有需要同步的数据'); return }
      sqlCount = (sqlToExecute.match(/;\n/g) || []).length
      actionDesc = `即将对目标库执行 ${sqlCount} 条数据同步语句（INSERT/UPDATE/DELETE），将分批执行，是否确认？`
    }

    await ElMessageBox.confirm(actionDesc, '同步确认', {
      confirmButtonText: '确认执行',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch { return }

  syncing.value = true
  cancelled.value = false
  activeStep.value = 3
  syncProgress.value = 0
  syncBatchInfo.current = 0
  syncBatchInfo.total = 0
  syncBatchInfo.insertCount = 0
  syncBatchInfo.updateCount = 0
  syncBatchInfo.deleteCount = 0

  try {
    if (syncMode.value === 'structure') {
      await executeStructureSync()
    } else {
      await executeDataSyncBatched()
    }
  } catch (e) {
    if (cancelled.value) {
      syncSuccess.value = false
      syncResult.value = '同步已取消'
    } else {
      syncSuccess.value = false
      syncResult.value = '同步失败: ' + (e.message || '未知错误')
    }
  } finally {
    syncing.value = false
    abortController = null
  }
}

async function executeStructureSync() {
  const sqlToApply = schemaDiffs.value.reduce((acc, d) => {
    if (d.columnDiffs) for (const cd of d.columnDiffs) acc += cd.alterStatement + '\n'
    if (d.indexDiffs) for (const id of d.indexDiffs) acc += id.alterStatement + '\n'
    return acc
  }, '')
  if (!sqlToApply.trim()) { ElMessage.info('没有需要执行的SQL'); return }

  syncStatusText.value = '正在执行结构同步...'
  syncProgress.value = 50

  const formData = new FormData()
  formData.append('connId', targetConnId.value)
  formData.append('schema', targetSchema.value)
  formData.append('sql', sqlToApply)
  const res = await http.post('/sync/applySchemaDiff', formData)
  const result = res.data.data || res.data
  syncSuccess.value = result.success
  syncResult.value = `成功执行 ${result.executedCount || 0} 条语句` + (result.errors && result.errors.length ? `，${result.errors.length} 条失败` : '')
  syncProgress.value = 100
}

async function executeDataSyncBatched() {
  const sqlToExecute = syncSQL.value || accumulatedSQL.value
  if (!sqlToExecute || sqlToExecute === '-- 无需同步') { ElMessage.info('没有需要同步的数据'); return }

  const statements = sqlToExecute.split(';\n').filter(s => s.trim())
  if (statements.length === 0) { ElMessage.info('没有需要同步的数据'); return }

  const batchSize = 200
  const batches = []
  for (let i = 0; i < statements.length; i += batchSize) {
    batches.push(statements.slice(i, i + batchSize).join(';\n') + ';\n')
  }

  syncBatchInfo.total = batches.length
  abortController = new AbortController()

  const targetConn = syncDirection.value === 'source_to_target' ? targetConnId.value : sourceConnId.value
  const targetSch = syncDirection.value === 'source_to_target' ? targetSchema.value : sourceSchema.value

  let totalInsert = 0
  let totalUpdate = 0
  let totalDelete = 0
  let totalErrors = 0
  const allErrors = []

  for (let i = 0; i < batches.length; i++) {
    if (cancelled.value) break

    syncBatchInfo.current = i + 1
    syncStatusText.value = `正在执行数据同步 (批次 ${i + 1}/${batches.length})...`
    syncProgress.value = Math.round(((i) / batches.length) * 100)

    const formData = new FormData()
    formData.append('connId', targetConn)
    formData.append('schema', targetSch)
    formData.append('sql', batches[i])

    try {
      const res = await http.post('/sync/applyDataSync', formData, { signal: abortController.signal })
      const result = res.data.data || res.data
      totalInsert += result.insertCount || 0
      totalUpdate += result.updateCount || 0
      totalDelete += result.deleteCount || 0
      if (result.errors && result.errors.length) {
        totalErrors += result.errors.length
        allErrors.push(...result.errors)
      }
    } catch (e) {
      if (cancelled.value) break
      totalErrors++
      allErrors.push(`批次 ${i + 1} 执行失败: ${e.message || '未知错误'}`)
    }

    syncBatchInfo.insertCount = totalInsert
    syncBatchInfo.updateCount = totalUpdate
    syncBatchInfo.deleteCount = totalDelete
  }

  syncProgress.value = 100
  syncSuccess.value = totalErrors === 0
  syncResult.value = `插入${totalInsert}行, 更新${totalUpdate}行, 删除${totalDelete}行`
    + (totalErrors > 0 ? `，${totalErrors} 条失败` : '')
    + (cancelled.value ? '（已取消）' : '')
}

function resetDialog() {
  activeStep.value = 0
  schemaDiffs.value = []
  dataDiff.value = {}
  syncSQL.value = ''
  accumulatedSQL.value = ''
  sqlStatementCount.value = 0
  selectedSchemaDiff.value = null
  chunkInfo.totalChunks = 0
  chunkInfo.currentChunk = 0
  chunkInfo.accumulatedStats = { add: 0, modify: 0, delete: 0 }
  syncProgress.value = 0
  syncBatchInfo.current = 0
  syncBatchInfo.total = 0
  syncBatchInfo.insertCount = 0
  syncBatchInfo.updateCount = 0
  syncBatchInfo.deleteCount = 0
}
</script>
