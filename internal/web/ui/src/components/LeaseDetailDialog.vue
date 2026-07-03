<template>
  <el-dialog title="租约详情" v-model="visible" width="520px" destroy-on-close>
    <el-descriptions :column="1" border>
      <el-descriptions-item :label="isV6 ? 'DUID' : 'MAC'">{{ lease ? (isV6 ? formatHex(lease.duid) : lease.mac_addr) : '-' }}</el-descriptions-item>
      <el-descriptions-item :label="isV6 ? 'IAID' : 'Client ID'">{{ lease ? (isV6 ? lease.iaid : formatHex(lease.client_id)) : '-' }}</el-descriptions-item>
      <el-descriptions-item label="IP 地址">{{ lease?.ip_addr }}</el-descriptions-item>
      <el-descriptions-item label="主机名">{{ lease?.hostname || '-' }}</el-descriptions-item>
      <el-descriptions-item label="状态">
        <el-tag :type="stateType(lease?.state)" size="small" effect="dark">{{ lease?.state }}</el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="开始时间">{{ formatDate(lease?.starts_at) }}</el-descriptions-item>
      <el-descriptions-item label="结束时间">{{ formatDate(lease?.ends_at) }}</el-descriptions-item>
    </el-descriptions>
  </el-dialog>
</template>

<script setup>
import { computed } from 'vue'
import { formatDate, formatHex, isLeaseV6 } from '../utils'

const props = defineProps({
  modelValue: Boolean,
  lease: Object,
})
const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const isV6 = computed(() => props.lease ? isLeaseV6(props.lease) : false)

function stateType(state) {
  if (state === 'active') return 'success'
  if (state === 'offered') return 'warning'
  return 'info'
}
</script>
