<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">绑定地址</h2>
      <div class="page-tools">
        <div class="search-form">
          <el-select
            v-model="selectedScopeIds"
            multiple
            filterable
            clearable
            collapse-tags
            collapse-tags-tooltip
            placeholder="按作用域筛选"
            style="width: 320px"
            @change="onScopeFilterChange"
          >
            <el-option
              v-for="scope in scopes"
              :key="scope.id"
              :label="scope.name"
              :value="scope.id"
            >
              <span style="float: left">{{ scope.name }}</span>
              <span style="float: right; color: #8492a6; font-size: 13px">{{ scope.v6 ? 'IPv6' : 'IPv4' }}</span>
            </el-option>
          </el-select>
          <el-button :icon="Refresh" @click="load">刷新</el-button>
        </div>
      </div>
    </div>
    <el-tabs v-model="activeTab" class="filter-tabs" type="border-card">
      <el-tab-pane label="全部" name="all" />
      <el-tab-pane label="IPv4" name="ipv4" />
      <el-tab-pane label="IPv6" name="ipv6" />
    </el-tabs>
    <el-empty v-if="!loading && !filteredGroups.length" description="暂无绑定地址数据" />
    <el-card v-for="group in filteredGroups" :key="group.scope.id" class="table-card" shadow="hover" v-loading="group.loading">
      <template #header>
        <div class="flex-between">
          <div class="card-header">
            <el-icon><CollectionTag /></el-icon>
            <span>{{ group.scope.name }}</span>
            <el-tag :type="group.scope.v6 ? 'info' : 'success'" size="small">{{ group.scope.v6 ? 'IPv6' : 'IPv4' }}</el-tag>
          </div>
          <div class="page-tools">
            <el-tag size="small" type="info" class="mr-2">共 {{ group.total }} 条</el-tag>
            <el-button v-if="auth.role !== 'readonly'" size="small" type="primary" :icon="Plus" @click="openForm(group.scope)">新增</el-button>
          </div>
        </div>
      </template>
      <el-table :data="group.reservations" size="default" stripe empty-text="暂无绑定地址">
        <el-table-column label="MAC / DUID" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">{{ row.duid || row.mac_addr }}</template>
        </el-table-column>
        <el-table-column prop="ip_addr" label="IP" min-width="160" show-overflow-tooltip />
        <el-table-column label="主机名" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.hostname || '-' }}</template>
        </el-table-column>
        <el-table-column label="描述" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.description || '-' }}</template>
        </el-table-column>
        <el-table-column label="配置组" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ groupName(row.group_id) || '-' }}</template>
        </el-table-column>
        <el-table-column v-if="auth.role !== 'readonly'" label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :icon="Edit" @click="openForm(group.scope, row)">编辑</el-button>
            <el-button size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-bar">
        <el-pagination
          background
          small
          layout="prev, pager, next"
          v-model:current-page="group.page"
          :page-size="PAGE_SIZE"
          :total="group.total"
          @current-change="loadScopeReservations(group)"
        />
      </div>
    </el-card>
    <ReservationFormDialog v-model="dialogVisible" :scope="currentScope" :reservation="current" @saved="load" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete, CollectionTag, Refresh } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { get, del, showError, showSuccess } from '../api/request'
import { PAGE_SIZE, isReservationV6 } from '../utils'
import ReservationFormDialog from '../components/ReservationFormDialog.vue'

const auth = useAuthStore()
const scopes = ref([])
const groups = ref([])
const resGroups = ref([])
const loading = ref(false)
const activeTab = ref('all')
const selectedScopeIds = ref([])

function groupName(groupId) {
  if (!groupId) return ''
  const g = resGroups.value.find(x => x.id === groupId)
  return g ? g.name : ''
}
const dialogVisible = ref(false)
const currentScope = ref(null)
const current = ref(null)

const filteredGroups = computed(() => {
  let list = groups.value
  if (selectedScopeIds.value.length) {
    const selected = new Set(selectedScopeIds.value)
    list = list.filter(g => selected.has(g.scope.id))
  }
  if (activeTab.value === 'all') return list
  return list.filter(g => (activeTab.value === 'ipv6') === g.scope.v6)
})

function onScopeFilterChange() {
  load()
}

async function load() {
  loading.value = true
  try {
    const [data, groupsData] = await Promise.all([get('/scopes?limit=1000'), get('/reservation-groups')])
    resGroups.value = groupsData || []
    scopes.value = data.items || []
    const scopeIdSet = new Set(scopes.value.map(s => s.id))
    selectedScopeIds.value = selectedScopeIds.value.filter(id => scopeIdSet.has(id))
    groups.value = scopes.value.map(s => ({ scope: s, reservations: [], page: 1, total: 0, loading: false }))
    await loadDataForVisibleGroups()
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

async function loadDataForVisibleGroups() {
  const visible = filteredGroups.value
  for (const g of visible) {
    await loadScopeReservations(g)
  }
}

async function loadScopeReservations(group) {
  group.loading = true
  try {
    const data = await get(`/scopes/${group.scope.id}/reservations?page=${group.page}&page_size=${PAGE_SIZE}`)
    group.reservations = data.items || []
    group.total = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    group.loading = false
  }
}

function openForm(scope, reservation = null) {
  currentScope.value = scope
  current.value = reservation
  dialogVisible.value = true
}

async function remove(row) {
  try {
    await ElMessageBox.confirm('确认删除这条绑定地址？', '提示', { type: 'warning' })
    const path = isReservationV6(row) ? `/v6-reservations/${row.id}` : `/reservations/${row.id}`
    await del(path)
    showSuccess('已删除')
    load()
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}

onMounted(load)
</script>
