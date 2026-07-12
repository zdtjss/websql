<template>
    <div class="sql-exec-error">
        <div class="sql-exec-error-title">SQL 执行异常</div>
        <div class="sql-exec-error-body">{{ error }}</div>
    </div>
</template>

<script lang="ts" setup>
defineProps<{
    error: string
}>()
</script>

<style scoped>
.sql-exec-error {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding: 40px 20px;
    text-align: center;
}

.sql-exec-error-title {
    font-size: 18px;
    font-weight: 600;
    color: #f56c6c;
    margin-bottom: 16px;
}

.sql-exec-error-body {
    font-size: 13px;
    color: var(--text-secondary, #909399);
    max-width: 600px;
    word-break: break-all;
    line-height: 1.6;
    white-space: pre-wrap;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
}

[data-theme='dark'] .sql-exec-error-title {
    color: #f89898;
}

[data-theme='dark'] .sql-exec-error-body {
    color: #b0b0b0;
}
</style>
