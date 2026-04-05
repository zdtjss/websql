<template>
  <div class="dir-management">
    <el-alert 
      title="目录管理提示"
      type="info"
      :closable="false"
      style="margin-bottom: 16px;"
    >
      <p>💡 双击节点名称可编辑，编辑后点击"保存目录结构"生效</p>
    </el-alert>
    <el-tree 
      ref="dirTree"
      :data="conCfgTreeData" 
      draggable 
      default-expand-all 
      :expand-on-click-node="false"
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
import { ref, defineEmits, onMounted } from 'vue'
import http from '@/js/utils/httpProxy'
import { ElMessage } from 'element-plus'

const emit = defineEmits(['tree-saved'])

const conCfgTreeData = ref([])
const dirTree = ref(null)

const listDirTree = () => {
  http.get("/listDirTree").then((resp) => {
    conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
  })
}

const appendTreeNode = (data) => {
  const newChild = { label: "", value: "", id: "", children: [] }
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
  
  if (data.id) {
    http.get("/delTreeNode", { params: { id: data.id } }).then((resp) => {
      conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "", id: "", children: [] }] : resp.data.data
    })
  }
}

const saveTree = () => {
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
