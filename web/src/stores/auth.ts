import { defineStore } from 'pinia'
import { api, json } from '../api'
import type { User } from '../types'

export const useAuthStore = defineStore('auth', {
  state: () => ({ user: null as User | null, checked: false }),
  actions: {
    async check() { try { this.user = await api<User>('/api/auth/me') } catch { this.user = null } finally { this.checked = true } },
    async login(username: string, password: string, remember: boolean, totpCode = '') {
      const result = await api<{ user: User }>('/api/auth/login', { method: 'POST', body: json({ username, password, remember, totpCode }) })
      this.user = result.user
    },
    async logout() { try { await api('/api/auth/logout', { method: 'POST' }) } finally { this.user = null } },
  },
})
