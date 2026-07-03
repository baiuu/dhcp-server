import { createRouter, createWebHashHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import LoginView from '../views/LoginView.vue'
import LayoutView from '../views/LayoutView.vue'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: LoginView,
    meta: { public: true }
  },
  {
    path: '/',
    component: LayoutView,
    redirect: '/dashboard',
    children: [
      { path: 'dashboard', name: 'Dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '概览', icon: 'Odometer' } },
      { path: 'scopes', name: 'Scopes', component: () => import('../views/ScopesView.vue'), meta: { title: '作用域', icon: 'OfficeBuilding' } },
      { path: 'leases', name: 'Leases', component: () => import('../views/LeasesView.vue'), meta: { title: '租约', icon: 'DocumentCopy' } },
      { path: 'ip-allocation-logs', name: 'IPAllocationLogs', component: () => import('../views/IPAllocationLogsView.vue'), meta: { title: 'IP 分配记录', icon: 'Histogram' } },
      { path: 'reservations', name: 'Reservations', component: () => import('../views/ReservationsView.vue'), meta: { title: '绑定地址', icon: 'CollectionTag' } },
      { path: 'reservation-groups', name: 'ReservationGroups', component: () => import('../views/ReservationGroupsView.vue'), meta: { title: '配置组', icon: 'SetUp' } },
      { path: 'users', name: 'Users', component: () => import('../views/UsersView.vue'), meta: { title: '用户', icon: 'User' } },
      { path: 'options', name: 'Options', component: () => import('../views/OptionsRefView.vue'), meta: { title: 'Options 参考', icon: 'Reading' } },
      { path: 'audit', name: 'Audit', component: () => import('../views/AuditLogsView.vue'), meta: { title: '审计日志', icon: 'Tickets' } },
      { path: 'system-logs', name: 'SystemLogs', component: () => import('../views/SystemLogsView.vue'), meta: { title: '系统日志', icon: 'Warning' } },
      { path: 'blacklist', name: 'Blacklist', component: () => import('../views/BlacklistView.vue'), meta: { title: 'MAC 黑名单', icon: 'CircleClose' } },
      { path: 'cluster-nodes', name: 'ClusterNodes', component: () => import('../views/ClusterNodesView.vue'), meta: { title: '集群节点', icon: 'Connection' } },
    ]
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/'
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

router.beforeEach((to, from, next) => {
  const auth = useAuthStore()
  if (!to.meta.public && !auth.token.value) {
    next('/login')
  } else if (to.path === '/login' && auth.token.value) {
    next('/dashboard')
  } else {
    next()
  }
})

export default router
