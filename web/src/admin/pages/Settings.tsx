import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Input from '@/components/Input'
import Icon from '@/components/Icon'

const tabs = [
  { key: 'basic', label: '基础配置', icon: 'tune' },
  { key: 'cache', label: '缓存策略', icon: 'cached' },
  { key: 'storage', label: '存储后端', icon: 'database' },
  { key: 'auth', label: '认证安全', icon: 'shield' },
] as const

type TabKey = (typeof tabs)[number]['key']

export default function Settings() {
  const [activeTab, setActiveTab] = useState<TabKey>('basic')
  const [settings, setSettings] = useState<Record<string, any>>({})

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'settings'],
    queryFn: () => adminApi.getSettings(),
  })

  useEffect(() => {
    if (data?.data) {
      // Flatten nested settings for display
      const flat: Record<string, any> = {}
      const d = data.data
      if (d.server) {
        flat.host = d.server.host
        flat.port = d.server.port
        flat.log_level = d.server.log_level
        flat.metrics_enabled = d.server.metrics_enabled
        flat.access_log_persist = d.server.access_log_persist
      }
      if (d.cache) {
        flat.max_size_gb = d.cache.max_size_gb
        flat.lru_threshold = d.cache.lru_threshold
        flat.ttl_index = d.cache.ttl_index
        flat.ttl_blob = d.cache.ttl_blob
        flat.lru_enabled = d.cache.lru_enabled
      }
      if (d.storage) {
        flat.storage_type = d.storage.type
        flat.storage_path = d.storage.path
        flat.s3_endpoint = d.storage.endpoint
        flat.s3_bucket = d.storage.bucket
        flat.s3_access_key = d.storage.access_key
        flat.s3_secret_key = d.storage.secret_key
        flat.s3_region = d.storage.region
      }
      if (d.database) {
        flat.db_driver = d.database.driver
        flat.db_dsn = d.database.dsn
      }
      if (d.auth) {
        flat.auth_enabled = d.auth.enabled
        flat.anonymous_proxy = d.auth.anonymous_proxy
        flat.jwt_secret = d.auth.jwt_secret
        flat.token_ttl = d.auth.token_ttl
      }
      setSettings(flat)
    }
  }, [data])

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Card className="h-40 animate-pulse" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Tabs */}
      <div className="flex gap-0 border-b border-outline-variant/10">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => setActiveTab(t.key)}
            className={`flex items-center gap-2 px-5 py-2.5 text-sm font-medium transition-colors cursor-pointer border-b-2 bg-transparent ${
              activeTab === t.key
                ? 'border-primary text-on-surface'
                : 'border-transparent text-on-surface-variant hover:text-on-surface'
            }`}
          >
            <Icon name={t.icon} size="sm" />
            {t.label}
          </button>
        ))}
      </div>

      {/* Note */}
      <div className="flex items-center gap-2 text-xs text-on-surface-variant bg-surface-container rounded-[0.25rem] px-4 py-2.5">
        <Icon name="info" size="sm" />
        修改配置需要重启服务
      </div>

      {/* Basic Tab */}
      {activeTab === 'basic' && (
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            基础配置
          </h3>
          <div className="space-y-4">
            <div className="grid gap-4 grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">监听地址</label>
                <Input value={settings.host || '0.0.0.0'} disabled />
              </div>
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">监听端口</label>
                <Input type="number" value={settings.port || 8080} disabled />
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-on-surface mb-1">日志级别</label>
              <select
                value={settings.log_level || 'info'}
                disabled
                className="w-48 bg-surface-low border-b-2 border-transparent text-sm text-on-surface px-3 py-2 rounded-[0.125rem] outline-none opacity-70 cursor-not-allowed"
              >
                <option value="debug">debug</option>
                <option value="info">info</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </select>
            </div>
            <div className="space-y-3">
              <label className="flex items-center gap-3 cursor-not-allowed opacity-70">
                <input
                  type="checkbox"
                  checked={settings.metrics_enabled ?? true}
                  disabled
                  className="h-4 w-4 rounded accent-primary"
                />
                <span className="text-sm text-on-surface">开启 Prometheus 指标</span>
              </label>
              <label className="flex items-center gap-3 cursor-not-allowed opacity-70">
                <input
                  type="checkbox"
                  checked={settings.access_log_persist ?? true}
                  disabled
                  className="h-4 w-4 rounded accent-primary"
                />
                <span className="text-sm text-on-surface">访问日志持久化</span>
              </label>
            </div>
          </div>
        </Card>
      )}

      {/* Cache Tab */}
      {activeTab === 'cache' && (
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            缓存策略
          </h3>
          <div className="space-y-4">
            <div className="grid gap-4 grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">最大缓存容量 (GB)</label>
                <Input type="number" value={settings.max_size_gb || 20} disabled />
              </div>
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">清理阈值 (%)</label>
                <Input type="number" value={settings.lru_threshold || 90} disabled />
              </div>
            </div>
            <div className="grid gap-4 grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">索引 TTL</label>
                <Input mono value={settings.ttl_index || '5m'} disabled />
              </div>
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">文件 TTL</label>
                <Input mono value={settings.ttl_blob || '72h'} disabled />
              </div>
            </div>
            <label className="flex items-center gap-3 cursor-not-allowed opacity-70">
              <input
                type="checkbox"
                checked={settings.lru_enabled ?? true}
                disabled
                className="h-4 w-4 rounded accent-primary"
              />
              <span className="text-sm text-on-surface">LRU 自动淘汰</span>
            </label>
          </div>
        </Card>
      )}

      {/* Storage Tab */}
      {activeTab === 'storage' && (
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            存储后端
          </h3>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-on-surface mb-1">存储类型</label>
              <select
                value={settings.storage_type || 'local'}
                disabled
                className="w-48 bg-surface-low border-b-2 border-transparent text-sm text-on-surface px-3 py-2 rounded-[0.125rem] outline-none opacity-70 cursor-not-allowed"
              >
                <option value="local">本地文件系统</option>
                <option value="s3">S3 兼容存储</option>
              </select>
            </div>

            {settings.storage_type === 's3' ? (
              <div className="grid gap-4 grid-cols-2">
                <div>
                  <label className="block text-sm font-medium text-on-surface mb-1">Endpoint</label>
                  <Input mono value={settings.s3_endpoint || ''} disabled />
                </div>
                <div>
                  <label className="block text-sm font-medium text-on-surface mb-1">Bucket</label>
                  <Input mono value={settings.s3_bucket || ''} disabled />
                </div>
                <div>
                  <label className="block text-sm font-medium text-on-surface mb-1">Access Key</label>
                  <Input value={settings.s3_access_key || ''} disabled />
                </div>
                <div>
                  <label className="block text-sm font-medium text-on-surface mb-1">Secret Key</label>
                  <Input type="password" value={settings.s3_secret_key || ''} disabled />
                </div>
                <div>
                  <label className="block text-sm font-medium text-on-surface mb-1">Region</label>
                  <Input value={settings.s3_region || ''} disabled />
                </div>
              </div>
            ) : (
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">缓存目录</label>
                <Input mono value={settings.storage_path || './data/cache'} disabled />
              </div>
            )}

            <div>
              <label className="block text-sm font-medium text-on-surface mb-1">数据库类型</label>
              <select
                value={settings.db_driver || 'sqlite'}
                disabled
                className="w-48 bg-surface-low border-b-2 border-transparent text-sm text-on-surface px-3 py-2 rounded-[0.125rem] outline-none opacity-70 cursor-not-allowed"
              >
                <option value="sqlite">SQLite</option>
                <option value="postgres">PostgreSQL</option>
              </select>
            </div>

            {settings.db_driver === 'postgres' && (
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">DSN</label>
                <Input mono value={settings.db_dsn || ''} disabled />
              </div>
            )}
          </div>
        </Card>
      )}

      {/* Auth Tab */}
      {activeTab === 'auth' && (
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            认证安全
          </h3>
          <div className="space-y-4">
            <div className="space-y-3">
              <label className="flex items-center gap-3 cursor-not-allowed opacity-70">
                <input
                  type="checkbox"
                  checked={settings.auth_enabled ?? true}
                  disabled
                  className="h-4 w-4 rounded accent-primary"
                />
                <span className="text-sm text-on-surface">启用登录认证</span>
              </label>
              <label className="flex items-center gap-3 cursor-not-allowed opacity-70">
                <input
                  type="checkbox"
                  checked={settings.anonymous_proxy ?? true}
                  disabled
                  className="h-4 w-4 rounded accent-primary"
                />
                <span className="text-sm text-on-surface">代理端点匿名访问（允许 pip/apt 无 Token 使用）</span>
              </label>
            </div>
            <div className="grid gap-4 grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">JWT Secret</label>
                <Input type="password" value={settings.jwt_secret || ''} disabled />
              </div>
              <div>
                <label className="block text-sm font-medium text-on-surface mb-1">Token 有效期</label>
                <select
                  value={settings.token_ttl || '168h'}
                  disabled
                  className="w-full bg-surface-low border-b-2 border-transparent text-sm text-on-surface px-3 py-2 rounded-[0.125rem] outline-none opacity-70 cursor-not-allowed"
                >
                  <option value="168h">7 天</option>
                  <option value="720h">30 天</option>
                  <option value="2160h">90 天</option>
                  <option value="never">永不过期</option>
                </select>
              </div>
            </div>
          </div>
        </Card>
      )}
    </div>
  )
}
