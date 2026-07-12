<template>
  <div v-if="items.length > 0" class="multi-sql-confirm">
    <div style="font-weight:600;margin-bottom:8px;font-size:14px;">
      检测到 {{ items.length }} 条需要确认的 SQL：
    </div>
    <div style="margin-bottom:8px;">
      <el-checkbox :model-value="selectAll" @change="(v: any) => emit('select-all', !!v)">
        全选
      </el-checkbox>
      <span style="font-size:12px;color:#909399;margin-left:8px;">
        已选择 {{ selectedCount }} 条
      </span>
    </div>
    <div v-for="(item, idx) in items" :key="idx" class="sql-confirm-item">
      <div class="sql-confirm-header">
        <el-checkbox v-model="item.selected" style="margin-right:8px;">
          <el-tag :type="item.riskLevel === 'high' ? 'danger' : 'warning'" size="small">
            {{ item.riskLevel === 'high' ? '高危' : '中危' }} - {{ item.type }}
          </el-tag>
        </el-checkbox>
        <span v-if="item.tableName" style="font-size:12px;color:#909399;">表：{{ item.tableName }}</span>
      </div>
      <pre class="sql-pre"><code v-html="highlightSql(item.sql)" /></pre>
    </div>
    <div style="display:flex;gap:8px;margin-top:12px;justify-content:flex-end;">
      <el-button size="small" @click="emit('cancel-all')">全部取消</el-button>
      <el-button size="small" type="danger" @click="emit('confirm-selected')" :disabled="selectedCount === 0">
        确认执行选中 ({{ selectedCount }} 条)
      </el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 批量 SQL 确认弹框：检测到多条危险 SQL 时，支持逐条选择和批量确认。
 */
import type { SqlRiskItem } from '../composables/useSqlConfirm'

defineProps<{
  /** 待确认的 SQL 列表 */
  items: SqlRiskItem[]
  /** 是否全选 */
  selectAll: boolean
  /** 已选中的数量 */
  selectedCount: number
  /** SQL 高亮函数 */
  highlightSql: (text: string) => string
}>()

const emit = defineEmits<{
  /** 全选/取消全选 */
  (e: 'select-all', val: boolean): void
  /** 确认执行选中的 SQL */
  (e: 'confirm-selected'): void
  /** 全部取消 */
  (e: 'cancel-all'): void
}>()
</script>
