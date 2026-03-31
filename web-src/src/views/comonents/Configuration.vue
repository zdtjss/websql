<template>
    <el-tabs v-model="defaultTabAdmin" type="card" style="height:500px;" @tab-click="loadCfgData">
        <el-tab-pane v-if="isRemote" label="角色" name="role">
            <el-table :data="roleList" :max-height="450" style="width: 100%;overflow-y: auto;"
                @cell-dblclick="roleDblClick">
                <el-table-column prop="name" label="角色名" :show-overflow-tooltip="true" width="150">
                    <template #default="scope">
                        <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                        <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="连接" :show-overflow-tooltip="true">
                    <template #default="scope">
                        <span v-show="!scope.row.editable">{{ scope.row.connNameListStr }}</span>
                        <el-tree-select ref="roleConnTree" v-model="scope.row.connIdList" v-show="scope.row.editable"
                            @check="checkPower" style="width:100%" :data="connListSelect" node-key="id" multiple
                            collapse-tags collapse-tags-tooltip :check-on-click-node="true" show-checkbox
                            placeholder="请选择" />
                    </template>
                </el-table-column>
                <el-table-column style="text-align: center; " width="80">
                    <template #header>
                        <span>操作</span>
                        <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加"
                            @click="addRole">
                            <Plus />
                        </el-icon>
                    </template>
                    <template #default="scope">
                        <el-icon v-show="scope.row.editable" @click="saveRole(scope.row); scope.row.editable = false"
                            title="保存" style="margin-right:5px;cursor: pointer;">
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
        <el-tab-pane v-if="isRemote" label="用户" name="user">
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
                        <el-select v-show="scope.row.editable" v-model="scope.row.roleId" multiple filterable
                            collapse-tags collapse-tags-tooltip placeholder="请选择">
                            <el-option v-for="item in roleList" :key="item.id" :label="item.name" :value="item.id" />
                        </el-select>
                    </template>
                </el-table-column>
                <el-table-column style="text-align: center; " width="80">
                    <template #header>
                        <span>操作</span>
                        <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加"
                            @click="addUser">
                            <Plus />
                        </el-icon>
                    </template>
                    <template #default="scope">
                        <el-icon v-show="scope.row.editable" @click="saveUser(scope.row); scope.row.editable = false"
                            title="保存" style="margin-right:5px;cursor: pointer;">
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
            <el-form v-model="connQuery">
                <el-row>
                    <el-form-item label="连接名称">
                        <el-input v-model="connQuery.name" />
                    </el-form-item>
                    <el-form-item label="所属层级" style="margin-left: 10px;">
                        <el-tree-select v-model="connQuery.parentId" :data="conCfgTreeData" clearable value-key="id" placeholder="请选择" style="width: 200px" />
                    </el-form-item>
                    <el-form-item>
                        <el-button @click="listConnCfg" style="margin-left:12px;">查询</el-button>
                    </el-form-item>
                </el-row>
            </el-form>
            <el-table :data="connList" :max-height="450" style="width: 100%;overflow-y: auto;" empty-text="暂无连接"
                @cell-dblclick="(row) => row.editable = true" v-loading="loadingConn">
                <el-table-column prop="name" label="连接名称" width="150" :show-overflow-tooltip="true">
                    <template #default="scope">
                        <el-input v-show="scope.row.editable" v-model="scope.row.name" />
                        <span v-show="!scope.row.editable">{{ scope.row.name }}</span>
                    </template>
                </el-table-column>
                <el-table-column prop="dbType" label="数据库类型" width="100">
                    <template #default="scope">
                        <span v-show="!scope.row.editable">{{dbTypeList.filter(t => t.value ===
                            scope.row.dbType)[0].label
                            }}</span>
                        <el-select v-show="scope.row.editable" v-model="scope.row.dbType" placeholder="请选择">
                            <el-option v-for="item in dbTypeList" :key="item.value" :label="item.label"
                                :value="item.value" />
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
                        <el-icon style="cursor: pointer;position: relative;left: 8px;top: -5px;" title="添加"
                            @click="addConn">
                            <Plus />
                        </el-icon>
                    </template>
                    <template #default="scope">
                        <el-icon v-show="scope.row.editable" @click="testDbConn(scope.row)" :loading="scope.row.testing"
                            title="测试连接" style="margin-right:5px;cursor: pointer;">
                            <Connection />
                        </el-icon>
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
        <el-tab-pane label="系统配置" name="system">
            <div style="max-width: 100%; padding: 20px; overflow-y: auto; max-height: 420px;">
                <el-divider content-position="left">AI 服务配置</el-divider>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">AI 提供商</div>
                    <el-radio-group v-model="systemConfig.aiProvider">
                        <el-radio value="ollama">Ollama</el-radio>
                        <el-radio value="openai">OpenAI</el-radio>
                    </el-radio-group>
                    <el-button @click="testAiConfig" :loading="aiTesting" size="small">测试连接</el-button>
                </div>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">Base URL</div>
                    <el-input v-model="systemConfig.aiBaseUrl" placeholder="http://localhost:11434" style="flex: 1;" />
                </div>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">Model</div>
                    <el-input v-model="systemConfig.aiModel" placeholder="e.g. qwen2.5:14b" style="flex: 1;" />
                </div>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">API Key</div>
                    <el-input v-model="systemConfig.aiApiKey" type="password" show-password placeholder="sk-..." style="flex: 1;" />
                </div>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">Temperature</div>
                    <el-slider v-model="systemConfig.aiTemperature" :min="0" :max="2" :step="0.1" show-input style="flex: 1;" />
                </div>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">Max Tokens</div>
                    <el-input-number v-model="systemConfig.aiMaxTokens" :min="0" :max="128000" :step="1024" placeholder="0=不限" style="width: 200px;" />
                    <span style="font-size: 12px; color: #909399;">0 表示不限制</span>
                </div>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">思考模式</div>
                    <el-switch v-model="systemConfig.aiEnableThinking" />
                    <span style="font-size: 12px; color: #909399;">启用后模型会输出思考过程（需模型支持）</span>
                </div>

                <el-divider content-position="left">外部用户认证</el-divider>
                <div style="margin-bottom: 16px; display: flex; align-items: center; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap;">认证接口 URL</div>
                    <el-input v-model="systemConfig.outterUser" placeholder="http://localhost:8081/api/login" style="flex: 1;" />
                    <el-button @click="testOutterUser" :loading="testingOutterUser" size="small">测试接口</el-button>
                </div>
                
                <el-divider content-position="left">IP 访问控制</el-divider>
                <div style="margin-bottom: 16px; display: flex; align-items: flex-start; gap: 12px;">
                    <div style="font-weight: 500; white-space: nowrap; margin-top: 8px;">允许的 IP 列表</div>
                    <el-input 
                        v-model="systemConfig.allowedIP" 
                        type="textarea"
                        :rows="4"
                        placeholder="请输入 IP 地址，每行一个" 
                        style="flex: 1;"
                    />
                </div>

                <div style="display: flex; justify-content: flex-end; padding-top: 12px;">
                    <el-button type="primary" @click="saveAllConfig" :loading="savingAll">保存</el-button>
                </div>
            </div>
        </el-tab-pane>
        <el-tab-pane label="目录" name="dir">
            <div style="display: flex; flex-direction: column; height: 460px;">
                <div style="flex: 1; overflow-y: auto; padding: 20px;">
                    <el-tree :data="conCfgTreeData" draggable default-expand-all :expand-on-click-node="false">
                        <template #default="{ node, data }">
                            <div style="width:80%;">
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
                <div style="text-align: right; padding: 10px 20px; border-top: 1px solid #e4e7ed;">
                    <el-button type="primary" @click="saveTree">保存</el-button>
                </div>
            </div>
        </el-tab-pane>
    </el-tabs>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import http from '@/js/utils/httpProxy'

const loadingConn = ref(false)


const roleConnIdList = ref([])
const formLabelWidth = '100px'
const userList = ref([])
const connListSelect = ref([])
const roleList = ref([])
const userQuery = ref({
    name: "",
    loginName: ""
})
const connQuery = ref({
    name: "",
    parentId: ""
})
const connList = ref([])
const roleConnTree = ref([])
const powerListChecked = []
const conCfgTreeData = ref([])
const dbTypeList = ref([{ label: "MySQL", value: "mysql" }, { label: "Oracle", value: "oracle" }])

const defaultTabAdmin = computed(() => {
    return props.isRemote ? "role" : "conn"
})

const props = defineProps({
    isRemote: Boolean,
})

onMounted(() => {
    loadCfgData({ props: { name: defaultTabAdmin.value } })
})

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
        listDirTree()
       /*  http.get("/listConn2")
            .then((resp) => {
                connList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
                setTimeout(listDirTree(), 1000)
            }) */
    } else if (pane.props.name === "dir") {
        listDirTree()
    } else if (pane.props.name === "system") {
        loadSystemConfig()
    }
}

function addRole() {
    roleList.value.push({ editable: true })
    roleDblClick({})
}

function roleDblClick(row) {
    row.editable = true
    powerListChecked.splice(0, powerListChecked.length)
    row.connIdList = row.powerList ? row.powerList.map(r => r.connId) : []
    http.get("/connBaseTree")
        .then((resp) => {
            connListSelect.value = resp.data.data
        })
}

function saveRole(row) {
    const param = Object.assign({}, row)
    param.connIdList = roleConnIdList.value
    http.post("/saveRole", param)
        .then((resp) => {
            loadCfgData({ props: { name: 'role' } })
            ElMessage("保存成功")
        })
}

function checkPower(checkedNodes, checkedKeys) {
    powerListChecked.value = []
    roleConnIdList.value = []
    powerListChecked.push(...checkedKeys.checkedKeys)
    roleConnIdList.value.push(...checkedKeys.checkedKeys)
    roleConnIdList.value.push(...checkedKeys.halfCheckedKeys)
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


function addConn() {
    connList.value.unshift({ dbType: "mysql", editable: true })
}

function saveConnCfg(row) {
    http.post("/saveConn", row)
        .then((resp) => {
            row.editable = false
            ElMessage("保存成功")
        })
}

function testDbConn(row) {
    row.testing = true
    http.post("/testDbConn", row)
        .then((resp) => {
            if (resp.data.code === 200) {
                ElMessage.success("数据库连接成功")
            } else {
                ElMessage.error("数据库连接失败：" + resp.data.msg)
            }
        })
        .catch(() => {
            ElMessage.error("数据库连接失败：无法连接到数据库")
        })
        .finally(() => {
            row.testing = false
        })
}

function listConnCfg() {
    loadingConn.value = true
    const param = new URLSearchParams()
    param.append("name", connQuery.value.name)
    param.append("parentId", connQuery.value.parentId || '')
    http.get("/listConn2", { params: param })
        .then((resp) => {
            connList.value = resp.data.data.map(e => Object.assign({ editable: false }, e))
            setTimeout(listDirTree(), 1000)
        }).finally(() => loadingConn.value = false)
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


// System Config
const systemConfig = ref({ 
    aiProvider: 'ollama', 
    aiBaseUrl: '', 
    aiModel: '',
    aiApiKey: '',
    aiTemperature: 0.7,
    aiMaxTokens: 0,
    aiEnableThinking: false,
    outterUser: '', 
    allowedIP: '127.0.0.1\n::1' 
})
const aiTesting = ref(false)
const testingOutterUser = ref(false)
const savingAll = ref(false)

function loadSystemConfig() {
    http.get("/system/config/all/get")
        .then((resp) => {
            if (resp.data && resp.data.data) {
                const data = resp.data.data
                systemConfig.value.aiProvider = data.aiProvider || 'ollama'
                systemConfig.value.aiBaseUrl = data.aiBaseUrl || ''
                systemConfig.value.aiModel = data.aiModel || ''
                systemConfig.value.aiApiKey = data.aiApiKey || ''
                systemConfig.value.aiTemperature = parseFloat(data.aiTemperature) || 0.7
                systemConfig.value.aiMaxTokens = parseInt(data.aiMaxTokens) || 0
                systemConfig.value.aiEnableThinking = data.aiEnableThinking === 'true'
                systemConfig.value.outterUser = data.outterUser || ''
                
                if (data.allowedIP && Array.isArray(data.allowedIP)) {
                    systemConfig.value.allowedIP = data.allowedIP.join('\n')
                }
            }
        })
}

function saveAllConfig() {
    savingAll.value = true
    // 将 IP 文本按行分割为数组
    const ips = systemConfig.value.allowedIP.split('\n').map(ip => ip.trim()).filter(ip => ip !== '')
    
    http.post("/system/config/all/save", {
        aiProvider: systemConfig.value.aiProvider,
        aiBaseUrl: systemConfig.value.aiBaseUrl,
        aiModel: systemConfig.value.aiModel,
        aiApiKey: systemConfig.value.aiApiKey,
        aiTemperature: String(systemConfig.value.aiTemperature),
        aiMaxTokens: String(systemConfig.value.aiMaxTokens),
        aiEnableThinking: String(systemConfig.value.aiEnableThinking),
        outterUser: systemConfig.value.outterUser,
        allowedIP: ips
    })
        .then(() => {
            ElMessage.success("保存成功")
        })
        .finally(() => savingAll.value = false)
}

function testAiConfig() {
    aiTesting.value = true
    http.post("/ai/config/test", {
        provider: systemConfig.value.aiProvider,
        baseUrl: systemConfig.value.aiBaseUrl,
        model: systemConfig.value.aiModel,
        apiKey: systemConfig.value.aiApiKey
    })
        .then(() => {
            ElMessage.success("连接成功")
        })
        .catch(() => {
            // error already shown by httpProxy interceptor
        })
        .finally(() => aiTesting.value = false)
}

function testOutterUser() {
    testingOutterUser.value = true
    http.post("/system/config/outterUser/test", { url: systemConfig.value.outterUser })
        .then((resp) => {
            if (resp.data.code === 200) {
                ElMessage.success("测试成功：" + JSON.stringify(resp.data.data))
            } else {
                ElMessage.error("测试失败：" + resp.data.msg)
            }
        })
        .catch(() => {
            ElMessage.error("测试失败：接口无响应")
        })
        .finally(() => testingOutterUser.value = false)
}

const appendTreeNode = (data) => {
    const newChild = { label: "", value: "", children: [] }
    conCfgTreeData.value.push(newChild)
}

const removeDir = (node, data) => {
    const removeNode = (list, targetNode) => {
        const index = list.findIndex(item => item === targetNode)
        if (index !== -1) {
            list.splice(index, 1)
            return true
        }
        for (const item of list) {
            if (item.children && removeNode(item.children, targetNode)) {
                return true
            }
        }
        return false
    }
    
    removeNode(conCfgTreeData.value, data)
    
    if (data.id) {
        http.get("/delTreeNode", { params: { id: data.id } })
            .then((resp) => {
                conCfgTreeData.value = resp.data.data.length === 0 ? [{ label: "", value: "" }] : resp.data.data
            })
    }
}
</script>