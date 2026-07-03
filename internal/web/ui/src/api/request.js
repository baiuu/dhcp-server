import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import router from '../router'

const API_PREFIX = '/api'

async function request(path, options = {}) {
  const auth = useAuthStore()
  const headers = {
    'Content-Type': 'application/json',
    ...options.headers,
  }
  if (auth.token.value) {
    headers.Authorization = `Bearer ${auth.token.value}`
  }

  const response = await fetch(`${API_PREFIX}${path}`, {
    ...options,
    headers,
  })

  if (response.status === 401) {
    auth.clearAuth()
    router.push('/login')
    throw new Error('登录已过期，请重新登录')
  }

  let data
  try {
    data = await response.json()
  } catch (e) {
    data = { error: response.statusText }
  }

  if (!response.ok) {
    const msg = data.error || data.message || response.statusText
    throw new Error(msg)
  }

  return data
}

export function get(path) {
  return request(path, { method: 'GET' })
}

export function post(path, body) {
  return request(path, { method: 'POST', body: JSON.stringify(body) })
}

export function put(path, body) {
  return request(path, { method: 'PUT', body: JSON.stringify(body) })
}

export function del(path) {
  return request(path, { method: 'DELETE' })
}

export function showError(err) {
  ElMessage.error(err?.message || String(err))
}

export function showSuccess(msg) {
  ElMessage.success(msg)
}
