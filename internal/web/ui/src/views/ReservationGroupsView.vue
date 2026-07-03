<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">配置组</h2>
      <div class="page-tools">
        <el-button v-if="auth.role !== 'readonly'" type="primary" :icon="Plus" @click="openForm()">新增配置组</el-button>
      </div>
    </div>
    <el-card shadow="hover" v-loading="loading">
      <el-table :data="groups" size="default" stripe empty-text="暂无配置组">
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
        <el-table-column label="更新时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button v-if="auth.role !== 'readonly'" size="small" :icon="Edit" @click="openForm(row)">编辑</el-button>
            <el-button v-if="auth.role !== 'readonly'" size="small" type="danger" :icon="Delete" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog :title="current ? '编辑配置组' : '新增配置组'" v-model="dialogVisible" width="560px" @closed="resetForm" destroy-on-close>
      <el-form :model="form" label-width="100px" :rules="rules" ref="formRef">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：办公区终端" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" placeholder="可选" />
        </el-form-item>

        <el-divider content-position="left">常用 Options</el-divider>
        <el-form-item label="DNS 服务器">
          <el-input v-model="dnsInput" placeholder="多个用逗号分隔，如 192.168.3.7, 8.8.8.8" @input="syncJsonFromFields" />
        </el-form-item>
        <el-form-item label="网关">
          <el-input v-model="gatewayInput" placeholder="多个用逗号分隔，如 192.168.2.1" @input="syncJsonFromFields" />
        </el-form-item>
        <el-form-item label="域名">
          <el-input v-model="domainInput" placeholder="如 example.com" @input="syncJsonFromFields" />
        </el-form-item>
        <el-form-item label="Option 主机名">
          <el-input v-model="hostnameOptionInput" placeholder="DHCP Option 12 主机名" @input="syncJsonFromFields" />
        </el-form-item>
        <el-form-item label="广播地址">
          <el-input v-model="broadcastInput" placeholder="如 192.168.2.255" @input="syncJsonFromFields" />
        </el-form-item>

        <el-form-item label="高级 Options">
          <el-input v-model="optionsInput" type="textarea" :rows="6" />
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
import { ref, onMounted } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { get, post, put, del, showError, showSuccess } from '../api/request'
import { formatDate } from '../utils'

const auth = useAuthStore()
const loading = ref(false)
const groups = ref([])
const dialogVisible = ref(false)
const saving = ref(false)
const current = ref(null)
const formRef = ref()
const form = ref({ name: '', description: '' })
const optionsInput = ref('{}')
const dnsInput = ref('')
const gatewayInput = ref('')
const domainInput = ref('')
const hostnameOptionInput = ref('')
const broadcastInput = ref('')

const rules = {
  name: [{ required: true, message: '请输入配置组名称', trigger: 'blur' }]
}

async function load() {
  loading.value = true
  try {
    groups.value = await get('/reservation-groups')
  } catch (err) {
    showError(err)
  } finally {
    loading.value = false
  }
}

function openForm(group = null) {
  current.value = group
  if (group) {
    form.value = { name: group.name || '', description: group.description || '' }
    optionsInput.value = JSON.stringify(group.options || {}, null, 2)
  } else {
    resetForm()
  }
  syncFieldsFromJson()
  dialogVisible.value = true
}

function resetForm() {
  form.value = { name: '', description: '' }
  optionsInput.value = '{}'
  dnsInput.value = ''
  gatewayInput.value = ''
  domainInput.value = ''
  hostnameOptionInput.value = ''
  broadcastInput.value = ''
}

function getOptionString(options, code) {
  const opt = options[code]
  if (!opt) return ''
  if (Array.isArray(opt.value)) return opt.value.join(', ')
  return opt.value || ''
}

function syncFieldsFromJson() {
  let options = {}
  try {
    options = JSON.parse(optionsInput.value || '{}')
  } catch (e) {
    return
  }
  dnsInput.value = getOptionString(options, '6')
  gatewayInput.value = getOptionString(options, '3')
  domainInput.value = getOptionString(options, '15')
  hostnameOptionInput.value = getOptionString(options, '12')
  broadcastInput.value = getOptionString(options, '28')
}

function parseIpList(s) {
  return s.split(/[,，;；\n]+/).map(x => x.trim()).filter(Boolean)
}

function setOption(options, code, type, value) {
  if (value === '' || (Array.isArray(value) && value.length === 0)) {
    delete options[code]
  } else {
    options[code] = { type, value }
  }
}

function syncJsonFromFields() {
  let options = {}
  try {
    options = JSON.parse(optionsInput.value || '{}')
  } catch (e) {
    options = {}
  }
  setOption(options, '6', 'ips', parseIpList(dnsInput.value))
  setOption(options, '3', 'ips', parseIpList(gatewayInput.value))
  setOption(options, '15', 'string', domainInput.value.trim())
  setOption(options, '12', 'string', hostnameOptionInput.value.trim())
  setOption(options, '28', 'ip', broadcastInput.value.trim())
  optionsInput.value = JSON.stringify(options, null, 2)
}

async function submit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  let options = {}
  try {
    options = JSON.parse(optionsInput.value || '{}')
  } catch (e) {
    showError('Options JSON 格式错误')
    return
  }
  const body = {
    name: form.value.name,
    description: form.value.description,
    options
  }
  saving.value = true
  try {
    if (current.value) {
      await put('/reservation-groups/' + current.value.id, body)
    } else {
      await post('/reservation-groups', body)
    }
    showSuccess('保存成功')
    dialogVisible.value = false
    load()
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}

async function remove(group) {
  try {
    await ElMessageBox.confirm(`确认删除配置组 "${group.name}"？绑定地址将恢复为自定义模式。`, '提示', { type: 'warning' })
    await del('/reservation-groups/' + group.id)
    showSuccess('已删除')
    load()
  } catch (err) {
    if (err !== 'cancel') showError(err)
  }
}

onMounted(load)
</script>
