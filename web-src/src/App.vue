<template>
  <el-container class="layout-container-demo">
    <el-aside>
      <el-tree :data="treeData" highlight-current="true" :load="loadTree" :lazy="true">
        <template #default="{ node }">
          <span class="custom-tree-node">
            <span>
              <a @click="addTab(node)">{{ node.label }}</a>
            </span>
          </span>
        </template>
      </el-tree>
    </el-aside>

    <el-container>
      <el-main>
        <el-tabs v-model="editableTabsValue" type="card" class="demo-tabs" closable @tab-remove="removeTab">
          <el-tab-pane v-for="item in editableTabs" :key="item.name" :label="item.title" :name="item.name">
            <component :is="item.component" :connId="item.connId" :schema="item.schema" />
          </el-tab-pane>
        </el-tabs>
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref } from 'vue'
import SQLEditor2 from './views/SQLEditor2.vue'
import http from './js/utils/httpProxy.js'

const treeData = ref([{
  label: "",
  type: "",
  data: {},
  children: []
}])

let tabIndex = 0
const editableTabsValue = ref('')
const editableTabs = ref([])

const addTab = (node) => {
  if (node.data.type !== "schema") {
    return
  }
  const newTabName = `${++tabIndex}`
  editableTabs.value.push({
    title: node.data.label,
    name: newTabName,
    connId: findConn(node),
    schema: node.data.label,
    component: SQLEditor2,
  })
  editableTabsValue.value = newTabName
}
const removeTab = (targetName) => {
  const tabs = editableTabs.value
  let activeName = editableTabsValue.value
  if (activeName === targetName) {
    tabs.forEach((tab, index) => {
      if (tab.name === targetName) {
        const nextTab = tabs[index + 1] || tabs[index - 1]
        if (nextTab) {
          activeName = nextTab.name
        }
      }
    })
  }
  editableTabsValue.value = activeName
  editableTabs.value = tabs.filter((tab) => tab.name !== targetName)
}

function loadTree(node, resolve) {
  http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: node.data.type } })
    .then((resp) => {
      resolve(resp.data.data)
    })
    .catch(function (error) {
      console.log(error);
    });
}

function findConn(node) {
  let connId = ""
  if (node.level === 0) {
    return connId
  } else if (node.level === 1) {
    connId = node.data.data.id
  } else {
    connId = findConn(node.parent)
  }
  return connId
}

</script>

<style scoped>
.layout-container-demo {
  width: calc(100vw * 0.98);
  height: calc(100vh * 0.98);
}

.layout-container-demo .el-header {
  position: relative;
  color: var(--el-text-color-primary);
}

.layout-container-demo .el-aside {
  color: var(--el-text-color-primary);
}

.layout-container-demo .el-menu {
  border-right: none;
}

.layout-container-demo .el-main {
  padding: 0;
}
</style>