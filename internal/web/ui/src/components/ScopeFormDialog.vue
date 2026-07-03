<template>
  <el-dialog :title="title" v-model="visible" width="640px" @closed="reset" destroy-on-close>
    <el-form :model="form" label-width="110px" :rules="rules" ref="formRef">
      <el-row :gutter="20">
        <el-col :span="12">
          <el-form-item label="名称" prop="name">
            <el-input v-model="form.name" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="IPv6">
            <el-switch v-model="form.v6" active-text="IPv6" inactive-text="IPv4" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item label="子网 (CIDR)" prop="subnet">
        <el-input v-model="form.subnet" :placeholder="form.v6 ? '2001:db8::/64' : '192.168.1.0/24'" />
      </el-form-item>
      <el-form-item v-if="form.v6" label="前缀 (PD)">
        <el-input v-model="form.prefix" placeholder="2001:db8:ff00::/56" />
      </el-form-item>
      <el-row :gutter="20">
        <el-col :span="12">
          <el-form-item label="起始 IP" prop="start_ip">
            <el-input v-model="form.start_ip" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="结束 IP" prop="end_ip">
            <el-input v-model="form.end_ip" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item label="网关">
        <el-input v-model="gatewayInput" placeholder="多个用逗号分隔" />
      </el-form-item>
      <el-form-item label="DNS">
        <el-input v-model="dnsInput" placeholder="多个用逗号分隔" />
      </el-form-item>
      <el-form-item label="保留/排除 IP">
        <el-input v-model="excludedInput" placeholder="逗号分隔，这些地址不会被 DHCP 分配" />
      </el-form-item>
      <el-form-item label="域名">
        <el-input v-model="form.domain_name" />
      </el-form-item>
      <el-row :gutter="20">
        <el-col :span="12">
          <el-form-item label="租期 (秒)">
            <el-input-number v-model="form.lease_time" :min="60" style="width: 100%" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="最大租期 (秒)">
            <el-input-number v-model="form.max_lease_time" :min="60" style="width: 100%" />
          </el-form-item>
        </el-col>
      </el-row>
      <el-form-item label="启用">
        <el-switch v-model="form.enabled" />
      </el-form-item>
      <el-form-item label="Options (JSON)">
        <el-input v-model="optionsInput" type="textarea" :rows="4" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="submit" :loading="saving">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { post, put, showError, showSuccess } from '../api/request'
import { parseCommaIPs } from '../utils'

const props = defineProps({
  modelValue: Boolean,
  scope: Object,
})
const emit = defineEmits(['update:modelValue', 'saved'])

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const title = computed(() => (props.scope ? '编辑作用域' : '新增作用域'))
const formRef = ref()
const saving = ref(false)
const gatewayInput = ref('')
const dnsInput = ref('')
const excludedInput = ref('')
const optionsInput = ref('{}')

const defaultForm = {
  name: '',
  v6: false,
  subnet: '',
  prefix: '',
  start_ip: '',
  end_ip: '',
  domain_name: '',
  lease_time: 3600,
  max_lease_time: 86400,
  enabled: true,
  options: {}
}
const form = ref({ ...defaultForm })

const rules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  subnet: [{ required: true, message: '请输入子网', trigger: 'blur' }],
  start_ip: [{ required: true, message: '请输入起始 IP', trigger: 'blur' }],
  end_ip: [{ required: true, message: '请输入结束 IP', trigger: 'blur' }],
}

watch(() => props.scope, (s) => {
  if (s) {
    form.value = { ...defaultForm, ...s }
    gatewayInput.value = (s.gateway || []).join(', ')
    dnsInput.value = (s.dns || []).join(', ')
    excludedInput.value = (s.excluded_ips || []).join(', ')
    optionsInput.value = JSON.stringify(s.options || {}, null, 2)
  } else {
    reset()
  }
}, { immediate: true })

function reset() {
  form.value = { ...defaultForm }
  gatewayInput.value = ''
  dnsInput.value = ''
  excludedInput.value = ''
  optionsInput.value = '{}'
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
    ...form.value,
    gateway: parseCommaIPs(gatewayInput.value),
    dns: parseCommaIPs(dnsInput.value),
    excluded_ips: parseCommaIPs(excludedInput.value),
    options
  }
  saving.value = true
  try {
    if (props.scope) {
      await put('/scopes/' + props.scope.id, body)
    } else {
      await post('/scopes', body)
    }
    showSuccess('保存成功')
    visible.value = false
    emit('saved')
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}
</script>
