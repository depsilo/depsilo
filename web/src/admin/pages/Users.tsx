import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Button from '@/components/Button'
import Badge from '@/components/Badge'
import Input from '@/components/Input'
import Icon from '@/components/Icon'
import DataTable from '@/components/DataTable'
import Modal from '@/components/Modal'

function formatTime(t: string | null): string {
  if (!t) return '-'
  const d = new Date(t)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export default function Users() {
  const queryClient = useQueryClient()

  // User dialog state
  const [userDialogOpen, setUserDialogOpen] = useState(false)
  const [editUserId, setEditUserId] = useState<number | null>(null)
  const [userForm, setUserForm] = useState({ username: '', password: '', role: 'readonly' })

  // Token dialog state
  const [tokenDialogOpen, setTokenDialogOpen] = useState(false)
  const [tokenForm, setTokenForm] = useState({ name: '', permissions: 'readonly', ttl: '7d' })
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [tokenResultOpen, setTokenResultOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  const { data: usersData, isLoading: usersLoading } = useQuery({
    queryKey: ['admin', 'users'],
    queryFn: () => adminApi.listUsers(),
  })

  const { data: tokensData, isLoading: tokensLoading } = useQuery({
    queryKey: ['admin', 'tokens'],
    queryFn: () => adminApi.listTokens(),
  })

  const users: any[] = usersData?.data?.items || usersData?.data || []
  const tokens: any[] = tokensData?.data?.items || tokensData?.data || []

  const createUserMutation = useMutation({
    mutationFn: (d: any) => adminApi.createUser(d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
      closeUserDialog()
    },
  })

  const updateUserMutation = useMutation({
    mutationFn: ({ id, data: d }: { id: number; data: any }) => adminApi.updateUser(id, d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
      closeUserDialog()
    },
  })

  const toggleUserMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      adminApi.updateUser(id, { enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
    },
  })

  const createTokenMutation = useMutation({
    mutationFn: (d: any) => adminApi.createToken(d),
    onSuccess: (res) => {
      const token = res.data?.token || res.data?.raw_token
      setCreatedToken(token)
      setTokenDialogOpen(false)
      setTokenResultOpen(true)
      queryClient.invalidateQueries({ queryKey: ['admin', 'tokens'] })
    },
  })

  const deleteTokenMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteToken(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'tokens'] })
    },
  })

  function closeUserDialog() {
    setUserDialogOpen(false)
    setEditUserId(null)
    setUserForm({ username: '', password: '', role: 'readonly' })
  }

  function openCreateUser() {
    setEditUserId(null)
    setUserForm({ username: '', password: '', role: 'readonly' })
    setUserDialogOpen(true)
  }

  function openEditUser(u: any) {
    setEditUserId(u.id)
    setUserForm({ username: u.username, password: '', role: u.role })
    setUserDialogOpen(true)
  }

  function handleUserSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (editUserId) {
      const payload: any = { role: userForm.role }
      if (userForm.password) payload.password = userForm.password
      updateUserMutation.mutate({ id: editUserId, data: payload })
    } else {
      createUserMutation.mutate(userForm)
    }
  }

  function handleTokenSubmit(e: React.FormEvent) {
    e.preventDefault()
    createTokenMutation.mutate(tokenForm)
  }

  function copyToken() {
    if (createdToken) {
      navigator.clipboard.writeText(createdToken)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const isUserSaving = createUserMutation.isPending || updateUserMutation.isPending

  const userColumns = [
    {
      key: 'username',
      label: '用户',
      render: (_val: unknown, row: any) => (
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary-container text-primary text-sm font-medium shrink-0">
            {row.username?.[0]?.toUpperCase() || '?'}
          </div>
          <span className="font-medium text-on-surface">{row.username}</span>
        </div>
      ),
    },
    {
      key: 'role',
      label: '角色',
      render: (val: unknown) => (
        <Badge variant={(val as string) === 'admin' ? 'pypi' : 'default'}>
          {val as string}
        </Badge>
      ),
    },
    {
      key: 'enabled',
      label: '状态',
      render: (val: unknown) => (
        <Badge variant={val ? 'success' : 'error'}>
          {val ? '启用' : '禁用'}
        </Badge>
      ),
    },
    {
      key: 'last_login_at',
      label: '最后登录',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'created_at',
      label: '创建时间',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'id',
      label: '操作',
      render: (_val: unknown, row: any) => (
        <div className="flex gap-1">
          <button
            onClick={(e) => { e.stopPropagation(); openEditUser(row) }}
            className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors p-1.5"
          >
            <Icon name="edit" size="sm" />
          </button>
          <button
            onClick={(e) => { e.stopPropagation(); toggleUserMutation.mutate({ id: row.id, enabled: !row.enabled }) }}
            className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors p-1.5"
          >
            <Icon name={row.enabled ? 'person_off' : 'person'} size="sm" />
          </button>
        </div>
      ),
    },
  ]

  const tokenColumns = [
    {
      key: 'name',
      label: '名称',
      render: (val: unknown) => <span className="font-medium text-on-surface">{val as string}</span>,
    },
    {
      key: 'permissions',
      label: '权限',
      render: (val: unknown) => <Badge variant="default">{val as string}</Badge>,
    },
    {
      key: 'last_used_at',
      label: '最后使用',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'expires_at',
      label: '过期时间',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{val ? formatTime(val as string) : '永不过期'}</span>
      ),
    },
    {
      key: 'id',
      label: '操作',
      render: (val: unknown) => (
        <Button
          variant="ghost"
          className="text-error text-xs"
          disabled={deleteTokenMutation.isPending}
          onClick={(e: React.MouseEvent) => { e.stopPropagation(); deleteTokenMutation.mutate(val as number) }}
        >
          撤销
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-end">
        <Button onClick={openCreateUser}>
          <Icon name="person_add" size="sm" />
          添加用户
        </Button>
      </div>

      {/* Users Table */}
      <Card className="p-0 overflow-hidden">
        {usersLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">加载中...</div>
        ) : users.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">暂无用户</div>
        ) : (
          <DataTable columns={userColumns} data={users} />
        )}
      </Card>

      {/* API Tokens Section */}
      <Card>
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium">
            API TOKENS
          </h3>
          <Button variant="secondary" onClick={() => setTokenDialogOpen(true)}>
            <Icon name="key" size="sm" />
            生成 Token
          </Button>
        </div>
        <div className="-mx-5 -mb-5 overflow-hidden rounded-b-[0.25rem]">
          {tokensLoading ? (
            <div className="p-8 text-center text-on-surface-variant text-sm">加载中...</div>
          ) : tokens.length === 0 ? (
            <div className="p-8 text-center text-on-surface-variant text-sm">暂无 Token</div>
          ) : (
            <DataTable columns={tokenColumns} data={tokens} />
          )}
        </div>
      </Card>

      {/* Create/Edit User Modal */}
      <Modal
        open={userDialogOpen}
        onClose={closeUserDialog}
        title={editUserId ? '编辑用户' : '添加用户'}
      >
        <form onSubmit={handleUserSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">用户名</label>
            <Input
              value={userForm.username}
              onChange={(e) => setUserForm({ ...userForm, username: e.target.value })}
              disabled={!!editUserId}
              required={!editUserId}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">
              {editUserId ? '新密码（留空不修改）' : '密码'}
            </label>
            <Input
              type="password"
              value={userForm.password}
              onChange={(e) => setUserForm({ ...userForm, password: e.target.value })}
              required={!editUserId}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">角色</label>
            <select
              value={userForm.role}
              onChange={(e) => setUserForm({ ...userForm, role: e.target.value })}
              className="w-full bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
            >
              <option value="admin">admin</option>
              <option value="readonly">readonly</option>
            </select>
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button type="button" variant="secondary" onClick={closeUserDialog}>取消</Button>
            <Button type="submit" disabled={isUserSaving}>
              {isUserSaving ? '保存中...' : '保存'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Create Token Modal */}
      <Modal
        open={tokenDialogOpen}
        onClose={() => setTokenDialogOpen(false)}
        title="生成 API Token"
      >
        <form onSubmit={handleTokenSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">名称</label>
            <Input
              value={tokenForm.name}
              onChange={(e) => setTokenForm({ ...tokenForm, name: e.target.value })}
              placeholder="如 CI/CD Pipeline"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">权限</label>
            <select
              value={tokenForm.permissions}
              onChange={(e) => setTokenForm({ ...tokenForm, permissions: e.target.value })}
              className="w-full bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
            >
              <option value="readonly">只读</option>
              <option value="readwrite">读写</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">有效期</label>
            <select
              value={tokenForm.ttl}
              onChange={(e) => setTokenForm({ ...tokenForm, ttl: e.target.value })}
              className="w-full bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
            >
              <option value="7d">7 天</option>
              <option value="30d">30 天</option>
              <option value="90d">90 天</option>
              <option value="never">永不过期</option>
            </select>
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button type="button" variant="secondary" onClick={() => setTokenDialogOpen(false)}>取消</Button>
            <Button type="submit" disabled={createTokenMutation.isPending}>
              {createTokenMutation.isPending ? '生成中...' : '生成'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Token Result Modal */}
      <Modal
        open={tokenResultOpen}
        onClose={() => setTokenResultOpen(false)}
        title="Token 已生成"
      >
        <p className="text-sm text-on-surface-variant mb-3">
          请立即复制此 Token，关闭后将无法再次查看。
        </p>
        <div className="flex items-center gap-2 bg-surface-container rounded-[0.25rem] p-3">
          <code className="flex-1 font-mono text-sm text-on-surface break-all">{createdToken}</code>
          <button
            onClick={copyToken}
            className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors p-1.5 shrink-0"
          >
            <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
          </button>
        </div>
        <p className="text-xs text-error mt-2">
          此 Token 仅展示一次，请妥善保管。
        </p>
        <div className="flex justify-end mt-4">
          <Button onClick={() => setTokenResultOpen(false)}>确定</Button>
        </div>
      </Modal>
    </div>
  )
}
