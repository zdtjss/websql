<template>
  <div class="conn-dir-management">
    <div class="tree-panel">
      <div class="panel-header">
        <div class="panel-header-left">
          <h2>连接与目录</h2>
          <span class="panel-header-desc">拖拽目录可调整层级，「未指定」用于存放无目录的连接</span>
        </div>
        <div class="panel-header-actions">
          <el-input
            v-model="filterText"
            class="filter-input"
            placeholder="搜索连接"
            clearable
            size="small"
          >
            <template #prefix>
              <el-icon><Search /></el-icon>
            </template>
          </el-input>
          <el-tag v-if="dirDirty" class="dirty-tag" type="warning" effect="light" size="small">
            未保存
          </el-tag>
          <el-button size="small" @click="appendRootDir">
            <el-icon><Plus /></el-icon>
            新建根目录
          </el-button>
          <el-button type="primary" size="small" :disabled="!dirDirty" @click="saveDirTree">
            <el-icon><Check /></el-icon>
            保存目录结构
          </el-button>
        </div>
      </div>

      <div class="panel-body">
        <el-tree
          ref="treeRef"
          :data="treeData"
          node-key="id"
          draggable
          default-expand-all
          :expand-on-click-node="false"
          :allow-drag="allowDrag"
          :allow-drop="allowDrop"
          :filter-node-method="filterNode"
          @node-drop="onNodeDrop"
        >
          <template #default="{ node, data }">
            <!-- 目录节点：标签形式展示，点击标签进入行内编辑 -->
            <div v-if="data.type === 'dir'" class="tree-node-content tree-node--dir">
              <el-icon class="node-icon node-icon--dir" :size="16"><Folder /></el-icon>
              <el-tag
                v-if="!data.editing"
                class="dir-tag"
                type="warning"
                effect="light"
                size="small"
                @click="startEditDir(data)"
              >
                <span class="dir-tag-label">{{ data.label || '未命名目录' }}</span>
                <el-icon class="dir-tag-edit" :size="12"><Edit /></el-icon>
              </el-tag>
              <el-input
                v-else
                :ref="(el) => setDirInputRef(data.id, el)"
                v-model="data.label"
                class="node-input"
                placeholder="请输入目录名称"
                size="small"
                @keyup.enter="finishEditDir(data)"
                @blur="finishEditDir(data)"
              />
              <span class="node-count">{{ (data.children || []).length }}</span>
              <div class="node-actions">
                <el-button type="primary" size="small" link @click.stop="appendChildDir(data)">
                  <el-icon><Plus /></el-icon>
                  子目录
                </el-button>
                <el-button type="primary" size="small" link @click.stop="openAddConn(data.id)">
                  <el-icon><Connection /></el-icon>
                  连接
                </el-button>
                <el-popconfirm
                  title="确定要删除这个目录吗？其下连接将变为「未指定」"
                  @confirm="removeDir(node, data)"
                >
                  <template #reference>
                    <el-button type="danger" size="small" link @click.stop>
                      <el-icon><Delete /></el-icon>
                      删除
                    </el-button>
                  </template>
                </el-popconfirm>
              </div>
            </div>

            <!-- 连接节点：行内展示完整连接信息 -->
            <div v-else-if="data.type === 'conn'" class="tree-node-content tree-node--conn">
              <el-icon class="node-icon node-icon--conn" :size="16"><Connection /></el-icon>
              <span class="conn-name" :title="data.label">{{ data.label }}</span>
              <el-tag class="conn-type-tag" size="small" effect="plain">{{ getDbTypeLabel(data.dbType) }}</el-tag>
              <div class="conn-meta">
                <span v-if="connUser(data)" class="conn-meta-item" :title="'用户名：' + connUser(data)">
                  <el-icon :size="12"><UserFilled /></el-icon>
                  {{ connUser(data) }}
                </span>
                <span v-if="connUrl(data)" class="conn-meta-item conn-url" :title="connUrl(data)">
                  {{ connUrl(data) }}
                </span>
                <span v-if="connVersion(data)" class="conn-meta-item">{{ connVersion(data) }}</span>
              </div>
              <div class="node-actions">
                <el-button type="primary" size="small" link @click.stop="openEditConn(data)">
                  <el-icon><Edit /></el-icon>
                  编辑
                </el-button>
                <el-button
                  type="primary"
                  size="small"
                  link
                  :loading="data.testing"
                  @click.stop="testConnNode(data)"
                >
                  <el-icon><Connection /></el-icon>
                  测试
                </el-button>
                <el-button
                  v-if="canManageDbResources(data)"
                  type="primary"
                  size="small"
                  link
                  @click.stop="openDbUserManager(data)"
                >
                  <el-icon><UserFilled /></el-icon>
                  库/用户
                </el-button>
                <el-popconfirm title="确定要删除这个连接吗？" @confirm="removeConn(data)">
                  <template #reference>
                    <el-button type="danger" size="small" link @click.stop>
                      <el-icon><Delete /></el-icon>
                      删除
                    </el-button>
                  </template>
                </el-popconfirm>
              </div>
            </div>

            <!-- 未指定（无目录）分组 -->
            <div v-else class="tree-node-content none-dir-node">
              <el-icon class="node-icon node-icon--none" :size="16"><Warning /></el-icon>
              <span class="node-label">未指定（无目录）</span>
              <el-tag size="small" type="warning" effect="plain" class="node-tag">{{ (data.children || []).length }}</el-tag>
              <div class="node-actions">
                <el-button type="primary" size="small" link @click.stop="openAddConn('')">
                  <el-icon><Plus /></el-icon>
                  连接
                </el-button>
              </div>
            </div>
          </template>
          <template #empty>
            <div class="tree-empty">
              <el-empty description="暂无目录或连接" :image-size="80" />
            </div>
          </template>
        </el-tree>
      </div>
    </div>

    <!-- 连接编辑对话框 -->
    <el-dialog v-model="connDialogVisible" :title="connDialogTitle" width="560px" top="6vh"
      class="conn-edit-dialog" :draggable="true" append-to-body>
      <el-form :model="connForm" label-width="100px">
        <el-form-item label="连接名称" required>
          <el-input v-model="connForm.name" placeholder="请输入连接名称" />
        </el-form-item>
        <el-form-item label="数据库类型" required>
          <el-select v-model="connForm.dbType" style="width: 100%">
            <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="所属目录">
          <el-tree-select
            v-model="connForm.parentId"
            :data="dirOptions"
            clearable
            check-strictly
            value-key="id"
            placeholder="未指定（无目录）"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="用户名">
          <el-input v-model="connForm.user" placeholder="请输入用户名" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="connForm.pwd"
            type="password"
            show-password
            :placeholder="connForm.id ? '留空则不修改密码' : '请输入密码'"
          />
        </el-form-item>
        <el-form-item label="连接信息" required>
          <div class="url-field">
            <el-input
              v-model="connForm.url"
              type="textarea"
              :rows="3"
              :placeholder="jdbcPlaceholder"
            />
            <!-- jdbc 串实时识别：拆出主机/端口/库(schema)与参数映射，仅用于本次提交，不额外落库 -->
            <div v-if="showJdbcPanel" class="jdbc-parse" :class="{ 'is-error': !jdbcInfo.ok }">
              <div class="jdbc-parse-head">
                <el-icon :size="13"><MagicStick /></el-icon>
                <span>JDBC 自动识别</span>
                <el-tag v-if="jdbcInfo.supported" size="small" effect="plain">{{ getDbTypeLabel(jdbcInfo.dbType) }}</el-tag>
                <el-tag v-else size="small" type="danger" effect="plain">未接入</el-tag>
                <span v-if="jdbcInfo.host" class="jdbc-target">
                  {{ jdbcInfo.host }}:{{ jdbcInfo.port }}
                  <template v-if="jdbcInfo.schema"> / {{ jdbcInfo.schema }}</template>
                  <template v-if="jdbcInfo.sid"> / SID {{ jdbcInfo.sid }}</template>
                </span>
              </div>
              <p v-if="!jdbcInfo.ok" class="jdbc-error">{{ jdbcInfo.error }}</p>
              <!-- 识别明细独立滚动：标题与目标信息常驻，避免长参数串把对话框撑高 -->
              <div v-else class="jdbc-parse-body">
                <div class="jdbc-row"><span class="jdbc-row-label">规范化连接串</span><code>{{ jdbcInfo.dsn }}</code></div>
                <div v-if="jdbcInfo.user" class="jdbc-row">
                  <span class="jdbc-row-label">串内用户名</span><code>{{ jdbcInfo.user }}</code>
                  <el-button
                    v-if="!connForm.user"
                    type="primary"
                    size="small"
                    link
                    @click="connForm.user = jdbcInfo.user"
                  >填入用户名</el-button>
                </div>
                <div v-if="jdbcInfo.hasPassword" class="jdbc-row jdbc-row--warn">
                  串内含口令，请在「密码」框单独填写（识别结果不自动写入）
                </div>
                <div v-if="jdbcInfo.dbType && jdbcInfo.dbType !== connForm.dbType" class="jdbc-row jdbc-row--warn">
                  识别为 {{ getDbTypeLabel(jdbcInfo.dbType) }}，与当前所选 {{ getDbTypeLabel(connForm.dbType) }} 不同，测试/保存时按识别结果下发
                </div>
                <div v-if="jdbcMappedParams.length || jdbcDroppedParams.length" class="jdbc-params">
                  <div class="jdbc-params-head" @click="jdbcParamsOpen = !jdbcParamsOpen">
                    <el-icon :size="12"><component :is="jdbcParamsOpen ? ArrowDown : ArrowRight" /></el-icon>
                    <span>连接参数共 {{ jdbcMappedParams.length + jdbcDroppedParams.length }} 项（映射 {{ jdbcMappedParams.length }} / 忽略 {{ jdbcDroppedParams.length }}）</span>
                  </div>
                  <template v-if="jdbcParamsOpen">
                    <div v-for="(p, i) in jdbcMappedParams" :key="'m' + i" class="jdbc-param">
                      <code>{{ p.source }}={{ p.value }}</code> → <code>{{ p.target }}={{ p.targetValue }}</code>
                      <span class="jdbc-note">{{ p.note }}</span>
                    </div>
                    <div v-for="(p, i) in jdbcDroppedParams" :key="'d' + i" class="jdbc-param jdbc-param--drop">
                      已忽略 <code>{{ p.source }}</code><span class="jdbc-note">{{ p.note }}</span>
                    </div>
                  </template>
                </div>
                <ul v-if="jdbcInfo.warnings.length" class="jdbc-warnings">
                  <li v-for="(w, i) in jdbcInfo.warnings" :key="i">{{ w }}</li>
                </ul>
              </div>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="只读连接">
          <div style="display: flex; align-items: center; gap: 10px;">
            <el-switch v-model="connForm.readOnly" active-text="只读" inactive-text="可写" />
            <span class="readonly-tip">开启后写语句会被拒绝，需临时解锁才能写入</span>
          </div>
        </el-form-item>
        <el-form-item label="SSH 隧道">
          <div style="width: 100%">
            <el-switch v-model="connForm.sshEnabled" active-text="经跳板机" inactive-text="直连" />
            <span class="readonly-tip" style="margin-left: 10px;">内网数据库可经 SSH 跳板机访问</span>
          </div>
        </el-form-item>
        <template v-if="connForm.sshEnabled">
          <el-form-item label="跳板机地址" required>
            <div style="display: flex; gap: 8px; width: 100%">
              <el-input v-model="connForm.sshHost" placeholder="host 或 IP" style="flex: 1" />
              <el-input-number v-model="connForm.sshPort" :min="1" :max="65535" controls-position="right"
                style="width: 130px" />
            </div>
          </el-form-item>
          <el-form-item label="SSH 用户" required>
            <el-input v-model="connForm.sshUser" placeholder="登录跳板机的用户名" />
          </el-form-item>
          <el-form-item label="认证方式">
            <el-radio-group v-model="connForm.sshAuthType">
              <el-radio value="password">密码</el-radio>
              <el-radio value="key">私钥文件</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="connForm.sshAuthType !== 'key'" label="SSH 密码">
            <el-input v-model="connForm.sshPassword" type="password" show-password
              :placeholder="connForm.id && hasSavedSsh ? '留空则不修改' : '请输入 SSH 密码'" />
          </el-form-item>
          <template v-else>
            <el-form-item label="私钥路径" required>
              <el-input v-model="connForm.sshKeyPath" placeholder="如 C:\\Users\\me\\.ssh\\id_rsa" />
            </el-form-item>
            <el-form-item label="私钥口令">
              <el-input v-model="connForm.sshKeyPassphrase" type="password" show-password
                placeholder="私钥无口令时留空" />
            </el-form-item>
          </template>
          <el-form-item label="known_hosts">
            <el-input v-model="connForm.sshKnownHostsPath"
              placeholder="留空则不校验主机密钥（会记录警告）" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button :loading="connTesting" @click="testConnForm">测试连接</el-button>
        <el-button v-if="connForm.id && connForm.readOnly" @click="unlockWrite">临时解锁写入</el-button>
        <el-button @click="connDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="connSaving" @click="submitConn">保存</el-button>
      </template>
    </el-dialog>

    <!-- 库与用户管理：管理连接内数据库用户、库/schema（仅 MySQL/MariaDB/Oracle） -->
    <DbUserSchemaManager
      v-model="dbMgntDialogVisible"
      :conn-id="dbMgntConnId"
      :db-type="dbMgntDbType"
      :conn-name="dbMgntConnName"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { listConn, saveConn, testDbConn, delConn, listDirTree, delTreeNode, saveTree, unlockConnWrite } from '@/api/conn'
import { ElMessage } from 'element-plus'
import { handleError } from '@/utils/errorHandler'
import { ArrowDown, ArrowRight, Check, Connection, Delete, Edit, Folder, MagicStick, Plus, Search, UserFilled, Warning } from '@element-plus/icons-vue'
import DbUserSchemaManager from '@/components/db-tools/DbUserSchemaManager.vue'
import { parseJdbcUrl } from '@/utils/jdbcUrl'

// 「未指定（无目录）」虚拟分组节点 id，不落库
const NONE_DIR_ID = '__none_dir__'
const TEMP_ID_PREFIX = 'temp_node_'

const treeRef = ref(null)
const treeData = ref([])
const filterText = ref('')
const dirDirty = ref(false)

let tempIdCounter = 0
const generateTempId = () => `${TEMP_ID_PREFIX}${Date.now()}_${tempIdCounter++}`
const isTempId = (id) => String(id || '').startsWith(TEMP_ID_PREFIX)

const dbTypeList = ref([
  { label: 'MySQL', value: 'mysql' },
  { label: 'Oracle', value: 'oracle' },
  { label: 'SQLite', value: 'sqlite' },
  { label: 'MariaDB', value: 'mariadb' }
])

const getDbTypeLabel = (dbType) => {
  const item = dbTypeList.value.find(t => t.value === dbType)
  return item ? item.label : dbType
}

// 连接节点行内展示的连接信息
const connUser = (data) => (data.raw && data.raw.user) || ''
const connUrl = (data) => (data.raw && data.raw.url) || ''
const connVersion = (data) => (data.raw && data.raw.dbVersion) || ''

// ===== 目录标签行内编辑 =====
const dirInputRefs = new Map()
const setDirInputRef = (id, el) => {
  if (el) dirInputRefs.set(id, el)
  else dirInputRefs.delete(id)
}
const focusDirInput = (id) => {
  nextTick(() => {
    const el = dirInputRefs.get(id)
    if (el && el.focus) el.focus()
  })
}

const startEditDir = (data) => {
  data.editOrigin = data.label
  data.editing = true
  focusDirInput(data.id)
}

const finishEditDir = (data) => {
  if (!data.editing) return
  data.editing = false
  const next = String(data.label || '').trim()
  // 名称清空时回退为原值，避免出现无名称目录
  data.label = next || data.editOrigin || ''
  if (data.label !== (data.editOrigin || '')) dirDirty.value = true
}

// ===== 树构建 =====
const buildTree = (dirs, conns) => {
  const cloneDir = (n) => ({
    id: n.id || '',
    label: n.label || '',
    type: 'dir',
    children: (n.children || []).map(cloneDir)
  })
  const rootDirs = dirs.map(cloneDir)

  const dirMap = new Map()
  const indexDirs = (list) => {
    for (const n of list) {
      dirMap.set(n.id, n)
      indexDirs(n.children)
    }
  }
  indexDirs(rootDirs)

  const noneChildren = []
  for (const c of conns) {
    const node = {
      id: c.id,
      label: c.name || '(未命名)',
      type: 'conn',
      dbType: c.dbType,
      raw: c,
      testing: false,
      children: []
    }
    const parent = c.parentId ? dirMap.get(c.parentId) : null
    if (parent) {
      parent.children.push(node)
    } else {
      noneChildren.push(node)
    }
  }

  const noneNode = { id: NONE_DIR_ID, label: '未指定', type: 'noneDir', children: noneChildren }
  return [...rootDirs, noneNode]
}

const loadData = async () => {
  const [dirResp, connResp] = await Promise.all([
    listDirTree(),
    // 统一树需要一次性拿到全部连接，pageSize 给一个足够大的值
    listConn({ name: '', parentId: '', page: 1, pageSize: 10000 })
  ])
  const dirs = Array.isArray(dirResp.data.data) ? dirResp.data.data : []
  const connResult = connResp.data.data || {}
  const conns = Array.isArray(connResult.data) ? connResult.data : []
  treeData.value = buildTree(dirs, conns)
  dirDirty.value = false
}

// ===== 树节点查找 / 移动 =====
const getNoneNode = () => treeData.value.find(n => n.type === 'noneDir')

const findDirNodeById = (id) => {
  if (!id) return null
  let found = null
  const walk = (list) => {
    for (const n of list) {
      if (n.type === 'dir' && n.id === id) { found = n; return }
      if (n.children && n.children.length) walk(n.children)
      if (found) return
    }
  }
  walk(treeData.value)
  return found
}

const findConnNodeById = (id) => {
  let found = null
  const walk = (list) => {
    for (const n of list) {
      if (n.type === 'conn' && n.id === id) { found = n; return }
      if (n.children && n.children.length) walk(n.children)
      if (found) return
    }
  }
  walk(treeData.value)
  return found
}

const detachNode = (target) => {
  const walk = (list) => {
    const i = list.indexOf(target)
    if (i !== -1) { list.splice(i, 1); return true }
    for (const n of list) {
      if (n.children && walk(n.children)) return true
    }
    return false
  }
  return walk(treeData.value)
}

const moveConnNodeTo = (node, parentId) => {
  detachNode(node)
  const dir = findDirNodeById(parentId)
  if (dir) {
    dir.children.push(node)
  } else {
    getNoneNode().children.push(node)
  }
  if (node.raw) node.raw.parentId = dir ? parentId : ''
}

// 只保留目录节点的纯目录树，用于 saveTree（连接/未指定分组不落库）
const extractDirs = (nodes) =>
  nodes
    .filter(n => n.type === 'dir')
    .map(n => ({ id: n.id, label: n.label, children: extractDirs(n.children || []) }))

// ===== 目录操作 =====
const appendRootDir = () => {
  const noneIndex = treeData.value.findIndex(n => n.type === 'noneDir')
  const node = { id: generateTempId(), label: '', type: 'dir', children: [], editing: true, editOrigin: '' }
  treeData.value.splice(noneIndex, 0, node)
  dirDirty.value = true
  focusDirInput(node.id)
}

const appendChildDir = (data) => {
  if (!data.children) data.children = []
  const child = { id: generateTempId(), label: '', type: 'dir', children: [], editing: true, editOrigin: '' }
  data.children.push(child)
  dirDirty.value = true
  nextTick(() => {
    const node = treeRef.value?.getNode(data.id)
    if (node) node.expand()
    focusDirInput(child.id)
  })
}

const removeDir = (node, data) => {
  // 收集子树的目录 id 与连接节点
  const dirIds = []
  const connsToMove = []
  const collect = (n) => {
    if (n.type === 'dir') {
      dirIds.push(n.id)
      ;(n.children || []).forEach(collect)
    } else if (n.type === 'conn') {
      connsToMove.push(n)
    }
  }
  collect(data)

  detachNode(data)

  // 目录下的连接不删除，改为「未指定」
  const noneNode = getNoneNode()
  for (const c of connsToMove) {
    if (c.raw) c.raw.parentId = ''
    noneNode.children.push(c)
  }

  // 已落库的目录节点同步删除（新增的临时节点未落库，无需请求）
  for (const id of dirIds) {
    if (!isTempId(id)) {
      delTreeNode(id).catch(() => {})
    }
  }
  dirDirty.value = true
}

const saveDirTree = () => {
  const dirs = extractDirs(treeData.value)
  saveTree(dirs).then(() => {
    ElMessage.success('保存成功')
    dirDirty.value = false
    loadData().catch(() => {})
  }).catch(() => {})
}

// ===== 拖拽 =====
const allowDrag = (node) => node.data.type === 'dir'

const containsNode = (parent, target) => {
  if (!parent.children) return false
  for (const child of parent.children) {
    if (child === target) return true
    if (containsNode(child, target)) return true
  }
  return false
}

const allowDrop = (draggingNode, dropNode, dropType) => {
  if (dropNode.data.type !== 'dir') return false
  // 禁止把目录拖入自身或其后代
  if (dropType === 'inner' && containsNode(draggingNode.data, dropNode.data)) return false
  return true
}

const onNodeDrop = () => {
  dirDirty.value = true
}

// ===== 过滤 =====
const filterNode = (value, data) => {
  if (!value) return true
  if (data.type === 'conn') return String(data.label || '').includes(value)
  // 目录 / 未指定分组始终展示，避免过滤时结构消失
  return true
}

watch(filterText, (val) => {
  nextTick(() => treeRef.value?.filter(val))
})

// ===== 连接编辑对话框 =====
const connDialogVisible = ref(false)
const connSaving = ref(false)
const connTesting = ref(false)
const connForm = ref({ id: '', name: '', dbType: 'mysql', parentId: '', user: '', pwd: '', url: '', readOnly: false, sshEnabled: false, sshHost: '', sshPort: 22, sshUser: '', sshAuthType: 'password', sshPassword: '', sshKeyPath: '', sshKeyPassphrase: '', sshKnownHostsPath: '' })
// 编辑已有连接时，是否已保存过 SSH 配置（决定口令 placeholder）
const hasSavedSsh = ref(false)

const connDialogTitle = computed(() => (connForm.value.id ? '编辑连接' : '添加连接'))

// 「所属目录」下拉数据：仅目录节点
const dirOptions = computed(() => {
  const map = (nodes) =>
    nodes
      .filter(n => n.type === 'dir')
      .map(n => ({ id: n.id, value: n.id, label: n.label, children: map(n.children || []) }))
  return map(treeData.value)
})

// ===== JDBC 串实时识别 =====
// 仅当输入以 jdbc: 开头时生效：拆出主机/端口/库(schema)与参数映射。
// 识别结果只服务本次编辑，不新增持久化字段：提交时只把规范化后的连接串写入既有 url。
const jdbcInfo = computed(() => parseJdbcUrl(connForm.value.url))
const jdbcMappedParams = computed(() => (jdbcInfo.value ? jdbcInfo.value.params.filter(p => p.action !== 'dropped') : []))
const jdbcDroppedParams = computed(() => (jdbcInfo.value ? jdbcInfo.value.params.filter(p => p.action === 'dropped') : []))
const jdbcPlaceholder = 'jdbc:mysql://host:3306/db?characterEncoding=utf8（自动识别）\n或 tcp(主机:端口)/库名?参数、主机:端口/服务名'

// 识别失败时，输入看着还不像完整连接串（没 / @ ( 定界符）就先不打扰，避免逐字报错
const showJdbcPanel = computed(() => {
  const j = jdbcInfo.value
  if (!j) return false
  return j.ok || /[/@(]/.test(String(connForm.value.url || '').trim().slice(5))
})

// 参数明细默认展开；长串可手动收起，避免把对话框撑得过高
const jdbcParamsOpen = ref(true)

// 下发给后端的 url / dbType：jdbc 输入取识别结果，其余原样透传
const resolveConnTarget = () => {
  const j = jdbcInfo.value
  if (!j) return { url: connForm.value.url, dbType: connForm.value.dbType }
  return { url: j.dsn, dbType: j.dbType || connForm.value.dbType }
}

const openAddConn = (parentId = '') => {
  hasSavedSsh.value = false
  connForm.value = { id: '', name: '', dbType: 'mysql', parentId: parentId || '', user: '', pwd: '', url: '', readOnly: false, sshEnabled: false, sshHost: '', sshPort: 22, sshUser: '', sshAuthType: 'password', sshPassword: '', sshKeyPath: '', sshKeyPassphrase: '', sshKnownHostsPath: '' }
  connDialogVisible.value = true
}

const openEditConn = (node) => {
  const raw = node.raw || {}
  const ssh = raw.ssh || null
  hasSavedSsh.value = !!ssh
  connForm.value = {
    id: node.id,
    name: raw.name || node.label || '',
    dbType: raw.dbType || node.dbType || 'mysql',
    parentId: raw.parentId || '',
    user: raw.user || '',
    pwd: '',
    url: raw.url || '',
    readOnly: !!raw.readOnly,
    sshEnabled: !!ssh,
    sshHost: ssh?.host || '',
    sshPort: ssh?.port || 22,
    sshUser: ssh?.user || '',
    sshAuthType: ssh?.authType || 'password',
    sshPassword: '',
    sshKeyPath: ssh?.keyPath || '',
    sshKeyPassphrase: '',
    sshKnownHostsPath: ssh?.knownHostsPath || '',
  }
  connDialogVisible.value = true
}

// 只读连接的临时写解锁（1 或 5 分钟，进程重启失效）
const unlockWrite = () => {
  if (!connForm.value.id) return
  unlockConnWrite({ connId: connForm.value.id, durationSecs: 300 })
    .then(() => ElMessage.success('已解锁写入 5 分钟，请尽快操作'))
    .catch((e) => handleError(e, '解锁写入'))
}

const submitConn = () => {
  const form = connForm.value
  if (!form.name || !form.name.trim()) { ElMessage.warning('请输入连接名称'); return }
  if (!form.dbType) { ElMessage.warning('请选择数据库类型'); return }
  if (!form.url || !form.url.trim()) { ElMessage.warning('请输入连接信息'); return }
  if (jdbcInfo.value && !jdbcInfo.value.ok) { ElMessage.warning(jdbcInfo.value.error); return }

  const target = resolveConnTarget()
  const isCreate = !form.id
  const payload = {
    id: form.id || '',
    name: form.name,
    dbType: target.dbType,
    parentId: form.parentId || '',
    user: form.user || '',
    url: target.url,
    readOnly: !!form.readOnly
  }
  if (form.sshEnabled) {
    if (!validateSshForm()) return
    payload.ssh = buildSshConfig()
  } else if (hasSavedSsh.value) {
    // 显式关闭隧道：传 null 让服务端清理已有配置与隧道
    payload.ssh = null
  }
  if (form.pwd) payload.pwd = form.pwd

  connSaving.value = true
  // skipGlobalError：避开拦截器全局弹窗与 handleError 叠加成两条提示
  saveConn(payload, { skipGlobalError: true }).then((resp) => {
    const saved = (resp.data && resp.data.data) || null
    const targetId = saved && saved.parentId != null ? saved.parentId : (form.parentId || '')

    if (isCreate) {
      const node = {
        id: (saved && saved.id) || '',
        label: (saved && saved.name) || form.name,
        type: 'conn',
        dbType: (saved && saved.dbType) || target.dbType,
        raw: saved || { ...payload },
        testing: false,
        children: []
      }
      const dir = findDirNodeById(targetId)
      if (dir) {
        dir.children.push(node)
      } else {
        getNoneNode().children.push(node)
      }
      if (node.raw) node.raw.parentId = dir ? targetId : ''
    } else {
      const node = findConnNodeById(form.id)
      if (node) {
        node.label = (saved && saved.name) || form.name
        node.dbType = (saved && saved.dbType) || form.dbType
        node.raw = saved || { ...payload }
        moveConnNodeTo(node, targetId)
      }
    }

    ElMessage.success('保存成功')
    connDialogVisible.value = false
    // 新增但未拿到落库 id 时，回源刷新一次，避免节点 key 为空
    if (isCreate && !(saved && saved.id)) {
      loadData().catch(() => {})
    }
  }).catch((e) => {
    // 保存前的连接校验与 SSH 隧道建链失败都要把后端原因透传出来，否则用户只看到“没反应”
    handleError(e, '保存连接')
  }).finally(() => {
    connSaving.value = false
  })
}

// 构造 SSH 配置：未开启返回 null（表示显式清除），无已存配置时保持 undefined 语义
const buildSshConfig = () => {
  const form = connForm.value
  if (!form.sshEnabled) return null
  return {
    host: form.sshHost,
    port: Number(form.sshPort) || 22,
    user: form.sshUser,
    authType: form.sshAuthType,
    password: form.sshPassword || undefined,
    keyPath: form.sshKeyPath || undefined,
    keyPassphrase: form.sshKeyPassphrase || undefined,
    knownHostsPath: form.sshKnownHostsPath || undefined,
  }
}

const validateSshForm = () => {
  const form = connForm.value
  if (!form.sshEnabled) return true
  if (!form.sshHost || !form.sshUser) { ElMessage.warning('请填写跳板机地址与 SSH 用户'); return false }
  if (form.sshAuthType === 'password' && !form.sshPassword && !hasSavedSsh.value) {
    ElMessage.warning('请填写 SSH 密码'); return false
  }
  if (form.sshAuthType === 'key' && !form.sshKeyPath) { ElMessage.warning('请填写私钥路径'); return false }
  return true
}

const testConnForm = () => {
  if (!connForm.value.url) { ElMessage.warning('请先填写连接信息'); return }
  if (jdbcInfo.value && !jdbcInfo.value.ok) { ElMessage.warning(jdbcInfo.value.error); return }
  if (!validateSshForm()) return
  const target = resolveConnTarget()
  const ssh = buildSshConfig()
  const payload = {
    id: connForm.value.id || '',
    dbType: target.dbType,
    user: connForm.value.user || '',
    url: target.url,
  }
  if (connForm.value.pwd) payload.pwd = connForm.value.pwd
  if (ssh) payload.ssh = ssh
  connTesting.value = true
  testDbConn(payload, { skipGlobalError: true })
    .then((resp) => {
      if (resp.data.code === 200) ElMessage.success('数据库连接成功')
    })
    .catch((e) => handleError(e, '测试连接'))
    .finally(() => { connTesting.value = false })
}

const testConnNode = (node) => {
  node.testing = true
  testDbConn(node.raw, { skipGlobalError: true })
    .then((resp) => {
      if (resp.data.code === 200) ElMessage.success('数据库连接成功')
    })
    .catch((e) => handleError(e, '测试连接'))
    .finally(() => { node.testing = false })
}

const removeConn = (node) => {
  if (!node.id) {
    detachNode(node)
    return
  }
  delConn(node.id).then(() => {
    detachNode(node)
    ElMessage.success('删除成功')
  }).catch(() => {})
}

// ===== 库与用户管理 =====
const DB_MANAGE_TYPES = ['mysql', 'mariadb', 'oracle']
const dbMgntDialogVisible = ref(false)
const dbMgntConnId = ref('')
const dbMgntDbType = ref('')
const dbMgntConnName = ref('')

const canManageDbResources = (node) => {
  return !!node.id && !isTempId(node.id) && DB_MANAGE_TYPES.includes((node.dbType || '').toLowerCase())
}

const openDbUserManager = (node) => {
  dbMgntConnId.value = node.id
  dbMgntDbType.value = (node.dbType || '').toLowerCase()
  dbMgntConnName.value = node.label || ''
  dbMgntDialogVisible.value = true
}

onMounted(() => {
  loadData().catch(() => {})
})
</script>

<style scoped>
.conn-dir-management {
  padding: 20px;
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.tree-panel {
  flex: 1;
  min-height: 0;
  background: var(--bg-primary);
  border-radius: 8px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.panel-header {
  padding: 14px 20px;
  border-bottom: 1px solid var(--border-primary);
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.panel-header-left {
  display: flex;
  align-items: baseline;
  gap: 12px;
  min-width: 0;
}

.panel-header-left h2 {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: var(--text-primary);
  white-space: nowrap;
}

.panel-header-desc {
  font-size: 12px;
  color: var(--text-tertiary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.panel-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.filter-input {
  width: 200px;
}

.dirty-tag {
  flex-shrink: 0;
}

.panel-body {
  padding: 12px 20px 16px;
  flex: 1;
  overflow-y: auto;
  min-height: 200px;
}

.tree-empty {
  padding: 40px 0;
}

.tree-node-content {
  display: flex;
  align-items: center;
  flex: 1;
  padding: 4px 0;
  gap: 8px;
  min-width: 0;
}

.node-icon {
  flex-shrink: 0;
}

.node-icon--dir {
  color: var(--warning-color);
}

.node-icon--conn {
  color: var(--accent-color);
}

.node-icon--none {
  color: var(--text-tertiary);
}

/* 目录：标签形式展示 */
.dir-tag {
  display: inline-flex;
  align-items: center;
  max-width: 320px;
  overflow: hidden;
  cursor: pointer;
  border-radius: 6px;
  font-weight: 500;
}

.dir-tag :deep(.el-tag__content) {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  overflow: hidden;
}

.dir-tag-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-tag-edit {
  opacity: 0;
  transition: opacity 0.2s;
}

.dir-tag:hover .dir-tag-edit {
  opacity: 0.7;
}

.node-count {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--text-tertiary);
}

.node-input {
  width: 240px;
  flex-shrink: 0;
}

.node-label {
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.none-dir-node .node-label {
  color: var(--text-secondary);
}

.node-tag {
  flex-shrink: 0;
}

/* 连接：行内展示名称 / 类型 / 账号 / 连接串 */
.conn-name {
  flex-shrink: 0;
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.conn-type-tag {
  flex-shrink: 0;
}

/* 只读连接说明文字 */
.readonly-tip {
  font-size: 12px;
  color: var(--db-text-tertiary, #909399);
}

/* JDBC 识别区：紧跟连接信息输入框，只读展示不落库 */
.url-field {
  width: 100%;
}

.jdbc-parse {
  margin-top: 6px;
  padding: 8px 10px;
  border: 1px solid var(--border-primary);
  border-left: 3px solid var(--accent-color, #409eff);
  border-radius: 6px;
  background: var(--bg-secondary, rgba(0, 0, 0, 0.02));
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-secondary);
  /* 面板限高：标题常驻，明细区内部滚动，避免拉长对话框 */
  display: flex;
  flex-direction: column;
  max-height: min(30vh, 220px);
  overflow: hidden;
}

.jdbc-parse.is-error {
  border-left-color: var(--el-color-danger, #f56c6c);
}

.jdbc-parse-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  flex: 0 0 auto;
  font-weight: 500;
  color: var(--text-primary);
}

.jdbc-parse-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding-right: 4px;
}

/* 长连接串/参数值强制断行，避免滚动区出现横向滚动条 */
.jdbc-parse-body code {
  word-break: break-all;
}

.jdbc-target {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: var(--text-primary);
}

.jdbc-error {
  margin: 6px 0 0;
  color: var(--el-color-danger, #f56c6c);
}

.jdbc-row {
  margin-top: 4px;
}

.jdbc-row-label {
  color: var(--text-tertiary);
  margin-right: 6px;
}

.jdbc-row--warn {
  color: var(--warning-color, #e6a23c);
}

.jdbc-params {
  margin-top: 6px;
  padding-top: 6px;
  border-top: 1px dashed var(--border-primary);
}

.jdbc-params-head {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  user-select: none;
  color: var(--text-secondary);
}

.jdbc-params-head:hover {
  color: var(--accent-color, #409eff);
}

.jdbc-param code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  color: var(--text-primary);
}

.jdbc-param--drop {
  color: var(--text-tertiary);
}

.jdbc-note {
  margin-left: 6px;
  color: var(--text-tertiary);
}

.jdbc-warnings {
  margin: 6px 0 0;
  padding-left: 16px;
  color: var(--warning-color, #e6a23c);
}

.conn-meta {
  display: flex;
  align-items: center;
  gap: 14px;
  flex: 1;
  min-width: 0;
  font-size: 12px;
  color: var(--text-tertiary);
}

.conn-meta-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.conn-url {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  max-width: 100%;
}

.node-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.2s;
}

.tree-node-content:hover .node-actions {
  opacity: 1;
}

:deep(.el-tree) {
  font-size: 14px;
  background: transparent;
}

:deep(.el-tree-node__content) {
  height: auto;
  min-height: 32px;
  padding: 4px 0;
}

:deep(.el-tree-node__content:hover) {
  background: var(--bg-hover);
}

:deep(.el-tree-node.is-current > .el-tree-node__content) {
  background: var(--bg-active);
}

:deep(.el-tree-node__expand-icon) {
  color: var(--text-tertiary);
}

:deep(.el-input__wrapper) {
  border-radius: 6px;
}
</style>

<!-- 连接对话框 append-to-body 后不在组件的 scoped 作用域内，需用全局样式 -->
<style>
.conn-edit-dialog .el-dialog__body {
  max-height: 68vh;
  overflow-y: auto;
}
</style>