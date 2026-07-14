<template>
  <div class="input-area">
    <!-- 提示文字 + 工具栏 -->
    <div class="input-label">
      <span>描述你的需求（数据查询 / 数据分析 / SQL 生成 / 数据导出 / 数据导入）</span>
      <slot name="toolbar" />
    </div>

    <!-- Schema / 表 / 模型 选择器 -->
    <div class="table-selector-row">
      <!-- Schema 选择器 -->
      <div class="table-selector-container" v-if="shouldShowSchemaSelector">
        <div class="selector-header">
          <label class="table-selector-label">数据库 / Schema</label>
          <span v-if="schemasLoading" class="selector-badge loading">加载中...</span>
          <span v-else-if="selectedSchemas.length" class="selector-badge">{{ selectedSchemas.length }} 已选</span>
        </div>
        <div v-if="schemasLoading" class="selector-skeleton">
          <div class="skeleton-shimmer"></div>
        </div>
        <el-tree-select v-else
          v-model="schemasRef"
          :data="processedConnList"
          :props="treeSelectProps"
          placeholder="搜索数据库 / Schema..."
          class="modern-tree-select"
          @change="emit('schema-change')"
          filterable
          multiple
          :check-on-click-node="true"
          collapse-tags
          collapse-tags-tooltip
        />
      </div>

      <!-- 表选择器 -->
      <div class="table-selector-container" :class="{ 'full-width': !shouldShowSchemaSelector }">
        <div class="selector-header">
          <label class="table-selector-label">相关表</label>
          <span v-if="tablesLoading" class="selector-badge loading">加载中...</span>
          <span v-else-if="selectedTables.length" class="selector-badge">{{ selectedTables.length }} 已选</span>
          <span v-else-if="tableList.length" class="selector-badge ready">{{ tableList.length }} 可用</span>
        </div>
        <div v-if="tablesLoading" class="selector-skeleton">
          <div class="skeleton-shimmer"></div>
        </div>
        <el-select v-else v-model="tablesRef" multiple filterable placeholder="搜索表名..." class="modern-select">
          <template #tag="{ data, deleteTag, selectDisabled }">
            <el-tag
              v-for="item in data.slice(0, 2)"
              :key="item.value"
              :closable="!selectDisabled && !item.isDisabled"
              @close="deleteTag($event, item)"
              size="small"
              disable-transitions
            >
              <el-tooltip :content="getTableComment(item.currentLabel)" :disabled="!getTableComment(item.currentLabel)" placement="top">
                <span>{{ item.currentLabel }}</span>
              </el-tooltip>
            </el-tag>
            <el-tooltip v-if="data.length > 2" placement="bottom">
              <template #content>
                <div v-for="item in data.slice(2)" :key="'c-' + item.value" style="line-height: 2;">
                  {{ item.currentLabel }}<span v-if="getTableComment(item.currentLabel)" style="color: var(--text-secondary); margin-left: 6px;">{{ getTableComment(item.currentLabel) }}</span>
                </div>
              </template>
              <el-tag size="small" type="info" disable-transitions>+ {{ data.length - 2 }}</el-tag>
            </el-tooltip>
          </template>
          <el-option v-for="table in tableList" :key="table.name + (table.schema || '')"
            :label="table.label || table.name"
            :value="table.label || table.name">
            <div class="table-option-content">
              <span class="table-option-name">{{ table.name }}</span>
              <span v-if="table.comment" class="table-option-comment">{{ table.comment }}</span>
              <span v-if="table.schema && selectedSchemas.length > 1" class="table-option-schema">{{ table.schema }}</span>
            </div>
          </el-option>
        </el-select>
      </div>

      <!-- AI 模型选择器 -->
      <div class="table-selector-container model-selector-container">
        <div class="selector-header">
          <label class="table-selector-label">AI 模型</label>
        </div>
        <el-select v-model="modelRef" filterable placeholder="选择模型..." class="modern-select">
          <el-option v-for="model in aiModelList" :key="model.id"
            :label="model.model"
            :value="model.id">
            <div class="model-option-content">
              <span class="model-option-name">{{ model.model }}</span>
            </div>
          </el-option>
        </el-select>
      </div>
    </div>

    <!-- 输入框 + 操作按钮 -->
    <div class="input-action-row">
      <div class="textarea-wrapper">
        <el-input
          v-model="questionRef"
          type="textarea"
          :rows="5"
          placeholder="描述你想查询的内容，或使用语音录入... (Ctrl+Enter 发送)"
          :disabled="loading"
          @keydown.ctrl.enter="emit('send')"
          class="question-input"
        />
        <el-button
          v-if="loading"
          type="danger"
          @click="emit('stop')"
          class="action-btn stop-btn"
          circle
        >
          <el-icon><VideoPause /></el-icon>
        </el-button>
        <el-button
          v-else
          type="primary"
          :disabled="!question.trim() && !hasUploadedFile"
          @click="emit('send')"
          class="action-btn send-btn"
          circle
        >
          <el-icon><Promotion /></el-icon>
        </el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 聊天输入区域组件。
 * 包含：工具栏插槽、Schema/表/模型选择器、输入框、发送/停止按钮。
 * 工具栏（文件上传、提示词、历史会话等）由父组件通过具名插槽 "toolbar" 提供。
 */
import { computed } from 'vue'
import { Promotion, VideoPause } from '@element-plus/icons-vue'

const props = defineProps<{
  /** 输入框内容 */
  question: string
  /** 是否正在加载 */
  loading: boolean
  /** 是否有已上传文件 */
  hasUploadedFile: boolean
  /** 选中的 schema 列表 */
  selectedSchemas: string[]
  /** 选中的表列表 */
  selectedTables: string[]
  /** 选中的 AI 模型 ID */
  selectedModel: string
  /** 处理后的连接列表（树结构） */
  processedConnList: any[]
  /** 是否显示 schema 选择器 */
  shouldShowSchemaSelector: boolean
  /** schema 是否加载中 */
  schemasLoading: boolean
  /** 表列表 */
  tableList: { name: string; comment?: string; schema?: string; label?: string }[]
  /** 表是否加载中 */
  tablesLoading: boolean
  /** AI 模型列表 */
  aiModelList: { id: string; model: string }[]
  /** 获取表注释 */
  getTableComment: (value: string) => string
}>()

const emit = defineEmits<{
  /** 更新输入框内容 */
  (e: 'update:question', val: string): void
  /** 更新选中的 schema 列表 */
  (e: 'update:selected-schemas', val: string[]): void
  /** 更新选中的表列表 */
  (e: 'update:selected-tables', val: string[]): void
  /** 更新选中的 AI 模型 ID */
  (e: 'update:selected-model', val: string): void
  /** schema 变化 */
  (e: 'schema-change'): void
  /** 发送消息 */
  (e: 'send'): void
  /** 停止生成 */
  (e: 'stop'): void
}>()

const questionRef = computed({
  get: () => props.question,
  set: (v: string) => emit('update:question', v),
})

/** el-tree-select 的 props 配置（value 非 TreeOptionProps 标准字段，需 as any） */
const treeSelectProps = { label: 'label', value: 'value', children: 'children', disabled: 'disabled' } as any

const schemasRef = computed({
  get: () => props.selectedSchemas,
  set: (v: string[]) => emit('update:selected-schemas', v),
})

const tablesRef = computed({
  get: () => props.selectedTables,
  set: (v: string[]) => emit('update:selected-tables', v),
})

const modelRef = computed({
  get: () => props.selectedModel,
  set: (v: string) => emit('update:selected-model', v),
})
</script>

<style scoped>
/* ========== 主容器 ========== */
.input-area {
  flex-shrink: 0;
  padding: 14px 20px 12px;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-primary);
  box-shadow: 0 -4px 16px rgba(0, 0, 0, 0.04);
  box-sizing: border-box;
}

/* ========== 提示标签 ========== */
.input-label {
  margin-bottom: 10px;
  font-size: 13px;
  color: var(--text-secondary);
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 500;
}

.input-label span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.input-label span::before {
  content: '\1F4A1';
  font-size: 14px;
}

/* ========== 选择器行 ========== */
.table-selector-row {
  display: flex;
  gap: 12px;
  margin-bottom: 10px;
  flex-shrink: 0;
  align-items: flex-start;
}

.table-selector-row .table-selector-container:first-child {
  flex: 0 0 22%;
}

.table-selector-row .table-selector-container:nth-child(2) {
  flex: 0 0 calc(56% - 12px);
}

.table-selector-row .model-selector-container {
  flex: 0 0 21%;
}

.table-selector-row .table-selector-container.full-width {
  flex: 0 0 calc(82% - 12px);
}

.table-selector-row .table-selector-container.full-width + .model-selector-container {
  flex: 0 0 18%;
}

.table-selector-container {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.selector-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.table-selector-label {
  font-size: 12px;
  color: var(--text-secondary);
  font-weight: 600;
  letter-spacing: 0.3px;
}

.selector-badge {
  display: inline-flex;
  align-items: center;
  padding: 1px 8px;
  font-size: 11px;
  font-weight: 600;
  border-radius: 100px;
  background: linear-gradient(135deg, rgba(0, 122, 204, 0.12), rgba(0, 122, 204, 0.05));
  color: var(--accent-color);
  white-space: nowrap;
  transition: all 0.3s ease;
}

.selector-badge.loading {
  animation: badgePulse 1.5s ease-in-out infinite;
}

.selector-badge.ready {
  background: linear-gradient(135deg, rgba(103, 194, 58, 0.12), rgba(103, 194, 58, 0.05));
  color: var(--success-color);
}

@keyframes badgePulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

/* 骨架屏 */
.selector-skeleton {
  width: 100%;
  height: 32px;
  border-radius: 8px;
  border: 1.5px solid var(--border-primary);
  overflow: hidden;
  position: relative;
  box-sizing: border-box;
}

.selector-skeleton::after {
  content: '\52A0\8F7D\4E2D...';
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 13px;
  color: var(--text-placeholder);
  pointer-events: none;
}

.skeleton-shimmer {
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, var(--bg-hover) 0%, var(--bg-active) 35%, var(--bg-hover) 65%);
  background-size: 200% 100%;
  animation: skeletonSlide 1.5s ease-in-out infinite;
  border-radius: 8px;
}

@keyframes skeletonSlide {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}

/* 选择器 */
.modern-tree-select,
.modern-select {
  width: 100%;
  margin: 0;
}

/* ========== 输入区域 ========== */
.input-action-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex-shrink: 0;
}

.textarea-wrapper {
  position: relative;
}

.question-input :deep(.el-textarea__inner) {
  border-radius: 12px;
  border: 1.5px solid var(--border-primary);
  transition: all 0.25s ease;
  font-size: 14px;
  line-height: 1.6;
  background: var(--bg-primary);
  padding-right: 52px;
  padding-bottom: 44px;
  resize: none;
}

.question-input :deep(.el-textarea__inner:hover) {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.06);
}

.question-input :deep(.el-textarea__inner:focus) {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(0, 122, 204, 0.1);
  outline: none;
}

/* 发送/停止按钮 - 位于文本框右下角 */
.action-btn {
  position: absolute;
  bottom: 10px;
  right: 10px;
  z-index: 1;
  width: 36px;
  height: 36px;
  transition: all 0.25s ease;
}

.action-btn :deep(.el-icon) {
  font-size: 16px;
}

.send-btn:hover:not(:disabled) {
  transform: scale(1.08);
  box-shadow: 0 4px 12px rgba(64, 158, 255, 0.35);
}

.send-btn:disabled {
  opacity: 0.5;
}

.stop-btn {
  animation: stopPulse 1.5s ease-in-out infinite;
}

@keyframes stopPulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}

/* ========== 下拉选项样式 ========== */
.table-option-content {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0;
  width: 100%;
}

.table-option-name {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 13px;
  flex-shrink: 0;
}

.table-option-comment {
  font-size: 12px;
  color: var(--text-tertiary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.table-option-comment::before {
  content: '\2014 ';
  color: var(--text-placeholder);
}

.table-option-schema {
  font-size: 11px;
  color: var(--accent-color);
  background: rgba(0, 122, 204, 0.08);
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 500;
  flex-shrink: 0;
}

.model-option-content {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.model-option-name {
  font-weight: 600;
  color: var(--text-primary);
  font-size: 13px;
}

/* ========== 暗色主题 ========== */
[data-theme="dark"] .input-area {
  background: var(--bg-secondary);
  border-top-color: var(--border-primary);
  box-shadow: 0 -4px 16px rgba(0, 0, 0, 0.2);
}

[data-theme="dark"] .question-input :deep(.el-textarea__inner) {
  background: #202032;
  border-color: var(--border-primary);
  color: var(--text-primary);
}

[data-theme="dark"] .question-input :deep(.el-textarea__inner:hover) {
  border-color: var(--border-secondary);
  box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.06);
}

[data-theme="dark"] .question-input :deep(.el-textarea__inner:focus) {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px rgba(137, 180, 250, 0.1);
}

[data-theme="dark"] .question-input :deep(.el-textarea__inner::placeholder) {
  color: var(--text-placeholder);
}

[data-theme="dark"] :deep(.modern-select .el-input__wrapper),
[data-theme="dark"] :deep(.modern-tree-select .el-input__wrapper) {
  background-color: #202032;
  box-shadow: 0 0 0 1px var(--border-primary) inset;
}

[data-theme="dark"] :deep(.modern-select .el-input__wrapper:hover),
[data-theme="dark"] :deep(.modern-tree-select .el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--border-secondary) inset;
}

[data-theme="dark"] :deep(.modern-select .el-input__wrapper.is-focus),
[data-theme="dark"] :deep(.modern-tree-select .el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px var(--accent-color) inset;
}

[data-theme="dark"] .table-selector-label {
  color: var(--text-secondary);
}

[data-theme="dark"] .selector-badge {
  background: linear-gradient(135deg, rgba(0, 122, 204, 0.2), rgba(0, 122, 204, 0.1));
}

[data-theme="dark"] .selector-badge.ready {
  background: linear-gradient(135deg, rgba(78, 201, 176, 0.2), rgba(78, 201, 176, 0.1));
}

[data-theme="dark"] .skeleton-shimmer {
  background: linear-gradient(90deg, var(--bg-hover) 0%, var(--bg-active) 35%, var(--bg-hover) 65%);
}
</style>
