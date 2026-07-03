<template>
  <div class="group-options-preview">
    <div v-for="item in displayItems" :key="item.code" class="preview-row">
      <span class="preview-label">{{ item.label }}</span>
      <div class="preview-value">
        <template v-if="item.list">
          <el-tag v-for="v in item.list" :key="v" size="small" type="primary" class="preview-tag">{{ v }}</el-tag>
          <span v-if="!item.list.length" class="preview-empty">-</span>
        </template>
        <pre v-else-if="item.isRaw" class="preview-code">{{ item.text }}</pre>
        <span v-else>{{ item.text || '-' }}</span>
      </div>
    </div>
    <div v-if="!displayItems.length" class="preview-empty-row">该配置组暂无 Options</div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  options: {
    type: Object,
    default: () => ({})
  }
})

const codeLabels = {
  '3': '网关',
  '6': 'DNS 服务器',
  '12': 'Option 主机名',
  '15': '域名',
  '28': '广播地址'
}

const knownOrder = ['6', '3', '15', '12', '28']

function formatValue(entry) {
  if (!entry || entry.value === undefined || entry.value === null) {
    return { text: '' }
  }
  const type = entry.type
  if (type === 'ips') {
    return { list: (entry.value || []).map(String).filter(Boolean) }
  }
  if (type === 'domains') {
    return { text: (entry.value || []).join(', ') }
  }
  if (type === 'string' || type === 'ip') {
    return { text: String(entry.value) }
  }
  if (type === 'hex') {
    return { text: String(entry.value), isRaw: true }
  }
  if (type === 'uint32') {
    return { text: String(entry.value) }
  }
  // fallback for unknown shapes
  return { text: JSON.stringify(entry.value, null, 2), isRaw: true }
}

const displayItems = computed(() => {
  const allCodes = Object.keys(props.options || {})
  const sortedCodes = [
    ...knownOrder.filter(c => allCodes.includes(c)),
    ...allCodes.filter(c => !knownOrder.includes(c))
  ]
  return sortedCodes.map(code => {
    const label = codeLabels[code] || `Option ${code}`
    const { text, list, isRaw } = formatValue(props.options[code])
    return { code, label, text, list, isRaw }
  })
})
</script>

<style scoped>
.group-options-preview {
  background-color: #f5f7fa;
  border: 1px solid #e4e7ed;
  border-radius: 6px;
  padding: 12px 16px;
}
.preview-row {
  display: flex;
  align-items: flex-start;
  min-height: 32px;
  line-height: 24px;
}
.preview-row + .preview-row {
  margin-top: 8px;
}
.preview-label {
  width: 110px;
  flex-shrink: 0;
  color: #606266;
  font-size: 14px;
}
.preview-value {
  flex: 1;
  color: #303133;
  font-size: 14px;
  word-break: break-all;
}
.preview-tag {
  margin-right: 6px;
  margin-bottom: 4px;
}
.preview-empty {
  color: #c0c4cc;
}
.preview-empty-row {
  text-align: center;
  color: #909399;
  padding: 12px 0;
}
.preview-code {
  margin: 0;
  padding: 8px 12px;
  background-color: #fff;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  font-size: 12px;
  color: #606266;
  white-space: pre-wrap;
  word-break: break-all;
  width: 100%;
}
</style>
