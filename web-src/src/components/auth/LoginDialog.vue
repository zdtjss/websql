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
import { useLogin } from '@/composables/useLogin'
import { getSystemConfig } from '@/api/system'
import http from '@/api/index'
import { ElMessage } from 'element-plus'
import { client, server } from '@passwordless-id/webauthn'
import { reactive, ref, useTemplateRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import { resetDefaultHomepageCache } from '@/router'

const router = useRouter()

const dialogVisible = defineModel({ default: false })

const emit = defineEmits(['login-success', 'login-cancel'])

const loginFormRef = useTemplateRef('loginFormRef')
const loginNameInput = useTemplateRef('loginNameInput')
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

watch(dialogVisible, (newVal) => {
  if (newVal) {
    tryAutoLogin()
  }
})

function handleClose() {
  dialogVisible.value = false
  emit('login-cancel')
}

function navigateAfterLogin() {
  resetDefaultHomepageCache()
  http.get('/system/config/all/get').then(resp => {
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
        if (resp.data.code !== 200) {
          ElMessage.error(resp.data.msg || '登录失败')
          return
        }
        sessionStorage.setItem('authentication', resp.headers.get('authentication'))
        sessionStorage.setItem('currentUser', JSON.stringify(resp.data.data))
        loginForm.value = {}
        dialogVisible.value = false
        emit('login-success', resp.data.data)
        ElMessage('登陆成功')
        navigateAfterLogin()
      }).catch(() => {
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
      dialogVisible.value = false
      emit('login-success', resp.data.data)
      ElMessage('登陆成功')
      navigateAfterLogin()
    } else {
      console.error('[LoginDialog] 登录失败 - code:', resp.data.code)
      ElMessage.error(resp.data.msg || '登录失败')
      dialogVisible.value = true
    }
  }).catch((error) => {
    console.error('[LoginDialog] 登录异常:', error)
    ElMessage.error('登录失败')
    dialogVisible.value = true
  })
}

async function loginBio() {
  const credential = window.localStorage.getItem(bioLocalStorageKey)
  let authentication = await client.authenticate({
    allowCredentials: credential == null ? [] : [JSON.parse(credential)],
    challenge: server.randomChallenge()
  })

  const params = new URLSearchParams()
  params.append('key', authentication.id)
  params.append('loginType', 'bio')
  http.post('/login', params).then((resp) => {
    if (resp.data.code == 200) {
      sessionStorage.setItem('authentication', resp.headers.get('authentication'))
      sessionStorage.setItem('currentUser', JSON.stringify(resp.data.data))
      loginForm.value = {}
      logining.value = false
      dialogVisible.value = false
      emit('login-success', resp.data.data)
      ElMessage('登陆成功')
      navigateAfterLogin()
    } else {
      console.error('[LoginDialog] bio登录失败 - code:', resp.data.code)
      ElMessage.error(resp.data.msg || '登录失败')
      dialogVisible.value = true
    }
  }).catch((error) => {
    console.error('[LoginDialog] bio登录异常:', error)
    ElMessage.error('登录失败')
    dialogVisible.value = true
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
      dialogVisible.value = false
      loginBio()
    }
  }
}

defineExpose({
  tryAutoLogin
})
</script>
