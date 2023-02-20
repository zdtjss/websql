<template>
  <el-container class="layout-container-demo">
    <el-aside :width="treeDivWidth">
      <div style="text-align: right;margin-right: 10px;">
        <el-icon v-show="isAdmin" color="#409EFC"
          @click="cfgDialogVisible = true; loadCfgData({ props: { name: 'role' } })"
          style="cursor: pointer;margin-left: 8px;" title="配置">
          <Tools />
        </el-icon>
        <el-icon v-show="!loginSucc" color="#409EFC" @click="loginDialogVisible = true"
          style="cursor: pointer;margin-left: 8px;" title="登录">
          <User />
        </el-icon>
        <el-icon v-show="loginSucc" color="#409EFC" @click="logout" style="cursor: pointer;margin-left: 8px;"
          title="退出">
          <SwitchButton />
        </el-icon>
      </div>
      <el-tree ref="connTree" :highlight-current="true" :load="loadTree" :lazy="true" :data="treeData" empty-text=""
        :props="{ isLeaf: 'isLeaf' }">
        <template #default="{ node, data }">
          <span>
            <a :title="data.data != null ? data.data.text : ''">{{ node.label }}</a>
          </span>
        </template>
      </el-tree>
    </el-aside>
    <div style="height: 100%; border: 1px solid #9e9e9e; cursor: col-resize;" @mousedown="resizeTreeArea"></div>
    <el-container>
      <el-main>
        <el-tabs v-model="editableTabsValue" type="card" class="demo-tabs" closable @tab-remove="removeTab">
          <el-tab-pane v-for="item in editableTabs" :key="item.tabId" :label="item.title" :name="item.tabId">
            <component :is="item.component" :tabId="item.tabId" :connId="item.connId" :schema="item.schema" />
          </el-tab-pane>
        </el-tabs>
      </el-main>
    </el-container>
    <el-dialog v-model="cfgDialogVisible" @close="cfgDialogVisible = false" :draggable="true" width="1000px"
      style="height:650px;">
      <el-tabs v-model="defaultTabAdmin" type="card" style="height:500px;" @tab-click="loadCfgData">
        <el-tab-pane label="角色" name="role">
          <el-table :data="roleList" :max-height="450" style="width: 100%;overflow-y: auto;"
            @cell-dblclick="roleDblClick">
            <el-table-column prop="name" label="角色名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column label="连接" :show-overflow-tooltip="true">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.connNameListStr }}</span>
                <el-tree-select ref="roleConnTree" v-show="scope.row.editable" v-model="scope.row.connIdList"
                  style="width:100%" :data="connListSelect" node-key="id" multiple collapse-tags collapse-tags-tooltip
                  placeholder="请选择" :render-after-expand="false" :check-on-click-node="true" show-checkbox
                  :check-strictly="false" />
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加" @click="addRole">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveRole(scope.row); scope.row.editable = false" title="保存"
                  style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delRole(scope.row)" confirm-button-text="是"
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
          <el-table :data="userList" :max-height="450" style="width: 100%;overflow-y: auto;" empty-text="请正确输入条件后查询"
            @cell-dblclick="(row) => row.editable = true">
            <el-table-column prop="name" label="姓名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="loginName" label="登录名" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.loginName" />
                <span v-show="!scope.row.editable">{{ scope.row.loginName }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pwd" label="密码" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.pwd" />
                <span v-show="!scope.row.editable">{{ scope.row.pwd }}</span>
              </template>
            </el-table-column>
            <el-table-column label="角色" :show-overflow-tooltip="true">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.roleName.join("、") }}</span>
                <el-select v-show="scope.row.editable" v-model="scope.row.roleId" multiple filterable collapse-tags
                  collapse-tags-tooltip placeholder="请选择">
                  <el-option v-for="item in roleList" :key="item.id" :label="item.name" :value="item.id" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column style="text-align: center; " width="80">
              <template #header>
                <span>操作</span>
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加" @click="addUser">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveUser(scope.row); scope.row.editable = false" title="保存"
                  style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delUser(scope.row)" confirm-button-text="是"
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
          <el-table :data="connList" :max-height="450" style="width: 100%;overflow-y: auto;"
            @cell-dblclick="(row) => row.editable = true">
            <el-table-column prop="name" label="连接名称" width="150" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="dbType" label="数据库类型" width="100">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{
                  dbTypeList.filter(t => t.value === scope.row.dbType)[0].label
                }}</span>
                <el-select v-show="scope.row.editable" v-model="scope.row.dbType" placeholder="请选择">
                  <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column prop="parentId" label="所属层级" width="130">
              <template #default="scope">
                <span v-show="!scope.row.editable">{{ scope.row.parentName }}</span>
                <el-tree-select v-show="scope.row.editable" v-model="scope.row.parentId" :data="conCfgTreeData"
                  clearable value-key="id" placeholder="未指定" />
              </template>
            </el-table-column>
            <el-table-column prop="user" label="用户名" width="150" :show-overflow-tooltip="true">
              <template #default="scope">
                <el-input v-show="scope.row.editable" v-model="scope.row.user" />
                <span v-show="!scope.row.editable">{{ scope.row.user }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="pwd" label="密码" width="120" :show-overflow-tooltip="true">
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
                <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加" @click="addConn">
                  <Plus />
                </el-icon>
              </template>
              <template #default="scope">
                <el-icon v-show="scope.row.editable" @click="saveConnCfg(scope.row); scope.row.editable = false"
                  title="保存" style="margin-right:5px;cursor: pointer;">
                  <Select />
                </el-icon>
                <el-popconfirm title="确定要删除?" @confirm="delConnCfg(scope.row)" confirm-button-text="是"
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
        <el-tab-pane label="目录" name="dir">
          <div style="padding: 65px 200px;">
            <el-tree :data="conCfgTreeData" draggable default-expand-all :expand-on-click-node="false">
              <template #default="{ node, data }">
                <div style="width:100%;">
                  <div style="display: inline-block;width: 100%;">
                    <el-input v-model="data.label"></el-input>
                  </div>
                  <div style="margin-left: 30px;display: inline-block;">
                    <a @click="appendTreeNode(data)">添加</a>
                    <el-popconfirm title="确定要删除?" @confirm="removeDir(node, data)" confirm-button-text="是"
                      cancel-button-text="否">
                      <template #reference>
                        <a style="margin-left: 8px">删除</a>
                      </template>
                    </el-popconfirm>
                  </div>
                </div>
              </template>
            </el-tree>
          </div>
          <div style="float: right; margin-right: 100px;">
              <el-button type="primary" @click="saveTree">保存</el-button>
            </div>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="cfgDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
    <!-- 登录对话框 -->
    <el-dialog v-model="loginDialogVisible" @close="loginDialogVisible = false" width="350px" @keyup.enter="login"
      @opened="loginName.focus()">
      <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" label-width="80px">
        <el-form-item label="用户名" prop="name">
          <el-input ref="loginName" v-model="loginForm.name" />
        </el-form-item>
        <el-form-item label="密&nbsp;&nbsp;&nbsp;码" prop="password">
          <el-input v-model="loginForm.password" type="password" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button type="primary" @click="login" :loading="logining">登录</el-button>
          <el-button @click="loginDialogVisible = false">关闭</el-button>
        </span>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup>
import { ref, reactive, shallowRef, onMounted } from 'vue'
import SQLEditor2 from './views/SQLEditor2.vue'
import http from './js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'
import dayjs from 'dayjs'

const sqlEditor = shallowRef(SQLEditor2)

const defaultTabAdmin = ref("role")

const editableTabsValue = ref('')
const editableTabs = ref([])

const connTree = ref()
const treeData = ref([])
const treeDivWidth = ref("260px")

const loginForm = ref({ name: "", password: "" })
const loginDialogVisible = ref(false)
const isAdmin = ref(false)
const loginName = ref()
const loginFormRef = ref()
const loginSucc = ref(!!sessionStorage.getItem("authentication"))

const logining = ref(false)
const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

const formLabelWidth = '100px'
const cfgDialogVisible = ref(false)
const userList = ref([])
const connListSelect = ref([])
const roleList = ref([])
const userQuery = ref({
  name: "",
  loginName: ""
})
const role = ref({})
const user = ref({})
const connList = ref([])
const roleConnTree = ref()
const conCfgTreeData = ref([])
const dbTypeList = ref([{ label: "MySQL", value: "mysql" }, { label: "Oracle", value: "oracle" }])

const conn = ref({ dbType: "mysql" })

onMounted(() => {
  const storedTabs = JSON.parse(localStorage.getItem("editableTabs") || "[]")
  storedTabs.forEach(tab => tab.component = sqlEditor)
  editableTabs.value.push(...storedTabs)
  editableTabsValue.value = localStorage.getItem("editableTabsValue") || ""
  if (!loginSucc.value) {
    loginDialogVisible.value = true
  }
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
  const startX = event.clientX
  const ogiWidth = new Number(treeDivWidth.value.substring(0, treeDivWidth.value.length - 2))
  document.onmousemove = (e) => {
    treeDivWidth.value = (ogiWidth + e.clientX - startX) + "px"
  }
  document.onmouseup = () => {
    document.onmouseup = null
    document.onmousemove = null
  }
}

function loadTree(node, resolve) {
  if ((Object.keys(node.data).length === 0 && !loginSucc.value) || node.data.type === 'column') {
    resolve([])
    return
  }
  http.get("/showTree", { params: { connId: findConn(node), key: node.data.type === 'dir' ? node.data.id : node.data.label, type: node.data.type, level: node.level } })
    .then((resp) => {
      if (node.data.type === "schema") {
        dbSchemaProxy.addTable(node.data.label, node.data.data.dbType, resp.data.data)

        addTab(node)

        /* http.get("/showTree", { params: { connId: findConn(node), key: node.data.label, type: "all_column" } })
          .then((resp2) => {
            dbSchemaProxy.addTable(node.data.label + "_col", resp2.data.data)
          }) */
      }
      if (resp.data.data) {
        resolve(resp.data.data.map(e => {
          if (e.type === "column") {
            return Object.assign({ isLeaf: true }, e)
          }
          return e
        }))
      }
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
  } else if (node.data.type === "conn") {
    connId = node.data.id
  } else {
    connId = findConn(node.parent)
  }
  return connId
}

function addConn() {
  connList.value.push({ dbType: "mysql", editable: true })
}

function saveConnCfg(row) {
  http.post("/saveConn", row)
    .then((resp) => {
      row.editable = false
      ElMessage("保存成功")
    })
}

function listConnCfg() {
  http.get("/listConn2")
    .then((resp) => {
      connList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
      setTimeout(listDirTree(), 1000)
    })
}

function delConnCfg(row) {
  if (row.id) {
    http.get("/delConn", { params: { id: row.id } })
      .then((resp) => {
        listConnCfg()
      })
  } else {
    connList.value = connList.value.filter(item => item != row)
  }
}

function login() {
  loginFormRef.value.validate(isValid => {
    if (isValid) {
      logining.value = true
      const params = new URLSearchParams();
      params.append("name", loginForm.value.name);
      params.append("password", loginForm.value.password);
      http.post("/login", params)
        .then((resp) => {
          refreshTree()
          isAdmin.value = resp.data.data.isAdmin
          sessionStorage.setItem("authentication", resp.headers.get("authentication"))
          loginForm.value = {}
          logining.value = false
          loginSucc.value = true
          loginDialogVisible.value = false
          ElMessage("登陆成功")
        }).finally(() => logining.value = false)
    }
  })
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      refreshTree()
      isAdmin.value = false
      loginSucc.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
    })
}

function refreshTree() {
  http.get("/showTree", { params: { connId: "", key: "", type: "dir", level: 0 } })
    .then((resp) => {
      treeData.value = resp.data.data
    })
}

function addUser() {
  userList.value.push({ "roleId": [], "roleName": [], "loginName": "", "name": "", "pwd": "", editable: true })
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

function saveUser(row) {
  http.post("/saveUser", row)
    .then((resp) => {
      row.editable = false
      ElMessage("保存成功")
      row.roleName = row.roleId.map((val) => roleList.value.filter(item => item.id === val)[0].name)
    })
}

function delUser(row) {
  debugger
  if (row.id) {
    http.get("/delUser", { params: { id: row.id } })
      .then((resp) => {
        findUser()
      })
  } else {
    userList.value = userList.value.filter(item => item != row)
  }
}

function loadCfgData(pane) {
  if (pane.props.name === "role") {
    http.get("/roleList")
      .then((resp) => {
        roleList.value = resp.data.data.map(e => {
          const row = Object.assign({ editable: false }, e)
          if (row.powerList) {
            row.connNameListStr = row.powerList.map(r => r.connName).join("、")
          }
          return row
        })
      })
  } else if (pane.props.name === "user") {

  } else if (pane.props.name === "conn") {
    http.get("/listConn2")
      .then((resp) => {
        connList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
        setTimeout(listDirTree(), 1000)
      })
  } else if (pane.props.name === "dir") {
    listDirTree()
  }
}

function addRole() {
  roleList.value.push({ editable: true })
  roleDblClick({})
}

function roleDblClick(row) {
  row.editable = true
  if (row.powerList) {
    row.connIdList = row.powerList.map(r => r.connId)
  }
  http.get("/connBaseTree")
    .then((resp) => {
      connListSelect.value = resp.data.data
    })
}

function saveRole(row) {
  const param = Object.assign({}, row)
  param.connIdList = []
  roleConnTree.value.getCheckedNodes(false, true).forEach((val) => param.connIdList.push(val.id))
  http.post("/saveRole", param)
    .then((resp) => {
      loadCfgData({ props: { name: 'role' } })
      ElMessage("保存成功")
    })
}

function delRole(row) {
  if (row.id) {
    http.get("/delRole", { params: { id: row.id } })
      .then((resp) => {
        loadCfgData({ props: { name: 'role' } })
      })
  } else {
    roleList.value = roleList.value.filter(item => item != row)
  }
}

function saveTree() {
  http.post("/saveTree", conCfgTreeData.value)
    .then((resp) => {
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
  conCfgTreeData.value.push(newChild)
}

const removeDir = (node, data) => {
  conCfgTreeData.value = conCfgTreeData.value.filter(item => item != data)
  if (data.id) {
    http.get("/delTreeNode", { params: { id: data.id } })
      .then((resp) => {
        conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "" }] : resp.data.data
      })
  }
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