<template>
  <el-table :data="data" size="default" stripe empty-text="暂无租约" @row-click="showDetail">
    <el-table-column label="MAC / DUID" min-width="240" show-overflow-tooltip>
      <template #default="{ row }">
        <el-tag size="small" :type="row.duid ? 'info' : 'success'">{{ row.duid ? 'v6' : 'v4' }}</el-tag>
        <span class="ml-2">{{ row.duid ? `${row.duid}/${row.iaid}` : row.mac_addr }}</span>
      </template>
    </el-table-column>
    <el-table-column prop="ip_addr" label="IP" min-width="160" show-overflow-tooltip />
    <el-table-column label="主机名" min-width="140" show-overflow-tooltip>
      <template #default="{ row }">{{ row.hostname || '-' }}</template>
    </el-table-column>
    <el-table-column label="状态" width="100">
      <template #default="{ row }">
        <el-tag :type="stateType(row.state)" size="small" effect="dark">{{ row.state }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="ends_at" label="过期时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
    <el-table-column v-if="auth.role !== 'readonly'" label="操作" width="140" fixed="right">
      <template #default="{ row }">
        <template v-if="row.reserved">
          <el-tag size="small" type="success" effect="dark">已绑定</el-tag>
        </template>
        <el-button-group v-else>
          <el-button v-if="row.state === 'active' || row.state === 'offered'" size="small" type="primary" :icon="CircleCheck" title="固定" @click.stop="bind(row)" />
          <el-button v-if="row.state === 'active' || row.state === 'offered'" size="small" :icon="Unlock" title="释放" @click.stop="release(row)" />
          <el-button v-else size="small" type="danger" :icon="Delete" title="删除" @click.stop="remove(row)" />
        </el-button-group>
      </template>
    </el-table-column>
  </el-table>
  <LeaseBindDialog v-model="bindVisible" :lease="current" @saved="$emit('refresh')" />
  <LeaseDetailDialog v-model="detailVisible" :lease="currentDetail" />
</template>

<script setup>
import { ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { CircleCheck, Unlock, Delete } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { post, del, showError, showSuccess } from '../api/request'
import { formatDate, isLeaseV6 } from '../utils'
import LeaseBindDialog from './LeaseBindDialog.vue'
import LeaseDetailDialog from './LeaseDetailDialog.vue'

defineProps({
  data: { type: Array, default: () => [] },
})
const emit = defineEmits(['refresh'])

const auth = useAuthStore()
const bindVisible = ref(false)
const current = ref(null)
const detailVisible = ref(false)
const currentDetail = ref(null)

function stateType(state) {
  if (state === 'active') return 'success'
  if (state === 'offered') return 'warning'
  return 'info'
}

function showDetail(row) {
  currentDetail.value = row
  detailVisible.value = true
}

function bind(row) {
  current.value = row
  bindVisible.value = true
}

async function release(row) {
  try {
    await ElMessageBox.confirm('确认释放该租约？', '提示', { type: 'warning' })
    const path = isLeaseV6(row) ? `/v6-leases/${row.id}/release` : `/leases/${row.id}/release`
    await post(path)
    showSuccess('已释放')
    emit('refresh')
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm('确认删除该租约？', '提示', { type: 'warning' })
    const path = isLeaseV6(row) ? `/v6-leases/${row.id}` : `/leases/${row.id}`
    await del(path)
    showSuccess('已删除')
    emit('refresh')
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}
</script>
