<template>
  <div class="input-area">
    <!-- 工具栏：文件上传、提示词、历史会话、录音、清空（由父组件通过插槽提供） -->
    <div class="input-label">
      <span>描述你的需求（数据查询 / 数据分析 / SQL 生成 / 数据导出 / 数据导入）</span>
      <slot name="toolbar" />
    </div>

    <!-- 数据库 / Schema / 表 / 模型 选择器 -->
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
          <label class="table-selector-label" style="padding-top: 2px;">相关表</label>
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
                  {{ item.currentLabel }}<span v-if="getTableComment(item.currentLabel)" style="color: var(--el-text-color-secondary); margin-left: 6px;">{{ getTableComment(item.currentLabel) }}</span>
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
          <label class="table-selector-label" style="padding-top: 2px;">AI 模型</label>
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

    <!-- 输入框 + 发送/停止按钮 -->
    <div class="input-action-row">
      <div style="flex:1;display:flex;flex-direction:column;gap:6px;">
        <el-input v-model="questionRef" type="textarea" :rows="5" placeholder="描述你想查询的内容，或使用语音录入... (Ctrl+Enter 发送)"
          :disabled="loading" @keydown.ctrl.enter="emit('send')" class="question-input" />
      </div>
      <div class="action-buttons">
        <el-button v-if="loading" type="danger" @click="emit('stop')"
          class="stop-btn" size="default">
          <el-icon>
            <VideoPause />
          </el-icon>
        </el-button>
        <el-button v-else type="primary" :disabled="!question.trim() && !hasUploadedFile" @click="emit('send')"
          class="send-btn" size="default">
          <el-icon>
            <Promotion />
          </el-icon>
        </el-button>
        <router-link v-if="canUseClassicView" to="/classical" class="switch-view-link" title="经典视图">
          <el-icon>
            <Switch />
          </el-icon>
          <span>经典视图</span>
        </router-link>
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
import { Promotion, Switch, VideoPause } from '@element-plus/icons-vue'

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
  /** 是否可使用经典视图 */
  canUseClassicView: boolean
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
