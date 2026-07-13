<template>
    <el-drawer :model-value="modelValue" title="SQL 执行历史" :size="sqlDrawerWidth + 'px'"
        @update:model-value="emit('update:modelValue', $event)">
        <div v-if="modelValue" class="drawer-drag-handle" :style="{ right: sqlDrawerWidth + 'px' }"
            @mousedown="onDrawerDragStart"></div>
        <div style="margin-bottom: 12px;">
            <el-input v-model="sqlHistorySearch" placeholder="搜索 SQL..." clearable size="small" />
        </div>
        <el-table :data="filteredSqlHistory" stripe size="small" style="width: 100%;"
            max-height="calc(100vh - 240px)">
            <el-table-column prop="exec_time" label="时间" width="160" resizable />
            <el-table-column prop="operation_type" label="类型" width="80" resizable>
                <template #default="scope">
                    <el-tag v-if="scope.row.operation_type === 'select'" type="info" size="small">SELECT</el-tag>
                    <el-tag v-else-if="scope.row.operation_type === 'update'" type="warning"
                        size="small">UPDATE</el-tag>
                    <el-tag v-else type="danger" size="small">DELETE</el-tag>
                </template>
            </el-table-column>
            <el-table-column prop="exec_sql" label="SQL" resizable>
                <template #default="scope">
                    <el-tooltip :content="scope.row.exec_sql" placement="top"
                        popper-class="sql-history-tooltip" :show-after="400">
                        <span class="sql-history-text" @click="emit('applySql', scope.row.exec_sql)">
                            {{ scope.row.exec_sql }}
                        </span>
                    </el-tooltip>
                </template>
            </el-table-column>
            <el-table-column label="操作" width="50" resizable>
                <template #default="scope">
                    <el-icon v-if="scope.row.operation_type !== 'select'" style="cursor: pointer;"
                        @click="emit('showBackup', scope.row.id)" title="查看备份数据">
                        <View />
                    </el-icon>
                </template>
            </el-table-column>
        </el-table>
        <div style="position: absolute; right: 10px; bottom: 5px;">
            <el-pagination layout="prev, pager, next" v-model:total="sqlHistoryTotal"
                v-model:page-size="sqlHistoryPageSize" v-model:current-page="sqlHistoryCurrent"
                @current-change="showSqlHistory" />
        </div>
    </el-drawer>
</template>

<script lang="ts" setup>
import { watch } from 'vue'
import { View } from '@element-plus/icons-vue'
import { useSqlHistory } from '../composables/useSqlHistory'

const props = defineProps<{
    modelValue: boolean
    connId: string
    schema: string
}>()

const emit = defineEmits<{
    'update:modelValue': [value: boolean]
    applySql: [sql: string]
    showBackup: [id: string]
}>()

const {
    sqlHistorySearch,
    sqlHistoryTotal,
    sqlHistoryCurrent,
    sqlHistoryPageSize,
    sqlDrawerWidth,
    filteredSqlHistory,
    showSqlHistory,
    onDrawerDragStart,
} = useSqlHistory({ connId: props.connId, schema: props.schema })

// 抽屉打开时加载历史数据（原 showSqlHistory 既加载又打开，拆分后由父组件控制可见性）
watch(
    () => props.modelValue,
    (visible) => {
        if (visible) {
            showSqlHistory()
        }
    }
)
</script>

<style scoped>
.drawer-drag-handle {
    position: fixed;
    top: 0;
    bottom: 0;
    width: 6px;
    cursor: ew-resize;
    z-index: 3000;
    background: transparent;
    transition: background 0.2s;
}

.drawer-drag-handle:hover {
    background: rgba(64, 158, 255, 0.3);
}

.sql-history-text {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
    color: #409eff;
}
</style>

<style>
/* sql-history-tooltip 是 el-tooltip 的 popper-class，需要在全局作用域 */
.sql-history-tooltip {
    max-width: 500px !important;
    word-break: break-all;
    white-space: pre-wrap;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    line-height: 1.5;
}
</style>
