<template>
  <el-container class="layout-container-demo">
    <el-aside>
      <div>
        <el-icon color="#409EFC" @click="conCfgAddDialogVisible = true" style="cursor: pointer;">
          <Plus />
        </el-icon>
        <el-icon color="#409EFC" @click="conCfgListDialogVisible = true; listConnCfg()" style="cursor: pointer;margin-left: 8px;">
          <List />
        </el-icon>
      </div>
      <el-tree :highlight-current="true" :load="loadTree" :lazy="true">
        <template #default="{ node, data }">
          <span class="custom-tree-node">
            <span>
              <a :title="data.data != null ? data.data.text : ''">{{ node.label }}</a>
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
    <el-dialog v-model="conCfgAddDialogVisible" @close="conCfgAddDialogVisible = false" width="600px">
      <el-form v-model="connCfg">
        <el-form-item label="连接名称" :label-width="formLabelWidth">
          <el-input v-model="connCfg.name" />
        </el-form-item>
        <el-form-item label="用户名" :label-width="formLabelWidth">
          <el-input v-model="connCfg.user" :label-width="formLabelWidth" />
        </el-form-item>
        <el-form-item label="密码" :label-width="formLabelWidth">
          <el-input type="password" v-model="connCfg.pwd" />
        </el-form-item>
        <el-form-item label="连接信息" :label-width="formLabelWidth">
          <el-input v-model="connCfg.url" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="saveConnCfg">保存</el-button>
          <el-button @click="conCfgAddDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <el-dialog v-model="conCfgListDialogVisible" @close="conCfgListDialogVisible = false" width="800px">
      <el-table :data="connCfgList" style="width: 100%">
        <el-table-column prop="name" label="连接名称" width="150"/>
        <el-table-column prop="user" label="用户名"  width="150"/>
        <el-table-column prop="pwd" label="密码" width="180"/>
        <el-table-column prop="url" label="连接信息" />
      </el-table>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="conCfgListDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup>
import { ref, shallowRef } from 'vue'
import SQLEditor2 from './views/SQLEditor2.vue'
import http from './js/utils/httpProxy.js'
import { useDBStore } from '@/stores/sql'

const sqlEditor = shallowRef(SQLEditor2)
const dbStore = useDBStore()

let tabIndex = 0
const editableTabsValue = ref('')
const editableTabs = ref([])

const formLabelWidth = '120px'
const conCfgAddDialogVisible = ref(false)
const conCfgListDialogVisible = ref(false)
const connCfgList = ref([])

const connCfg = ref({})

const addTab = (data, node) => {
  if (node.data.type !== "schema") {
    return
  }
  const newTabName = `${++tabIndex}`
  editableTabs.value.push({
    title: node.data.label,
    name: newTabName,
    connId: findConn(node),
    schema: node.data.label,
    component: sqlEditor,
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
      if (node.data.type === "schema") {
        dbStore.addTable(node.data.label, resp.data.data)
        addTab(node.data, node)
        /* http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: "table" } })
          .then((resp) => {
            dbStore.addTable(node.data.label + "_col", resp.data.data)
          }) */
      }
      resolve(resp.data.data)
    })
    .catch((error) => {
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

function saveConnCfg() {
  http.post("/saveConn", connCfg.value)
    .then((resp) => {
      conCfgDialogVisible.value = false
      alert("添加成功")
    })
}

function listConnCfg() {
  http.get("/listConn2")
    .then((resp) => {
      connCfgList.value = resp.data.data
    })
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