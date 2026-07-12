<template>
    <el-tabs :model-value="modelValue" type="border-card" class="batch-tabs" @tab-change="onTabChange">
        <el-tab-pane v-for="tab in tabs" :key="tab.name" :name="tab.name">
            <template #label>
                <span v-if="tab.type === 'modify-summary'" class="batch-tab-label batch-tab-modify">
                    <span class="batch-tab-index">M</span>
                    <span class="batch-tab-sql">修改汇总</span>
                    <el-tag v-if="tab.hasError" type="danger" size="small">{{ tab.allFailed ? '全部失败' : '部分失败' }}</el-tag>
                    <el-tag v-else-if="tab.hasRollback" type="info" size="small">已回滚</el-tag>
                    <el-tag v-else type="warning" size="small">{{ tab.modifyCount }}条</el-tag>
                </span>
                <el-tooltip v-else :content="tab.item?.sql || ''" placement="bottom" :show-after="400"
                    popper-class="sql-history-tooltip">
                    <span class="batch-tab-label" :class="{ 'batch-tab-error': tab.item?.status === 'error' }">
                        <span class="batch-tab-sql">结果集 {{ tab.queryNum }}</span>
                        <el-tag v-if="tab.item?.status === 'error'" type="danger" size="small">错误</el-tag>
                        <el-tag v-if="tab.item?.status === 'success'" type="success" size="small">{{
                            (tab.item?.data || []).length }}行</el-tag>
                    </span>
                </el-tooltip>
            </template>
        </el-tab-pane>
    </el-tabs>
</template>

<script lang="ts" setup>
defineProps<{
    tabs: any[]
    modelValue: string
}>()

const emit = defineEmits<{
    'update:modelValue': [value: string]
    change: [name: string | number]
}>()

function onTabChange(name: string | number) {
    emit('update:modelValue', String(name))
    emit('change', name)
}
</script>

<style scoped>
.batch-tabs {
    flex-shrink: 0;
    border-bottom: none;
}

.batch-tabs :deep(.el-tabs__header) {
    margin-bottom: 0;
}

.batch-tabs :deep(.el-tabs__content) {
    display: none;
}

.batch-tabs :deep(.el-tabs__item) {
    padding: 0 12px;
    height: 32px;
    line-height: 32px;
    font-size: 12px;
}

.batch-tab-label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
}

.batch-tab-index {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--el-color-primary-light-8);
    color: var(--el-color-primary);
    font-size: 11px;
    font-weight: 600;
    flex-shrink: 0;
}

.batch-tab-error .batch-tab-index {
    background: var(--el-color-danger-light-8);
    color: var(--el-color-danger);
}

.batch-tab-sql {
    white-space: nowrap;
    font-size: 12px;
    flex-shrink: 0;
}

.batch-tab-error .batch-tab-sql {
    color: var(--el-color-danger);
}

.batch-tab-rolled-back .batch-tab-index {
    background: var(--el-color-info-light-8);
    color: var(--el-color-info);
}

.batch-tab-rolled-back .batch-tab-sql {
    color: var(--el-color-info);
    text-decoration: line-through;
}

.batch-tab-modify .batch-tab-index {
    background: var(--el-color-warning-light-8);
    color: var(--el-color-warning);
    font-size: 10px;
    font-weight: 700;
}
</style>
