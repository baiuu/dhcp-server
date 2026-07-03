<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">用户</h2>
      <div class="page-tools">
        <el-button v-if="auth.role !== 'readonly'" type="primary" :icon="Plus" @click="openForm()">新增用户</el-button>
      </div>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-table :data="users" size="default" stripe empty-text="暂无用户">
        <el-table-column prop="username" label="用户名" min-width="160" />
        <el-table-column label="角色" width="120">
          <template #default="{ row }">
            <el-tag :type="row.role === 'admin' ? 'primary' : 'info'" size="small" effect="dark">{{ row.role }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
        <el-table-column label="操作" width="260" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Key" @click="openPassword(row.username)">改密</el-button>
            <el-button v-if="auth.role !== 'readonly'" size="small" :icon="Edit" @click="openForm(row)">编辑</el-button>
            <el-button v-if="auth.role !== 'readonly'" size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-bar">
        <el-pagination
          background
          layout="total, prev, pager, next"
          v-model:current-page="page"
          :page-size="PAGE_SIZE"
          :total="total"
          @current-change="load"
        />
      </div>
    </el-card>
    <UserFormDialog v-model="dialogVisible" :user="current" @saved="load" />
    <PasswordDialog v-model="passwordVisible" :username="passwordUser" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, Key } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { get, del, showError, showSuccess } from '../api/request'
import { PAGE_SIZE, formatDate } from '../utils'
import UserFormDialog from '../components/UserFormDialog.vue'
import PasswordDialog from '../components/PasswordDialog.vue'

const auth = useAuthStore()
const loading = ref(false)
const users = ref([])
const page = ref(1)
const total = ref(0)
const dialogVisible = ref(false)
const current = ref(null)
const passwordVisible = ref(false)
const passwordUser = ref('')

async function load() {
  loading.value = true
  try {
    const data = await get(`/users?page=${page.value}&page_size=${PAGE_SIZE}`)
    users.value = data.items || []
    total.value = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

function openForm(user = null) {
  current.value = user
  dialogVisible.value = true
}

function openPassword(username) {
  passwordUser.value = username
  passwordVisible.value = true
}

async function remove(user) {
  try {
    await ElMessageBox.confirm(`确认删除用户 "${user.username}"？`, '提示', { type: 'warning' })
    await del('/users/' + user.id)
    showSuccess('已删除')
    load()
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}

onMounted(load)
</script>
