<template>
  <el-dialog :title="title" v-model="visible" width="440px" @closed="reset" destroy-on-close>
    <el-form :model="form" label-width="80px" :rules="rules" ref="formRef">
      <el-form-item label="用户名" prop="username">
        <el-input v-model="form.username" :disabled="!!props.user" />
      </el-form-item>
      <el-form-item v-if="!props.user" label="密码" prop="password">
        <el-input v-model="form.password" type="password" show-password />
      </el-form-item>
      <el-form-item label="角色" prop="role">
        <el-select v-model="form.role" style="width: 100%">
          <el-option label="admin" value="admin" />
          <el-option label="readonly" value="readonly" />
        </el-select>
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

const props = defineProps({
  modelValue: Boolean,
  user: Object,
})
const emit = defineEmits(['update:modelValue', 'saved'])

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const title = computed(() => (props.user ? '编辑用户' : '新增用户'))
const formRef = ref()
const saving = ref(false)
const defaultForm = { username: '', password: '', role: 'admin' }
const form = ref({ ...defaultForm })

const rules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: !props.user, message: '请输入密码', trigger: 'blur' }],
  role: [{ required: true, message: '请选择角色', trigger: 'change' }],
}

watch(() => props.user, (u) => {
  if (u) {
    form.value = { username: u.username, password: '', role: u.role }
  } else {
    reset()
  }
}, { immediate: true })

function reset() {
  form.value = { ...defaultForm }
}

async function submit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  const body = { username: form.value.username, role: form.value.role }
  if (!props.user) body.password = form.value.password
  saving.value = true
  try {
    if (props.user) {
      await put('/users/' + props.user.id, body)
    } else {
      await post('/users', body)
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
