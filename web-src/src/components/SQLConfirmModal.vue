<template>
  <el-dialog
    v-model="visible"
    title="SQL 执行确认"
    width="700px"
    :close-on-click-modal="false"
    :destroy-on-close="true"
  >
    <div style="display: flex; flex-direction: column; gap: 16px;">
      <!-- 操作信息 -->
      <el-alert
        :title="`操作类型：${getOperationDescription(operationType)}`"
        :type="riskLevel === 'high' ? 'error' : riskLevel === 'medium' ? 'warning' : 'info'"
        :closable="false"
        show-icon
      >
        <template #default>
          <div style="display: flex; flex-direction: column; gap: 8px;">
            <div v-if="description" style="margin-bottom: 4px;">{{ description }}</div>
            <div style="display: flex; gap: 16px; flex-wrap: wrap;">
              <span>
                风险等级：
                <el-tag :type="riskLevel === 'high' ? 'danger' : riskLevel === 'medium' ? 'warning' : 'success'" size="small">
                  {{ riskLevel.toUpperCase() }}
                </el-tag>
              </span>
              <span v-if="tableName">
                表名：
                <el-tag type="info" size="small">{{ tableName }}</el-tag>
              </span>
              <span v-if="affectedRows !== undefined">
                预计影响：
                <strong>{{ affectedRows }}</strong> 行
              </span>
            </div>
          </div>
        </template>
      </el-alert>

      <!-- SQL 语句显示 -->
      <div>
        <div style="font-weight: 600; margin-bottom: 8px; font-size: 14px;">SQL 语句：</div>
        <div
          style="
            background-color: #f5f7fa;
            padding: 12px;
            border-radius: 4px;
            font-family: 'Courier New', monospace;
            font-size: 13px;
            max-height: 300px;
            overflow: auto;
            border: 1px solid #d9d9d9;
          "
        >
          <pre v-html="formatSQL(sql)" style="margin: 0; white-space: pre-wrap; word-break: break-all;"></pre>
        </div>
      </div>

      <!-- 风险提示 -->
      <el-alert
        v-if="riskLevel === 'high'"
        title="高危操作警告"
        type="error"
        :closable="false"
        show-icon
      >
        <template #default>
          <ul style="margin: 0; padding-left: 20px;">
            <li>此操作可能导致数据丢失且不可恢复</li>
            <li>请确保已备份重要数据</li>
            <li>请仔细检查 SQL 语句和 WHERE 条件</li>
            <li>建议在测试环境先验证</li>
          </ul>
        </template>
      </el-alert>

      <el-alert
        v-if="riskLevel === 'medium'"
        title="操作提醒"
        type="warning"
        :closable="false"
        show-icon
      >
        <template #default>
          此操作会修改数据，请确保了解操作的影响范围
        </template>
      </el-alert>
    </div>

    <template #footer>
      <span class="dialog-footer">
        <el-button @click="handleCancel">取消</el-button>
        <el-button
          type="primary"
          :danger="riskLevel === 'high'"
          @click="handleConfirm"
          :loading="confirmLoading"
        >
          {{ riskLevel === 'high' ? '确认执行（高危）' : '确认执行' }}
        </el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'

const props = defineProps({
  modelValue: Boolean,
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
  tableName: { type: String, default: '' }
})

const emit = defineEmits(['update:modelValue', 'confirm', 'cancel'])

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const confirmLoading = ref(false)

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
  
  // 关键字高亮
  const keywords = [
    'SELECT', 'FROM', 'WHERE', 'INSERT', 'UPDATE', 'DELETE', 'INTO',
    'SET', 'VALUES', 'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER',
    'ON', 'GROUP BY', 'ORDER BY', 'HAVING', 'LIMIT', 'OFFSET',
    'AS', 'AND', 'OR', 'NOT', 'IN', 'BETWEEN', 'LIKE', 'IS NULL'
  ]
  
  let formatted = sqlText
  keywords.forEach(keyword => {
    const regex = new RegExp(`\\b${keyword}\\b`, 'gi')
    formatted = formatted.replace(
      regex,
      `<span style="color: #d73a49; font-weight: bold;">${keyword}</span>`
    )
  })
  
  // 字符串高亮（单引号包裹的内容）
  formatted = formatted.replace(
    /'[^']*'/g,
    '<span style="color: #032f62;">$&</span>'
  )
  
  return formatted
}

// 处理确认按钮点击
const handleConfirm = async () => {
  confirmLoading.value = true
  try {
    // 生成确认标记
    const userName = getCurrentUser()
    const timestamp = new Date().toISOString()
    const confirmedSql = `${props.sql.trim()}\n\n-- CONFIRMED: ${userName} ${timestamp}`
    
    // 调用父组件的确认回调
    await emit('confirm', confirmedSql)
    
    ElMessage.success('操作成功')
  } catch (error) {
    ElMessage.error(`操作失败：${error.message}`)
  } finally {
    confirmLoading.value = false
  }
}

// 处理取消按钮点击
const handleCancel = () => {
  emit('cancel')
  visible.value = false
}

// 获取当前用户信息
const getCurrentUser = () => {
  const userStr = sessionStorage.getItem('userInfo') || localStorage.getItem('userInfo')
  if (userStr) {
    try {
      const user = JSON.parse(userStr)
      return user.username || user.name || 'anonymous'
    } catch {
      return 'anonymous'
    }
  }
  return 'anonymous'
}
</script>

<style scoped>
.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
