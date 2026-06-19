<template>
  <el-dialog
    v-model="visible"
    :title="'数据库对象 - ' + schema"
    width="1060px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
  >
    <el-tabs v-model="activeTab" type="card" @tab-change="onTabChange">
      <el-tab-pane label="存储过程" name="procedures">
        <el-table :data="procedureList" style="width: 100%" v-loading="loading" max-height="480">
          <el-table-column prop="name" label="名称" width="240" show-overflow-tooltip resizable />
          <el-table-column prop="type" label="类型" width="100" resizable>
            <template #default="scope">
              <el-tag size="small" :type="scope.row.type === 'PROCEDURE' ? 'primary' : 'success'">
                {{ scope.row.type === 'PROCEDURE' ? '过程' : '函数' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="returnType" label="返回值" width="120" show-overflow-tooltip resizable />
          <el-table-column prop="comment" label="注释" min-width="160" show-overflow-tooltip resizable />
          <el-table-column label="操作" width="100" resizable>
            <template #default="scope">
              <el-button size="small" link type="primary" @click="viewObjectDetail(scope.row)">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!loading && procedureList.length === 0" description="没有存储过程或函数" />
      </el-tab-pane>

      <el-tab-pane label="触发器" name="triggers">
        <el-table :data="triggerList" style="width: 100%" v-loading="loading" max-height="480">
          <el-table-column prop="name" label="触发器名" width="220" show-overflow-tooltip resizable />
          <el-table-column prop="tableName" label="所在表" width="180" show-overflow-tooltip resizable />
          <el-table-column prop="timing" label="时机" width="80" resizable>
            <template #default="scope">
              <el-tag size="small" :type="scope.row.timing === 'BEFORE' ? 'warning' : 'danger'">{{ scope.row.timing }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="event" label="事件" width="80" resizable>
            <template #default="scope">
              <el-tag size="small">{{ scope.row.event }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100" resizable>
            <template #default="scope">
              <el-button size="small" link type="primary" @click="viewObjectDetail(scope.row)">查看</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!loading && triggerList.length === 0" description="没有触发器" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="detailVisible"
    :title="detailTitle"
    width="800px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
  >
    <el-icon style="position:absolute;right:18px;cursor:pointer;z-index:9999;" size="16" @click="copyDetail">
      <CopyDocument />
    </el-icon>
    <div style="max-height: 500px;overflow-y:auto;">
      <pre><code class="language-sql" v-html="highlightedCode"></code></pre>
    </div>
    <template #footer>
      <el-button @click="detailVisible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { CopyDocument } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { computed, ref, watch } from 'vue'
import { getHljs, highlightSql } from '@/utils/lazyDeps'
import { execSQL } from '@/api/sql'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()

const visible = defineModel({ default: false })
const { connId, schema } = defineProps({
  connId: String,
  schema: String,
})

const activeTab = ref('procedures')
const loading = ref(false)
const procedureList = ref([])
const triggerList = ref([])

const detailVisible = ref(false)
const detailTitle = ref('')
const detailCode = ref('')
const highlightedCode = ref('')

watch(detailCode, async (val) => {
  if (!val) { highlightedCode.value = ''; return }
  highlightedCode.value = await highlightSql(val)
}, { immediate: true })

function onTabChange(name) {
  if (name === 'procedures') loadProcedures()
  else if (name === 'triggers') loadTriggers()
}

function loadProcedures() {
  if (procedureList.value.length > 0) return
  const dbType = (dbSchemaProxy.getDbType(schema) || '').toLowerCase()
  if (dbType !== 'mysql') {
    procedureList.value = []
    return
  }
  loading.value = true
  const sql = `SELECT ROUTINE_NAME, ROUTINE_TYPE, DTD_IDENTIFIER, CREATED, ROUTINE_COMMENT, ROUTINE_DEFINITION FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = '${schema}' ORDER BY ROUTINE_TYPE, ROUTINE_NAME`
  execSQL({ connId, schema, sql, maxLine: '500' })
    .then(resp => {
      const data = resp.data.data?.data || []
      procedureList.value = data.map(r => ({
        name: r.ROUTINE_NAME || r.routine_name,
        type: r.ROUTINE_TYPE || r.routine_type,
        returnType: r.DTD_IDENTIFIER || r.dtd_identifier,
        comment: r.ROUTINE_COMMENT || r.routine_comment || '',
        definition: r.ROUTINE_DEFINITION || r.routine_definition || '',
      }))
    })
    .catch(() => { procedureList.value = [] })
    .finally(() => { loading.value = false })
}

function loadTriggers() {
  if (triggerList.value.length > 0) return
  const dbType = (dbSchemaProxy.getDbType(schema) || '').toLowerCase()
  if (dbType !== 'mysql') {
    triggerList.value = []
    return
  }
  loading.value = true
  const sql = `SELECT TRIGGER_NAME, EVENT_MANIPULATION, EVENT_OBJECT_TABLE, ACTION_TIMING, ACTION_STATEMENT, CREATED FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = '${schema}' ORDER BY EVENT_OBJECT_TABLE, ACTION_TIMING, EVENT_MANIPULATION`
  execSQL({ connId, schema, sql, maxLine: '500' })
    .then(resp => {
      const data = resp.data.data?.data || []
      triggerList.value = data.map(r => ({
        name: r.TRIGGER_NAME || r.trigger_name,
        event: r.EVENT_MANIPULATION || r.event_manipulation,
        tableName: r.EVENT_OBJECT_TABLE || r.event_object_table,
        timing: r.ACTION_TIMING || r.action_timing,
        definition: r.ACTION_STATEMENT || r.action_statement || '',
      }))
    })
    .catch(() => { triggerList.value = [] })
    .finally(() => { loading.value = false })
}

function viewObjectDetail(row) {
  detailTitle.value = row.name
  detailCode.value = row.definition || '-- 没有可用的定义'
  detailVisible.value = true
}

function copyDetail() {
  navigator.clipboard.writeText(detailCode.value).then(() => {
    ElMessage({ message: '已复制到剪贴板', type: 'success' })
  })
}
</script>