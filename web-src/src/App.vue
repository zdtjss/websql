<template>
  <el-container class="layout-container-demo">
    <el-aside :width="treeDivWidth">
      <div>
        <el-icon color="#409EFC" @click="treeListDialogVisible = true; listDirTree()" style="cursor: pointer;"
          title="添加目录">
          <FolderAdd />
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
    <div style="height: 100%; border: 1px solid  gray; cursor: col-resize;" @mousedown="resizeTreeArea"></div>
    <el-container>
      <el-main>
        <el-tabs v-model="editableTabsValue" type="card" class="demo-tabs" closable @tab-remove="removeTab">
          <el-tab-pane v-for="item in editableTabs" :key="item.tabId" :label="item.title" :name="item.tabId">
            <component :is="item.component" :tabId="item.tabId" :connId="item.connId" :schema="item.schema" />
          </el-tab-pane>
        </el-tabs>
      </el-main>
    </el-container>
    <el-dialog v-model="conCfgAddDialogVisible" @close="conCfgAddDialogVisible = false" :draggable="true" width="600px">
      <el-form v-model="connCfg">
        <el-form-item label="连接名称" :label-width="formLabelWidth">
          <el-input v-model="connCfg.name" />
        </el-form-item>
        <el-form-item label="数据库类型" :label-width="formLabelWidth">
          <el-select v-model="connCfg.dbType" placeholder="请选择">
            <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="所属层级" :label-width="formLabelWidth">
          <el-tree-select v-model="connCfg.treeNode" :data="conCfgTreeData" :render-after-expand="false" clearable
            value-key="label" placeholder="请选择" />
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
    <el-dialog v-model="userAddDialogVisible" @close="userAddDialogVisible = false" :draggable="true" width="600px">
      <el-form v-model="user">
        <el-form-item label="姓名" :label-width="formLabelWidth">
          <el-input v-model="user.name" />
        </el-form-item>
        <el-form-item label="登录名" :label-width="formLabelWidth">
          <el-input v-model="user.loginName" />
        </el-form-item>
        <el-form-item label="密码" :label-width="formLabelWidth">
          <el-input type="password" v-model="user.pwd" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="saveConnCfg">保存</el-button>
          <el-button @click="userAddDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <el-dialog v-model="roleAddDialogVisible" @close="roleAddDialogVisible = false" :draggable="true" width="600px">
      <el-form v-model="role">
        <el-form-item label="角色名" :label-width="formLabelWidth">
          <el-input v-model="role.name" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="saveConnCfg">保存</el-button>
          <el-button @click="roleAddDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <el-dialog v-model="conCfgListDialogVisible" @close="conCfgListDialogVisible = false" :draggable="true"
      width="1000px" style="height:650px;">
      <el-tabs v-model="defaultTabAdmin" type="card" style="height:500px;">
        <el-tab-pane label="角色" name="role">
          <el-table :data="connCfgList" :max-height="450" style="width: 100%;overflow-y: auto;"
            @cell-dblclick="(row) => row.editable = true">
            <el-table-column prop="name" label="角色名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="dbType" label="用户">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.dbType }}</span>
                <el-select v-show="scope.row.editable" v-model="scope.row.dbType" placeholder="请选择">
                  <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column prop="treeNode" label="连接">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.treeNode }}</span>
                <el-tree-select v-show="scope.row.editable" v-model="scope.row.treeNode" :data="conCfgTreeData"
                  clearable value-key="label" placeholder="未指定" />
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加"
                  @click="roleAddDialogVisible = true; listDirTree()">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveConnCfg(scope.row); scope.row.editable = false"
                  title="保存" style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delConnCfg(scope.row.id)" confirm-button-text="是"
                  cancel-button-text="否">
                  <template #reference>
                    <el-icon style="cursor: pointer;" title="删除">
                      <Delete />
                    </el-icon>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="用户" name="user">
          <el-form v-model="userQuery">
            <el-row>
              <el-form-item label="姓名" :label-width="formLabelWidth">
                <el-input v-model="userQuery.name" />
              </el-form-item>
              <el-form-item label="登录名" :label-width="formLabelWidth">
                <el-input v-model="userQuery.loginName" />
              </el-form-item>
              <el-form-item>
                <el-button @click="findUser" style="margin-left:12px;">查询</el-button>
              </el-form-item>
            </el-row>
          </el-form>
          <el-table :data="userList" :max-height="450" style="width: 100%;overflow-y: auto;"
            @cell-dblclick="(row) => row.editable = true">
            <el-table-column prop="name" label="姓名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.user" />
                <span v-show="!scope.row.editable">{{ scope.row.user }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="loginName" label="登录名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.user" />
                <span v-show="!scope.row.editable">{{ scope.row.user }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pwd" label="密码" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.pwd" />
                <span v-show="!scope.row.editable">{{ scope.row.pwd }}</span>
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加"
                  @click="userAddDialogVisible = true; listDirTree()">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveConnCfg(scope.row); scope.row.editable = false"
                  title="保存" style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delConnCfg(scope.row.id)" confirm-button-text="是"
                  cancel-button-text="否">
                  <template #reference>
                    <el-icon style="cursor: pointer;" title="删除">
                      <Delete />
                    </el-icon>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="连接" name="conn">
          <el-table :data="connCfgList" :max-height="450" style="width: 100%;overflow-y: auto;"
            @cell-dblclick="(row) => row.editable = true">
            <el-table-column prop="name" label="连接名称" width="120" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <!-- <el-table-column prop="dbType" label="数据库类型" width="120">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.dbType }}</span>
                <el-select v-show="scope.row.editable" v-model="scope.row.dbType" placeholder="请选择">
                  <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
                </el-select>
              </template>
            </el-table-column> -->
            <el-table-column prop="treeNode" label="所属层级" width="130">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.treeNode }}</span>
                <el-tree-select v-show="scope.row.editable" v-model="scope.row.treeNode" :data="conCfgTreeData"
                  clearable value-key="label" placeholder="未指定" />
              </template>
            </el-table-column>
            <el-table-column prop="user" label="用户名" width="150" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.user" />
                <span v-show="!scope.row.editable">{{ scope.row.user }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pwd" label="密码" width="180" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.pwd" />
                <span v-show="!scope.row.editable">{{ scope.row.pwd }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="url" label="连接信息" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.url" type="textarea" />
                <span v-show="!scope.row.editable">{{ scope.row.url }}</span>
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加"
                  @click="conCfgAddDialogVisible = true; listDirTree()">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveConnCfg(scope.row); scope.row.editable = false"
                  title="保存" style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delConnCfg(scope.row.id)" confirm-button-text="是"
                  cancel-button-text="否">
                  <template #reference>
                    <el-icon style="cursor: pointer;" title="删除">
                      <Delete />
                    </el-icon>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
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
import { ref, shallowRef, onMounted } from 'vue'
import SQLEditor2 from './views/SQLEditor2.vue'
import http from './js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'
import dayjs from 'dayjs'

const sqlEditor = shallowRef(SQLEditor2)

const defaultTabAdmin = ref("role")

const editableTabsValue = ref('')
const editableTabs = ref([])

const treeDivWidth = ref("300px")
const resizeTreeAreaFlag = ref(false)

const formLabelWidth = '100px'
const conCfgAddDialogVisible = ref(false)
const conCfgListDialogVisible = ref(false)
const roleAddDialogVisible = ref(false)
const userAddDialogVisible = ref(false)
const userList = ref([])
const roleList = ref([])
const userQuery = ref({
  name: "",
  loginName: ""
})
const role = ref({})
const user = ref({})
const connCfgList = ref([])

const conCfgTreeData = ref([])
const treeListDialogVisible = ref(false)
const dbTypeList = ref([{ label: "MySQL", value: "mysql" }])

const connCfg = ref({ dbType: "mysql" })

onMounted(() => {
  const storedTabs = JSON.parse(localStorage.getItem("editableTabs") || "[]")
  storedTabs.forEach(tab => tab.component = sqlEditor)
  editableTabs.value.push(...storedTabs)
  editableTabsValue.value = localStorage.getItem("editableTabsValue") || ""
})

const addTab = (node) => {
  if (node.data.type !== "schema") {
    return
  }
  const tabId = dayjs().format("YYYYMMDDHHmmssSSS")
  editableTabs.value.push({
    tabId: tabId,
    title: node.data.label,
    connId: findConn(node),
    schema: node.data.label,
    component: sqlEditor,
  })
  editableTabsValue.value = tabId
  restoreTab()
}
const removeTab = (targetName) => {
  const tabs = editableTabs.value
  let activeName = editableTabsValue.value
  if (activeName === targetName) {
    tabs.forEach((tab, index) => {
      if (tab.tabId === targetName) {
        const nextTab = tabs[index + 1] || tabs[index - 1]
        if (nextTab) {
          activeName = nextTab.tabId
        }
      }
    })
  }
  editableTabsValue.value = activeName
  editableTabs.value = tabs.filter((tab) => tab.tabId !== targetName)
  restoreTab()
}

function restoreTab() {
  const waitStoredTabs = JSON.parse(JSON.stringify(editableTabs.value))
  waitStoredTabs.forEach(tab => tab.component = null)
  localStorage.setItem("editableTabs", JSON.stringify(waitStoredTabs))
  localStorage.setItem("editableTabsValue", editableTabsValue.value)
  if (editableTabs.value.length == 0) {
    // 清空可能带来负面清理
    localStorage.clear()
    dbSchemaProxy.cleanCache()
  }
}

function resizeTreeArea(event) {
  const ogiWidth = new Number(treeDivWidth.value.substring(0, treeDivWidth.value.length - 2))
  let deviation = 0
  if (event.clientX > ogiWidth) {
    deviation = event.clientX - ogiWidth
  }
  document.onmousemove = (e) => {
    treeDivWidth.value = (e.clientX - deviation) + "px"
  }
  document.onmouseup = () => {
    document.onmouseup = null
    document.onmousemove = null
  }
}

function mouseUp() {
  resizeTreeAreaFlag.value = false
  document.removeEventListener('mousemove', this.mouseMove)
}

function loadTree(node, resolve) {
  if (node.data.type === 'column') {
    resolve([])
    return
  }
  http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: node.data.type, level: node.level } })
    .then((resp) => {
      if (node.data.type === "schema") {
        dbSchemaProxy.addTable(node.data.label, resp.data.data)

        addTab(node)

        /* http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: "all_column" } })
          .then((resp2) => {
            dbSchemaProxy.addTable(node.data.label + "_col", resp2.data.data)
          }) */
      }
      resolve(resp.data.data)
      node.loaded = false
    })
    .catch((error) => {
      console.log(error);
      node.loading = false
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

function saveConnCfg(row) {
  http.post("/saveConn", row.target ? connCfg.value : row)
    .then((resp) => {
      connCfg.value = {}
      conCfgAddDialogVisible.value = false
      ElMessage("保存成功")
    })
}

function listConnCfg() {
  http.get("/listConn2")
    .then((resp) => {
      connCfgList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
      setTimeout(listDirTree(), 1000)
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

function findUser() {
  if (!userQuery.value.name && !userQuery.value.loginName) {
    ElMessage("请指定查询条件")
    return
  }
  http.get("/findUser", { params: userQuery.value })
    .then((resp) => {
      userList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
    })
}

</script>

<style scoped>
.layout-container-demo {
  /* width: calc(100vw * 0.98); */
  height: calc(100vh * 0.97);
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