<template>
  <div class="dir-management">
    <div class="dir-panel">
      <div class="panel-header">
        <div class="panel-header-left">
          <h2>目录管理</h2>
          <span class="panel-header-desc">拖拽可调整目录顺序，勾选可批量选择目录</span>
        </div>
        <div class="panel-header-actions">
          <el-button size="small" @click="checkAll">
            <el-icon><Select /></el-icon>
            全选
          </el-button>
          <el-button size="small" @click="uncheckAll">
            <el-icon><Close /></el-icon>
            取消全选
          </el-button>
        </div>
      </div>
      <div class="panel-body">
        <el-tree
          ref="dirTree"
          :data="conCfgTreeData"
          draggable
          default-expand-all
          :expand-on-click-node="false"
          show-checkbox
          node-key="id"
          :allow-drop="allowDrop"
          @check="handleCheckChange"
        >
          <template #default="{ node, data }">
            <div class="tree-node-content">
              <el-icon class="node-icon" :size="16">
                <Folder />
              </el-icon>
              <el-input
                v-model="data.label"
                class="node-input"
                placeholder="请输入目录名称"
                size="small"
              />
              <div class="node-actions">
                <el-button type="primary" size="small" link @click="appendTreeNode(data)">
                  <el-icon><Plus /></el-icon>
                  添加子节点
                </el-button>
                <el-popconfirm title="确定要删除这个目录吗？" @confirm="removeDir(node, data)">
                  <template #reference>
                    <el-button type="danger" size="small" link>
                      <el-icon><Delete /></el-icon>
                      删除
                    </el-button>
                  </template>
                </el-popconfirm>
              </div>
            </div>
          </template>
          <template #empty>
            <div class="tree-empty">
              <el-empty description="暂无目录数据" :image-size="80" />
            </div>
          </template>
        </el-tree>
      </div>
      <div class="panel-footer">
        <el-button type="primary" @click="saveTree">
          <el-icon><Check /></el-icon>
          保存目录结构
        </el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick, watch } from 'vue'
import http from '@/utils/httpProxy'
import { ElMessage } from 'element-plus'
import { Check, Delete, Plus, Select, Close, Folder } from '@element-plus/icons-vue'

const emit = defineEmits(['tree-saved'])

const conCfgTreeData = ref([])
const dirTree = ref(null)
let nodeIdCounter = 0

const generateUniqueId = () => {
  return `temp_node_${Date.now()}_${nodeIdCounter++}`
}

const ensureNodeIds = (nodes) => {
  for (const node of nodes) {
    if (!node.id || node.id === '') {
      node.id = generateUniqueId()
    }
    if (node.children && node.children.length > 0) {
      ensureNodeIds(node.children)
    }
  }
}

const getAllChildIds = (node) => {
  const ids = []
  if (node.children && node.children.length > 0) {
    for (const child of node.children) {
      ids.push(child.id)
      ids.push(...getAllChildIds(child))
    }
  }
  return ids
}

const getAllAncestorIds = (nodes, targetId, path = []) => {
  for (const node of nodes) {
    if (node.id === targetId) {
      return [...path]
    }
    if (node.children && node.children.length > 0) {
      const found = getAllAncestorIds(node.children, targetId, [...path, node.id])
      if (found.length > 0 || node.children.some(c => c.id === targetId)) {
        return [...path, node.id]
      }
    }
  }
  return []
}

const userCheckedKeys = ref(new Set())
let isProgrammaticChange = false

const handleCheckChange = (data, checkedInfo) => {
  if (isProgrammaticChange) {
    return
  }

  const { checkedKeys } = checkedInfo
  const isChecked = checkedKeys.includes(data.id)

  if (isChecked) {
    userCheckedKeys.value.add(data.id)
    const childIds = getAllChildIds(data)
    for (const childId of childIds) {
      userCheckedKeys.value.delete(childId)
    }
    const ancestorIds = getAllAncestorIds(conCfgTreeData.value, data.id)
    for (const ancestorId of ancestorIds) {
      userCheckedKeys.value.delete(ancestorId)
    }
  } else {
    userCheckedKeys.value.delete(data.id)
    const childIds = getAllChildIds(data)
    for (const childId of childIds) {
      userCheckedKeys.value.delete(childId)
    }
  }
}

const checkAll = () => {
  const getAllNodeKeys = (nodes) => {
    const result = []
    for (const node of nodes) {
      if (node.id) {
        result.push(node.id)
      }
      if (node.children && node.children.length > 0) {
        result.push(...getAllNodeKeys(node.children))
      }
    }
    return result
  }
  const allNodeKeys = getAllNodeKeys(conCfgTreeData.value)
  isProgrammaticChange = true
  userCheckedKeys.value = new Set(allNodeKeys)
  nextTick(() => {
    dirTree.value.setCheckedKeys(allNodeKeys)
    nextTick(() => {
      isProgrammaticChange = false
    })
  })
}

const uncheckAll = () => {
  isProgrammaticChange = true
  userCheckedKeys.value = new Set()
  nextTick(() => {
    dirTree.value.setCheckedKeys([])
    nextTick(() => {
      isProgrammaticChange = false
    })
  })
}

const extractCheckedKeys = (nodeList) => {
  const keys = []
  for (const node of nodeList) {
    if (node.checked) {
      keys.push(node.id)
    }
    if (node.children && node.children.length > 0) {
      keys.push(...extractCheckedKeys(node.children))
    }
  }
  return keys
}

const initTreeCheckedState = () => {
  if (!dirTree.value) return

  const checkedKeys = extractCheckedKeys(conCfgTreeData.value)
  userCheckedKeys.value = new Set(checkedKeys)

  if (checkedKeys.length > 0) {
    isProgrammaticChange = true
    nextTick(() => {
      dirTree.value.setCheckedKeys(checkedKeys)
      nextTick(() => {
        isProgrammaticChange = false
      })
    })
  }
}

watch(conCfgTreeData, () => {
  nextTick(() => {
    initTreeCheckedState()
  })
}, { immediate: false })

const listDirTree = () => {
  http.get("/listDirTree").then((resp) => {
    const data = resp.data.data
    if (Array.isArray(data) && data.length > 0) {
      ensureNodeIds(data)
      conCfgTreeData.value = data
    } else {
      conCfgTreeData.value = []
    }
  })
}

const appendTreeNode = (data) => {
  const newChild = { label: "", value: "", id: generateUniqueId(), children: [] }
  if (!data.children) {
    data.children = []
  }
  data.children.push(newChild)
}

const allowDrop = (draggingNode, dropNode, type) => {
  return type !== 'inner'
}

const removeDir = (node, data) => {
  const removeNode = (list, targetNode) => {
    const index = list.findIndex(item => item === targetNode)
    if (index !== -1) {
      list.splice(index, 1)
      return true
    }
    for (const item of list) {
      if (item.children && removeNode(item.children, targetNode)) {
        return true
      }
    }
    return false
  }

  removeNode(conCfgTreeData.value, data)

  userCheckedKeys.value.delete(data.id)
  const childIds = getAllChildIds(data)
  for (const childId of childIds) {
    userCheckedKeys.value.delete(childId)
  }

  if (data.id && !String(data.id).startsWith('temp_node_')) {
    http.get("/delTreeNode", { params: { id: data.id } }).then(() => {
      ElMessage.success("删除成功")
    }).catch(() => {
      ElMessage.error("删除失败，请重试")
    })
  }
}

const saveTree = () => {
  const treeData = conCfgTreeData.value
  if (treeData.length === 0) {
    ElMessage.warning("目录为空，无需保存")
    return
  }

  const checkedKeys = Array.from(userCheckedKeys.value)

  const applyCheckedState = (nodes) => {
    for (const node of nodes) {
      node.checked = checkedKeys.includes(node.id)
      if (node.children && node.children.length > 0) {
        applyCheckedState(node.children)
      }
    }
  }
  applyCheckedState(treeData)

  http.post("/saveTree", treeData).then(() => {
    ElMessage.success("保存成功")
    emit('tree-saved', treeData)
  })
}

onMounted(() => {
  listDirTree()
})
</script>

<style scoped>
.dir-management {
  padding: 20px;
  max-height: calc(100vh - 150px);
  overflow-y: auto;
}

.dir-panel {
  background: var(--bg-primary);
  border-radius: 8px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.panel-header {
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-primary);
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.panel-header-left {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.panel-header-left h2 {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
}

.panel-header-desc {
  font-size: 12px;
  color: var(--text-tertiary);
}

.panel-header-actions {
  display: flex;
  gap: 8px;
}

.panel-body {
  padding: 16px 20px;
  flex: 1;
  overflow-y: auto;
  min-height: 200px;
}

.tree-empty {
  padding: 40px 0;
}

.tree-node-content {
  display: flex;
  align-items: center;
  flex: 1;
  padding: 4px 0;
  gap: 8px;
}

.node-icon {
  color: var(--warning-color);
  flex-shrink: 0;
}

.node-input {
  width: 260px;
  flex-shrink: 0;
}

.node-actions {
  display: flex;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.2s;
}

.tree-node-content:hover .node-actions {
  opacity: 1;
}

.panel-footer {
  padding: 16px 20px;
  border-top: 1px solid var(--border-primary);
  display: flex;
  justify-content: flex-end;
}

:deep(.el-tree) {
  font-size: 14px;
  background: transparent;
}

:deep(.el-tree-node__content) {
  height: auto;
  padding: 4px 0;
}

:deep(.el-tree-node__content:hover) {
  background: var(--bg-hover);
}

:deep(.el-tree-node.is-current > .el-tree-node__content) {
  background: var(--bg-active);
}

:deep(.el-tree-node__expand-icon) {
  color: var(--text-tertiary);
}

:deep(.el-input__wrapper) {
  border-radius: 6px;
}
</style>
