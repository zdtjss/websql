<template>
  <div class="permission-tree-container">
    <el-tree
      ref="treeRef"
      :key="treeKey"
      :data="data"
      :props="treeProps"
      node-key="id"
      show-checkbox
      check-strictly
      :default-expand-all="true"
      :default-checked-keys="checkedKeys"
      :check-on-click-leaf="false"
      @check="handleCheck"
      @node-click="handleNodeClick"
    >
      <template #default="{ node, data: nodeData }">
        <span class="tree-node">
          <el-icon v-if="nodeData.level === 'dir'"><Folder /></el-icon>
          <el-icon v-else-if="nodeData.level === 'conn'"><Connection /></el-icon>
          <el-icon v-else-if="nodeData.level === 'schema'"><Folder /></el-icon>
          <el-icon v-else-if="nodeData.level === 'table'"><Grid /></el-icon>
          <el-icon v-else-if="nodeData.level === 'column'"><Document /></el-icon>
          <span
            class="node-label"
            :class="{ 'node-clickable': nodeData.level !== 'column' && nodeData.level !== 'dir' }"
          >{{ node.label }}</span>
          <el-tag v-if="isImplicitNode(nodeData.id)" size="small" type="warning" class="implicit-tag">子级已选</el-tag>
          <span v-if="nodeData.data && nodeData.data.comment" class="node-comment">{{ nodeData.data.comment }}</span>
        </span>
      </template>
    </el-tree>
  </div>
</template>

<script setup lang="ts">
/**
 * 权限树展示组件：只负责渲染与事件上抛，不持有 baseline/working 状态。
 */
import { useTemplateRef } from 'vue'
import { Connection, Document, Folder, Grid } from '@element-plus/icons-vue'

interface TreeNode {
  id: string
  label: string
  level: 'dir' | 'conn' | 'schema' | 'table' | 'column'
  children?: TreeNode[]
  data?: { comment?: string; schemaName?: string; tableName?: string }
}

const props = defineProps<{
  data: TreeNode[]
  checkedKeys: string[]
  implicitKeys: Set<string>
  treeKey: number
}>()

const emit = defineEmits<{
  (e: 'check', node: TreeNode, checked: boolean): void
  (e: 'node-click', node: TreeNode): void
}>()

const treeRef = useTemplateRef('treeRef')
const treeProps = { children: 'children', label: 'label' }

function isImplicitNode(id: string): boolean {
  return props.implicitKeys.has(id)
}

function handleCheck(nodeData: TreeNode, state: { checkedKeys: Array<string | number> }) {
  const keys = (state.checkedKeys || []).map((key) => String(key))
  emit('check', nodeData, keys.includes(nodeData.id))
}

function handleNodeClick(nodeData: TreeNode) {
  emit('node-click', nodeData)
}

/** 由父组件在需要时同步视觉勾选（树节点已存在时可靠）。 */
function setCheckedKeys(keys: string[]) {
  treeRef.value?.setCheckedKeys(keys)
}

defineExpose({ setCheckedKeys })
</script>

<style scoped>
.permission-tree-container {
  height: 100%;
  overflow: auto;
}
.tree-node {
  display: flex;
  align-items: center;
  gap: 4px;
}
.node-clickable {
  cursor: pointer;
}
.implicit-tag {
  margin-left: 6px;
}
.node-comment {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
