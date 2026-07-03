<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">作用域</h2>
      <div class="page-tools">
        <el-button v-if="auth.role !== 'readonly'" type="primary" :icon="Plus" @click="openForm()">新增作用域</el-button>
      </div>
    </div>
    <el-tabs v-model="activeTab" class="filter-tabs" type="border-card">
      <el-tab-pane label="全部" name="all" />
      <el-tab-pane label="IPv4" name="ipv4" />
      <el-tab-pane label="IPv6" name="ipv6" />
    </el-tabs>
    <el-card shadow="hover" v-loading="loading">
      <el-table :data="scopes" size="default" stripe empty-text="暂无作用域">
        <el-table-column prop="name" label="名称" min-width="140" show-overflow-tooltip />
        <el-table-column label="类型" width="80">
          <template #default="{ row }">
            <el-tag :type="row.v6 ? 'info' : 'success'" size="small">{{ row.v6 ? 'IPv6' : 'IPv4' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="子网/前缀" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ row.v6 ? (row.prefix || row.subnet) : row.subnet }}</template>
        </el-table-column>
        <el-table-column label="范围" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ row.start_ip }} - {{ row.end_ip }}</template>
        </el-table-column>
        <el-table-column label="网关" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ (row.gateway || []).join(', ') || '-' }}</template>
        </el-table-column>
        <el-table-column label="DNS" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ (row.dns || []).join(', ') || '-' }}</template>
        </el-table-column>
        <el-table-column label="保留/排除 IP" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ (row.excluded_ips || []).join(', ') || '-' }}</template>
        </el-table-column>
        <el-table-column prop="lease_time" label="租期" width="90" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small" effect="dark">{{ row.enabled ? '启用' : '禁用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="auth.role !== 'readonly'" label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button-group>
              <el-button size="small" :icon="Edit" title="编辑" @click="openForm(row)" />
              <el-button size="small" type="danger" :icon="Delete" title="删除" @click="remove(row)" />
            </el-button-group>
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
    <ScopeFormDialog v-model="dialogVisible" :scope="current" @saved="load" />
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { get, del, showError, showSuccess } from '../api/request'
import { PAGE_SIZE } from '../utils'
import ScopeFormDialog from '../components/ScopeFormDialog.vue'

const auth = useAuthStore()
const loading = ref(false)
const scopes = ref([])
const activeTab = ref('all')
const page = ref(1)
const total = ref(0)
const dialogVisible = ref(false)
const current = ref(null)

watch(activeTab, () => { page.value = 1; load() })

async function load() {
  loading.value = true
  try {
    const params = new URLSearchParams({ page: String(page.value), page_size: String(PAGE_SIZE) })
    if (activeTab.value === 'ipv4') params.set('v6', 'false')
    if (activeTab.value === 'ipv6') params.set('v6', 'true')
    const data = await get('/scopes?' + params.toString())
    scopes.value = data.items || []
    total.value = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

function openForm(scope = null) {
  current.value = scope
  dialogVisible.value = true
}

async function remove(scope) {
  try {
    await ElMessageBox.confirm(`确认删除作用域 "${scope.name}"？相关绑定地址、租约和前缀将一并删除。`, '提示', { type: 'warning' })
    await del('/scopes/' + scope.id)
    showSuccess('已删除')
    load()
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}

onMounted(load)
</script>
