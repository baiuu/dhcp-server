<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">系统日志</h2>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-form :inline="true" :model="filters" class="filter-form">
        <el-form-item label="级别">
          <el-select v-model="filters.level" clearable placeholder="全部" style="width: 120px">
            <el-option label="警告" value="WARN" />
            <el-option label="错误" value="ERROR" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点">
          <el-select v-model="filters.node_id" clearable placeholder="全部节点" style="width: 180px">
            <el-option v-for="n in nodes" :key="n.node_id" :label="n.node_id" :value="n.node_id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onSearch">查询</el-button>
          <el-button @click="resetFilters">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table :data="logs" size="default" stripe empty-text="暂无日志">
        <el-table-column label="时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
        <el-table-column label="级别" width="90">
          <template #default="{ row }">
            <el-tag size="small" effect="dark" :type="row.level === 'ERROR' ? 'danger' : 'warning'">{{ row.level }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="node_id" label="节点" min-width="160" show-overflow-tooltip />
        <el-table-column prop="message" label="消息" min-width="200" show-overflow-tooltip />
        <el-table-column label="详情" min-width="300" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ formatAttrs(row.attrs) }}</span>
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
  </div>
</template>

<script setup>
import { ref, onMounted, reactive } from 'vue'
import { get, showError } from '../api/request'
import { PAGE_SIZE, formatDate } from '../utils'

const loading = ref(false)
const logs = ref([])
const nodes = ref([])
const page = ref(1)
const total = ref(0)

const filters = reactive({
  level: '',
  node_id: ''
})

function formatAttrs(attrs) {
  if (!attrs) return '-'
  try {
    const obj = typeof attrs === 'string' ? JSON.parse(attrs) : attrs
    return Object.entries(obj).map(([k, v]) => `${k}=${v}`).join(', ')
  } catch {
    return String(attrs)
  }
}

function buildQuery() {
  const params = new URLSearchParams({ page: page.value, page_size: PAGE_SIZE })
  if (filters.level) params.append('level', filters.level)
  if (filters.node_id) params.append('node_id', filters.node_id)
  return params.toString()
}

async function load() {
  loading.value = true
  try {
    const data = await get(`/system-logs?${buildQuery()}`)
    logs.value = data.items || []
    total.value = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
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
  filters.level = ''
  filters.node_id = ''
  page.value = 1
  load()
}

onMounted(async () => {
  await loadNodes()
  await load()
})
</script>

<style scoped>
.filter-form {
  margin-bottom: 16px;
}
</style>
