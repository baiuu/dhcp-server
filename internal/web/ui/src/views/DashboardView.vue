<template>
  <div class="page-container">
    <el-row :gutter="20" class="stretch-row">
      <el-col :xs="24" :sm="12" :md="6" v-for="(s, idx) in statList" :key="idx">
        <el-card shadow="hover" class="stat-card" :body-style="{ padding: '0 20px', display: 'flex', alignItems: 'center', gap: '10px', height: '100%' }">
          <div class="stat-icon" :class="s.bg"><el-icon size="28" color="#fff"><component :is="s.icon" /></el-icon></div>
          <div class="stat-info">
            <div class="stat-num">{{ s.value }}</div>
            <div class="stat-label">{{ s.label }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="12" class="section stretch-row">
      <el-col :xs="24" :lg="12">
        <el-card shadow="hover" class="chart-card">
          <template #header>
            <div class="card-header"><el-icon><Histogram /></el-icon><span>作用域 IP 使用率</span></div>
          </template>
          <canvas ref="scopeChartRef" v-show="scopeHasData"></canvas>
          <el-empty v-if="!scopeHasData" description="暂无数据" :image-size="80" />
        </el-card>
      </el-col>
      <el-col :xs="24" :lg="12">
        <el-card shadow="hover" class="chart-card">
          <template #header>
            <div class="card-header"><el-icon><PieChart /></el-icon><span>租约状态分布</span></div>
          </template>
          <canvas ref="leaseChartRef" v-show="leaseHasData"></canvas>
          <el-empty v-if="!leaseHasData" description="暂无数据" :image-size="80" />

        </el-card>
      </el-col>
    </el-row>

    <el-card class="section" shadow="hover">
      <template #header>
        <div class="card-header"><el-icon><List /></el-icon><span>最近活跃租约</span></div>
      </template>
      <el-table :data="activeLeases" size="default" stripe>
        <el-table-column label="MAC / DUID" min-width="220">
          <template #default="{ row }">
            <el-tag size="small" :type="row.duid ? 'info' : 'success'">{{ row.duid ? 'v6' : 'v4' }}</el-tag>
            <span class="ml-2">{{ row.duid ? `${row.duid}/${row.iaid}` : row.mac_addr }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="ip_addr" label="IP" />
        <el-table-column label="主机名">
          <template #default="{ row }">{{ row.hostname || '-' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="stateType(row.state)" size="small" effect="dark">{{ row.state }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="过期时间" width="180" :formatter="(_, __, val) => formatDate(val)" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick, reactive, computed } from 'vue'
import { Chart, BarController, DoughnutController, BarElement, ArcElement, CategoryScale, LinearScale, Title, Tooltip, Legend } from 'chart.js'
import { OfficeBuilding, DocumentCopy, CollectionTag, CircleClose } from '@element-plus/icons-vue'
import { get, showError } from '../api/request'
import { formatDate, parseIPRangeSize } from '../utils'

Chart.register(BarController, DoughnutController, BarElement, ArcElement, CategoryScale, LinearScale, Title, Tooltip, Legend)

const stats = reactive({ scopes: 0, leases: 0, reservations: 0, blacklist: 0 })
const scopes = ref([])
const activeLeases = ref([])
const scopeChartRef = ref(null)
const leaseChartRef = ref(null)
const scopeHasData = ref(false)
const leaseHasData = ref(false)
let scopeChart = null
let leaseChart = null

const statList = computed(() => [
  { label: '作用域', value: stats.scopes, icon: OfficeBuilding, bg: 'bg-primary' },
  { label: '活跃租约', value: stats.leases, icon: DocumentCopy, bg: 'bg-success' },
  { label: '绑定地址', value: stats.reservations, icon: CollectionTag, bg: 'bg-warning' },
  { label: 'MAC 黑名单', value: stats.blacklist, icon: CircleClose, bg: 'bg-danger' },
])

function stateType(state) {
  if (state === 'active') return 'success'
  if (state === 'offered') return 'warning'
  return 'info'
}

function renderScopeChart() {
  const labels = scopes.value.map(s => s.name)
  const data = scopes.value.map(s => {
    const total = parseIPRangeSize(s.start_ip, s.end_ip)
    const used = activeLeases.value.filter(l => l.scope_id === s.id).length
    return total ? Math.round(used / total * 100) : 0
  })
  scopeHasData.value = labels.length > 0
  if (!scopeHasData.value) {
    if (scopeChart) { scopeChart.destroy(); scopeChart = null }
    return
  }
  if (scopeChart) scopeChart.destroy()
  scopeChart = new Chart(scopeChartRef.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [{ label: '使用率 %', data, backgroundColor: '#409eff', borderRadius: 4 }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } },
      scales: { y: { beginAtZero: true, max: 100, title: { display: true, text: '使用率 %' } } }
    }
  })
}

function renderLeaseChart() {
  const counts = {}
  activeLeases.value.forEach(l => { counts[l.state] = (counts[l.state] || 0) + 1 })
  leaseHasData.value = Object.keys(counts).length > 0
  if (!leaseHasData.value) {
    if (leaseChart) { leaseChart.destroy(); leaseChart = null }
    return
  }
  if (leaseChart) leaseChart.destroy()
  leaseChart = new Chart(leaseChartRef.value, {
    type: 'doughnut',
    data: {
      labels: Object.keys(counts),
      datasets: [{
        data: Object.values(counts),
        backgroundColor: ['#67c23a', '#e6a23c', '#f56c6c', '#909399']
      }]
    },
    options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { position: 'right' } } }
  })
}

async function countReservations() {
  let count = 0
  for (const s of scopes.value) {
    try {
      const data = await get(`/scopes/${s.id}/reservations?limit=1`)
      count += data.total || 0
    } catch (e) {}
  }
  return count
}

async function load() {
  try {
    const data = await get('/dashboard')
    scopes.value = data.scopes || []
    activeLeases.value = data.active_leases || []
    stats.scopes = scopes.value.length
    stats.leases = data.lease_count || 0
    stats.reservations = await countReservations()
    try {
      const bl = await get('/mac-blacklist')
      const list = bl.items || bl || []
      stats.blacklist = list.length
    } catch (e) { stats.blacklist = 0 }
    await nextTick()
    renderScopeChart()
    renderLeaseChart()
  } catch (err) {
    showError(err)
  }
}

onMounted(load)
</script>

<style scoped>
.stat-icon {
  width: 52px;
  height: 52px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.bg-primary { background: linear-gradient(135deg, #409eff, #79bbff); }
.bg-success { background: linear-gradient(135deg, #67c23a, #95d475); }
.bg-warning { background: linear-gradient(135deg, #e6a23c, #f3d19e); }
.bg-danger { background: linear-gradient(135deg, #f56c6c, #fab6b6); }
.stat-info { flex: 1; text-align: left; }
.stat-num {
  font-size: 26px;
  font-weight: 700;
  line-height: 1;
  margin-bottom: 6px;
}
.stat-label {
  color: #909399;
  font-size: 13px;
}
.stat-info { flex: 1; text-align: left; }
.stretch-row { align-items: stretch; }
.stretch-row .el-col { display: flex; }
.stretch-row .el-col > .el-card { width: 100%; }
.stat-card { height: 100px; }
.section { margin-top: 24px; }
.chart-card {
  height: 320px;
  display: flex;
  flex-direction: column;
}
.chart-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 0;
}
.chart-card canvas {
  max-height: 240px;
  width: 100% !important;
}
.card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.ml-2 { margin-left: 8px; }
</style>
