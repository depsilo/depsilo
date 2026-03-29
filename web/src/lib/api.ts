import axios from 'axios'

const api = axios.create({ baseURL: '/api/v1' })

// Request interceptor: attach JWT from localStorage
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor: 401 → redirect to /admin/login
api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401 && window.location.pathname.startsWith('/admin')) {
      localStorage.removeItem('token')
      window.location.href = '/admin/login'
    }
    return Promise.reject(err)
  }
)

export const statsApi = {
  getStats: () => api.get('/stats'),
}

export const authApi = {
  login: (data: { username: string; password: string }) => api.post('/auth/login', data),
  logout: () => api.post('/auth/logout'),
  refresh: () => api.post('/auth/refresh'),
}

export const adminApi = {
  getDashboard: () => api.get('/admin/dashboard'),

  // Cache
  listCache: (params: Record<string, any>) => api.get('/admin/cache', { params }),
  deleteCache: (id: number) => api.delete(`/admin/cache/${id}`),
  cleanupCache: () => api.post('/admin/cache/cleanup'),

  // Upstreams
  listUpstreams: () => api.get('/admin/upstreams'),
  createUpstream: (data: any) => api.post('/admin/upstreams', data),
  updateUpstream: (id: number, data: any) => api.put(`/admin/upstreams/${id}`, data),
  deleteUpstream: (id: number) => api.delete(`/admin/upstreams/${id}`),
  checkUpstream: (id: number) => api.post(`/admin/upstreams/${id}/check`),

  // Logs
  listLogs: (params: Record<string, any>) => api.get('/admin/logs', { params }),

  // Users
  listUsers: () => api.get('/admin/users'),
  createUser: (data: any) => api.post('/admin/users', data),
  updateUser: (id: number, data: any) => api.put(`/admin/users/${id}`, data),
  deleteUser: (id: number) => api.delete(`/admin/users/${id}`),

  // Tokens
  listTokens: () => api.get('/admin/tokens'),
  createToken: (data: any) => api.post('/admin/tokens', data),
  deleteToken: (id: number) => api.delete(`/admin/tokens/${id}`),

  // Settings
  getSettings: () => api.get('/admin/settings'),
  updateSettings: (data: any) => api.put('/admin/settings', data),
}

export default api
