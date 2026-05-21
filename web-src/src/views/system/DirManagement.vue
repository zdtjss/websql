<template>
  <div class="dir-management">
    <el-alert 
      title="目录管理提示"
      type="info"
      :closable="false"
      style="margin-bottom: 16px;"
    >
      <p>💡 双击节点名称可编辑，编辑后点击"保存目录结构"生效；使用复选框可批量选择目录</p>
    </el-alert>
    <div class="tree-toolbar">
      <el-button type="primary" size="small" @click="checkAll">
        <el-icon><Select /></el-icon>
        全选
      </el-button>
      <el-button type="warning" size="small" @click="uncheckAll">
        <el-icon><Close /></el-icon>
        取消全选
      </el-button>
    </div>
    <el-tree 
      ref="dirTree"
      :data="conCfgTreeData" 
      draggable 
      default-expand-all 
      :expand-on-click-node="false"
      show-checkbox
      node-key="id"
      @check="handleCheckChange"
    >
      <template #default="{ node, data }">
        <div class="tree-node-content">
          <el-input 
            v-model="data.label" 
            style="width: 300px; margin-right: 16px;" 
            placeholder="目录名称"
          />
          <el-button type="primary" size="small" @click="appendTreeNode(data)">
            <el-icon><Plus /></el-icon>
            添加子节点
          </el-button>
          <el-popconfirm title="确定要删除这个目录吗？" @confirm="removeDir(node, data)">
            <template #reference>
              <el-button type="danger" size="small" style="margin-left: 8px;">
                <el-icon><Delete /></el-icon>
                删除
              </el-button>
            </template>
          </el-popconfirm>
        </div>
      </template>
    </el-tree>
    <div class="tree-actions">
      <el-button type="primary" @click="saveTree">
        <el-icon><Check /></el-icon>
        保存目录结构
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick, watch } from 'vue'
import http from '@/utils/httpProxy'
import { ElMessage } from 'element-plus'
import { Check, Delete, Plus, Select, Close } from '@element-plus/icons-vue'

const emit = defineEmits(['tree-saved'])

const conCfgTreeData = ref([])
const dirTree = ref(null)
let nodeIdCounter = 0

const generateUniqueId = () => {
  return `temp_node_${Date.now()}_${nodeIdCounter++}`
}

// 递归为没有 id 的节点生成唯一 ID，保留后端已有的 id
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

// 获取某节点的所有子节点 ID
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

// 获取某节点的所有祖先节点 ID
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

// 用户主动勾选的节点集合（用于保存时区分级联勾选）
const userCheckedKeys = ref(new Set())
// 标记是否正在通过程序设置勾选状态
let isProgrammaticChange = false

// 监听勾选变化
const handleCheckChange = (data, checkedInfo) => {
  if (isProgrammaticChange) {
    return
  }
  
  const { checkedKeys } = checkedInfo
  const isChecked = checkedKeys.includes(data.id)
  
  if (isChecked) {
    // 用户勾选节点：将该节点加入，同时移除其所有子节点（因为子节点是级联选中的）
    userCheckedKeys.value.add(data.id)
    const childIds = getAllChildIds(data)
    for (const childId of childIds) {
      userCheckedKeys.value.delete(childId)
    }
    // 同时移除其所有祖先节点（因为父节点被级联选中了）
    const ancestorIds = getAllAncestorIds(conCfgTreeData.value, data.id)
    for (const ancestorId of ancestorIds) {
      userCheckedKeys.value.delete(ancestorId)
    }
  } else {
    // 用户取消节点：移除该节点及其所有子节点
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

// 从数据中提取已勾选的节点
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

// 初始化树的勾选状态
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

// 监听数据变化，初始化勾选状态
watch(conCfgTreeData, () => {
  nextTick(() => {
    initTreeCheckedState()
  })
}, { immediate: false })

const listDirTree = () => {
  http.get("/listDirTree").then((resp) => {
    const data = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
    ensureNodeIds(data)
    conCfgTreeData.value = data
  })
}

const appendTreeNode = (data) => {
  const newChild = { label: "", value: "", id: generateUniqueId(), children: [] }
  if (!data.children) {
    data.children = []
  }
  data.children.push(newChild)
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
  
  // 从 userCheckedKeys 中移除被删除节点及其子节点
  userCheckedKeys.value.delete(data.id)
  const childIds = getAllChildIds(data)
  for (const childId of childIds) {
    userCheckedKeys.value.delete(childId)
  }
  
  if (data.id) {
    http.get("/delTreeNode", { params: { id: data.id } }).then((resp) => {
      conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
    })
  }
}

const saveTree = () => {
  const checkedKeys = Array.from(userCheckedKeys.value)
  
  // 将勾选状态附加到树数据上
  const applyCheckedState = (nodes) => {
    for (const node of nodes) {
      node.checked = checkedKeys.includes(node.id)
      if (node.children && node.children.length > 0) {
        applyCheckedState(node.children)
      }
    }
  }
  applyCheckedState(conCfgTreeData.value)
  
  http.post("/saveTree", conCfgTreeData.value).then(() => {
    ElMessage.success("保存成功")
    emit('tree-saved', conCfgTreeData.value)
  })
}

onMounted(() => {
  listDirTree()
})
</script>

<style scoped>
.dir-management {
  padding: 20px;
}

.tree-toolbar {
  margin-bottom: 12px;
  display: flex;
  gap: 8px;
}

.tree-node-content {
  display: flex;
  align-items: center;
  padding: 8px 0;
}

.tree-actions {
  margin-top: 20px;
  text-align: right;
  padding: 20px;
  border-top: 1px solid #e4e7ed;
}

:deep(.el-tree) {
  font-size: 14px;
}
</style>
