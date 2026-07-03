import { ref } from 'vue'

const TOKEN_KEY = 'dhcp_token'
const ROLE_KEY = 'dhcp_role'

const token = ref(localStorage.getItem(TOKEN_KEY) || '')
const role = ref(localStorage.getItem(ROLE_KEY) || '')

export function useAuthStore() {
  function setAuth(newToken, newRole) {
    token.value = newToken
    role.value = newRole
    localStorage.setItem(TOKEN_KEY, newToken)
    localStorage.setItem(ROLE_KEY, newRole)
  }

  function clearAuth() {
    token.value = ''
    role.value = ''
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(ROLE_KEY)
  }

  return {
    token,
    role,
    setAuth,
    clearAuth,
  }
}
