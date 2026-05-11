<template>
  <el-dialog
    v-model="visible"
    title="表结构对比"
    width="960px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
    @opened="initTables"
  >
    <div style="display:flex;gap:12px;margin-bottom:16px;align-items:center;">
      <span style="font-weight:500;">源表:</span>
      <el-select v-model="sourceTable" placeholder="选择源表" style="width:220px;" @change="onSourceChange">
        <el-option v-for="t in tableNames" :key="t.name" :label="t.name" :value="t.name">
          <span>{{ t.name }}</span>
          <span style="float:right;color:var(--text-tertiary);font-size:12px;">{{ t.type === 'VIEW' ? '视图' : '表' }}</span>
        </el-option>
      </el-select>
      <span style="font-weight:500;">目标表:</span>
      <el-select v-model="targetTable" placeholder="选择目标表" style="width:220px;" @change="onTargetChange">
        <el-option v-for="t in tableNames" :key="t.name" :label="t.name" :value="t.name">
          <span>{{ t.name }}</span>
          <span style="float:right;color:var(--text-tertiary);font-size:12px;">{{ t.type === 'VIEW' ? '视图' : '表' }}</span>
        </el-option>
      </el-select>
      <el-button type="primary" @click="compareTables" :loading="comparing" :disabled="!sourceTable || !targetTable">
        开始对比
      </el-button>
    </div>

    <div v-if="compared" v-loading="comparing">
      <el-descriptions :column="2" border style="margin-bottom:16px;">
        <el-descriptions-item label="源表">{{ sourceTable }} ({{ sourceCols.length }} 列)</el-descriptions-item>
        <el-descriptions-item label="目标表">{{ targetTable }} ({{ targetCols.length }} 列)</el-descriptions-item>
      </el-descriptions>

      <div v-if="diffCols.length > 0">
        <h4 style="margin:12px 0 8px;color:var(--text-secondary);">差异列 ({{ diffCols.length }})</h4>
        <el-table :data="diffCols" size="small" max-height="300" stripe>
          <el-table-column label="状态" width="90" resizable>
            <template #default="scope">
              <el-tag size="small" :type="scope.row.status === '新增' ? 'success' : scope.row.status === '删除' ? 'danger' : 'warning'">
                {{ scope.row.status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="column" label="列名" width="200" resizable />
          <el-table-column prop="sourceDef" label="源表定义" min-width="200" show-overflow-tooltip resizable />
          <el-table-column prop="targetDef" label="目标表定义" min-width="200" show-overflow-tooltip resizable />
        </el-table>
      </div>
      <el-empty v-if="diffCols.length === 0" description="两个表结构完全一致" />

      <div v-if="diffCols.length > 0" style="margin-top:16px;">
        <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px;">
          <h4 style="margin:0;color:var(--text-secondary);">同步 SQL (将目标表同步为源表结构)</h4>
          <el-button size="small" @click="copySyncSql">复制 SQL</el-button>
        </div>
        <pre style="background:var(--bg-secondary);padding:12px;border-radius:6px;max-height:200px;overflow-y:auto;font-size:13px;"><code>{{ syncSql }}</code></pre>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ElMessage } from 'element-plus'
import { computed, ref } from 'vue'
import http from '../js/utils/httpProxy.js'
import { dbSchemaProxy } from '../stores/sql'

const props = defineProps({
  modelValue: Boolean,
  connId: String,
  schema: String,
})

const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const tableNames = ref([])
const sourceTable = ref('')
const targetTable = ref('')
const sourceCols = ref([])
const targetCols = ref([])
const diffCols = ref([])
const syncSql = ref('')
const comparing = ref(false)
const compared = ref(false)

async function execQuery(sql) {
  const params = new URLSearchParams()
  params.append('connId', props.connId)
  params.append('schema', props.schema)
  params.append('sql', sql)
  params.append('maxLine', '500')
  const resp = await http.post('/execSQL', params)
  return resp.data.data?.data || []
}

async function initTables() {
  compared.value = false
  const dbType = (dbSchemaProxy.getDbType(props.schema) || '').toLowerCase()
  if (dbType === 'mysql') {
    const rows = await execQuery(`SELECT TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = '${props.schema}' AND TABLE_TYPE IN ('BASE TABLE', 'VIEW') ORDER BY TABLE_NAME`)
    tableNames.value = rows.map(r => ({ name: r.TABLE_NAME || r.table_name, type: r.TABLE_TYPE || r.table_type }))
  } else if (dbType === 'sqlite') {
    const rows = await execQuery("SELECT name AS TABLE_NAME, type AS TABLE_TYPE FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY type, name")
    tableNames.value = rows.map(r => ({ name: r.TABLE_NAME || r.name, type: (r.TABLE_TYPE || r.type) === 'view' ? 'VIEW' : 'TABLE' }))
  } else {
    tableNames.value = []
  }
}

async function loadColumns(tableName) {
  const dbType = (dbSchemaProxy.getDbType(props.schema) || '').toLowerCase()
  const schema = props.schema
  if (dbType === 'mysql') {
    const rows = await execQuery(`SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT, ORDINAL_POSITION FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '${schema}' AND TABLE_NAME = '${tableName}' ORDER BY ORDINAL_POSITION`)
    return rows.map(r => ({
      name: r.COLUMN_NAME || r.column_name,
      type: r.COLUMN_TYPE || r.column_type,
      nullable: (r.IS_NULLABLE || r.is_nullable) === 'YES',
      defaultVal: r.COLUMN_DEFAULT ?? r.column_default,
      extra: r.EXTRA || r.extra || '',
      comment: r.COLUMN_COMMENT || r.column_comment || '',
    }))
  } else if (dbType === 'sqlite') {
    const rows = await execQuery(`PRAGMA table_info('${tableName}')`)
    if (!Array.isArray(rows)) return []
    return rows.map(r => ({
      name: r.name,
      type: r.type || '',
      nullable: r.notnull === 0,
      defaultVal: r.dflt_value,
      extra: r.pk ? 'PRIMARY KEY' : '',
      comment: '',
    }))
  }
  return []
}

function colSignature(col) {
  const parts = [col.name, col.type]
  if (!col.nullable) parts.push('NOT NULL')
  if (col.defaultVal !== null && col.defaultVal !== undefined) parts.push('DEFAULT ' + col.defaultVal)
  if (col.extra && col.extra !== '') parts.push(col.extra)
  return parts.join(' ')
}

async function onSourceChange() {
  if (sourceTable.value) {
    sourceCols.value = await loadColumns(sourceTable.value)
  }
}

async function onTargetChange() {
  if (targetTable.value) {
    targetCols.value = await loadColumns(targetTable.value)
  }
}

async function compareTables() {
  comparing.value = true
  compared.value = false
  try {
    if (sourceCols.value.length === 0) sourceCols.value = await loadColumns(sourceTable.value)
    if (targetCols.value.length === 0) targetCols.value = await loadColumns(targetTable.value)

    const srcMap = {}
    const tgtMap = {}
    sourceCols.value.forEach(c => { srcMap[c.name.toLowerCase()] = c })
    targetCols.value.forEach(c => { tgtMap[c.name.toLowerCase()] = c })

    const diffs = []
    const alterParts = []

    for (const col of sourceCols.value) {
      const tgtCol = tgtMap[col.name.toLowerCase()]
      if (!tgtCol) {
        diffs.push({ status: '新增', column: col.name, sourceDef: colSignature(col), targetDef: '-' })
        alterParts.push(`ADD COLUMN \`${col.name}\` ${col.type}${col.nullable ? '' : ' NOT NULL'}${col.defaultVal != null ? ' DEFAULT ' + col.defaultVal : ''}${col.extra ? ' ' + col.extra : ''}`)
      } else {
        const srcSig = colSignature(col)
        const tgtSig = colSignature(tgtCol)
        if (srcSig !== tgtSig) {
          diffs.push({ status: '修改', column: col.name, sourceDef: srcSig, targetDef: tgtSig })
          alterParts.push(`MODIFY COLUMN \`${col.name}\` ${col.type}${col.nullable ? '' : ' NOT NULL'}${col.defaultVal != null ? ' DEFAULT ' + col.defaultVal : ''}${col.extra ? ' ' + col.extra : ''}`)
        }
      }
    }

    for (const col of targetCols.value) {
      if (!srcMap[col.name.toLowerCase()]) {
        diffs.push({ status: '删除', column: col.name, sourceDef: '-', targetDef: colSignature(col) })
        alterParts.push(`DROP COLUMN \`${col.name}\``)
      }
    }

    diffCols.value = diffs
    syncSql.value = alterParts.length > 0 ? `ALTER TABLE \`${targetTable.value}\`\n  ${alterParts.join(',\n  ')};` : ''
    compared.value = true
  } catch (err) {
    console.error('对比失败:', err)
  } finally {
    comparing.value = false
  }
}

function copySyncSql() {
  navigator.clipboard.writeText(syncSql.value).then(() => {
    ElMessage({ message: '已复制到剪贴板', type: 'success' })
  })
}
</script>