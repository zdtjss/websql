<template>
  <div class="db-pagination">
    <el-pagination
      :current-page="currentPage"
      :page-size="pageSize"
      :page-sizes="[20, 50, 100]"
      :total="total"
      layout="total, sizes, prev, pager, next"
      size="small"
      @update:current-page="(v: number) => emit('update:currentPage', v)"
      @update:page-size="(v: number) => emit('update:pageSize', v)"
      @current-change="onPageChange"
      @size-change="onSizeChange"
    />
  </div>
</template>

<script lang="ts" setup>
defineProps<{
  currentPage: number
  pageSize: number
  total: number
}>()

const emit = defineEmits<{
  (e: 'update:currentPage', value: number): void
  (e: 'update:pageSize', value: number): void
  (e: 'page-change'): void
  (e: 'size-change'): void
}>()

function onPageChange() {
  emit('page-change')
}

function onSizeChange() {
  emit('size-change')
}
</script>

<style scoped>
.db-pagination {
  padding: 8px 14px;
  border-top: 1px solid var(--border-primary);
  display: flex;
  justify-content: flex-end;
  background: var(--bg-toolbar);
}
</style>
