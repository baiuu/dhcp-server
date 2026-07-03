<template>
  <div class="layout-wrapper">
    <aside class="sidebar">
      <div class="logo">
        <el-icon size="28"><Switch /></el-icon>
        <span>DHCP Server</span>
      </div>
      <el-menu
        :default-active="route.path"
        router
        background-color="#1e3c72"
        text-color="#fff"
        active-text-color="#ffd04b"
        class="menu"
      >
        <el-menu-item v-for="item in menuItems" :key="item.path" :index="item.path">
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.title }}</span>
        </el-menu-item>
      </el-menu>
    </aside>
    <div class="main-area">
      <header class="top-header">
        <div class="header-actions">
          <el-tag v-if="auth.role === 'readonly'" type="warning" effect="dark">只读</el-tag>
          <el-dropdown @command="handleCommand">
            <span class="user-info">
              <el-icon><UserFilled /></el-icon>
              {{ auth.role }}
              <el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="password">修改密码</el-dropdown-item>
                <el-dropdown-item divided command="logout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>
      <main class="main-content">
        <router-view />
      </main>
    </div>
  </div>
  <PasswordDialog v-model="passwordVisible" />
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { showSuccess } from '../api/request'
import PasswordDialog from '../components/PasswordDialog.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const passwordVisible = ref(false)

const menuItems = computed(() =>
  router.getRoutes().find(r => r.path === '/').children.map(c => ({
    path: '/' + c.path,
    title: c.meta.title,
    icon: c.meta.icon,
  }))
)

function handleCommand(cmd) {
  if (cmd === 'logout') {
    auth.clearAuth()
    showSuccess('已退出登录')
    router.push('/login')
  } else if (cmd === 'password') {
    passwordVisible.value = true
  }
}
</script>

<style scoped>
.layout-wrapper {
  display: flex;
  height: 100vh;
  overflow: hidden;
}
.sidebar {
  width: 220px;
  flex-shrink: 0;
  background-color: #1e3c72;
  color: #fff;
  display: flex;
  flex-direction: column;
  height: 100vh;
}
.logo {
  height: 60px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  font-size: 18px;
  font-weight: 600;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}
.menu {
  flex: 1;
  border-right: none;
  overflow-y: auto;
}
.main-area {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  height: 100vh;
}
.top-header {
  height: 60px;
  flex-shrink: 0;
  background-color: #fff;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 0 24px;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
  z-index: 10;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
.user-info {
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
}
.main-content {
  flex: 1;
  min-height: 0;
  background-color: #f0f2f5;
  overflow-y: auto;
}
</style>
