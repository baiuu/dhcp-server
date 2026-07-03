<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">审计日志</h2>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-table :data="logs" size="default" stripe empty-text="暂无日志">
        <el-table-column label="时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
        <el-table-column prop="username" label="用户" width="140" />
        <el-table-column label="动作" width="120">
          <template #default="{ row }">
            <el-tag size="small" effect="dark">{{ row.action }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="resource" label="资源" width="140" />
        <el-table-column prop="resource_id" label="资源ID" min-width="200" show-overflow-tooltip />
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
import { ref, onMounted } from 'vue'
import { get, showError } from '../api/request'
import { PAGE_SIZE, formatDate } from '../utils'

const loading = ref(false)
const logs = ref([])
const page = ref(1)
const total = ref(0)

async function load() {
  loading.value = true
  try {
    const data = await get(`/audit-logs?page=${page.value}&page_size=${PAGE_SIZE}`)
    logs.value = data.items || []
    total.value = data.total || 0
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
