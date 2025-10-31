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
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import http from '@/js/utils/httpProxy'

const loadingConn = ref(false)

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
    param.connIdList = []
    param.connIdList.push(...roleConnTree.value.getHalfCheckedKeys())
    param.connIdList.push(...roleConnTree.value.getCheckedKeys())
    http.post("/saveRole", param)
        .then((resp) => {
            loadCfgData({ props: { name: 'role' } })
            ElMessage("保存成功")
        })
}

function checkPower(data, status) {
    powerListChecked.push(...status.checkedKeys)
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