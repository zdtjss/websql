<template>
    <div class="db-inline-bar">
        <el-button type="warning" size="small" :loading="saving" @click="emit('save')">
            <span>保存更改</span>
        </el-button>
        <el-button size="small" @click="emit('discard')">
            <span>放弃更改</span>
        </el-button>
        <span class="inline-count">{{ changeCount }} 处更改</span>
    </div>
</template>

<script lang="ts" setup>
defineProps<{
    changeCount: number
    saving: boolean
}>()

const emit = defineEmits<{
    save: []
    discard: []
}>()
</script>

<style scoped>
.db-inline-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    background: var(--bg-row-changed);
    border-top: 1px solid var(--border-primary);
    border-bottom: 1px solid var(--border-primary);
    margin-top: -1px;
}

.db-inline-bar .el-button {
    height: 26px;
    padding: 0 8px;
    font-size: 12px;
}

.inline-count {
    font-size: 12px;
    color: var(--warning-color);
    margin-left: 4px;
}

[data-theme='dark'] .db-inline-bar {
    background: var(--bg-row-changed);
    border-top: 1px solid var(--border-secondary);
    border-bottom: 1px solid var(--border-secondary);
}

[data-theme='dark'] .inline-count {
    color: var(--warning-color);
}
</style>
