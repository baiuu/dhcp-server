<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">租约</h2>
      <div class="page-tools">
        <div class="search-form">
          <el-input v-model="searchQuery" placeholder="搜索 MAC / DUID" style="width: 260px" clearable @keyup.enter="search" />
          <el-button type="primary" :icon="Search" @click="search">搜索</el-button>
        </div>
      </div>
    </div>

    <el-tabs v-model="activeTab" class="filter-tabs" type="border-card">
      <el-tab-pane label="全部" name="all" />
      <el-tab-pane label="IPv4" name="ipv4" />
      <el-tab-pane label="IPv6" name="ipv6" />
    </el-tabs>

    <el-empty v-if="!loading && !filteredGroups.length && !searchResults.length" description="暂无租约数据" />

    <el-card v-if="searchResults.length" class="table-card" shadow="hover">
      <template #header>
        <div class="card-header"><el-icon><Search /></el-icon><span>搜索结果 ({{ searchResults.length }})</span></div>
      </template>
      <LeaseTable :data="searchResults" @refresh="load" />
    </el-card>

    <el-card v-for="group in filteredGroups" :key="group.scope.id" class="table-card" shadow="hover" v-loading="group.loading">
      <template #header>
        <div class="flex-between">
          <div class="card-header">
            <el-icon><DocumentCopy /></el-icon>
            <span>{{ group.scope.name }}</span>
            <el-tag :type="group.scope.v6 ? 'info' : 'success'" size="small">{{ group.scope.v6 ? 'IPv6' : 'IPv4' }}</el-tag>
          </div>
          <el-tag size="small" type="info">共 {{ group.total }} 条</el-tag>
        </div>
      </template>
      <LeaseTable :data="group.leases" :scope="group.scope" @refresh="loadScopeLeases(group)" />
      <div class="pagination-bar">
        <el-pagination
          background
          small
          layout="prev, pager, next"
          v-model:current-page="group.page"
          :page-size="PAGE_SIZE"
          :total="group.total"
          @current-change="loadScopeLeases(group)"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Search, DocumentCopy } from '@element-plus/icons-vue'
import { get, showError } from '../api/request'
import { PAGE_SIZE, isStandardMAC } from '../utils'
import LeaseTable from '../components/LeaseTable.vue'

const scopes = ref([])
const groups = ref([])
const activeTab = ref('all')
const searchQuery = ref('')
const searchResults = ref([])
const loading = ref(false)

const filteredGroups = computed(() => {
  if (activeTab.value === 'all') return groups.value
  return groups.value.filter(g => (activeTab.value === 'ipv6') === g.scope.v6)
})

async function load() {
  loading.value = true
  try {
    const data = await get('/scopes?limit=1000')
    scopes.value = data.items || []
    groups.value = scopes.value.map(s => ({ scope: s, leases: [], page: 1, total: 0, loading: false }))
    for (const g of groups.value) {
      await loadScopeLeases(g)
    }
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

async function loadScopeLeases(group) {
  group.loading = true
  try {
    const data = await get(`/scopes/${group.scope.id}/leases?page=${group.page}&page_size=${PAGE_SIZE}`)
    group.leases = data.items || []
    group.total = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    group.loading = false
  }
}

async function search() {
  const q = searchQuery.value.trim()
  if (!q) {
    searchResults.value = []
    return
  }
  try {
    const isMac = isStandardMAC(q)
    const param = isMac ? `mac=${encodeURIComponent(q)}` : `duid=${encodeURIComponent(q)}`
    const data = await get('/leases/search?' + param)
    searchResults.value = [...(data.v4 || []), ...(data.v6 || [])]
  } catch (err) {
    showError(err)
  }
}

onMounted(load)
</script>
