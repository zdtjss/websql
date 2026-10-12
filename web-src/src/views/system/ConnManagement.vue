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
    <el-dialog v-model="connDialogVisible" :title="connDialogTitle" width="560px" :draggable="true" append-to-body>
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
          <el-input
            v-model="connForm.url"
            type="textarea"
            :rows="3"
            placeholder="如 jdbc:mysql://host:3306/db 或 主机:端口/库名"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :loading="connTesting" @click="testConnForm">测试连接</el-button>
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
import { listConn, saveConn, testDbConn, delConn, listDirTree, delTreeNode, saveTree } from '@/api/conn'
import { ElMessage } from 'element-plus'
import { Check, Connection, Delete, Edit, Folder, Plus, Search, UserFilled, Warning } from '@element-plus/icons-vue'
import DbUserSchemaManager from '@/components/db-tools/DbUserSchemaManager.vue'

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
const connForm = ref({ id: '', name: '', dbType: 'mysql', parentId: '', user: '', pwd: '', url: '' })

const connDialogTitle = computed(() => (connForm.value.id ? '编辑连接' : '添加连接'))

// 「所属目录」下拉数据：仅目录节点
const dirOptions = computed(() => {
  const map = (nodes) =>
    nodes
      .filter(n => n.type === 'dir')
      .map(n => ({ id: n.id, value: n.id, label: n.label, children: map(n.children || []) }))
  return map(treeData.value)
})

const openAddConn = (parentId = '') => {
  connForm.value = { id: '', name: '', dbType: 'mysql', parentId: parentId || '', user: '', pwd: '', url: '' }
  connDialogVisible.value = true
}

const openEditConn = (node) => {
  const raw = node.raw || {}
  connForm.value = {
    id: node.id,
    name: raw.name || node.label || '',
    dbType: raw.dbType || node.dbType || 'mysql',
    parentId: raw.parentId || '',
    user: raw.user || '',
    pwd: '',
    url: raw.url || ''
  }
  connDialogVisible.value = true
}

const submitConn = () => {
  const form = connForm.value
  if (!form.name || !form.name.trim()) { ElMessage.warning('请输入连接名称'); return }
  if (!form.dbType) { ElMessage.warning('请选择数据库类型'); return }
  if (!form.url || !form.url.trim()) { ElMessage.warning('请输入连接信息'); return }

  const isCreate = !form.id
  const payload = {
    id: form.id || '',
    name: form.name,
    dbType: form.dbType,
    parentId: form.parentId || '',
    user: form.user || '',
    url: form.url
  }
  if (form.pwd) payload.pwd = form.pwd

  connSaving.value = true
  saveConn(payload).then((resp) => {
    const saved = (resp.data && resp.data.data) || null
    const targetId = saved && saved.parentId != null ? saved.parentId : (form.parentId || '')

    if (isCreate) {
      const node = {
        id: (saved && saved.id) || '',
        label: (saved && saved.name) || form.name,
        type: 'conn',
        dbType: (saved && saved.dbType) || form.dbType,
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
  }).finally(() => {
    connSaving.value = false
  })
}

const testConnForm = () => {
  if (!connForm.value.url) { ElMessage.warning('请先填写连接信息'); return }
  connTesting.value = true
  testDbConn(connForm.value)
    .then((resp) => {
      if (resp.data.code === 200) ElMessage.success('数据库连接成功')
    })
    .catch(() => {})
    .finally(() => { connTesting.value = false })
}

const testConnNode = (node) => {
  node.testing = true
  testDbConn(node.raw)
    .then((resp) => {
      if (resp.data.code === 200) ElMessage.success('数据库连接成功')
    })
    .catch(() => {})
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