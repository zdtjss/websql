<template>
  <el-splitter style="height: calc(100vh - 16px);">
    <el-splitter-panel :collapsible="true" size="15%">
      <el-tree ref="connTree" :highlight-current="true" :load="loadTree" :lazy="true" :data="treeData" empty-text=""
        :props="{ isLeaf: 'isLeaf' }">
        <template #default="{ node, data }">
          <div class="table-node-wrapper">
            <a :title="data.data != null ? data.data.text : ''" :class="data.type">{{ node.label }}</a>
            <i v-if="data.type === 'schema'" class="icon-table-manager icon icon16" title="表管理"
              @click.stop="openTableManager(node)"></i>
            <i v-if="data.type === 'table'" class="icon-view-table icon icon16" title="查看表信息"
              @click.stop="viewTableInfo(node)"></i>
            <i v-if="data.type === 'table'" class="icon-browse-data icon icon16" title="浏览数据"
              @click.stop="openDataBrowserFromNode(node)"></i>
            <i v-if="data.type === 'view'" class="icon-view-table icon icon16" title="查看视图信息"
              @click.stop="viewViewInfo(node)"></i>
          </div>
        </template>
      </el-tree>
    </el-splitter-panel>
    <el-splitter-panel>
      <el-tabs v-if="!!editableTabsValue" v-model="editableTabsValue" type="card" class="demo-tabs" closable
        @tab-remove="removeTab">
        <el-tab-pane v-for="item in editableTabs" :key="item.tabId" :name="item.tabId">
          <template #label>
            <span>
              <span :title="item.connName ? item.connName + '/' + item.title : item.title">{{ item.title }}</span>
            </span>
          </template>
          <component :is="item.component" :tabId="item.tabId" :connId="item.connId" :schema="item.schema" :tableName="item.tableName" :dbType="item.dbType" :schemaPath="item.connName ? item.connName + '/' + item.title : item.title" @openDataBrowser="openDataBrowser" @openTableManager="openTableManagerFromChild" />
        </el-tab-pane>
      </el-tabs>
    </el-splitter-panel>
  </el-splitter>

  <!-- 表管理对话框 -->
  <el-dialog v-model="tableMgntDialogVisible" :title="tableMgntTitle"
    @close="tableMgntDialogVisible = false; tableMeta = {}" :draggable="true" destroy-on-close width="1000px"
    style="height:650px;">
    <TableEditor :tableMeta="tableMeta" @tableDrop="tableMgntDialogVisible = false; tableMeta = {}" />
    <template #footer>
      <div class="dialog-footer" style="position: absolute;right: 15px;bottom: 20px;">
        <el-button @click="tableMgntDialogVisible = false; tableMeta = {}">关闭</el-button>
      </div>
    </template>
  </el-dialog>

  <!-- 视图查看对话框 -->
  <el-dialog v-model="viewDialogVisible" :title="tableMgntTitle" @close="viewDialogVisible = false; tableMeta = {}"
    :draggable="true" destroy-on-close width="1000px" style="height:650px;">
    <ViewDialog :tableMeta="tableMeta" />
    <template #footer>
      <div class="dialog-footer" style="position: absolute;right: 15px;bottom: 20px;">
        <el-button @click="viewDialogVisible = false; tableMeta = {}">关闭</el-button>
      </div>
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
</template>

<script setup>
import { ref, reactive, shallowRef, onMounted } from 'vue'
import { client, parsers, server } from '@passwordless-id/webauthn'
import SQLEditor2 from './SQLEditor2.vue'
import TableEditor from './comonents/TableEditor.vue'
import ViewDialog from './comonents/ViewDialog.vue'
import TableManager from './TableManager.vue'
import DataBrowser from './DataBrowser.vue'
import http from '@/js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'

const showLoginBtn = ref(true)

const sqlEditor = shallowRef(SQLEditor2)
const tableManagerComp = shallowRef(TableManager)
const dataBrowserComp = shallowRef(DataBrowser)

const editableTabsValue = ref('')
const editableTabs = ref([])

const connTree = ref()
const treeData = ref([])

const loginForm = ref({ name: "", password: "" })
const loginDialogVisible = ref(false)
const currentUser = ref({
  id: "",
  name: "",
  isAdmin: false
})
const loginName = ref()
const loginFormRef = ref()
const loginSucc = ref(!!sessionStorage.getItem("authentication"))

const bioLocalStorageKey = "nway_websql_bio_credential_id"

const isRemote = ref(null)

const logining = ref(false)
const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
})

const tableMgntDialogVisible = ref(false)
const viewDialogVisible = ref(false)
const tableMeta = ref({})
const tableMgntTitle = ref("")

onMounted(() => {
  getSysModel()
  refreshTree()
  const storedTabs = JSON.parse(localStorage.getItem("editableTabs") || "[]")
  storedTabs.forEach(tab => {
    if (tab.tabId && tab.tabId.startsWith('tablemgr-')) {
      tab.component = tableManagerComp
    } else if (tab.tabId && tab.tabId.startsWith('databrowser-')) {
      tab.component = dataBrowserComp
    } else {
      tab.component = sqlEditor
    }
  })
  editableTabs.value.push(...storedTabs)
  editableTabsValue.value = localStorage.getItem("editableTabsValue") || ""

  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization
})

const addTab = (node) => {
  if (node.data.type !== "schema") {
    return
  }
  const tabId = Date.now().toString(36)
  const conn = findConn(node)
  editableTabs.value.push({
    tabId: tabId,
    title: node.data.label,
    connId: conn.id,
    connName: conn.label,
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

function loadTree(node, resolve) {

  if ((Object.keys(node.data).length === 0 && !loginSucc.value && isRemote.value) || node.data.type === 'table' || node.data.type === 'view') {
    resolve([])
    return
  }
  const conn = findConn(node)
  http.get("/showTree", { params: { connId: conn.id, key: node.data.type === 'dir' ? node.data.id : node.data.label, type: node.data.type, level: node.level } })
    .then((resp) => {
      if (node.data.type === "schema") {
        dbSchemaProxy.addTable(node.data.label, node.data.data.dbType, resp.data.data)
        addTab(node)
      }
      if (resp.data.data) {
        resolve(resp.data.data.map(e => {
          if (e.type === "table" || e.type === "view") {
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
  let conn = ""
  if (node.level === 0) {
    return conn
  } else if (node.data.type === "conn") {
    conn = node.data
  } else {
    conn = findConn(node.parent)
  }
  return conn
}

async function register() {

  if (!client.isAvailable()) {
    ElMessage({
      showClose: true,
      message: '您的设备不支持生物识别',
      type: 'error',
    })
    return;
  }

  let registration = await client.register({
    challenge: server.randomChallenge(),
    user: { id: currentUser.value.id, name: currentUser.value.name }
  })

  const parsed = parsers.parseRegistration(registration)
  console.log(JSON.stringify(parsed))

  window.localStorage.setItem(bioLocalStorageKey, JSON.stringify({ id: parsed.credential.id, transports: parsed.credential.transports }))

  const params = new URLSearchParams();
  params.append("bioKey", parsed.credential.id);
  http.post("/saveUserBio", params).then((resp) => {
    if (resp.data.code == 200) {
      ElMessage("注册成功")
    } else {
      ElMessage(data.msg)
    }
  }).catch((error) => {
    ElMessage(error)
  });
}

function toLogin() {
  const searchParams = new URLSearchParams(window.location.search);
  const authorization = searchParams.get('authorization');
  if (authorization) {
    loginByToken(authorization)
  } else {
    const credentialId = window.localStorage.getItem(bioLocalStorageKey)
    if (credentialId && client.isAvailable()) {
      loginBio()
    } else {
      loginDialogVisible.value = true
    }
  }
}

function loginByToken(token) {
  const params = new URLSearchParams();
  params.append("key", token);
  params.append("loginType", "token");
  http.post("/login", params).then((resp) => {
    if (resp.data.code == 200) {
      currentUser.value = resp.data.data
      sessionStorage.setItem("authentication", resp.data.data["authentication"])
      refreshTree()
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
    } else {
      ElMessage(data.msg)
    }
  }).catch((error) => {
    ElMessage(error)
  });
}

function login() {
  loginFormRef.value.validate(isValid => {
    if (isValid) {
      logining.value = true
      const params = new URLSearchParams();
      params.append("name", loginForm.value.name);
      params.append("password", loginForm.value.password);
      params.append("loginType", "pwd");
      http.post("/login", params, {
        headers: {
          "Content-Type": "application/x-www-form-urlencoded"
        }
      }).then((resp) => {
        currentUser.value = resp.data.data
        sessionStorage.setItem("authentication", resp.headers.get("authentication"))
        refreshTree()
        loginForm.value = {}
        logining.value = false
        loginSucc.value = true
        loginDialogVisible.value = false
        ElMessage("登陆成功")
      }).finally(() => logining.value = false)
    }
  })
}

async function loginBio() {

  const credential = window.localStorage.getItem(bioLocalStorageKey)
  // 第一个参数指定值，可以简化用户选择的操作
  let authentication = await client.authenticate({
    allowCredentials: credential == null ? [] : [JSON.parse(credential)],
    challenge: server.randomChallenge()
  })

  const authenticationParsed = await parsers.parseAuthentication(authentication);

  const params = new URLSearchParams();
  params.append("key", authenticationParsed.credentialId);
  params.append("loginType", "bio");
  http.post("/login", params).then((resp) => {
    if (resp.data.code == 200) {
      currentUser.value = resp.data.data
      sessionStorage.setItem("authentication", resp.headers.get("authentication"))
      refreshTree()
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
    } else {
      ElMessage(data.msg)
      loginDialogVisible.value = true
    }
  }).catch((error) => {
    ElMessage(error)
  });
}

function logout() {
  http.post("/logout")
    .then((resp) => {
      refreshTree()
      currentUser.value = {}
      loginSucc.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("authentication")
    })
}

function getSysModel() {
  http.get("/sysMode").then((resp) => {
    isRemote.value = resp.data.data.isRemote
    if (!loginSucc.value && isRemote.value) {
      toLogin()
    }
  })
}

function refreshTree() {
  http.get("/showTree", { params: { connId: "", key: "", type: "dir", level: 0 } })
    .then((resp) => {
      treeData.value = resp.data.data
    })
}

// 查看表信息处理函数
function viewTableInfo(node) {
  tableMgntTitle.value = node.label + (node.data.data && node.data.data.text ? "(" + node.data.data.text + ")" : '')
  tableMeta.value = { connId: node.parent.parent.data.id, schema: node.parent.data.label, tableName: node.label }
  tableMgntDialogVisible.value = true
}

function viewViewInfo(node) {
  tableMgntTitle.value = node.label + (node.data.data && node.data.data.text ? "(" + node.data.data.text + ")" : '')
  tableMeta.value = { connId: node.parent.parent.data.id, schema: node.parent.data.label, tableName: node.label }
  viewDialogVisible.value = true
}

function openTableManager(node) {
  const conn = findConn(node)
  const tabId = 'tablemgr-' + conn.id + '-' + node.data.label
  const existing = editableTabs.value.find(t => t.tabId === tabId)
  if (existing) {
    editableTabsValue.value = tabId
    return
  }
  editableTabs.value.push({
    tabId: tabId,
    title: '🗂 表管理 - ' + node.data.label,
    connId: conn.id,
    connName: conn.label,
    schema: node.data.label,
    dbType: node.data.data?.dbType || dbSchemaProxy.getDbType(node.data.label) || '',
    component: tableManagerComp,
  })
  editableTabsValue.value = tabId
  restoreTab()
}

function openDataBrowser({ connId, schema, tableName }) {
  const tabId = 'databrowser-' + connId + '-' + schema + '-' + tableName
  const existing = editableTabs.value.find(t => t.tabId === tabId)
  if (existing) {
    editableTabsValue.value = tabId
    return
  }
  editableTabs.value.push({
    tabId: tabId,
    title: '📋 ' + tableName,
    connId: connId,
    schema: schema,
    tableName: tableName,
    component: dataBrowserComp,
  })
  editableTabsValue.value = tabId
  restoreTab()
}

function openDataBrowserFromNode(node) {
  const connId = node.parent.parent.data.id
  const schema = node.parent.data.label
  const tableName = node.label
  openDataBrowser({ connId, schema, tableName })
}

function openTableManagerFromChild({ connId, schema, schemaPath }) {
  const connName = schemaPath ? schemaPath.split('/')[0] : ''
  const node = {
    level: 2,
    data: {
      label: schema,
      type: 'schema',
      data: {}
    },
    parent: {
      level: 1,
      data: {
        id: connId,
        label: connName,
        type: 'conn'
      }
    }
  }
  openTableManager(node)
}

</script>

<style scoped>
.layout-container-demo {
  /* width: calc(100vw * 0.98); */
  height: calc(100vh);
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

.table-node-wrapper {
  position: relative;
  display: inline-block;
  padding-right: 64px;
}

.icon-view-table {
  background-image: url("@/assets/icon/view_info.svg");
  background-size: 16px 16px;
  background-repeat: no-repeat;
  background-position: center;
  width: 16px;
  height: 16px;
  position: absolute;
  right: -20px;
  top: 50%;
  transform: translateY(-50%);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s ease-in-out;
}

.table-node-wrapper:hover .icon-view-table {
  opacity: 1;
}


.icon-table-manager {
  width: 16px;
  height: 16px;
  position: absolute;
  right: -40px;
  top: 50%;
  transform: translateY(-50%);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s ease-in-out;
  font-style: normal;
  font-size: 12px;
  line-height: 16px;
  text-align: center;
}
.icon-table-manager::after {
  content: '🗂';
}
.table-node-wrapper:hover .icon-table-manager {
  opacity: 1;
}
.icon-table-manager:hover {
  opacity: 0.8 !important;
}
.icon-view-table:hover {
  opacity: 0.8 !important;
}

.icon-view-table:hover {
  opacity: 0.8;
}

.icon-browse-data {
  width: 16px;
  height: 16px;
  position: absolute;
  right: -40px;
  top: 50%;
  transform: translateY(-50%);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s ease-in-out;
  font-style: normal;
  font-size: 12px;
  line-height: 16px;
  text-align: center;
}
.icon-browse-data::after {
  content: '📋';
}
.table-node-wrapper:hover .icon-browse-data {
  opacity: 1;
}
.icon-browse-data:hover {
  opacity: 0.8 !important;
}
</style>

<style lang="less" scoped>
.el-tree-node {
  overflow-x: auto;
}
</style>