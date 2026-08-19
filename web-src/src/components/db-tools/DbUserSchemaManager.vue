<template>
  <el-dialog :model-value="modelValue" width="880px" :draggable="true" align-center
    :close-on-click-modal="false" destroy-on-close class="classical-dialog"
    @update:model-value="v => emit('update:modelValue', v)" @open="onOpen">
    <template #header>
      <div class="main-header">
        <el-icon class="main-header-icon"><Coin /></el-icon>
        <span class="main-header-title">库与用户管理</span>
        <el-tag v-if="connName" size="small" effect="plain" round class="conn-tag">{{ connName }}</el-tag>
      </div>
    </template>

    <el-tabs v-model="activeTab">
      <!-- 库/Schema 管理 -->
      <el-tab-pane name="schema">
        <template #label>
          <span class="tab-label">
            <el-icon><Coin /></el-icon>
            <span>库/Schema</span>
            <em v-if="schemaList.length" class="tab-count">{{ schemaList.length }}</em>
          </span>
        </template>
        <div class="tab-toolbar">
          <el-button type="primary" size="small" @click="openSchemaCreate">
            <el-icon><Plus /></el-icon><span>新建库</span>
          </el-button>
          <el-button size="small" :loading="schemasLoading" @click="loadSchemas">
            <el-icon v-if="!schemasLoading"><Refresh /></el-icon><span>刷新</span>
          </el-button>
          <span class="toolbar-tip">
            <el-icon><InfoFilled /></el-icon>{{ schemaTip }}
          </span>
        </div>
        <el-table :data="schemaList" v-loading="schemasLoading" height="380" size="small" stripe>
          <el-table-column type="index" label="#" width="50" align="center" />
          <el-table-column prop="name" label="名称" min-width="240" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="name-cell">
                <el-icon class="name-icon"><Coin /></el-icon>
                <span>{{ row.name }}</span>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100" align="center">
            <template #default="{ row }">
              <el-popconfirm :title="`确定删除 ${row.name} 吗？` + dropSchemaConfirmSuffix"
                :icon="WarningFilled" icon-color="#E6A23C" :width="280"
                confirm-button-text="删除" confirm-button-type="danger"
                @confirm="onDropSchema(row)">
                <template #reference>
                  <el-button type="danger" link size="small" :loading="droppingSchema === row.name">删除</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="schemasLoading ? '正在加载...' : '暂无库'" :image-size="60" />
          </template>
        </el-table>
      </el-tab-pane>

      <!-- 数据库用户管理 -->
      <el-tab-pane name="user">
        <template #label>
          <span class="tab-label">
            <el-icon><User /></el-icon>
            <span>数据库用户</span>
            <em v-if="userList.length" class="tab-count">{{ userList.length }}</em>
          </span>
        </template>
        <div class="tab-toolbar">
          <el-button type="primary" size="small" @click="openUserCreate">
            <el-icon><Plus /></el-icon><span>新建用户</span>
          </el-button>
          <el-button size="small" :loading="usersLoading" @click="loadUsers">
            <el-icon v-if="!usersLoading"><Refresh /></el-icon><span>刷新</span>
          </el-button>
          <span class="toolbar-tip">
            <el-icon><InfoFilled /></el-icon>{{ userTip }}
          </span>
        </div>
        <el-alert v-if="userRestricted" type="warning" :closable="false" show-icon class="restricted-alert"
          title="当前连接账号无 mysql.user 系统表查询权限，仅显示当前连接账号"
          description="如需查看完整用户列表，请为连接账号授予相应权限或使用管理员账号连接" />
        <el-table :data="userList" v-loading="usersLoading" height="380" size="small" stripe>
          <el-table-column type="index" label="#" width="50" align="center" />
          <el-table-column prop="username" label="用户名" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="name-cell">
                <el-icon class="name-icon user"><User /></el-icon>
                <span>{{ row.username }}</span>
              </span>
            </template>
          </el-table-column>
          <el-table-column v-if="isMysql" prop="host" label="主机" width="170">
            <template #default="{ row }">
              <el-tag size="small" effect="plain">{{ row.host }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="210" align="center">
            <template #default="{ row }">
              <el-button type="primary" link size="small" @click="openPrivilege(row)">
                <el-icon><Lock /></el-icon><span>权限</span>
              </el-button>
              <el-button type="primary" link size="small" @click="openResetPwd(row)">
                <el-icon><Key /></el-icon><span>重置密码</span>
              </el-button>
              <el-popconfirm :title="`确定删除用户 ${userLabel(row)} 吗？` + dropUserConfirmSuffix"
                :icon="WarningFilled" icon-color="#E6A23C" :width="280"
                confirm-button-text="删除" confirm-button-type="danger"
                @confirm="onDropUser(row)">
                <template #reference>
                  <el-button type="danger" link size="small" :loading="droppingUser === userLabel(row)">删除</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="usersLoading ? '正在加载...' : '暂无用户'" :image-size="60" />
          </template>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">关闭</el-button>
    </template>

    <!-- 新建库对话框 -->
    <el-dialog v-model="schemaDlgVisible" width="500px" :draggable="true" append-to-body destroy-on-close
      :close-on-click-modal="false" @close="resetSchemaForm" @opened="() => schemaInputRef?.focus()">
      <template #header>
        <div class="dlg-header">
          <el-icon class="dlg-header-icon primary"><FolderAdd /></el-icon>
          <span>{{ isOracle ? '新建 Schema' : '新建库' }}</span>
        </div>
      </template>
      <el-form ref="schemaFormRef" :model="schemaForm" :rules="schemaRules" label-position="top" class="dlg-form">
        <el-form-item :label="isOracle ? 'Schema 名' : '库名'" prop="schema">
          <el-input ref="schemaInputRef" v-model="schemaForm.schema"
            :placeholder="isOracle ? '请输入 schema 名' : '请输入库名'" clearable />
        </el-form-item>
        <template v-if="isOracle">
          <el-form-item label="密码" prop="password">
            <el-input v-model="schemaForm.password" type="password" placeholder="Oracle schema 关联用户密码，6-128 位" show-password />
          </el-form-item>
          <el-form-item label="确认密码" prop="confirmPassword">
            <el-input v-model="schemaForm.confirmPassword" type="password" placeholder="请再次输入密码" show-password />
          </el-form-item>
          <el-form-item label="表空间">
            <el-input v-model="schemaForm.tableSpace" placeholder="默认 users" clearable />
          </el-form-item>
          <div class="form-tip">
            <el-icon><InfoFilled /></el-icon>
            <span>Oracle 中 schema 与用户一一对应，创建 schema 即创建同名用户</span>
          </div>
        </template>
        <template v-else>
          <el-form-item label="字符集">
            <el-select v-model="schemaForm.charset" placeholder="默认" clearable filterable allow-create>
              <el-option v-for="cs in charsetOptions" :key="cs" :label="cs" :value="cs" />
            </el-select>
          </el-form-item>
          <el-form-item label="排序规则">
            <el-select v-model="schemaForm.collation" placeholder="默认" clearable filterable allow-create>
              <el-option v-for="col in collationOptions" :key="col" :label="col" :value="col" />
            </el-select>
          </el-form-item>
          <div class="form-tip">
            <el-icon><InfoFilled /></el-icon>
            <span>字符集与排序规则留空时使用服务端默认值</span>
          </div>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="schemaDlgVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onCreateSchema">
          <el-icon><Check /></el-icon><span>创建</span>
        </el-button>
      </template>
    </el-dialog>

    <!-- 新建用户对话框 -->
    <el-dialog v-model="userDlgVisible" width="500px" :draggable="true" append-to-body destroy-on-close
      :close-on-click-modal="false" @close="resetUserForm" @opened="() => userInputRef?.focus()">
      <template #header>
        <div class="dlg-header">
          <el-icon class="dlg-header-icon primary"><UserFilled /></el-icon>
          <span>新建用户</span>
        </div>
      </template>
      <el-form ref="userFormRef" :model="userForm" :rules="userRules" label-position="top" class="dlg-form">
        <el-form-item label="用户名" prop="username">
          <el-input ref="userInputRef" v-model="userForm.username" placeholder="请输入用户名" clearable />
        </el-form-item>
        <el-form-item v-if="isMysql" label="主机" prop="host">
          <el-select v-model="userForm.host" filterable allow-create placeholder="选择或输入主机模式">
            <el-option v-for="h in hostOptions" :key="h" :label="h" :value="h" />
          </el-select>
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input v-model="userForm.password" type="password" placeholder="6-128 位，不含引号和反斜杠" show-password />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input v-model="userForm.confirmPassword" type="password" placeholder="请再次输入密码" show-password />
        </el-form-item>
        <div v-if="isMysql" class="form-tip">
          <el-icon><InfoFilled /></el-icon>
          <span>主机常用 %（任意主机）、localhost（本机），也可输入具体 IP</span>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="userDlgVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onCreateUser">
          <el-icon><Check /></el-icon><span>创建</span>
        </el-button>
      </template>
    </el-dialog>

    <!-- 重置换密码对话框 -->
    <el-dialog v-model="resetDlgVisible" width="480px" :draggable="true" append-to-body destroy-on-close
      :close-on-click-modal="false" @close="resetPwdForm" @opened="() => pwdInputRef?.focus()">
      <template #header>
        <div class="dlg-header">
          <el-icon class="dlg-header-icon warning"><Key /></el-icon>
          <span>重置密码</span>
        </div>
      </template>
      <el-form ref="pwdFormRef" :model="pwdForm" :rules="pwdRules" label-position="top" class="dlg-form">
        <el-form-item label="用户">
          <el-tag effect="plain" class="target-tag">{{ userLabel(resetTarget) }}</el-tag>
        </el-form-item>
        <el-form-item label="新密码" prop="password">
          <el-input ref="pwdInputRef" v-model="pwdForm.password" type="password" placeholder="6-128 位，不含引号和反斜杠" show-password />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input v-model="pwdForm.confirmPassword" type="password" placeholder="请再次输入新密码" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resetDlgVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onResetPwd">确认重置</el-button>
      </template>
    </el-dialog>

    <!-- 用户权限管理对话框 -->
    <el-dialog v-model="privDlgVisible" width="700px" :draggable="true" append-to-body destroy-on-close
      :close-on-click-modal="false" @close="resetPrivForm">
      <template #header>
        <div class="dlg-header">
          <el-icon class="dlg-header-icon primary"><Lock /></el-icon>
          <span>权限管理</span>
        </div>
      </template>
      <el-form label-position="top" class="dlg-form">
        <el-form-item label="用户">
          <el-tag effect="plain" class="target-tag">{{ userLabel(privTarget) }}</el-tag>
        </el-form-item>
      </el-form>
      <div class="priv-section">
        <div class="priv-section-title">当前权限</div>
        <el-table :data="privList" v-loading="privLoading" size="small" max-height="250" stripe>
          <el-table-column prop="privilege" label="权限" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">
              <el-tag size="small" :type="row.object === '[ROLE]' ? 'success' : ''">{{ row.privilege }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="object" label="对象" min-width="140" show-overflow-tooltip />
          <el-table-column prop="grantOption" label="可转授" width="80" align="center">
            <template #default="{ row }">
              <el-icon v-if="row.grantOption" class="grant-option-yes"><Check /></el-icon>
              <span v-else class="grant-option-no">-</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="70" align="center">
            <template #default="{ row }">
              <el-popconfirm :title="`确定撤销 ${row.privilege}${row.object ? ' ON ' + row.object : ''} 吗？`"
                :icon="WarningFilled" icon-color="#E6A23C" :width="280"
                confirm-button-text="撤销" confirm-button-type="danger"
                @confirm="onRevokePriv(row)">
                <template #reference>
                  <el-button type="danger" link size="small" :loading="revokingPriv === privKey(row)">撤销</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="privLoading ? '正在加载...' : '暂无权限'" :image-size="50" />
          </template>
        </el-table>
      </div>
      <div class="priv-section">
        <div class="priv-section-title">授予权限</div>
        <div class="grant-row">
          <el-select v-model="grantForm.privileges" multiple :placeholder="isOracle ? '选择系统权限或角色' : '选择权限类型'"
            style="width: 220px" filterable allow-create>
            <el-option v-for="p in privilegeOptions" :key="p" :label="p" :value="p" />
          </el-select>
          <el-input v-model="grantForm.object" :placeholder="objectPlaceholder" style="width: 220px" clearable />
          <el-checkbox v-model="grantForm.grantOption">{{ isOracle ? 'WITH ADMIN OPTION' : 'WITH GRANT OPTION' }}</el-checkbox>
          <el-button type="primary" size="small" :loading="grantLoading" @click="onGrantPriv">
            <el-icon><Plus /></el-icon><span>授予</span>
          </el-button>
        </div>
        <div class="form-tip">
          <el-icon><InfoFilled /></el-icon>
          <span>{{ grantTip }}</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="privDlgVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Coin, User, UserFilled, Plus, Refresh, InfoFilled, WarningFilled, Key, FolderAdd, Check, Lock } from '@element-plus/icons-vue'
import {
  listDbUsers,
  createDbUser,
  resetDbUserPassword,
  dropDbUser,
  listAdminSchemas,
  createDbSchema,
  dropDbSchema,
  listDbUserPrivileges,
  grantDbUserPrivilege,
  revokeDbUserPrivilege,
} from '@/api/conn'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  connId: { type: String, default: '' },
  dbType: { type: String, default: '' },
  connName: { type: String, default: '' },
})

const emit = defineEmits(['update:modelValue', 'schemas-changed'])

const isMysql = computed(() => ['mysql', 'mariadb'].includes((props.dbType || '').toLowerCase()))
const isOracle = computed(() => (props.dbType || '').toLowerCase() === 'oracle')

const schemaTip = computed(() => isOracle.value
  ? 'Oracle 中 schema 与用户一一对应，删除 schema 将级联删除其中所有对象'
  : '删除库将删除库中所有表和数据，请谨慎操作')
const userTip = computed(() => isOracle.value
  ? 'Oracle 删除用户将级联删除其 schema 下的所有对象'
  : '按用户名与主机（host）组合标识用户')
const dropSchemaConfirmSuffix = computed(() => isOracle.value ? '将级联删除 schema 内所有对象！' : '其中所有表和数据将被删除！')
const dropUserConfirmSuffix = computed(() => isOracle.value ? '其 schema 及对象将被级联删除！' : '')

const activeTab = ref('schema')
const schemaList = ref([])
const userList = ref([])
const userRestricted = ref(false)
const schemasLoading = ref(false)
const usersLoading = ref(false)
const saving = ref(false)
const droppingSchema = ref('')
const droppingUser = ref('')

const schemaInputRef = ref()
const userInputRef = ref()
const pwdInputRef = ref()

function onOpen() {
  loadSchemas()
  loadUsers()
}

// ===== 库/Schema =====

function loadSchemas() {
  if (!props.connId) return
  schemasLoading.value = true
  listAdminSchemas(props.connId).then(resp => {
    const list = resp.data?.data || []
    schemaList.value = list.map(name => ({ name }))
  }).catch(() => {
    schemaList.value = []
  }).finally(() => {
    schemasLoading.value = false
  })
}

const schemaDlgVisible = ref(false)
const schemaFormRef = ref()
const schemaForm = ref({ schema: '', password: '', confirmPassword: '', charset: '', collation: '', tableSpace: '' })

const charsetOptions = ['utf8mb4', 'utf8', 'gbk', 'big5', 'latin1', 'binary']
const collationOptions = ['utf8mb4_general_ci', 'utf8mb4_unicode_ci', 'utf8mb4_0900_ai_ci', 'utf8_general_ci', 'gbk_chinese_ci']

const identifierRule = (label) => [
  { required: true, message: `请输入${label}`, trigger: 'blur' },
  { pattern: /^[a-zA-Z_][a-zA-Z0-9_$]{0,63}$/, message: `${label}须以字母或下划线开头，仅含字母、数字、下划线、$`, trigger: 'blur' },
]
const passwordRule = [
  { required: true, message: '请输入密码', trigger: 'blur' },
  { min: 6, max: 128, message: '密码长度需在 6-128 位之间', trigger: 'blur' },
]
// confirmPasswordRule 根据传入的密码字段来源生成"两次输入一致"校验
const confirmPasswordRule = (getPassword) => [
  { required: true, message: '请再次输入密码', trigger: 'blur' },
  {
    validator: (rule, value, callback) => {
      if (value !== getPassword()) {
        callback(new Error('两次输入的密码不一致'))
      } else {
        callback()
      }
    },
    trigger: 'blur',
  },
]

// rules 依赖 dbType（Oracle 需密码，MySQL 不需），用 computed 保证切换连接后规则同步更新
const schemaRules = computed(() => ({
  schema: identifierRule(isOracle.value ? 'Schema名' : '库名'),
  password: isOracle.value ? passwordRule : [],
  confirmPassword: isOracle.value ? confirmPasswordRule(() => schemaForm.value.password) : [],
}))
const userRules = computed(() => ({
  username: identifierRule('用户名'),
  host: isMysql.value ? [{ required: true, message: '请选择或输入主机', trigger: 'blur' }] : [],
  password: passwordRule,
  confirmPassword: confirmPasswordRule(() => userForm.value.password),
}))
const pwdRules = computed(() => ({
  password: passwordRule,
  confirmPassword: confirmPasswordRule(() => pwdForm.value.password),
}))

function openSchemaCreate() {
  resetSchemaForm()
  schemaDlgVisible.value = true
}

function resetSchemaForm() {
  schemaForm.value = { schema: '', password: '', confirmPassword: '', charset: '', collation: '', tableSpace: '' }
  schemaFormRef.value?.clearValidate()
}

function onCreateSchema() {
  schemaFormRef.value.validate(valid => {
    if (!valid) return
    saving.value = true
    createDbSchema({
      connId: props.connId,
      schema: schemaForm.value.schema,
      password: isOracle.value ? schemaForm.value.password : undefined,
      charset: !isOracle.value && schemaForm.value.charset ? schemaForm.value.charset : undefined,
      collation: !isOracle.value && schemaForm.value.collation ? schemaForm.value.collation : undefined,
      tableSpace: isOracle.value && schemaForm.value.tableSpace ? schemaForm.value.tableSpace : undefined,
    }).then(() => {
      ElMessage.success('创建成功')
      schemaDlgVisible.value = false
      loadSchemas()
      emit('schemas-changed')
    }).finally(() => {
      saving.value = false
    })
  })
}

function onDropSchema(row) {
  droppingSchema.value = row.name
  dropDbSchema(props.connId, row.name).then(() => {
    ElMessage.success('删除成功')
    loadSchemas()
    emit('schemas-changed')
  }).finally(() => {
    droppingSchema.value = ''
  })
}

// ===== 数据库用户 =====

function loadUsers() {
  if (!props.connId) return
  usersLoading.value = true
  listDbUsers(props.connId).then(resp => {
    const result = resp.data?.data || { users: [], restricted: false }
    userList.value = result.users || []
    userRestricted.value = !!result.restricted
  }).catch(() => {
    userList.value = []
  }).finally(() => {
    usersLoading.value = false
  })
}

function userLabel(row) {
  if (!row) return ''
  return isMysql.value && row.host ? `${row.username}@${row.host}` : row.username
}

const userDlgVisible = ref(false)
const userFormRef = ref()
const userForm = ref({ username: '', host: '%', password: '', confirmPassword: '' })
const hostOptions = ['%', 'localhost', '127.0.0.1']

function openUserCreate() {
  resetUserForm()
  userDlgVisible.value = true
}

function resetUserForm() {
  userForm.value = { username: '', host: '%', password: '', confirmPassword: '' }
  userFormRef.value?.clearValidate()
}

function onCreateUser() {
  userFormRef.value.validate(valid => {
    if (!valid) return
    saving.value = true
    createDbUser({
      connId: props.connId,
      username: userForm.value.username,
      host: isMysql.value ? userForm.value.host : undefined,
      password: userForm.value.password,
    }).then(() => {
      ElMessage.success('创建成功')
      userDlgVisible.value = false
      loadUsers()
      if (isOracle.value) {
        // Oracle 创建用户即创建 schema，同步刷新库列表
        loadSchemas()
        emit('schemas-changed')
      }
    }).finally(() => {
      saving.value = false
    })
  })
}

function onDropUser(row) {
  droppingUser.value = userLabel(row)
  dropDbUser({
    connId: props.connId,
    username: row.username,
    host: isMysql.value ? row.host : undefined,
  }).then(() => {
    ElMessage.success('删除成功')
    loadUsers()
    if (isOracle.value) {
      loadSchemas()
      emit('schemas-changed')
    }
  }).finally(() => {
    droppingUser.value = ''
  })
}

// ===== 重置密码 =====

const resetDlgVisible = ref(false)
const pwdFormRef = ref()
const pwdForm = ref({ password: '', confirmPassword: '' })
const resetTarget = ref(null)

function openResetPwd(row) {
  resetTarget.value = row
  resetPwdForm()
  resetDlgVisible.value = true
}

function resetPwdForm() {
  pwdForm.value = { password: '', confirmPassword: '' }
  pwdFormRef.value?.clearValidate()
}

function onResetPwd() {
  pwdFormRef.value.validate(valid => {
    if (!valid) return
    saving.value = true
    resetDbUserPassword({
      connId: props.connId,
      username: resetTarget.value.username,
      host: isMysql.value ? resetTarget.value.host : undefined,
      password: pwdForm.value.password,
    }).then(() => {
      ElMessage.success('密码已重置')
      resetDlgVisible.value = false
    }).finally(() => {
      saving.value = false
    })
  })
}

// ===== 用户权限管理 =====

const privDlgVisible = ref(false)
const privTarget = ref(null)
const privList = ref([])
const privLoading = ref(false)
const grantLoading = ref(false)
const revokingPriv = ref('')

const grantForm = ref({ privileges: [], object: '', grantOption: false })

const mysqlPrivilegeOptions = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'ALTER', 'INDEX', 'REFERENCES', 'EXECUTE', 'ALL PRIVILEGES']
const oraclePrivilegeOptions = ['CREATE SESSION', 'CREATE TABLE', 'CREATE VIEW', 'CREATE PROCEDURE', 'CREATE TRIGGER', 'CREATE SEQUENCE', 'CREATE SYNONYM', 'UNLIMITED TABLESPACE', 'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CONNECT', 'RESOURCE', 'DBA']

const privilegeOptions = computed(() => isOracle.value ? oraclePrivilegeOptions : mysqlPrivilegeOptions)

const objectPlaceholder = computed(() => isOracle.value
  ? '对象名，如 SCOTT.EMP；系统权限/角色留空'
  : '对象，如 mydb.* 或 *.*')

const grantTip = computed(() => isOracle.value
  ? '多选授予系统权限或角色；对象权限（如 SELECT）需填写对象名（OWNER.TABLE）'
  : '多选授予权限；对象示例：*.*（全局）、mydb.*（库级）、mydb.users（表级）')

function openPrivilege(row) {
  privTarget.value = row
  resetPrivForm()
  loadPrivileges()
  privDlgVisible.value = true
}

function resetPrivForm() {
  grantForm.value = { privileges: [], object: '', grantOption: false }
  privList.value = []
  revokingPriv.value = ''
}

function loadPrivileges() {
  if (!privTarget.value) return
  privLoading.value = true
  listDbUserPrivileges(
    props.connId,
    privTarget.value.username,
    isMysql.value ? privTarget.value.host : undefined,
  ).then(resp => {
    privList.value = resp.data?.data?.privileges || []
  }).catch(() => {
    privList.value = []
  }).finally(() => {
    privLoading.value = false
  })
}

function privKey(row) {
  return `${row.privilege}|${row.object || ''}`
}

function onGrantPriv() {
  if (!grantForm.value.privileges.length) {
    ElMessage.warning('请选择权限')
    return
  }
  grantLoading.value = true
  grantDbUserPrivilege({
    connId: props.connId,
    username: privTarget.value.username,
    host: isMysql.value ? privTarget.value.host : undefined,
    privileges: grantForm.value.privileges.join(','),
    object: grantForm.value.object || undefined,
    grantOption: grantForm.value.grantOption,
  }).then(() => {
    ElMessage.success('授权成功')
    loadPrivileges()
  }).finally(() => {
    grantLoading.value = false
  })
}

function onRevokePriv(row) {
  revokingPriv.value = privKey(row)
  revokeDbUserPrivilege({
    connId: props.connId,
    username: privTarget.value.username,
    host: isMysql.value ? privTarget.value.host : undefined,
    privileges: row.privilege,
    object: row.object || undefined,
  }).then(() => {
    ElMessage.success('撤销成功')
    loadPrivileges()
  }).finally(() => {
    revokingPriv.value = ''
  })
}
</script>

<style scoped>
.main-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.main-header-icon {
  font-size: 18px;
  color: var(--el-color-primary);
}

.main-header-title {
  font-size: 16px;
  font-weight: 600;
}

.conn-tag {
  margin-left: 4px;
}

.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.tab-count {
  font-style: normal;
  font-size: 11px;
  line-height: 16px;
  min-width: 18px;
  text-align: center;
  padding: 0 5px;
  border-radius: 9px;
  background: var(--el-fill-color-dark);
  color: var(--el-text-color-secondary);
}

:deep(.el-tabs__item.is-active) .tab-count {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.tab-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  margin-top: 8px;
}

.toolbar-tip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: auto;
}

.restricted-alert {
  margin-bottom: 12px;
}

.name-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.name-icon {
  color: var(--el-color-primary);
  flex-shrink: 0;
}

.name-icon.user {
  color: var(--el-color-success);
}

.dlg-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
}

.dlg-header-icon {
  font-size: 18px;
}

.dlg-header-icon.primary {
  color: var(--el-color-primary);
}

.dlg-header-icon.warning {
  color: var(--el-color-warning);
}

.dlg-form {
  padding: 2px 6px 0;
}

.form-tip {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-light);
  border-radius: 4px;
  padding: 7px 10px;
  margin-top: 2px;
}

.target-tag {
  font-weight: 500;
}

.priv-section {
  margin-top: 8px;
}

.priv-section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 8px;
}

.grant-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.grant-option-yes {
  color: var(--el-color-success);
}

.grant-option-no {
  color: var(--el-text-color-placeholder);
}
</style>
