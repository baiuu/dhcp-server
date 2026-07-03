<template>
  <el-dialog :title="title" v-model="visible" width="560px" @closed="reset" destroy-on-close>
    <el-form :model="form" label-width="100px" :rules="rules" ref="formRef">
      <el-form-item :label="idLabel" prop="idValue">
        <el-input v-model="form.idValue" :placeholder="idPlaceholder" />
      </el-form-item>
      <el-form-item label="IP 地址" prop="ipValue">
        <el-input v-model="form.ipValue" :placeholder="isV6 ? '2001:db8::50' : '192.168.1.50'" />
      </el-form-item>
      <el-form-item label="主机名">
        <el-input v-model="form.hostValue" />
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
        <el-divider content-position="left">常用 Options（覆盖作用域默认值）</el-divider>

        <el-form-item label="DNS 服务器">
          <el-input
            v-model="dnsInput"
            placeholder="多个用逗号分隔，如 192.168.3.7, 8.8.8.8"
            @input="syncJsonFromFields"
          />
        </el-form-item>
        <el-form-item label="网关">
          <el-input
            v-model="gatewayInput"
            placeholder="多个用逗号分隔，如 192.168.2.1"
            @input="syncJsonFromFields"
          />
        </el-form-item>
        <el-form-item label="域名">
          <el-input
            v-model="domainInput"
            placeholder="如 example.com"
            @input="syncJsonFromFields"
          />
        </el-form-item>
        <el-form-item label="Option 主机名">
          <el-input
            v-model="hostnameOptionInput"
            placeholder="DHCP Option 12 主机名"
            @input="syncJsonFromFields"
          />
        </el-form-item>
        <el-form-item label="广播地址" v-if="!isV6">
          <el-input
            v-model="broadcastInput"
            placeholder="如 192.168.2.255"
            @input="syncJsonFromFields"
          />
        </el-form-item>

        <el-form-item label="高级 Options">
          <el-input v-model="optionsInput" type="textarea" :rows="4" />
        </el-form-item>
      </template>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="submit" :loading="saving">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { get, post, put, showError, showSuccess } from '../api/request'
import { isValidMAC, isValidDUID } from '../utils'
import GroupOptionsPreview from './GroupOptionsPreview.vue'

const props = defineProps({
  modelValue: Boolean,
  scope: Object,
  reservation: Object,
})
const emit = defineEmits(['update:modelValue', 'saved'])

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const isV6 = computed(() => props.scope ? props.scope.v6 : false)
const idLabel = computed(() => isV6.value ? 'DUID' : 'MAC 地址')
const idPlaceholder = computed(() => isV6.value ? '00:01:00:01:...' : '00:11:22:33:44:55')
const title = computed(() => (props.reservation ? '编辑绑定地址' : '新增绑定地址'))
const isRealGroupSelected = computed(() => {
  const gid = form.value.group_id
  return gid && gid !== '__default__' && gid !== '__custom__'
})
const selectedGroup = computed(() => groups.value.find(g => g.id === form.value.group_id))
const selectedGroupName = computed(() => selectedGroup.value?.name || '')


const formRef = ref()
const saving = ref(false)
const optionsInput = ref('{}')
const customOptionsBackup = ref('{}')
const groups = ref([])
const defaultForm = { idValue: '', ipValue: '', hostValue: '', description: '', group_id: '__default__' }
const form = ref({ ...defaultForm })

const dnsInput = ref('')
const gatewayInput = ref('')
const domainInput = ref('')
const hostnameOptionInput = ref('')
const broadcastInput = ref('')

const rules = {
  idValue: [{ required: true, message: '请输入' + idLabel.value, trigger: 'blur' }],
  ipValue: [{ required: true, message: '请输入 IP 地址', trigger: 'blur' }],
}

function resolveGroupSelect(r) {
  if (r.group_id) return r.group_id
  const opts = r.options || {}
  return Object.keys(opts).length > 0 ? '__custom__' : '__default__'
}

watch(() => props.reservation, (r) => {
  if (r) {
    form.value = {
      idValue: isV6.value ? r.duid : r.mac_addr,
      ipValue: r.ip_addr,
      hostValue: r.hostname || '',
      description: r.description || '',
      group_id: resolveGroupSelect(r)
    }
    optionsInput.value = JSON.stringify(r.options || {}, null, 2)
    customOptionsBackup.value = optionsInput.value
    syncFieldsFromJson()
  } else {
    reset()
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

function resetOptionFields() {
  dnsInput.value = ''
  gatewayInput.value = ''
  domainInput.value = ''
  hostnameOptionInput.value = ''
  broadcastInput.value = ''
}

function reset() {
  form.value = { ...defaultForm }
  optionsInput.value = '{}'
  customOptionsBackup.value = '{}'
  resetOptionFields()
}

async function loadGroups() {
  try {
    groups.value = await get('/reservation-groups')
  } catch (err) {
    groups.value = []
  }
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
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  if (isV6.value) {
    if (!isValidDUID(form.value.idValue)) {
      showError('DUID 格式不正确')
      return
    }
  } else {
    if (!isValidMAC(form.value.idValue)) {
      showError('MAC 格式不正确')
      return
    }
  }
  let options = {}
  let group_id = form.value.group_id
  if (group_id === '__custom__') {
    group_id = ''
    try {
      options = JSON.parse(optionsInput.value || '{}')
    } catch (e) {
      showError('Options JSON 格式错误')
      return
    }
  } else if (group_id === '__default__') {
    group_id = ''
    options = {}
  } else {
    // real group selected, custom options are not active
    options = {}
  }
  const body = {
    ip_addr: form.value.ipValue,
    hostname: form.value.hostValue,
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
    if (props.reservation) {
      const path = isV6.value ? `/v6-reservations/${props.reservation.id}` : `/reservations/${props.reservation.id}`
      await put(path, body)
    } else {
      await post('/scopes/' + props.scope.id + '/reservations', body)
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

onMounted(loadGroups)
</script>
