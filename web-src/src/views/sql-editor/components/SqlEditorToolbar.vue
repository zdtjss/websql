<template>
    <div class="sql-toolbar">
        <div class="toolbar-left">
            <el-button :type="exectingSql ? 'danger' : 'primary'" @click="emit('exec')"
                :title="exectingSql ? '终止执行' : 'F9 执行选中内容或光标所在语句'">
                <el-icon style="margin-right: 4px;">
                    <Loading v-if="exectingSql" />
                    <VideoPlay v-else />
                </el-icon>{{ exectingSql ? '终止' : '执行' }}
            </el-button>
            <el-divider direction="vertical" />
            <el-button @click="emit('format')" title="Ctrl + Shift + F">美化</el-button>
            <el-button type="success" @click="emit('optimize')" title="AI SQL优化建议">优化</el-button>
            <el-divider direction="vertical" />
            <el-dropdown @command="(c: string) => emit('export', c)">
                <el-button :disabled="resultLength === 0">
                    导出结果<el-icon class="el-icon--right">
                        <ArrowDown />
                    </el-icon>
                </el-button>
                <template #dropdown>
                    <el-dropdown-menu>
                        <el-dropdown-item :disabled="resultLength === 0" command="insert">SQL  新增</el-dropdown-item>
                        <el-dropdown-item :disabled="resultLength === 0" command="update">SQL  修改</el-dropdown-item>
                        <el-dropdown-item :disabled="resultLength === 0" command="xlsx" divided>Excel (.xlsx)</el-dropdown-item>
                        <el-dropdown-item :disabled="resultLength === 0" command="csv">CSV</el-dropdown-item>
                        <el-dropdown-item :disabled="resultLength === 0" command="json">JSON</el-dropdown-item>
                    </el-dropdown-menu>
                </template>
            </el-dropdown>
            <el-divider direction="vertical" />
            <el-button @click="emit('openTableManager')">表管理</el-button>
            <el-button @click="emit('showHistory')">历史</el-button>
            <el-button @click="emit('showSnippet')" title="SQL 收藏夹">收藏</el-button>
            <el-upload :show-file-list="false" accept=".sql" :http-request="handleExecFile">
                <el-button title="执行 SQL 文件">执行文件</el-button>
            </el-upload>
            <el-button @click="emit('refreshSchema')" :loading="refreshingSchema" title="刷新表结构（更新自动补全）">
                <el-icon style="margin-right: 4px;"><Refresh /></el-icon>
            </el-button>
        </div>
        <div class="toolbar-right">
            <span v-if="executionTime !== null" class="exec-time">{{ formatDuration(executionTime) }}</span>
            <span v-if="canInlineEdit && resultLength > 0" class="inline-edit-badge"
                title="当前结果集有主键，支持双击单元格内联编辑">✎ 可编辑</span>
            <el-tooltip
                :content="roleForbidModify ? '当前角色禁止修改数据' : (canModify ? '当前允许修改数据，点击切换为只读' : '当前为只读模式，点击允许修改数据')"
                placement="bottom" :show-after="400">
                <label class="modify-toggle">
                    <el-switch :model-value="canModify" size="small" :disabled="roleForbidModify"
                        @update:model-value="(v: string | number | boolean) => emit('update:canModify', !!v)" />
                    <span class="modify-label">{{ canModify ? '可写' : '只读' }}</span>
                </label>
            </el-tooltip>
            <el-divider direction="vertical" />
            <span class="max-rows-label">行数上限</span>
            <el-input :model-value="maxLine" style="width: 56px;" size="small"
                @update:model-value="(v: string) => emit('update:maxLine', v)" />
        </div>
    </div>
</template>

<script lang="ts" setup>
import { ArrowDown, VideoPlay, Loading, Refresh } from '@element-plus/icons-vue'

defineProps<{
    exectingSql: boolean
    executionTime: number | null
    resultLength: number
    canInlineEdit: boolean
    canModify: boolean
    roleForbidModify: boolean
    maxLine: string
    refreshingSchema: boolean
}>()

const emit = defineEmits<{
    exec: []
    format: []
    optimize: []
    export: [command: string]
    openTableManager: []
    showHistory: []
    showSnippet: []
    execFile: [options: any]
    refreshSchema: []
    'update:canModify': [value: boolean]
    'update:maxLine': [value: string]
}>()

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
    const minutes = Math.floor(ms / 60000)
    const seconds = Math.round((ms % 60000) / 1000)
    return `${minutes}m ${seconds}s`
}

function handleExecFile(opts: any): Promise<void> {
    emit('execFile', opts)
    return Promise.resolve()
}
</script>

<style scoped>
.sql-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    background: var(--bg-toolbar);
    border-bottom: 1px solid var(--border-primary);
    gap: 4px;
}

.sql-toolbar .toolbar-left {
    display: flex;
    align-items: center;
    gap: 8px;
}

.sql-toolbar .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
}

.sql-toolbar .el-button {
    height: 28px;
    padding: 0 10px;
    font-size: 13px;
    border-radius: 6px;
}

.sql-toolbar .el-divider--vertical {
    margin: 0;
    height: 16px;
}

.modify-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
}

.modify-label {
    font-size: 12px;
    color: var(--text-secondary);
    user-select: none;
}

.max-rows-label {
    font-size: 12px;
    color: var(--text-tertiary);
    white-space: nowrap;
}

.exec-time {
    font-size: 12px;
    color: #67c23a;
    font-weight: 500;
    white-space: nowrap;
    margin-right: 4px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
}

.inline-edit-badge {
    font-size: 12px;
    color: #409eff;
    font-weight: 500;
    white-space: nowrap;
    margin-right: 4px;
    cursor: default;
}
</style>
