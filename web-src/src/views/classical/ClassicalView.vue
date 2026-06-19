<template>
  <div class="classical-layout">
    <el-splitter style="height: 100vh;">
      <el-splitter-panel :collapsible="true" size="300px" :min="300" :max="500">
        <div class="sidebar-panel">
          <div class="sidebar-header">
            <span class="sidebar-title">📂 数据库</span>
            <div class="sidebar-header-actions">
              <el-popover ref="searchPopoverRef" placement="bottom-start" :width="560" trigger="click" :show-arrow="false" :offset="4" popper-class="global-search-popover" @show="onSearchPopoverShow" @hide="onSearchPopoverHide">
                <template #reference>
                  <el-button text size="small" class="theme-toggle-btn" title="全局搜索">
                    <el-icon :size="14"><Search /></el-icon>
                  </el-button>
                </template>
                <GlobalSearchDialog :visible="searchPopoverVisible" :conn-id="searchConnId" :schema="searchSchema" @select="onSearchSelect" />
              </el-popover>
              <el-button text size="small" class="sidebar-refresh-btn" @click="refreshTree" title="刷新">
                <el-icon :size="14"><Refresh /></el-icon>
              </el-button>
              <el-button text size="small" class="theme-toggle-btn" title="AI 对话" @click="openAiChat">
                <el-icon :size="14"><MagicStick /></el-icon>
              </el-button>
              <el-button text size="small" class="theme-toggle-btn" @click="toggleTheme" :title="currentTheme === 'light' ? '切换到深色模式' : '切换到浅色模式'">
                <el-icon :size="14"><component :is="currentTheme === 'light' ? Moon : Sunny" /></el-icon>
              </el-button>
              <el-button v-if="(currentUser.isAdmin || !isRemote) && loginSucc" text size="small" class="theme-toggle-btn" @click="openSystemManagement" title="系统设置">
                <el-icon :size="14"><Setting /></el-icon>
              </el-button>
              <el-button v-if="!loginSucc && showLoginBtn && isRemote" text size="small" class="sidebar-refresh-btn" @click="toLogin" title="登录">
                <el-icon :size="14"><User /></el-icon>
              </el-button>
            </div>
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
                    <template v-if="data.type === 'conn'">
                      <el-tooltip content="服务器状态" placement="top" :show-after="400">
                        <el-icon :size="14" class="tree-action-icon" @click.stop="viewServerStatus(node)"><Monitor /></el-icon>
                      </el-tooltip>
                      <el-tooltip content="实时监控" placement="top" :show-after="400">
                        <el-icon :size="14" class="tree-action-icon" @click.stop="openMonitorPanel(node)"><TrendCharts /></el-icon>
                      </el-tooltip>
                      <el-tooltip content="刷新" placement="top" :show-after="400">
                        <el-icon :size="14" class="tree-action-icon" @click.stop="refreshTree()"><Refresh /></el-icon>
                      </el-tooltip>
                    </template>
                    <template v-if="data.type === 'schema'">
                      <el-tooltip content="SQL编辑器" placement="top" :show-after="400">
                        <el-icon :size="14" class="tree-action-icon" @click.stop="addTab(node)"><ChatLineSquare /></el-icon>
                      </el-tooltip>
                      <el-tooltip content="表管理" placement="top" :show-after="400">
                        <el-icon :size="14" class="tree-action-icon" @click.stop="openTableManager(node)"><Tickets /></el-icon>
                      </el-tooltip>
                      <el-dropdown trigger="hover" @command="(cmd) => handleTreeDropdownAction(node, cmd)">
                        <el-icon :size="14" class="tree-action-icon tree-action-more"><MoreFilled /></el-icon>
                        <template #dropdown>
                          <el-dropdown-menu>
                            <el-dropdown-item command="viewSchemaObjects">⚙️ 数据库对象</el-dropdown-item>
                            <el-dropdown-item command="viewERDiagram">🔗 ER关系图</el-dropdown-item>
                            <el-dropdown-item command="openSyncDialog">🔄 数据同步</el-dropdown-item>
                            <el-dropdown-item command="openBackupDialog">📦 备份恢复</el-dropdown-item>
                            <el-dropdown-item command="openDictDialog">📖 数据字典</el-dropdown-item>
                            <el-dropdown-item command="openCompareDialog" divided>🔍 结构比较</el-dropdown-item>
                          </el-dropdown-menu>
                        </template>
                      </el-dropdown>
                    </template>
                    <template v-if="data.type === 'table'">
                      <el-dropdown trigger="hover" @command="(cmd) => handleTreeDropdownAction(node, cmd)">
                        <el-icon :size="14" class="tree-action-icon tree-action-more"><MoreFilled /></el-icon>
                        <template #dropdown>
                          <el-dropdown-menu>
                            <el-dropdown-item command="viewTableInfo">ℹ️ 查看表结构</el-dropdown-item>
                            <el-dropdown-item command="openDataBrowserFromNode">📄 浏览数据</el-dropdown-item>
                          </el-dropdown-menu>
                        </template>
                      </el-dropdown>
                    </template>
                    <template v-if="data.type === 'view'">
                      <el-dropdown trigger="hover" @command="(cmd) => handleTreeDropdownAction(node, cmd)">
                        <el-icon :size="14" class="tree-action-icon tree-action-more"><MoreFilled /></el-icon>
                        <template #dropdown>
                          <el-dropdown-menu>
                            <el-dropdown-item command="viewViewInfo">👁️ 查看视图</el-dropdown-item>
                          </el-dropdown-menu>
                        </template>
                      </el-dropdown>
                    </template>
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
              <div v-if="item.loading" class="tab-loading">
                <el-icon class="is-loading" :size="24"><Loading /></el-icon>
                <span>正在加载表和字段数据...</span>
              </div>
              <component v-else :is="item.component" :tabId="item.tabId" :connId="item.connId" :schema="item.schema" :tableName="item.tableName" :dbType="item.dbType" :schemaPath="item.connName ? item.connName + '/' + item.title : item.title" @openDataBrowser="openDataBrowser" @openTableManager="openTableManagerFromChild" @viewTableInfo="viewTableInfoFromChild" />
            </el-tab-pane>
          </el-tabs>
          <div v-else class="empty-workspace">
            <div class="empty-icon">🗄️</div>
            <div class="empty-text">展开左侧数据库，点击图标打开功能</div>
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

    <!-- 修改密码对话框 -->
    <el-dialog v-model="changePwdDialogVisible" width="400px" @keyup.enter="submitChangePassword" class="login-dialog">
      <template #header>
        <div class="login-header">
          <span class="login-icon">🔑</span>
          <span>修改密码</span>
        </div>
      </template>
      <el-form ref="changePwdFormRef" :model="changePwdForm" :rules="changePwdRules" label-width="90px">
        <el-form-item label="旧密码" prop="oldPassword">
          <el-input v-model="changePwdForm.oldPassword" type="password" placeholder="请输入旧密码" show-password />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input v-model="changePwdForm.newPassword" type="password" placeholder="请输入新密码（至少6位）" show-password />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input v-model="changePwdForm.confirmPassword" type="password" placeholder="请再次输入新密码" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="changePwdDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitChangePassword" :loading="changingPwd">确认修改</el-button>
      </template>
    </el-dialog>

    <SchemaObjectsDialog
      v-model="schemaObjectsVisible"
      :conn-id="schemaObjectsConnId"
      :schema="schemaObjectsSchema"
    />

    <ServerStatusPanel
      v-model="serverStatusVisible"
      :conn-id="serverStatusConnId"
      :schema="serverStatusSchema"
    />

    <EnhancedMonitorPanel
      v-model:visible="monitorPanelVisible"
      :conn-id="monitorConnId"
      :schema="monitorSchema"
    />

    <DataSyncDialog v-model="syncDialogVisible" :conn-id="syncConnId" :schema="syncSchema" />
    <BackupRestoreDialog v-model="backupDialogVisible" :conn-id="backupConnId" :schema="backupSchema" />
    <DataDictDialog v-model="dictDialogVisible" :conn-id="dictConnId" :schema="dictSchema" />
    <SchemaCompareDialog v-model="compareDialogVisible" :conn-id="compareConnId" :schema="compareSchema" />
  </div>
</template>

<script setup>
import { showTree } from '@/api/conn'
import {
  loginByPassword,
  loginByToken as loginByTokenApi,
  loginByBio as loginByBioApi,
  logout as logoutApi,
  changePassword as changePasswordApi,
  saveUserBio as saveUserBioApi,
  getSysMode,
} from '@/api/auth'
import { getSystemConfig } from '@/api/system'
import { useDbSchemaStore } from '@/stores/dbSchema'
const dbSchemaProxy = useDbSchemaStore()
import { client, parsers, server } from '@passwordless-id/webauthn'
import { ChatLineSquare, Loading, Monitor, MoreFilled, Moon, Refresh, Search, Setting, Sunny, Tickets, TrendCharts } from '@element-plus/icons-vue'
import { MagicStick, User } from '@element-plus/icons-vue'
import { onMounted, reactive, ref, shallowRef, useTemplateRef } from 'vue'
import { useRouter } from 'vue-router'
import { resetDefaultHomepageCache } from '@/router'
import TableEditor from '@/components/data/TableEditor.vue'
import ViewDialog from '@/components/data/ViewDialog.vue'
import DataBrowser from '@/views/data/DataBrowser.vue'
import SQLEditor2 from '@/views/sql-editor/SQLEditor2.vue'
import TableManager from '@/views/data/TableManager.vue'
import SchemaObjectsDialog from '@/components/db-tools/SchemaObjectsDialog.vue'
import ServerStatusPanel from '@/components/db-tools/ServerStatusPanel.vue'
import EnhancedMonitorPanel from '@/components/db-tools/EnhancedMonitorPanel.vue'
import ERDiagramDialog from '@/components/db-tools/ERDiagramDialog.vue'
import DataSyncDialog from '@/components/db-tools/DataSyncDialog.vue'
import BackupRestoreDialog from '@/components/db-tools/BackupRestoreDialog.vue'
import DataDictDialog from '@/components/db-tools/DataDictDialog.vue'
import SchemaCompareDialog from '@/components/db-tools/SchemaCompareDialog.vue'
import GlobalSearchDialog from '@/components/db-tools/GlobalSearchDialog.vue'
import { useTheme } from '@/utils/useTheme.ts'

const router = useRouter()

const showLoginBtn = ref(true)

const sqlEditor = shallowRef(SQLEditor2)
const tableManagerComp = shallowRef(TableManager)
const dataBrowserComp = shallowRef(DataBrowser)
const erDiagramComp = shallowRef(ERDiagramDialog)

const editableTabsValue = ref('')
const editableTabs = ref([])

const connTree = ref()
const treeData = ref([])

const loginForm = ref({ name: "", password: "" })
const loginDialogVisible = ref(false)
function parseCurrentUser() {
  try {
    const stored = sessionStorage.getItem("currentUser")
    return stored ? JSON.parse(stored) : { id: "", name: "", isAdmin: false }
  } catch {
    return { id: "", name: "", isAdmin: false }
  }
}
const currentUser = ref(parseCurrentUser())
const loginName = ref()
const loginFormRef = useTemplateRef('loginFormRef')
const loginSucc = ref(!!sessionStorage.getItem("authentication"))

const bioLocalStorageKey = "nway_websql_bio_credential_id"

const isRemote = ref(sessionStorage.getItem("isRemote") === "true")

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
const schemaObjectsVisible = ref(false)
const schemaObjectsConnId = ref('')
const schemaObjectsSchema = ref('')
const serverStatusVisible = ref(false)
const serverStatusConnId = ref('')
const serverStatusSchema = ref('')
const monitorPanelVisible = ref(false)
const monitorConnId = ref('')
const monitorSchema = ref('')
const syncDialogVisible = ref(false)
const syncConnId = ref('')
const syncSchema = ref('')
const backupDialogVisible = ref(false)
const backupConnId = ref('')
const backupSchema = ref('')
const dictDialogVisible = ref(false)
const dictConnId = ref('')
const dictSchema = ref('')
const compareDialogVisible = ref(false)
const compareConnId = ref('')
const compareSchema = ref('')
const searchPopoverRef = useTemplateRef('searchPopoverRef')
const searchPopoverVisible = ref(false)
const searchConnId = ref('')
const searchSchema = ref('')
const tableMeta = ref({})
const tableMgntTitle = ref("")
const treeLoading = ref(false)

// 修改密码
const changePwdDialogVisible = ref(false)
const changingPwd = ref(false)
const changePwdForm = ref({ oldPassword: '', newPassword: '', confirmPassword: '' })
const changePwdFormRef = useTemplateRef('changePwdFormRef')
const changePwdRules = reactive({
  oldPassword: [{ required: true, message: '请输入旧密码', trigger: 'blur' }],
  newPassword: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    { min: 6, message: '密码长度不能少于6位', trigger: 'blur' },
  ],
  confirmPassword: [
    { required: true, message: '请确认新密码', trigger: 'blur' },
    {
      validator: (rule, value, callback) => {
        if (value !== changePwdForm.value.newPassword) {
          callback(new Error('两次输入的密码不一致'))
        } else {
          callback()
        }
      },
      trigger: 'blur',
    },
  ],
})

function openAiChat() {
  router.push('/ai')
}

function navigateAfterLogin() {
  resetDefaultHomepageCache()
  getSystemConfig().then(resp => {
    if (resp.data && resp.data.data && resp.data.data.defaultHomepage) {
      const homepage = resp.data.data.defaultHomepage
      localStorage.setItem('defaultHomepage', homepage)
      const currentPath = router.currentRoute.value.path
      if (homepage === 'classical' && currentPath !== '/classical') {
        router.push('/classical')
      } else if (homepage === 'ai' && currentPath !== '/ai' && currentPath !== '/') {
        router.push('/ai')
      }
    }
  }).catch(() => {})
}

function openSystemManagement() {
  sessionStorage.setItem('systemManagement_user', JSON.stringify(currentUser.value))
  router.push('/system-management')
}

function submitChangePassword() {
  changePwdFormRef.value.validate(isValid => {
    if (isValid) {
      changingPwd.value = true
      changePasswordApi(changePwdForm.value.oldPassword, changePwdForm.value.newPassword).then((resp) => {
        ElMessage.success('密码修改成功，请重新登录')
        changePwdDialogVisible.value = false
        // 修改密码后自动退出
        logout()
      }).finally(() => {
        changingPwd.value = false
      })
    }
  })
}

const { currentTheme, toggleTheme, initTheme } = useTheme()

onMounted(() => {
  initTheme()
  if (sessionStorage.getItem("isRemote") === null) {
    getSysModel()
  }
  if (!treeLoading.value) {
    refreshTree()
  }
  const storedTabs = JSON.parse(localStorage.getItem("editableTabs") || "[]")
  storedTabs.forEach(tab => {
    if (tab.tabId && tab.tabId.startsWith('tablemgr-')) {
      tab.component = tableManagerComp
    } else if (tab.tabId && tab.tabId.startsWith('databrowser-')) {
      tab.component = dataBrowserComp
    } else if (tab.tabId && tab.tabId.startsWith('erdiagram-')) {
      tab.component = erDiagramComp
    } else {
      tab.component = sqlEditor
    }
  })
  editableTabs.value.push(...storedTabs)
  editableTabsValue.value = localStorage.getItem("editableTabsValue") || ""

  const authorization = new URLSearchParams(window.location.search).get('authorization')
  showLoginBtn.value = !authorization

  // 监听会话过期事件
  window.addEventListener('session-expired', (e) => {
    const msg = e.detail?.message
    if (msg) {
      ElMessage.warning(msg)
    }
    loginSucc.value = false
    currentUser.value = {}
    sessionStorage.removeItem("currentUser")
    if (isRemote.value) {
      toLogin()
    }
  })
})

const addTab = async (node) => {
  if (node.data.type !== "schema") {
    return
  }
  const tabId = Date.now().toString(36)
  const conn = findConn(node)
  const schemaName = node.data.label
  const dbType = node.data.data?.dbType || ''

  // 检查 schema 数据是否已缓存，未缓存则先加载
  const alreadyLoaded = dbSchemaProxy.getTable(schemaName).length > 0

  const tab = {
    tabId: tabId,
    title: schemaName,
    connId: conn.id,
    connName: conn.label,
    schema: schemaName,
    dbType: dbType,
    component: sqlEditor,
    loading: !alreadyLoaded,
  }
  editableTabs.value.push(tab)
  editableTabsValue.value = tabId
  restoreTab()

  if (!alreadyLoaded) {
    try {
      const resp = await showTree({ connId: conn.id, key: schemaName, type: 'schema', level: 2, schema: schemaName })
      if (resp.data.data) {
        dbSchemaProxy.addTable(schemaName, dbType, resp.data.data, conn.id)
      }
    } catch (e) {
      console.error('[ClassicalView] 预加载schema数据失败:', e)
    } finally {
      const t = editableTabs.value.find(t => t.tabId === tabId)
      if (t) t.loading = false
    }
  }
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
  waitStoredTabs.forEach(tab => { tab.component = null; delete tab.loading })
  localStorage.setItem("editableTabs", JSON.stringify(waitStoredTabs))
  localStorage.setItem("editableTabsValue", editableTabsValue.value)
  if (editableTabs.value.length == 0) {
    localStorage.removeItem("editableTabs")
    localStorage.removeItem("editableTabsValue")
    dbSchemaProxy.cleanCache()
  }
}

function loadTree(node, resolve) {

  if (node.level === 0) {
    resolve([])
    return
  }
  if (node.data.type === 'table' || node.data.type === 'view') {
    resolve([])
    return
  }
  const conn = findConn(node)
  const schema = node.data.type === 'table' ? node.parent?.data?.label || '' : ''
  showTree({ connId: conn.id, key: node.data.type === 'dir' ? node.data.id : node.data.label, type: node.data.type, level: node.level, schema })
    .then((resp) => {
      if (node.data.type === "schema") {
        dbSchemaProxy.addTable(node.data.label, node.data.data.dbType, resp.data.data, conn.id)
      }
      if (resp.data.data) {
        resolve(resp.data.data.map(e => {
          if (e.type === "table" || e.type === "view") {
            return Object.assign({ isLeaf: true }, e)
          }
          return e
        }))
      }
    })
    .catch((error) => {
      console.error(error);
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

  window.localStorage.setItem(bioLocalStorageKey, JSON.stringify({ id: parsed.credential.id, transports: parsed.credential.transports }))

  saveUserBioApi(parsed.credential.id).then((resp) => {
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
  loginByTokenApi(token).then((resp) => {
    if (resp.data.code == 200) {
      currentUser.value = resp.data.data
      sessionStorage.setItem("currentUser", JSON.stringify(resp.data.data))
      sessionStorage.setItem("authentication", resp.data.data["authentication"])
      refreshTree()
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
      navigateAfterLogin()
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
      loginByPassword({ name: loginForm.value.name, password: loginForm.value.password }).then((resp) => {
        currentUser.value = resp.data.data
        sessionStorage.setItem("currentUser", JSON.stringify(resp.data.data))
        sessionStorage.setItem("authentication", resp.headers.get("authentication"))
        refreshTree()
        loginForm.value = {}
        logining.value = false
        loginSucc.value = true
        loginDialogVisible.value = false
        ElMessage("登陆成功")
        navigateAfterLogin()
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

  loginByBioApi(authenticationParsed.credentialId).then((resp) => {
    if (resp.data.code == 200) {
      currentUser.value = resp.data.data
      sessionStorage.setItem("currentUser", JSON.stringify(resp.data.data))
      sessionStorage.setItem("authentication", resp.headers.get("authentication"))
      refreshTree()
      loginForm.value = {}
      logining.value = false
      loginSucc.value = true
      loginDialogVisible.value = false
      ElMessage("登陆成功")
      navigateAfterLogin()
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
  logoutApi()
    .then((resp) => {
      refreshTree()
      currentUser.value = {}
      loginSucc.value = false
      ElMessage(resp.data.data)
      sessionStorage.removeItem("currentUser")
      sessionStorage.removeItem("authentication")
    })
}

function getSysModel() {
  getSysMode().then((resp) => {
    isRemote.value = resp.data.data.isRemote
    if (!loginSucc.value && isRemote.value) {
      toLogin()
    }
  })
}

function refreshNode() {
  refreshTree()
}

function refreshTree() {
  if (treeLoading.value) return
  treeLoading.value = true
  treeData.value = []
  showTree({ connId: "", key: "", type: "dir", level: 0 })
    .then((resp) => {
      treeData.value = resp.data.data
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

function viewSchemaObjects(node) {
  const conn = findConn(node)
  schemaObjectsConnId.value = conn.id
  schemaObjectsSchema.value = node.data.label
  schemaObjectsVisible.value = true
}

function viewServerStatus(node) {
  serverStatusConnId.value = node.data.id
  const schemas = node.childNodes || []
  serverStatusSchema.value = schemas.length > 0 ? schemas[0].data.label : ''
  serverStatusVisible.value = true
}

function viewERDiagram(node) {
  const conn = findConn(node)
  const tabId = 'erdiagram-' + conn.id + '-' + node.data.label
  const existing = editableTabs.value.find(t => t.tabId === tabId)
  if (existing) {
    editableTabsValue.value = tabId
    return
  }
  editableTabs.value.push({
    tabId: tabId,
    title: '🔗 ' + node.data.label,
    connId: conn.id,
    connName: conn.label,
    schema: node.data.label,
    dbType: node.data.data?.dbType || '',
    component: erDiagramComp,
  })
  editableTabsValue.value = tabId
  restoreTab()
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

function openDataBrowser({ connId, schema, tableName, dbType }) {
  const tabId = 'databrowser-' + connId + '-' + schema + '-' + tableName
  const existing = editableTabs.value.find(t => t.tabId === tabId)
  if (existing) {
    if (dbType && !existing.dbType) {
      existing.dbType = dbType
    }
    editableTabsValue.value = tabId
    return
  }
  editableTabs.value.push({
    tabId: tabId,
    title: '📋 ' + tableName,
    connId: connId,
    schema: schema,
    tableName: tableName,
    dbType: dbType || dbSchemaProxy.getDbType(schema) || '',
    component: dataBrowserComp,
  })
  editableTabsValue.value = tabId
  restoreTab()
}

function openDataBrowserFromNode(node) {
  const connId = node.parent.parent.data.id
  const schema = node.parent.data.label
  const tableName = node.label
  const dbType = node.data.data?.dbType || dbSchemaProxy.getDbType(schema) || ''
  openDataBrowser({ connId, schema, tableName, dbType })
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

function viewTableInfoFromChild({ connId, schema, tableName }) {
  tableMgntTitle.value = tableName
  tableMeta.value = { connId, schema, tableName }
  tableMgntDialogVisible.value = true
}

function openSyncDialog(node) {
  const conn = findConn(node)
  syncConnId.value = conn.id
  syncSchema.value = node.data.label
  syncDialogVisible.value = true
}

function openMonitorPanel(node) {
  monitorConnId.value = node.data.id
  const schemas = node.childNodes || []
  monitorSchema.value = schemas.length > 0 ? schemas[0].data.label : ''
  monitorPanelVisible.value = true
}

function openBackupDialog(node) {
  const conn = findConn(node)
  backupConnId.value = conn.id
  backupSchema.value = node.data.label
  backupDialogVisible.value = true
}

function openDictDialog(node) {
  const conn = findConn(node)
  dictConnId.value = conn.id
  dictSchema.value = node.data.label
  dictDialogVisible.value = true
}

function openCompareDialog(node) {
  const conn = findConn(node)
  compareConnId.value = conn.id
  compareSchema.value = node.data.label
  compareDialogVisible.value = true
}

function handleTreeDropdownAction(node, command) {
  switch (command) {
    case 'refreshNode': refreshTree(); break
    case 'viewServerStatus': viewServerStatus(node); break
    case 'openMonitorPanel': openMonitorPanel(node); break
    case 'viewSchemaObjects': viewSchemaObjects(node); break
    case 'viewERDiagram': viewERDiagram(node); break
    case 'openSyncDialog': openSyncDialog(node); break
    case 'openBackupDialog': openBackupDialog(node); break
    case 'openDictDialog': openDictDialog(node); break
    case 'openCompareDialog': openCompareDialog(node); break
    case 'viewTableInfo': viewTableInfo(node); break
    case 'openDataBrowserFromNode': openDataBrowserFromNode(node); break
    case 'viewViewInfo': viewViewInfo(node); break
  }
}

function onSearchPopoverShow() {
  searchPopoverVisible.value = true
  const activeTab = editableTabs.value.find(t => t.tabId === editableTabsValue.value)
  if (activeTab) {
    searchConnId.value = activeTab.connId
    searchSchema.value = activeTab.schema
  } else {
    searchConnId.value = ''
    searchSchema.value = ''
  }
}

function onSearchPopoverHide() {
  searchPopoverVisible.value = false
}

function onSearchSelect(obj) {
  searchPopoverRef.value?.hide()
  const connId = obj.connId || searchConnId.value
  const schema = obj.schema || searchSchema.value
  const dbType = obj.dbType || dbSchemaProxy.getDbType(schema) || ''
  if (obj.type === 'table' || obj.type === 'view') {
    const tableName = obj.name.includes('.') ? obj.name.split('.')[0] : obj.name
    openDataBrowser({ connId, schema, tableName, dbType })
  } else if (obj.type === 'column') {
    const parts = obj.name.split('.')
    if (parts.length >= 2) {
      viewTableInfoFromChild({ connId, schema, tableName: parts[0] })
    }
  }
}

</script>

<style scoped>
.classical-layout {
  height: 100vh;
  overflow: hidden;
  background: var(--bg-tertiary);
}

/* ── Sidebar ── */
.sidebar-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-primary);
}

.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px 8px;
  border-bottom: 1px solid var(--border-secondary);
}

.sidebar-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: 0.5px;
}

.sidebar-refresh-btn {
  color: var(--text-tertiary);
  padding: 4px;
  border-radius: 4px;
}
.sidebar-refresh-btn:hover {
  color: var(--accent-color);
  background: var(--bg-inline-bar);
}

.theme-toggle-btn {
  color: var(--text-tertiary);
  padding: 4px;
  border-radius: 4px;
}
.theme-toggle-btn:hover {
  color: var(--accent-color);
  background: var(--bg-inline-bar);
}

.sidebar-header-actions {
  display: flex;
  align-items: center;
}

.sidebar-user-btn {
  color: var(--text-secondary);
  padding: 4px;
  border-radius: 4px;
}
.sidebar-user-btn:hover {
  color: var(--accent-color);
  background: var(--bg-inline-bar);
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
  color: var(--text-primary);
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
  color: var(--text-primary);
}

.tree-node--conn .tree-node-label {
  font-weight: 500;
  color: var(--accent-color);
}

.tree-node--schema .tree-node-label {
  color: var(--text-primary);
}

.tree-node--table .tree-node-label,
.tree-node--view .tree-node-label {
  color: var(--text-secondary);
  font-size: 12.5px;
}

.tree-action-icon {
  cursor: pointer;
  color: var(--text-tertiary);
  padding: 2px;
  border-radius: 3px;
  transition: all 0.15s ease;
}

.tree-action-icon:hover {
  color: var(--accent-color);
  background: var(--bg-inline-bar);
}

/* ── Main Content ── */
.main-content {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-main);
}

.main-tabs {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.main-tabs :deep(.el-tabs__header) {
  background: var(--bg-toolbar);
  border-bottom: 1px solid var(--border-primary);
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
  background: var(--bg-main);
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
  color: var(--text-placeholder);
  gap: 16px;
}

.empty-icon {
  font-size: 56px;
  opacity: 0.5;
}

.empty-text {
  font-size: 14px;
}

/* ── Tab Loading ── */
.tab-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 12px;
  color: var(--text-secondary);
  font-size: 14px;
}

.tab-loading .el-icon {
  color: var(--accent-color);
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
  background-color: var(--tree-node-hover);
}
:deep(.el-tree-node.is-current > .el-tree-node__content) {
  background-color: var(--tree-node-active);
}
</style>

<style>
.global-search-popover {
  padding: 12px 14px !important;
}
.global-search-popover .el-scrollbar {
  border: 1px solid #ebeef5;
  border-radius: 6px;
}
</style>