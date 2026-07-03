<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">集群节点</h2>
      <div class="page-tools">
        <el-tag type="info">Cluster: {{ clusterId || '-' }}</el-tag>
        <el-button :icon="Refresh" circle @click="load" />
      </div>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-table :data="nodes" size="default" stripe empty-text="暂无集群节点">
        <el-table-column prop="node_id" label="节点 ID" min-width="160" show-overflow-tooltip />
        <el-table-column prop="role" label="角色" width="120">
          <template #default="{ row }">
            <el-tag size="small" effect="dark">{{ row.role || 'active' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="listen_addr" label="监听地址" min-width="180" show-overflow-tooltip />
        <el-table-column prop="version" label="版本" width="120" />
        <el-table-column label="健康状态" width="120">
          <template #default="{ row }">
            <el-tag :type="row.healthy ? 'success' : 'danger'" size="small" effect="dark">
              {{ row.healthy ? '健康' : '异常' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后心跳" width="180" :formatter="(_, __, val) => formatDate(val)" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { get, showError } from '../api/request'
import { formatDate } from '../utils'

const loading = ref(false)
const clusterId = ref('')
const nodes = ref([])

async function load() {
  loading.value = true
  try {
    const data = await get('/cluster/nodes')
    clusterId.value = data.cluster_id || ''
    nodes.value = data.nodes || []
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
