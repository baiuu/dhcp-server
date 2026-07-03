<template>
  <div class="page-container">
    <div class="page-header">
      <h2 class="page-title">DHCP Options 参考</h2>
    </div>
    <el-card shadow="hover" class="table-card">
      <template #header>
        <div class="card-header"><el-icon><Reading /></el-icon><span>Options 定义</span></div>
      </template>
      <el-alert type="info" :closable="false" class="mb-4">
        在作用域或绑定地址的 Options 字段中，使用 JSON 格式：<code>{"code": {"type": "...", "value": ...}}</code>
      </el-alert>
      <el-table :data="COMMON_OPTIONS" size="default" border stripe max-height="520">
        <el-table-column prop="code" label="Code" width="80" />
        <el-table-column prop="name" label="名称" min-width="180" />
        <el-table-column label="Type" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="示例 Value" min-width="240">
          <template #default="{ row }"><code>{{ formatExample(row.example) }}</code></template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="hover" class="table-card">
      <template #header>
        <div class="card-header"><el-icon><MapLocation /></el-icon><span>路由选项生成器 (121/249)</span></div>
      </template>
      <div v-for="(route, idx) in routes" :key="idx" class="route-row">
        <el-input v-model="route.destination" placeholder="目的网络" style="flex: 1" />
        <el-input-number v-model="route.mask" :min="0" :max="128" placeholder="掩码位数" style="width: 140px" />
        <el-input v-model="route.router" placeholder="网关" style="flex: 1" />
        <el-button type="danger" :icon="Delete" @click="routes.splice(idx, 1)">删除</el-button>
      </div>
      <div class="mt-4">
        <el-button :icon="Plus" @click="addRoute">添加路由</el-button>
        <el-button type="primary" :icon="MagicStick" @click="generate">生成 JSON</el-button>
      </div>
      <pre v-if="routeJson" class="json-output">{{ routeJson }}</pre>

      <el-divider />
      <h3 class="mb-3">完整示例</h3>
      <pre class="json-output">{
  "1": {"type":"ip","value":"255.255.255.0"},
  "3": {"type":"ips","value":["192.168.1.1"]},
  "6": {"type":"ips","value":["8.8.8.8","8.8.4.4"]},
  "28": {"type":"ip","value":"192.168.1.255"},
  "42": {"type":"ips","value":["192.168.1.2"]},
  "66": {"type":"string","value":"tftp.example.com"},
  "67": {"type":"string","value":"pxelinux.0"},
  "121": {"type":"routes","value":[{"destination":"10.0.0.0","mask":8,"router":"192.168.1.1"}]}
}</pre>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Plus, Delete, MagicStick, Reading, MapLocation } from '@element-plus/icons-vue'
import { COMMON_OPTIONS } from '../utils/options'

const routes = ref([{ destination: '', mask: null, router: '' }])
const routeJson = ref('')

function addRoute() {
  routes.value.push({ destination: '', mask: null, router: '' })
}

function generate() {
  const list = routes.value.filter(r => r.destination && r.router && r.mask !== null)
  const out = {
    "121": { type: 'routes', value: list },
    "249": { type: 'routes', value: list }
  }
  routeJson.value = JSON.stringify(out, null, 2)
}

function formatExample(ex) {
  if (typeof ex === 'string') return ex
  return JSON.stringify(ex)
}
</script>

<style scoped>
.route-row {
  display: flex;
  gap: 12px;
  margin-bottom: 12px;
  align-items: center;
}
.json-output {
  background: #f5f7fa;
  padding: 16px;
  border-radius: 8px;
  overflow: auto;
  margin-top: 16px;
  font-family: monospace;
  font-size: 13px;
}
</style>
