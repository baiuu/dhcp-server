<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">MAC 黑名单</h2>
      <div class="page-tools">
        <el-button v-if="auth.role !== 'readonly'" type="primary" :icon="Plus" @click="dialogVisible = true">新增黑名单</el-button>
      </div>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-table :data="pagedList" size="default" stripe empty-text="暂无黑名单">
        <el-table-column prop="mac_addr" label="MAC 地址" min-width="180" />
        <el-table-column label="原因" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ row.reason || '-' }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
        <el-table-column v-if="auth.role !== 'readonly'" label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-bar">
        <el-pagination
          background
          layout="total, prev, pager, next"
          v-model:current-page="page"
          :page-size="PAGE_SIZE"
          :total="list.length"
        />
      </div>
    </el-card>
    <el-dialog title="新增 MAC 黑名单" v-model="dialogVisible" width="460px" @closed="reset">
      <el-form :model="form" label-width="90px" :rules="rules" ref="formRef">
        <el-form-item label="MAC 地址" prop="mac_addr">
          <el-input v-model="form.mac_addr" placeholder="00:11:22:33:44:55" />
        </el-form-item>
        <el-form-item label="原因">
          <el-input v-model="form.reason" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit" :loading="saving">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Plus, Delete } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { get, post, del, showError, showSuccess } from '../api/request'
import { PAGE_SIZE, formatDate, isValidMAC } from '../utils'

const auth = useAuthStore()
const loading = ref(false)
const list = ref([])
const page = ref(1)
const dialogVisible = ref(false)
const saving = ref(false)
const formRef = ref()
const form = ref({ mac_addr: '', reason: '' })

const rules = {
  mac_addr: [{ required: true, message: '请输入 MAC 地址', trigger: 'blur' }],
}

const pagedList = computed(() => {
  const start = (page.value - 1) * PAGE_SIZE
  return list.value.slice(start, start + PAGE_SIZE)
})

async function load() {
  loading.value = true
  try {
    const data = await get('/mac-blacklist')
    list.value = data.items || data || []
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

function reset() {
  form.value = { mac_addr: '', reason: '' }
}

async function submit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  if (!isValidMAC(form.value.mac_addr)) {
    showError('MAC 格式不正确')
    return
  }
  saving.value = true
  try {
    await post('/mac-blacklist', form.value)
    showSuccess('已添加')
    dialogVisible.value = false
    load()
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm('确认删除这条黑名单？', '提示', { type: 'warning' })
    await del('/mac-blacklist/' + row.id)
    showSuccess('已删除')
    load()
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}

onMounted(load)
</script>
