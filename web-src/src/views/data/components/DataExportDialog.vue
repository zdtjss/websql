<template>
  <!--
    Export menu — surfaces 4 formats (xlsx / csv / json / sql).
    Despite the "Dialog" name in the file (kept for alignment with the
    split plan), this is a dropdown button because the current product
    behaviour triggers export immediately on format pick. If a real
    preview/options dialog is needed later, it can be added here without
    touching the parent.
  -->
  <el-dropdown @command="(cmd: ExportFormat) => emit('export', cmd)">
    <el-button size="small" type="warning" :loading="exporting">
      导出<el-icon class="el-icon--right"><ArrowDown /></el-icon>
    </el-button>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item command="xlsx">Excel (.xlsx)</el-dropdown-item>
        <el-dropdown-item command="csv">CSV</el-dropdown-item>
        <el-dropdown-item command="json">JSON</el-dropdown-item>
        <el-dropdown-item command="sql">SQL INSERT</el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script lang="ts" setup>
import { ArrowDown } from '@element-plus/icons-vue'
import type { ExportFormat } from '../composables/useDataExport'

defineProps<{
  exporting: boolean
}>()

const emit = defineEmits<{
  (e: 'export', format: ExportFormat): void
}>()
</script>
