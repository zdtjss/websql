<template>
    <el-drawer :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)">
        <template #header>
            <h3>备份的数据</h3>
            <a @click="emit('export')" style="margin-right: 10px; cursor: pointer;">下载</a>
        </template>
        <template #default>
            <pre style="white-space: pre;">{{ backupData }}</pre>
        </template>
    </el-drawer>
</template>

<script lang="ts" setup>
defineProps<{
    modelValue: boolean
    backupData: string
}>()

const emit = defineEmits<{
    'update:modelValue': [value: boolean]
    export: []
}>()
</script>
