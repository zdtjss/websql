<template>
  <div class="sql-confirm-inline" v-if="visible">
    <!-- <el-alert :title="`操作类型：${getOperationDescription(operationType)}`"
      :type="riskLevel === 'high' ? 'error' : riskLevel === 'medium' ? 'warning' : 'info'" :closable="false" show-icon>
      <template #default>
        <div style="display: flex; flex-direction: column; gap: 0px;">
          <div style="display: flex; gap: 1px; flex-wrap: wrap;">
            <span v-if="tableName">
              表名：<el-tag type="info" size="small">{{ tableName }}</el-tag>
            </span>
            <span v-if="affectedRows !== undefined">
              预计影响：<strong>{{ affectedRows }}</strong> 行
            </span>
          </div>
        </div>
      </template>
    </el-alert> -->

    <!-- SQL 语句显示 -->
    <div style="margin-top: 12px;">
      <div style="font-weight: 600; margin-bottom: 8px; font-size: 14px;">SQL ：</div>
      <div style="
          background-color: #f5f7fa;
          padding: 12px;
          border-radius: 5px;
          font-family: 'Courier New', monospace;
          font-size: 13px;
          max-height: 200px;
          overflow: auto;
          border: 1px solid #d9d9d9;
        ">
        <pre v-html="formatSQL(sql)" style="margin: 0; white-space: pre-wrap; word-break: break-all;"></pre>
      </div>
    </div>
    <!-- 风险提示 -->
    <el-alert v-if="riskLevel === 'high'" title="高危操作警告" type="error" :closable="false" show-icon
      style="margin-top: 12px;">
      <template #default>
        <div v-if="description" style="margin-bottom: 1px;">{{ description }}</div>
        <ul v-else style="margin: 0; padding-left: 20px;">
          <li>此操作可能导致数据丢失且不可恢复</li>
          <li>请确保已备份重要数据</li>
          <li>请仔细检查 SQL 语句和 WHERE 条件</li>
          <li>建议在测试环境先验证</li>
        </ul>
      </template>
    </el-alert>

    <el-alert v-if="riskLevel === 'medium'" title="操作提醒" type="warning" :closable="false" show-icon
      style="margin-top: 12px;">
      <template #default>
        <div v-if="description" style="margin-bottom: 1px;">{{ description }}</div>
        <div v-else>
          此操作会修改数据，请确保了解操作的影响范围
        </div>
      </template>
    </el-alert>
    <!-- 操作按钮 -->
    <div style="display: flex; gap: 1px; margin-top: 3px; justify-content: flex-end;">
      <el-button size="small" @click="handleCancel">取消</el-button>
      <el-button size="small" :type="riskLevel === 'high' ? 'danger' : 'primary'" @click="handleConfirm"
        :loading="confirmLoading">
        <span v-if="riskLevel === 'high'">执行（高危）</span>
        <span v-else>执行</span>
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ElMessage } from 'element-plus'
import { computed, ref } from 'vue'

const visible = defineModel({ default: false })

const {
  sql,
  operationType,
  riskLevel,
  description,
  affectedRows,
  tableName,
  loading: parentLoading
} = defineProps({
  sql: { type: String, default: '' },
  operationType: {
    type: String,
    default: 'SELECT',
    validator: (value) => ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'DDL'].includes(value)
  },
  riskLevel: {
    type: String,
    default: 'low',
    validator: (value) => ['low', 'medium', 'high'].includes(value)
  },
  description: { type: String, default: '' },
  affectedRows: { type: Number, default: undefined },
  tableName: { type: String, default: '' },
  loading: { type: Boolean, default: false }
})

const emit = defineEmits(['confirm', 'cancel'])

const confirmLoading = computed(() => parentLoading || internalLoading.value)
const internalLoading = ref(false)

// 获取操作类型的描述
const getOperationDescription = (type) => {
  const descriptions = {
    SELECT: '查询数据（只读）',
    INSERT: '插入数据（写操作）',
    UPDATE: '更新数据（写操作）',
    DELETE: '删除数据（高危）',
    DDL: '结构变更（高危）'
  }
  return descriptions[type] || '未知操作'
}

// 格式化 SQL（简单的高亮）
const formatSQL = (sqlText) => {
  if (!sqlText) return ''

  // 1. 先将字符串字面量替换为占位符，避免关键字高亮污染字符串内容
  const stringPlaceholders = []
  let processed = sqlText.replace(/'[^']*'/g, (match) => {
    const placeholder = `\x00STR${stringPlaceholders.length}\x00`
    stringPlaceholders.push(match)
    return placeholder
  })

  // 2. 关键字高亮
  const keywords = [
    'SELECT', 'FROM', 'WHERE', 'INSERT', 'UPDATE', 'DELETE', 'INTO',
    'SET', 'VALUES', 'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER',
    'ON', 'GROUP BY', 'ORDER BY', 'HAVING', 'LIMIT', 'OFFSET',
    'AS', 'AND', 'OR', 'NOT', 'IN', 'BETWEEN', 'LIKE', 'IS NULL'
  ]

  keywords.forEach(keyword => {
    const regex = new RegExp(`\\b${keyword}\\b`, 'gi')
    processed = processed.replace(
      regex,
      `<span style="color: #d73a49; font-weight: bold;">${keyword}</span>`
    )
  })

  // 3. 还原字符串字面量并高亮
  stringPlaceholders.forEach((str, i) => {
    processed = processed.replace(`\x00STR${i}\x00`, `<span style="color: #032f62;">${escapeHtml(str)}</span>`)
  })

  return processed
}

// HTML 转义
function escapeHtml(text) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

// 处理确认按钮点击
const handleConfirm = () => {
  internalLoading.value = true
  // 生成确认标记
  const userName = getCurrentUser()
  const timestamp = new Date().toISOString()
  const confirmedSql = `${sql.trim()}\n\n-- CONFIRMED: ${userName} ${timestamp}`

  // emit 是同步的，父组件执行完成后应通过 loading prop 控制 confirmLoading
  emit('confirm', confirmedSql)
}

// 处理取消按钮点击
const handleCancel = () => {
  emit('cancel')
  visible.value = false
}

// 获取当前用户信息
function getCurrentUser() {
  const userStr = sessionStorage.getItem('currentUser')
  if (userStr) {
    try {
      const user = JSON.parse(userStr);
      return user.name || 'anonymous'
    } catch {
      return 'anonymous'
    }
  }
  return 'anonymous'
}
</script>

<style scoped>
.sql-confirm-inline {
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  padding: 16px;
  background-color: #fff;
  margin-top: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}
</style>