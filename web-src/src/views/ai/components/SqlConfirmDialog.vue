<template>
  <SQLConfirmInline
    v-model="visibleRef"
    :sql="sql"
    :operation-type="operationType"
    :risk-level="riskLevel"
    :description="description"
    :table-name="tableName"
    @confirm="emit('confirm', $event)"
    @cancel="emit('cancel')"
  />
</template>

<script setup lang="ts">
/**
 * 单条 SQL 确认弹框：封装 SQLConfirmInline 组件，统一对外接口。
 */
import { computed } from 'vue'
import SQLConfirmInline from '@/components/ai/SQLConfirmInline.vue'

const props = defineProps<{
  /** 是否显示 */
  visible: boolean
  /** SQL 语句 */
  sql: string
  /** 操作类型 */
  operationType: string
  /** 风险等级 */
  riskLevel: string
  /** 风险描述 */
  description: string
  /** 表名 */
  tableName: string
}>()

const emit = defineEmits<{
  /** 更新 visible */
  (e: 'update:visible', val: boolean): void
  /** 确认执行 */
  (e: 'confirm', sql: string): void
  /** 取消 */
  (e: 'cancel'): void
}>()

const visibleRef = computed({
  get: () => props.visible,
  set: (v: boolean) => emit('update:visible', v),
})
</script>
