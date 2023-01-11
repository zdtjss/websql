<template>
  <el-container class="layout-container-demo">
    <el-aside width="350px">
      <div>
        <el-icon color="#409EFC" @click="treeListDialogVisible = true; listDirTree()" style="cursor: pointer;"
          title="添加目录">
          <FolderAdd />
        </el-icon>
        <el-icon color="#409EFC" @click="conCfgAddDialogVisible = true; listDirTree()"
          style="cursor: pointer;margin-left: 8px;" title="添加连接">
          <Plus />
        </el-icon>
        <el-icon color="#409EFC" @click="conCfgListDialogVisible = true; listConnCfg()"
          style="cursor: pointer;margin-left: 8px;" title="连接列表">
          <List />
        </el-icon>
      </div>
      <el-tree :highlight-current="true" :load="loadTree" :lazy="true">
        <template #default="{ node, data }">
          <span>
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
        <el-form-item label="所属层级" :label-width="formLabelWidth">
          <el-tree-select v-model="connCfg.treeNode" :data="conCfgTreeData" :render-after-expand="false"
            value-key="label" placeholder="请选择" />
        </el-form-item>
        <el-form-item label="数据库类型" :label-width="formLabelWidth">
          <el-select v-model="connCfg.dbType" placeholder="请选择">
            <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="用户名" :label-width="formLabelWidth">
          <el-input v-model="connCfg.user" :label-width="formLabelWidth" />
        </el-form-item>
        <el-form-item label="密码" :label-width="formLabelWidth">
          <el-input type="password" v-model="connCfg.pwd" />
        </el-form-item>
        <el-form-item label="连接信息" :label-width="formLabelWidth">
          <el-input v-model="connCfg.url" type="textarea" :autosize="{ minRows: 2, maxRows: 4 }" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="saveConnCfg">保存</el-button>
          <el-button @click="conCfgAddDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <el-dialog v-model="conCfgListDialogVisible" @close="conCfgListDialogVisible = false" width="860px">
      <el-table :data="connCfgList" style="width: 100%;max-height: 500px;overflow-y: auto;">
        <el-table-column prop="name" label="连接名称" width="100" />
        <el-table-column prop="dbType" label="数据库类型" width="100" />
        <el-table-column prop="user" label="用户名" width="150" />
        <el-table-column prop="pwd" label="密码" width="180" />
        <el-table-column prop="url" label="连接信息" />
        <el-table-column label="操作" style="text-align: center; " width="80">
          <template #default="scope">
            <el-popconfirm title="确定要删除?" @confirm="delConnCfg(scope.row.id)" confirm-button-text="是"
              cancel-button-text="否">
              <template #reference>
                <el-button size="small">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="conCfgListDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <el-dialog v-model="treeListDialogVisible" @close="treeListDialogVisible = false" width="350px">
      <el-tree :data="conCfgTreeData" draggable default-expand-all :expand-on-click-node="false">
        <template #default="{ node, data }">
          <span>
            <span>
              <el-input v-model="data.label" size="small"></el-input>
            </span>
            <span style="right: 0px;position: absolute;">
              <a @click="appendTreeNode(data)">添加</a>
              <el-popconfirm title="确定要删除?" @confirm="removeTreeNode(node, data)" confirm-button-text="是"
                cancel-button-text="否">
                <template #reference>
                  <a style="margin-left: 8px">删除</a>
                </template>
              </el-popconfirm>
            </span>
          </span>
        </template>
      </el-tree>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="saveTree">保存</el-button>
          <el-button @click="treeListDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup>
import { ref, shallowRef } from 'vue'
import SQLEditor2 from './views/SQLEditor2.vue'
import http from './js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'

const sqlEditor = shallowRef(SQLEditor2)

let tabIndex = 0
const editableTabsValue = ref('')
const editableTabs = ref([])

const formLabelWidth = '100px'
const conCfgAddDialogVisible = ref(false)
const conCfgListDialogVisible = ref(false)
const connCfgList = ref([])

const conCfgTreeData = ref([])
const treeListDialogVisible = ref(false)
const dbTypeList = ref([{ label: "MySQL", value: "mysql" }])

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
  if (node.data.type === 'column') {
    resolve([])
    return
  }
  http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: node.data.type, level: node.level } })
    .then((resp) => {
      if (node.data.type === "schema") {
        debugger
        dbSchemaProxy.addTable(node.data.label, resp.data.data)
        console.log(dbSchemaProxy)

        addTab(node.data, node)
        
        /* http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: "all_column" } })
          .then((resp2) => {
            dbSchemaProxy.addTable(node.data.label + "_col", resp2.data.data)
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
  } else if (node.data.type === 'conn') {
    connId = node.data.id
  } else {
    connId = findConn(node.parent)
  }
  return connId
}

function saveConnCfg() {
  http.post("/saveConn", connCfg.value)
    .then((resp) => {
      connCfg.value = {}
      conCfgAddDialogVisible.value = false
      ElMessage("添加成功")
    })
}

function listConnCfg() {
  http.get("/listConn2")
    .then((resp) => {
      connCfgList.value = resp.data.data
    })
}

function delConnCfg(id) {
  http.get("/delConn", { params: { id: id } })
    .then((resp) => {
      listConnCfg()
    })
}

function saveTree() {
  http.post("/saveTree", conCfgTreeData.value)
    .then((resp) => {
      treeListDialogVisible.value = false
      ElMessage("保存成功")
    })
}

function listDirTree() {
  http.get("/listDirTree")
    .then((resp) => {
      conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "" }] : resp.data.data
    })
}

const appendTreeNode = (data) => {
  const newChild = { label: "", value: "", children: [] }
  if (!data.children) {
    data.children = []
  }
  data.children.push(newChild)
  conCfgTreeData.value = [...conCfgTreeData.value]
}

const removeTreeNode = (node, data) => {
  const parent = node.parent
  const children = parent.data.children || parent.data
  const index = children.findIndex((d) => d.id === data.id)
  children.splice(index, 1)
  conCfgTreeData.value = [...conCfgTreeData.value]
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

<style lang="less" scoped>
.el-tree-node {
  overflow-x: auto;
}
</style>