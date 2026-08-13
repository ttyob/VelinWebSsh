import { createRouter, createWebHistory } from 'vue-router'
import LoginView from './views/LoginView.vue'
import WorkspaceView from './views/WorkspaceView.vue'
import ChangePasswordView from './views/ChangePasswordView.vue'
import { useAuthStore } from './stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/change-password', component: ChangePasswordView },
    { path: '/workspace', component: WorkspaceView },
    { path: '/:pathMatch(.*)*', redirect: '/workspace' },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.checked) await auth.check()
  if (!to.meta.public && !auth.user) return '/login'
  if (auth.user?.forcePasswordChange && to.path !== '/change-password') return '/change-password'
  if (!auth.user?.forcePasswordChange && to.path === '/change-password') return '/workspace'
  if (to.path === '/login' && auth.user) return '/workspace'
})

export default router
