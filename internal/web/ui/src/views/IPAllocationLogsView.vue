<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">IP 分配记录</h2>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-form :inline="true" :model="filters" class="filter-form">
        <el-form-item label="作用域">
          <el-select v-model="filters.scope_id" clearable placeholder="全部" style="width: 160px">
            <el-option v-for="s in scopes" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点">
          <el-select v-model="filters.node_id" clearable placeholder="全部节点" style="width: 180px">
            <el-option v-for="n in nodes" :key="n.node_id" :label="n.node_id" :value="n.node_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="MAC/DUID">
          <el-input v-model="filters.mac" clearable placeholder="MAC 或 DUID" style="width: 180px" />
        </el-form-item>
        <el-form-item label="IP/Prefix">
          <el-input v-model="filters.ip" clearable placeholder="IP 或 Prefix" style="width: 160px" />
        </el-form-item>
        <el-form-item label="动作">
          <el-select v-model="filters.action" clearable placeholder="全部" style="width: 120px">
            <el-option label="提供" value="offer" />
            <el-option label="确认" value="ack" />
            <el-option label="续约" value="renew" />
            <el-option label="释放" value="release" />
            <el-option label="拒绝" value="decline" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">查询</el-button>
          <el-button @click="resetFilters">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table :data="logs" size="default" stripe empty-text="暂无记录">
        <el-table-column label="时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
        <el-table-column label="动作" width="90">
          <template #default="{ row }">
            <el-tag size="small" effect="dark" :type="actionType(row.action)">{{ actionLabel(row.action) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="scope_name" label="作用域" min-width="160" show-overflow-tooltip />
        <el-table-column prop="node_id" label="节点" min-width="160" show-overflow-tooltip />
        <el-table-column label="MAC / DUID" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ row.mac_addr || row.duid || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="IP / Prefix" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ row.ip_addr || row.prefix || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="relay_ip" label="Relay" width="140" show-overflow-tooltip />
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
  </div>
</template>

<script setup>
import { ref, onMounted, reactive } from 'vue'
import { get, showError } from '../api/request'
import { PAGE_SIZE, formatDate } from '../utils'

const loading = ref(false)
const logs = ref([])
const scopes = ref([])
const nodes = ref([])
const page = ref(1)
const total = ref(0)

const filters = reactive({
  scope_id: '',
  node_id: '',
  mac: '',
  ip: '',
  action: ''
})

function actionType(action) {
  const map = { offer: 'info', ack: 'success', renew: 'success', release: 'warning', decline: 'danger' }
  return map[action] || ''
}

function actionLabel(action) {
  const map = { offer: '提供', ack: '确认', renew: '续约', release: '释放', decline: '拒绝' }
  return map[action] || action
}

function buildQuery() {
  const params = new URLSearchParams({ page: page.value, page_size: PAGE_SIZE })
  if (filters.scope_id) params.append('scope_id', filters.scope_id)
  if (filters.node_id) params.append('node_id', filters.node_id)
  if (filters.mac) params.append('mac', filters.mac)
  if (filters.ip) params.append('ip', filters.ip)
  if (filters.action) params.append('action', filters.action)
  return params.toString()
}

async function load() {
  loading.value = true
  try {
    const data = await get(`/ip-allocation-logs?${buildQuery()}`)
    logs.value = data.items || []
    total.value = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

async function loadScopes() {
  try {
    const data = await get('/scopes')
    scopes.value = data || []
  } catch (err) {
    showError(err)
  }
}

async function loadNodes() {
  try {
    const data = await get('/cluster/nodes')
    nodes.value = data.nodes || []
  } catch (err) {
    showError(err)
  }
}

function onSearch() {
  page.value = 1
  load()
}

function resetFilters() {
  filters.scope_id = ''
  filters.node_id = ''
  filters.mac = ''
  filters.ip = ''
  filters.action = ''
  page.value = 1
  load()
}

onMounted(async () => {
  await loadScopes()
  await loadNodes()
  await load()
})
</script>

<style scoped>
.filter-form {
  margin-bottom: 16px;
}
</style>
