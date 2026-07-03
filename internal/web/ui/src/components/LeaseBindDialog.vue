<template>
  <el-dialog title="固定为绑定地址" v-model="visible" width="520px" @closed="reset" destroy-on-close>
    <el-form :model="form" label-width="120px">
      <el-form-item>
        <template #label>
          <el-checkbox v-model="form.useId">保留 {{ idLabel }}</el-checkbox>
        </template>
        <el-input v-model="form.idValue" :disabled="!form.useId" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <el-checkbox v-model="form.useIp">绑定地址</el-checkbox>
        </template>
        <el-input v-model="form.ipValue" :disabled="!form.useIp" />
      </el-form-item>
      <el-form-item>
        <template #label>
          <el-checkbox v-model="form.useHost">保留主机名</el-checkbox>
        </template>
        <el-input v-model="form.hostValue" :disabled="!form.useHost" />
      </el-form-item>
      <el-form-item label="描述">
        <el-input v-model="form.description" />
      </el-form-item>
      <el-form-item label="配置组">
        <el-select v-model="form.group_id" clearable placeholder="请选择" style="width: 100%">
          <el-option label="默认" value="__default__" />
          <el-option label="使用自定义配置组" value="__custom__" />
          <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
        </el-select>
      </el-form-item>

      <template v-if="isRealGroupSelected">
        <el-divider content-position="left">{{ selectedGroupName }} 配置组 Options（只读）</el-divider>
        <GroupOptionsPreview :options="selectedGroup?.options || {}" />
      </template>

      <template v-if="form.group_id === '__custom__'">
        <el-divider content-position="left">常用 Options（覆盖配置组 / 作用域默认值）</el-divider>

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
        <el-form-item label="广播地址" v-if="!isV6">
          <el-input v-model="broadcastInput" placeholder="如 192.168.2.255" @input="syncJsonFromFields" />
        </el-form-item>

        <el-form-item label="高级 Options">
          <el-input v-model="optionsInput" type="textarea" :rows="4" />
        </el-form-item>
      </template>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="submit" :loading="saving">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { get, post, showError, showSuccess } from '../api/request'
import { isLeaseV6 } from '../utils'
import GroupOptionsPreview from './GroupOptionsPreview.vue'

const props = defineProps({
  modelValue: Boolean,
  lease: Object,
})
const emit = defineEmits(['update:modelValue', 'saved'])

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const isV6 = computed(() => props.lease ? isLeaseV6(props.lease) : false)
const idLabel = computed(() => isV6.value ? 'DUID' : 'MAC 地址')
const isRealGroupSelected = computed(() => {
  const gid = form.value.group_id
  return gid && gid !== '__default__' && gid !== '__custom__'
})
const selectedGroup = computed(() => groups.value.find(g => g.id === form.value.group_id))
const selectedGroupName = computed(() => selectedGroup.value?.name || '')


const saving = ref(false)
const groups = ref([])
const optionsInput = ref('{}')
const customOptionsBackup = ref('{}')
const dnsInput = ref('')
const gatewayInput = ref('')
const domainInput = ref('')
const hostnameOptionInput = ref('')
const broadcastInput = ref('')
const form = ref({
  useId: true,
  idValue: '',
  useIp: true,
  ipValue: '',
  useHost: false,
  hostValue: '',
  description: '从租约绑定',
  group_id: '__default__'
})

watch(() => props.lease, (l) => {
  if (l) {
    form.value = {
      useId: true,
      idValue: isV6.value ? l.duid : l.mac_addr,
      useIp: true,
      ipValue: l.ip_addr,
      useHost: !!l.hostname,
      hostValue: l.hostname || '',
      description: '从租约绑定',
      group_id: '__default__'
    }
    optionsInput.value = '{}'
    customOptionsBackup.value = '{}'
    resetOptionFields()
  }
}, { immediate: true })

watch(() => form.value.group_id, (newVal, oldVal) => {
  if (oldVal === undefined) return
  if (newVal === '__custom__') {
    if (customOptionsBackup.value && customOptionsBackup.value !== '{}') {
      optionsInput.value = customOptionsBackup.value
    }
    syncFieldsFromJson()
  } else if (newVal === '__default__') {
    optionsInput.value = '{}'
    resetOptionFields()
  } else {
    // selected a real group
    if (oldVal === '__custom__') {
      customOptionsBackup.value = optionsInput.value
    }
    optionsInput.value = '{}'
    resetOptionFields()
  }
})

async function loadGroups() {
  try {
    groups.value = await get('/reservation-groups')
  } catch (err) {
    groups.value = []
  }
}

function reset() {
  form.value = {
    useId: true,
    idValue: '',
    useIp: true,
    ipValue: '',
    useHost: false,
    hostValue: '',
    description: '从租约绑定',
    group_id: '__default__'
  }
  optionsInput.value = '{}'
  customOptionsBackup.value = '{}'
  resetOptionFields()
}

function resetOptionFields() {
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
  if (!isV6.value) {
    setOption(options, '28', 'ip', broadcastInput.value.trim())
  }
  optionsInput.value = JSON.stringify(options, null, 2)
}

async function submit() {
  if (!form.value.useId || !form.value.useIp) {
    showError('MAC/DUID 和 IP 地址必须保留')
    return
  }
  let options = {}
  try {
    options = JSON.parse(optionsInput.value || '{}')
  } catch (e) {
    showError('Options JSON 格式错误')
    return
  }
  let group_id = form.value.group_id
  if (group_id === '__custom__') {
    group_id = ''
  } else if (group_id === '__default__') {
    group_id = ''
    options = {}
  } else {
    // real group selected, custom options are not active
    options = {}
  }
  const body = {
    ip_addr: form.value.ipValue,
    hostname: form.value.useHost ? form.value.hostValue : '',
    description: form.value.description,
    group_id,
    options
  }
  if (isV6.value) {
    body.duid = form.value.idValue
  } else {
    body.mac_addr = form.value.idValue
  }
  saving.value = true
  try {
    await post('/scopes/' + props.lease.scope_id + '/reservations', body)
    showSuccess('已绑定为绑定地址')
    visible.value = false
    emit('saved')
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}

onMounted(loadGroups)
</script>
