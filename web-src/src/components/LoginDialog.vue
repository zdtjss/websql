<template>
  <el-dialog v-model="dialogVisible" @close="handleClose" width="350px" @keyup.enter="login" @opened="loginNameInput?.focus()">
    <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" label-width="80px">
      <el-form-item label="用户名" prop="name">
        <el-input ref="loginNameInput" v-model="loginForm.name" />
      </el-form-item>
      <el-form-item label="密&nbsp;&nbsp;&nbsp;码" prop="password">
        <el-input v-model="loginForm.password" type="password" />
      </el-form-item>
    </el-form>
    <template #footer>
      <span class="dialog-footer">
        <el-button type="primary" @click="login" :loading="logining">登录</el-button>
        <el-button @click="handleClose">关闭</el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup>
import http from '@/js/utils/httpProxy.js'
import { ElMessage } from 'element-plus'
import { client, server } from '@passwordless-id/webauthn'
import { computed, reactive, ref } from 'vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue', 'login-success', 'login-cancel'])

const dialogVisible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const loginFormRef = ref()
const loginNameInput = ref()
const loginForm = ref({ name: '', password: '' })
const logining = ref(false)

const loginRules = reactive({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' }
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' }
  ]
})

const bioLocalStorageKey = 'nway_websql_bio_credential_id'

function handleClose() {
  emit('update:modelValue', false)
  emit('login-cancel')
}

function login() {
  loginFormRef.value.validate(isValid => {
    if (isValid) {
      logining.value = true
      const params = new URLSearchParams()
      params.append('name', loginForm.value.name)
      params.append('password', loginForm.value.password)
      params.append('loginType', 'pwd')
      http.post('/login', params, {
        headers: {
          'Content-Type': 'application/x-www-form-urlencoded'
        }
      }).then((resp) => {
        sessionStorage.setItem('authentication', resp.headers.get('authentication'))
        sessionStorage.setItem('currentUser', JSON.stringify(resp.data.data))
        loginForm.value = {}
        logining.value = false
        emit('update:modelValue', false)
        emit('login-success', resp.data.data)
        ElMessage('登陆成功')
      }).finally(() => {
        logining.value = false
      })
    }
  })
}

function loginByToken(token) {
  const params = new URLSearchParams()
  params.append('key', token)
  params.append('loginType', 'token')
  http.post('/login', params).then((resp) => {
    if (resp.data.code == 200) {
      sessionStorage.setItem('authentication', resp.data.data['authentication'])
      sessionStorage.setItem('currentUser', JSON.stringify(resp.data.data))
      loginForm.value = {}
      logining.value = false
      emit('update:modelValue', false)
      emit('login-success', resp.data.data)
      ElMessage('登陆成功')
    } else {
      console.error('[LoginDialog] 登录失败 - code:', resp.data.code)
      ElMessage('登录失败')
    }
  }).catch((error) => {
    console.error('[LoginDialog] 登录异常:', error)
    ElMessage('登录失败')
  })
}

async function loginBio() {
  const credential = window.localStorage.getItem(bioLocalStorageKey)
  let authentication = await client.authenticate({
    allowCredentials: credential == null ? [] : [JSON.parse(credential)],
    challenge: server.randomChallenge()
  })

  const authenticationParsed = await JSON.parse(authentication)

  const params = new URLSearchParams()
  params.append('key', authenticationParsed.credentialId)
  params.append('loginType', 'bio')
  http.post('/login', params).then((resp) => {
    if (resp.data.code == 200) {
      sessionStorage.setItem('authentication', resp.headers.get('authentication'))
      sessionStorage.setItem('currentUser', JSON.stringify(resp.data.data))
      loginForm.value = {}
      logining.value = false
      emit('update:modelValue', false)
      emit('login-success', resp.data.data)
      ElMessage('登陆成功')
    } else {
      console.error('[LoginDialog] bio登录失败 - code:', resp.data.code)
      ElMessage('登录失败')
      emit('update:modelValue', true)
    }
  }).catch((error) => {
    console.error('[LoginDialog] bio登录异常:', error)
    ElMessage('登录失败')
  })
}

function tryAutoLogin() {
  const searchParams = new URLSearchParams(window.location.search)
  const authorization = searchParams.get('authorization')
  if (authorization) {
    loginByToken(authorization)
  } else {
    const credentialId = window.localStorage.getItem(bioLocalStorageKey)
    if (credentialId && client.isAvailable()) {
      loginBio()
    } else {
      emit('update:modelValue', true)
    }
  }
}

defineExpose({
  tryAutoLogin
})
</script>
