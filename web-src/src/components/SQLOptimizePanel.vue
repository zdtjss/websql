<template>
  <el-drawer v-model="drawerVisible" title="SQL优化分析" direction="btt" size="45%" :before-close="handleClose">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px">
      <div>
        <el-button type="primary" size="small" @click="runExplain" :loading="explaining">执行计划</el-button>
        <el-button type="success" size="small" @click="runOptimize" :loading="optimizing" style="margin-left:8px">AI优化建议</el-button>
      </div>
      <div v-if="optimizeResult">
        <el-tag :type="scoreType" size="large" effect="dark">评分: {{optimizeResult.score}}/100</el-tag>
      </div>
    </div>

    <div v-if="optimizeResult && optimizeResult.suggestions && optimizeResult.suggestions.length" style="margin-bottom:12px">
      <h4 style="margin-bottom:8px;color:#303133">优化建议</h4>
      <el-alert v-for="(sug, idx) in optimizeResult.suggestions" :key="idx" :title="sug.title" :type="sug.severity === 'critical' ? 'error' : sug.severity === 'warning' ? 'warning' : 'info'" :description="sug.description" :closable="false" show-icon style="margin-bottom:8px">
        <template v-if="sug.fixSql" #default>
          <div style="display:flex;align-items:center;gap:8px;margin-top:5px">
            <pre style="background:#1e1e1e;color:#d4d4d4;padding:8px;border-radius:4px;font-size:12px;flex:1;margin:0;max-height:120px;overflow:auto"><code>{{sug.fixSql}}</code></pre>
            <el-button size="small" type="primary" link @click="copyText(sug.fixSql)">复制</el-button>
          </div>
        </template>
      </el-alert>
    </div>

    <div v-if="explainResult" style="margin-bottom:12px">
      <el-collapse v-model="activeCollapse">
        <el-collapse-item title="EXPLAIN 执行计划" name="explain">
          <div v-if="explainResult.raw" style="background:#f5f7fa;padding:10px;border-radius:4px;font-family:monospace;font-size:12px;white-space:pre-wrap;margin-bottom:8px">{{explainResult.raw}}</div>
          <el-table v-if="explainResult.rows && explainResult.rows.length" :data="explainResult.rows" stripe size="small" max-height="250" border>
            <el-table-column v-for="col in explainResult.columns" :key="col.name" :prop="col.name" :label="col.name" width="130" :show-overflow-tooltip="true" />
          </el-table>
        </el-collapse-item>
      </el-collapse>
    </div>

    <el-empty v-if="!explaining && !optimizing && !explainResult && !optimizeResult" description="点击上方按钮开始分析SQL" :image-size="60" />
  </el-drawer>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import http from '@/js/utils/httpProxy.js'

const props = defineProps({
  visible: Boolean,
  connId: String,
  schema: String,
  sql: String,
  dbType: String
})
const emit = defineEmits(['update:visible'])

const drawerVisible = computed({ get: () => props.visible, set: v => emit('update:visible', v) })

const explaining = ref(false)
const optimizing = ref(false)
const explainResult = ref(null)
const optimizeResult = ref(null)
const activeCollapse = ref([])

const scoreType = computed(() => {
  if (!optimizeResult.value) return ''
  if (optimizeResult.value.score >= 80) return 'success'
  if (optimizeResult.value.score >= 50) return 'warning'
  return 'danger'
})

function handleClose() {
  drawerVisible.value = false
}

function copyText(text) {
  navigator.clipboard.writeText(text).then(() => ElMessage.success('已复制到剪贴板'))
}

async function runExplain() {
  if (!props.sql?.trim()) { ElMessage.warning('SQL不能为空'); return }
  explaining.value = true
  try {
    const formData = new FormData()
    formData.append('connId', props.connId)
    formData.append('schema', props.schema)
    formData.append('sql', props.sql)
    const res = await http.post('/sqlopt/explain', formData)
    explainResult.value = res.data
    activeCollapse.value = ['explain']
  } catch (e) {
    ElMessage.error('执行计划分析失败: ' + (e.message || '未知错误'))
  } finally {
    explaining.value = false
  }
}

async function runOptimize() {
  if (!props.sql?.trim()) { ElMessage.warning('SQL不能为空'); return }
  optimizing.value = true
  try {
    const formData = new FormData()
    formData.append('connId', props.connId)
    formData.append('schema', props.schema)
    formData.append('sql', props.sql)
    formData.append('useExplain', 'true')
    const res = await http.post('/sqlopt/optimize', formData)
    optimizeResult.value = res.data
    if (res.data.explainPlan) {
      explainResult.value = res.data.explainPlan
      activeCollapse.value = ['explain']
    }
    if (res.data.suggestions && res.data.suggestions.length) {
      ElMessage.success(`发现 ${res.data.suggestions.length} 条优化建议`)
    } else {
      ElMessage.success('SQL看起来不错，没有发现需要优化的地方')
    }
  } catch (e) {
    ElMessage.error('优化分析失败: ' + (e.message || '未知错误'))
  } finally {
    optimizing.value = false
  }
}

watch(() => props.sql, () => {
  explainResult.value = null
  optimizeResult.value = null
})
</script>
