<template>
  <div class="classical-layout">
    <el-splitter style="height: 100vh;">
      <el-splitter-panel :collapsible="true" size="300px" :min="300" :max="500">
        <div class="sidebar-panel">
          <div class="sidebar-header">
            <span class="sidebar-title">📂 数据库</span>
            <el-button text size="small" class="sidebar-refresh-btn" @click="refreshTree" title="刷新">
              <el-icon :size="14"><Refresh /></el-icon>
            </el-button>
          </div>
          <div class="sidebar-tree">
            <el-tree ref="connTree" :highlight-current="true" :load="loadTree" :lazy="true" :data="treeData" empty-text=""
              :props="{ isLeaf: 'isLeaf' }" :indent="16">
              <template #default="{ node, data }">
                <div class="tree-node" :class="'tree-node--' + data.type">
                  <span class="tree-node-icon">
                    <span v-if="data.type === 'dir'">📁</span>
                    <span v-else-if="data.type === 'conn'">🔗</span>
                    <span v-else-if="data.type === 'schema'">🗄️</span>
                    <span v-else-if="data.type === 'table'">📋</span>
                    <span v-else-if="data.type === 'view'">👁️</span>
                    <span v-else>📄</span>
                  </span>
                  <span class="tree-node-label" :title="data.data != null ? data.data.text : ''">{{ node.label }}</span>
                  <span class="tree-node-actions">
                    <el-tooltip v-if="data.type === 'schema'" content="表管理" placement="top" :show-after="400">
                      <el-icon :size="14" class="tree-action-icon" @click.stop="openTableManager(node)"><Grid /></el-icon>
                    </el-tooltip>
                    <el-tooltip v-if="data.type === 'table'" content="查看表结构" placement="top" :show-after="400">
                      <el-icon :size="14" class="tree-action-icon" @click.stop="viewTableInfo(node)"><InfoFilled /></el-icon>
                    </el-tooltip>
                    <el-tooltip v-if="data.type === 'table'" content="浏览数据" placement="top" :show-after="400">
                      <el-icon :size="14" class="tree-action-icon" @click.stop="openDataBrowserFromNode(node)"><Document /></el-icon>
                    </el-tooltip>
                    <el-tooltip v-if="data.type === 'view'" content="查看视图" placement="top" :show-after="400">
                      <el-icon :size="14" class="tree-action-icon" @click.stop="viewViewInfo(node)"><View /></el-icon>
                    </el-tooltip>
                  </span>
                </div>
              </template>
            </el-tree>
          </div>
        </div>
      </el-splitter-panel>
      <el-splitter-panel>
        <div class="main-content">
          <el-tabs v-if="!!editableTabsValue" v-model="editableTabsValue" type="card" class="main-tabs" closable
            @tab-remove="removeTab">
            <el-tab-pane v-for="item in editableTabs" :key="item.tabId" :name="item.tabId">
              <template #label>
                <span class="tab-label" :title="item.connName ? item.connName + '/' + item.title : item.title">
                  {{ item.title }}
                </span>
              </template>
              <component :is="item.component" :tabId="item.tabId" :connId="item.connId" :schema="item.schema" :tableName="item.tableName" :dbType="item.dbType" :schemaPath="item.connName ? item.connName + '/' + item.title : item.title" @openDataBrowser="openDataBrowser" @openTableManager="openTableManagerFromChild" />
            </el-tab-pane>
          </el-tabs>
          <div v-else class="empty-workspace">
            <div class="empty-icon">🗄️</div>
            <div class="empty-text">点击左侧数据库展开表结构</div>
          </div>
        </div>
      </el-splitter-panel>
    </el-splitter>

    <!-- 表管理对话框 -->
    <el-dialog v-model="tableMgntDialogVisible" :title="tableMgntTitle"
      @close="tableMgntDialogVisible = false; tableMeta = {}" :draggable="true" destroy-on-close width="1060px"
      class="classical-dialog">
      <TableEditor :tableMeta="tableMeta" @tableDrop="tableMgntDialogVisible = false; tableMeta = {}" />
      <template #footer>
        <el-button @click="tableMgntDialogVisible = false; tableMeta = {}">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 视图查看对话框 -->
    <el-dialog v-model="viewDialogVisible" :title="tableMgntTitle" @close="viewDialogVisible = false; tableMeta = {}"
      :draggable="true" destroy-on-close width="1060px" class="classical-dialog">
      <ViewDialog :tableMeta="tableMeta" />
      <template #footer>
        <el-button @click="viewDialogVisible = false; tableMeta = {}">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 登录对话框 -->
    <el-dialog v-model="loginDialogVisible" @close="loginDialogVisible = false" width="380px" @keyup.enter="login"
      @opened="loginName.focus()" class="login-dialog">
      <template #header>
        <div class="login-header">
          <span class="login-icon">🔐</span>
          <span>登录</span>
        </div>
      </template>
      <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" label-width="80px">
        <el-form-item label="用户名" prop="name">
          <el-input ref="loginName" v-model="loginForm.name" placeholder="请输入用户名" />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input v-model="loginForm.password" type="password" placeholder="请输入密码" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="loginDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="login" :loading="logining">登录</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import http from '@/js/utils/httpProxy.js'
import { dbSchemaProxy } from '@/stores/sql'
import { client, parsers, server } from '@passwordless-id/webauthn'
import { Document, Grid, InfoFilled, Refresh, View } from '@element-plus/icons-vue'
import { onMounted, reactive, ref, shallowRef } from 'vue'
import TableEditor from './comonents/TableEditor.vue'
import ViewDialog from './comonents/ViewDialog.vue'
import DataBrowser from './DataBrowser.vue'
import SQLEditor2 from './SQLEditor2.vue'
import TableManager from './TableManager.vue'

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
const treeLoading = ref(false)

onMounted(() => {
  getSysModel()
  if (!treeLoading.value) {
    refreshTree()
  }
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
      console.error('[ClassicalView] 生物识别注册失败 - code:', resp.data.code)
      ElMessage("注册失败")
    }
  }).catch((error) => {
    console.error('[ClassicalView] 生物识别注册异常:', error)
    ElMessage("注册失败")
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
      console.error('[ClassicalView] token登录失败 - code:', resp.data.code)
      ElMessage("登录失败")
    }
  }).catch((error) => {
    console.error('[ClassicalView] token登录异常:', error)
    ElMessage("登录失败")
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
      console.error('[ClassicalView] bio登录失败 - code:', resp.data.code)
      ElMessage("登录失败")
      loginDialogVisible.value = true
    }
  }).catch((error) => {
    console.error('[ClassicalView] bio登录异常:', error)
    ElMessage("登录失败")
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
  if (treeLoading.value) return
  treeLoading.value = true
  treeData.value = []
  http.get("/showTree", { params: { connId: "", key: "", type: "dir", level: 0 } })
    .then((resp) => {
      treeData.value = resp.data.data
      if (connTree.value) {
        connTree.value.setData(treeData.value)
      }
    })
    .finally(() => {
      treeLoading.value = false
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
.classical-layout {
  height: 100vh;
  overflow: hidden;
  background: #f5f7fa;
}

/* ── Sidebar ── */
.sidebar-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-right: 1px solid #ebeef5;
}

.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px 8px;
  border-bottom: 1px solid #f0f2f5;
}

.sidebar-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
  letter-spacing: 0.5px;
}

.sidebar-refresh-btn {
  color: #909399;
  padding: 4px;
  border-radius: 4px;
}
.sidebar-refresh-btn:hover {
  color: #409eff;
  background: #ecf5ff;
}

.sidebar-tree {
  flex: 1;
  overflow: auto;
  padding: 4px 0;
}

/* Tree node styling */
.tree-node {
  display: flex;
  align-items: center;
  width: 100%;
  padding-right: 4px;
  font-size: 13px;
  line-height: 1.6;
  min-width: 0;
}

.tree-node-icon {
  font-size: 14px;
  margin-right: 6px;
  flex-shrink: 0;
}

.tree-node-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #303133;
}

.tree-node-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-left: 4px;
  opacity: 0;
  transition: opacity 0.15s ease;
  flex-shrink: 0;
}

.tree-node:hover .tree-node-actions {
  opacity: 1;
}

/* Type-specific node styling for visual hierarchy */
.tree-node--dir .tree-node-label {
  font-weight: 600;
  color: #303133;
}

.tree-node--conn .tree-node-label {
  font-weight: 500;
  color: #409eff;
}

.tree-node--schema .tree-node-label {
  color: #303133;
}

.tree-node--table .tree-node-label,
.tree-node--view .tree-node-label {
  color: #606266;
  font-size: 12.5px;
}

.tree-action-icon {
  cursor: pointer;
  color: #909399;
  padding: 2px;
  border-radius: 3px;
  transition: all 0.15s ease;
}

.tree-action-icon:hover {
  color: #409eff;
  background: #ecf5ff;
}

/* ── Main Content ── */
.main-content {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #fff;
}

.main-tabs {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.main-tabs :deep(.el-tabs__header) {
  background: #fafbfc;
  border-bottom: 1px solid #ebeef5;
  padding: 0 8px;
  margin-bottom: 0;
}

.main-tabs :deep(.el-tabs__item) {
  font-size: 13px;
  padding: 0 16px;
  height: 36px;
  line-height: 36px;
  border-radius: 6px 6px 0 0;
  transition: all 0.2s ease;
}

.main-tabs :deep(.el-tabs__item.is-active) {
  background: #fff;
  font-weight: 500;
}

.main-tabs :deep(.el-tabs__content) {
  flex: 1;
  overflow: hidden;
}

.main-tabs :deep(.el-tab-pane) {
  height: 100%;
}

.tab-label {
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  vertical-align: middle;
}

/* ── Empty State ── */
.empty-workspace {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #c0c4cc;
  gap: 16px;
}

.empty-icon {
  font-size: 56px;
  opacity: 0.5;
}

.empty-text {
  font-size: 14px;
}

/* ── Login Dialog ── */
.login-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
}

.login-icon {
  font-size: 20px;
}

/* ── Dialog improvements ── */
.classical-dialog :deep(.el-dialog__body) {
  padding: 12px 20px;
  max-height: 65vh;
  overflow-y: auto;
}
</style>

<style lang="less" scoped>
:deep(.el-tree-node__content) {
  height: 32px;
}
:deep(.el-tree-node__content:hover) {
  background-color: #f0f7ff;
}
:deep(.el-tree-node.is-current > .el-tree-node__content) {
  background-color: #ecf5ff;
}
</style>