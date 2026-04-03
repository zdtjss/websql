<template>
  <el-splitter style="height: calc(100vh - 16px);">
    <el-splitter-panel :collapsible="true" size="15%">
      <div style="text-align: right;margin-right: 10px;">
        <el-icon v-show="currentUser.isAdmin || !isRemote" color="#409EFC" @click="cfgDialogVisible = true"
          style="cursor: pointer;margin-left: 8px;" title="配置">
          <Tools />
        </el-icon>
        <div v-if="showLoginBtn" style="display: inline-block;">
          <el-icon v-show="!loginSucc && isRemote" color="#409EFC" @click="toLogin"
            style="cursor: pointer;margin-left: 8px;" title="登录">
            <User />
          </el-icon>
          <el-icon v-show="loginSucc && isRemote && client.isAvailable()" color="#409EFC" @click="register"
            style="cursor: pointer;margin-left: 8px;" title="注册指纹/面容">
            <svg t="1712659928093" class="icon" viewBox="0 0 1024 1024" version="1.1" xmlns="http://www.w3.org/2000/svg"
              p-id="7116" width="200" height="200">
              <path
                d="M218.763636 509.672727a346.763636 297.890909 90 1 0 595.781819 0 346.763636 297.890909 90 1 0-595.781819 0Z"
                fill="#EAF3FF" p-id="7117"></path>
              <path
                d="M991.418182 539.927273c-13.963636 0-23.272727-9.309091-23.272727-23.272728 0-146.618182-72.145455-281.6-195.49091-365.381818-11.636364-6.981818-13.963636-20.945455-6.981818-32.581818 6.981818-11.636364 20.945455-13.963636 32.581818-6.981818 134.981818 90.763636 214.109091 242.036364 214.109091 402.618182 2.327273 16.290909-6.981818 25.6-20.945454 25.6zM200.145455 228.072727c83.781818-95.418182 204.8-148.945455 330.472727-148.945454 58.181818 0 116.363636 11.636364 169.890909 34.909091 11.636364 4.654545 25.6 0 30.254545-11.636364 4.654545-11.636364 0-25.6-11.636363-30.254545-60.509091-25.6-123.345455-37.236364-188.509091-37.236364-139.636364 0-272.290909 60.509091-365.381818 165.236364-9.309091 9.309091-6.981818 23.272727 2.327272 32.581818 4.654545 4.654545 9.309091 4.654545 16.290909 4.654545 4.654545-2.327273 11.636364-4.654545 16.29091-9.309091zM90.763636 516.654545c0-72.145455 18.618182-144.290909 53.527273-209.454545 6.981818-11.636364 2.327273-25.6-9.309091-32.581818-11.636364-6.981818-25.6-2.327273-32.581818 9.309091-39.563636 69.818182-58.181818 151.272727-58.181818 230.4 0 13.963636 9.309091 23.272727 23.272727 23.272727s23.272727-6.981818 23.272727-20.945455z m125.672728-79.127272c37.236364-144.290909 165.236364-242.036364 314.181818-242.036364 146.618182 0 274.618182 100.072727 311.854545 242.036364 2.327273 11.636364 16.290909 20.945455 27.927273 16.290909 11.636364-2.327273 20.945455-16.290909 16.290909-27.927273-41.890909-162.909091-190.836364-274.618182-358.4-274.618182-169.890909 0-316.509091 114.036364-358.4 279.272728-2.327273 11.636364 4.654545 25.6 16.290909 27.927272h4.654546c11.636364-4.654545 23.272727-11.636364 25.6-20.945454z m567.854545 79.127272c0-58.181818-20.945455-114.036364-55.854545-160.581818-6.981818-9.309091-23.272727-11.636364-32.581819-2.327272-9.309091 6.981818-11.636364 23.272727-2.327272 32.581818 30.254545 37.236364 46.545455 83.781818 46.545454 130.327272 0 13.963636 9.309091 23.272727 23.272728 23.272728s20.945455-9.309091 20.945454-23.272728z m-463.127273 0c0-114.036364 93.090909-207.127273 207.127273-207.127272 37.236364 0 72.145455 9.309091 104.727273 27.927272 11.636364 6.981818 25.6 2.327273 32.581818-9.30909 6.981818-11.636364 2.327273-25.6-9.309091-32.581819-39.563636-23.272727-83.781818-34.909091-128-34.909091-139.636364 0-253.672727 114.036364-253.672727 253.672728 0 13.963636 9.309091 23.272727 23.272727 23.272727s23.272727-6.981818 23.272727-20.945455z m346.763637 0c0-76.8-62.836364-139.636364-139.636364-139.636363s-139.636364 62.836364-139.636364 139.636363c0 13.963636 9.309091 23.272727 23.272728 23.272728s23.272727-9.309091 23.272727-23.272728c0-51.2 41.890909-93.090909 93.090909-93.090909s93.090909 41.890909 93.090909 93.090909c0 13.963636 9.309091 23.272727 23.272727 23.272728s23.272727-9.309091 23.272728-23.272728zM83.781818 549.236364c4.654545-13.963636 6.981818-27.927273 6.981818-44.218182 0-13.963636-9.309091-23.272727-23.272727-23.272727s-23.272727 9.309091-23.272727 23.272727c0 9.309091-2.327273 18.618182-4.654546 27.927273-4.654545 11.636364 2.327273 25.6 13.963637 30.254545h6.981818c9.309091 2.327273 18.618182-4.654545 23.272727-13.963636z m719.127273 372.363636c62.836364-130.327273 95.418182-269.963636 95.418182-416.581818 0-13.963636-9.309091-23.272727-23.272728-23.272727s-23.272727 9.309091-23.272727 23.272727c0 139.636364-30.254545 272.290909-90.763636 395.636363-4.654545 11.636364 0 25.6 11.636363 30.254546 2.327273 2.327273 6.981818 2.327273 9.309091 2.327273 9.309091 2.327273 16.290909-2.327273 20.945455-11.636364z m-176.872727 69.818182c102.4-141.963636 155.927273-309.527273 155.927272-486.4 0-13.963636-9.309091-23.272727-23.272727-23.272727s-23.272727 9.309091-23.272727 23.272727c0 165.236364-51.2 323.490909-148.945455 458.472727-6.981818 9.309091-4.654545 25.6 4.654546 32.581818 4.654545 2.327273 9.309091 4.654545 13.963636 4.654546 9.309091 0 16.290909-2.327273 20.945455-9.309091z m-151.272728 4.654545c102.4-109.381818 167.563636-246.690909 188.509091-395.636363 2.327273-11.636364-6.981818-23.272727-20.945454-25.6-11.636364-2.327273-23.272727 6.981818-25.6 20.945454-18.618182 139.636364-79.127273 267.636364-174.545455 370.036364-9.309091 9.309091-9.309091 23.272727 0 32.581818 4.654545 4.654545 9.309091 6.981818 16.290909 6.981818 4.654545-2.327273 11.636364-4.654545 16.290909-9.309091z m-128-37.236363c130.327273-114.036364 207.127273-279.272727 207.127273-453.818182 0-13.963636-9.309091-23.272727-23.272727-23.272727s-23.272727 9.309091-23.272727 23.272727c0 160.581818-69.818182 311.854545-190.836364 418.909091-9.309091 9.309091-11.636364 23.272727-2.327273 32.581818 4.654545 4.654545 11.636364 6.981818 18.618182 6.981818 2.327273 2.327273 9.309091 0 13.963636-4.654545z m-104.727272-65.163637c72.145455-53.527273 125.672727-125.672727 160.581818-207.127272 4.654545-11.636364 0-25.6-11.636364-30.254546-11.636364-4.654545-25.6 0-30.254545 13.963636-30.254545 74.472727-79.127273 139.636364-144.290909 186.181819-9.309091 6.981818-11.636364 23.272727-4.654546 32.581818 4.654545 6.981818 11.636364 9.309091 18.618182 9.309091 2.327273 0 6.981818 0 11.636364-4.654546z m186.181818-293.236363c6.981818-32.581818 9.309091-62.836364 9.309091-95.418182 0-13.963636-9.309091-23.272727-23.272728-23.272727s-23.272727 9.309091-23.272727 23.272727c0 30.254545-2.327273 58.181818-9.309091 86.109091-2.327273 11.636364 4.654545 25.6 18.618182 27.927272h4.654546c9.309091 2.327273 20.945455-6.981818 23.272727-18.618181z m-267.636364 209.454545c79.127273-53.527273 132.654545-134.981818 151.272727-228.072727 2.327273-11.636364-4.654545-25.6-18.618181-27.927273-11.636364-2.327273-25.6 4.654545-27.927273 18.618182-16.290909 81.454545-65.163636 151.272727-132.654546 197.818182-11.636364 6.981818-13.963636 20.945455-6.981818 32.581818 6.981818 6.981818 13.963636 11.636364 23.272728 11.636364 4.654545 0 9.309091-2.327273 11.636363-4.654546z m-53.527273-102.4c62.836364-48.872727 100.072727-121.018182 100.072728-202.472727 0-13.963636-9.309091-23.272727-23.272728-23.272727s-23.272727 9.309091-23.272727 23.272727c0 65.163636-30.254545 125.672727-81.454545 165.236363-9.309091 6.981818-11.636364 23.272727-4.654546 32.581819 4.654545 6.981818 11.636364 9.309091 18.618182 9.309091 4.654545 0 9.309091-2.327273 13.963636-4.654546z"
                fill="#2D85FF" p-id="7118"></path>
            </svg>
          </el-icon>
          <el-icon v-show="loginSucc" color="#409EFC" @click="logout" style="cursor: pointer;margin-left: 8px;"
            title="退出">
            <SwitchButton />
          </el-icon>
        </div>
      </div>
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

  <!-- 配置管理对话框 -->
  <el-dialog v-model="cfgDialogVisible" @close="cfgDialogVisible = false" :draggable="true" width="1000px"
    style="height:650px;">
    <Configuration :isRemote="isRemote" />
    <template #footer>
      <div class="dialog-footer" style="position: absolute;right: 15px;bottom: 20px;">
        <el-button @click="cfgDialogVisible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>

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
import SQLEditor2 from './views/SQLEditor2.vue'
import Configuration from './views/comonents/Configuration.vue'
import TableEditor from './views/comonents/TableEditor.vue'
import ViewDialog from './views/comonents/ViewDialog.vue'
import TableManager from './views/TableManager.vue'
import DataBrowser from './views/DataBrowser.vue'
import http from './js/utils/httpProxy.js'
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

const cfgDialogVisible = ref(false)

const tableMgntDialogVisible = ref(false)
const viewDialogVisible = ref(false)
const tableMeta = ref({})
const tableMgntTitle = ref("")

onMounted(() => {
  getSysModel()
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