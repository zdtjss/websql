<template>
  <el-dialog
    v-model="visible"
    title="可视化查询构建器"
    width="960px"
    :draggable="true"
    destroy-on-close
    class="classical-dialog"
  >
    <div style="display:flex;gap:16px;">
      <div style="flex:1;min-width:0;">
        <el-form label-width="60px" size="small">
          <el-form-item label="表">
            <el-select v-model="builder.table" placeholder="选择表" style="width:100%;" @change="onTableChange">
              <el-option v-for="t in tableList" :key="t.name" :label="t.name + (t.comment ? ' - ' + t.comment : '')" :value="t.name" />
            </el-select>
          </el-form-item>

          <el-form-item label="字段">
            <div style="display:flex;flex-wrap:wrap;gap:4px;max-height:120px;overflow-y:auto;" v-loading="loadingFields" element-loading-text="加载字段中...">
              <template v-for="col in columnList" :key="col.name">
                <el-tooltip :content="col.comment || col.type || col.name" placement="top" :show-after="300">
                  <el-checkbox :model-value="isColumnSelected(col.name)"
                    @change="toggleColumn(col.name)" size="small" :label="col.name">
                    {{ col.name }}
                  </el-checkbox>
                </el-tooltip>
              </template>
            </div>
          </el-form-item>

          <el-form-item label="别名">
            <div v-for="(sel, idx) in selectedColumns" :key="idx" style="display:flex;gap:4px;margin-bottom:4px;">
              <span style="min-width:120px;">{{ sel.column }}</span>
              <el-input v-model="sel.alias" placeholder="别名" style="width:120px;" />
            </div>
          </el-form-item>

          <el-form-item label="条件">
            <div v-for="(cond, idx) in builder.conditions" :key="idx" style="display:flex;gap:4px;margin-bottom:4px;align-items:center;">
              <el-select v-model="cond.field" placeholder="字段" style="width:130px;">
                <el-option v-for="col in columnList" :key="col.name" :label="col.name" :value="col.name" />
              </el-select>
              <el-select v-model="cond.operator" style="width:100px;">
                <el-option v-for="op in operators" :key="op.value" :label="op.label" :value="op.value" />
              </el-select>
              <el-input v-model="cond.value" placeholder="值" style="flex:1;" />
              <el-button text size="small" type="danger" @click="builder.conditions.splice(idx, 1)">×</el-button>
            </div>
            <el-button size="small" @click="addCondition">+ 添加条件</el-button>
          </el-form-item>

          <el-form-item label="排序">
            <div v-for="(ord, idx) in builder.orders" :key="idx" style="display:flex;gap:4px;margin-bottom:4px;align-items:center;">
              <el-select v-model="ord.field" placeholder="字段" style="width:150px;">
                <el-option v-for="col in columnList" :key="col.name" :label="col.name" :value="col.name" />
              </el-select>
              <el-select v-model="ord.direction" style="width:80px;">
                <el-option label="升序" value="ASC" />
                <el-option label="降序" value="DESC" />
              </el-select>
              <el-button text size="small" type="danger" @click="builder.orders.splice(idx, 1)">×</el-button>
            </div>
            <el-button size="small" @click="addOrder">+ 添加排序</el-button>
          </el-form-item>

          <el-form-item label="限制">
            <el-input-number v-model="builder.limit" :min="0" :max="10000" placeholder="无限制" />
            <span style="margin-left:8px;color:var(--text-tertiary);font-size:12px;">0 表示不限制</span>
          </el-form-item>

          <el-form-item label="聚合">
            <el-checkbox v-model="builder.distinct" size="small" style="margin-right:8px;">DISTINCT</el-checkbox>
            <el-select v-model="builder.groupBy" placeholder="GROUP BY" clearable style="width:150px;" multiple>
              <el-option v-for="col in columnList" :key="col.name" :label="col.name" :value="col.name" />
            </el-select>
          </el-form-item>
        </el-form>
      </div>

      <div style="width:360px;display:flex;flex-direction:column;">
        <div style="font-weight:600;margin-bottom:8px;color:var(--text-secondary);">生成的 SQL</div>
        <pre style="flex:1;background:var(--bg-secondary);padding:12px;border-radius:6px;overflow:auto;font-size:13px;white-space:pre-wrap;"><code>{{ generatedSql || '请先选择表和字段' }}</code></pre>
        <div style="display:flex;gap:8px;margin-top:8px;">
          <el-button type="primary" size="small" @click="executeSql" :disabled="!generatedSql">执行</el-button>
          <el-button size="small" @click="copySql" :disabled="!generatedSql">复制</el-button>
          <el-button size="small" @click="insertSql" :disabled="!generatedSql">插入编辑器</el-button>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../js/utils/httpProxy.js'
import { dbSchemaProxy } from '../stores/sql'

const props = defineProps({
  modelValue: Boolean,
  connId: String,
  schema: String,
})

const emit = defineEmits(['update:modelValue', 'execute', 'insert'])

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const tableList = ref([])
const columnList = ref([])
const loadingFields = ref(false)

const builder = reactive({
  table: '',
  conditions: [],
  orders: [],
  limit: 0,
  distinct: false,
  groupBy: [],
})

const selectedColumns = reactive([])

const operators = [
  { label: '等于 (=)', value: '=' },
  { label: '不等于 (!=)', value: '!=' },
  { label: '大于 (>)', value: '>' },
  { label: '小于 (<)', value: '<' },
  { label: '大于等于 (>=)', value: '>=' },
  { label: '小于等于 (<=)', value: '<=' },
  { label: '包含 (LIKE)', value: 'LIKE' },
  { label: '不包含 (NOT LIKE)', value: 'NOT LIKE' },
  { label: '为空', value: 'IS NULL' },
  { label: '不为空', value: 'IS NOT NULL' },
  { label: '范围 (IN)', value: 'IN' },
]

function initTableList() {
  try {
    tableList.value = dbSchemaProxy.getTable(props.schema).map(t => ({
      name: t.label,
      comment: '',
    }))
  } catch {
    tableList.value = []
  }
}

async function onTableChange(tableName) {
  builder.conditions = []
  builder.orders = []
  builder.groupBy = []
  selectedColumns.splice(0, selectedColumns.length)
  loadingFields.value = true
  columnList.value = []

  const dbType = (dbSchemaProxy.getDbType(props.schema) || '').toLowerCase()
  if (dbType === 'mysql') {
    const params = new URLSearchParams()
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('sql', `SELECT COLUMN_NAME, DATA_TYPE, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '${props.schema}' AND TABLE_NAME = '${tableName}' ORDER BY ORDINAL_POSITION`)
    params.append('maxLine', '200')
    try {
      const resp = await http.post('/execSQL', params)
      columnList.value = (resp.data.data?.data || []).map(r => ({
        name: r.COLUMN_NAME || r.column_name,
        type: r.DATA_TYPE || r.data_type,
        comment: r.COLUMN_COMMENT || r.column_comment || '',
      }))
    } catch {
      columnList.value = []
    }
  } else if (dbType === 'sqlite') {
    const params = new URLSearchParams()
    params.append('connId', props.connId)
    params.append('schema', props.schema)
    params.append('sql', `PRAGMA table_info('${tableName}')`)
    params.append('maxLine', '200')
    try {
      const resp = await http.post('/execSQL', params)
      const data = resp.data.data?.data || []
      columnList.value = data.map(r => ({ name: r.name, type: r.type || '', comment: '' }))
    } catch {
      columnList.value = []
    }
  }
  loadingFields.value = false
}

function isColumnSelected(colName) {
  return selectedColumns.some(c => c.column === colName)
}

function toggleColumn(colName) {
  const idx = selectedColumns.findIndex(c => c.column === colName)
  if (idx >= 0) {
    selectedColumns.splice(idx, 1)
  } else {
    selectedColumns.push({ column: colName, alias: '' })
  }
}

function addCondition() {
  builder.conditions.push({ field: '', operator: '=', value: '' })
}

function addOrder() {
  builder.orders.push({ field: '', direction: 'ASC' })
}

const generatedSql = computed(() => {
  if (!builder.table || selectedColumns.length === 0) return ''

  const parts = ['SELECT']

  if (builder.distinct) {
    parts[0] += ' DISTINCT'
  }

  const cols = selectedColumns.map(c => {
    let colExpr = '`' + c.column + '`'
    if (c.alias) colExpr += ' AS `' + c.alias + '`'
    return colExpr
  })
  parts.push('\n  ' + cols.join(',\n  '))

  parts.push('\nFROM `' + builder.table + '`')

  if (builder.conditions.length > 0) {
    const whereParts = builder.conditions
      .filter(c => c.field)
      .map(c => {
        const fieldRef = '`' + c.field + '`'
        if (c.operator === 'IS NULL') return fieldRef + ' IS NULL'
        if (c.operator === 'IS NOT NULL') return fieldRef + ' IS NOT NULL'
        if (c.operator === 'IN') return fieldRef + ' IN (' + c.value + ')'
        const val = isNaN(c.value) && c.operator !== 'LIKE' && c.operator !== 'NOT LIKE' ? "'" + c.value.replace(/'/g, "\\'") + "'" : c.value
        return fieldRef + ' ' + c.operator + ' ' + val
      })
    if (whereParts.length > 0) {
      parts.push('WHERE ' + whereParts.join('\n  AND '))
    }
  }

  if (builder.groupBy.length > 0) {
    parts.push('GROUP BY ' + builder.groupBy.map(g => '`' + g + '`').join(', '))
  }

  if (builder.orders.length > 0) {
    const orderParts = builder.orders
      .filter(o => o.field)
      .map(o => '`' + o.field + '` ' + o.direction)
    if (orderParts.length > 0) {
      parts.push('ORDER BY ' + orderParts.join(', '))
    }
  }

  if (builder.limit > 0) {
    parts.push('LIMIT ' + builder.limit)
  }

  return parts.join('\n') + ';'
})

function executeSql() {
  if (generatedSql.value) {
    emit('execute', generatedSql.value)
    visible.value = false
  }
}

function insertSql() {
  if (generatedSql.value) {
    emit('insert', generatedSql.value)
    visible.value = false
  }
}

function copySql() {
  navigator.clipboard.writeText(generatedSql.value).then(() => {
    ElMessage({ message: 'SQL 已复制到剪贴板', type: 'success' })
  })
}

onMounted(() => {
  initTableList()
})
</script>