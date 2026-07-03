<template>
  <el-dialog title="修改密码" v-model="visible" width="440px" @closed="reset" destroy-on-close>
    <p v-if="username" class="text-muted mb-4">用户: {{ username }}</p>
    <el-form :model="form" label-width="80px" :rules="rules" ref="formRef">
      <el-form-item label="原密码" prop="oldPassword">
        <el-input v-model="form.oldPassword" type="password" show-password />
      </el-form-item>
      <el-form-item label="新密码" prop="newPassword">
        <el-input v-model="form.newPassword" type="password" show-password />
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
import { post, showError, showSuccess } from '../api/request'

const props = defineProps({
  modelValue: Boolean,
  username: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const formRef = ref()
const saving = ref(false)
const defaultForm = { oldPassword: '', newPassword: '' }
const form = ref({ ...defaultForm })

const rules = {
  oldPassword: [{ required: true, message: '请输入原密码', trigger: 'blur' }],
  newPassword: [{ required: true, message: '请输入新密码', trigger: 'blur' }],
}

watch(() => props.modelValue, (v) => {
  if (v) reset()
})

function reset() {
  form.value = { ...defaultForm }
}

async function submit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    await post('/users/change-password', {
      old_password: form.value.oldPassword,
      new_password: form.value.newPassword
    })
    showSuccess('密码已修改')
    visible.value = false
  } catch (err) {
    showError(err)
  } finally {
    saving.value = false
  }
}
</script>
