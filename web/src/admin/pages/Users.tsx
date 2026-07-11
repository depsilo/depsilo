import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import ButtonV2 from '@/components/Button'
import BadgeV2 from '@/components/Badge'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import Icon from '@/components/Icon'
import DataTableV2 from '@/components/DataTable'
import ModalV2 from '@/components/Modal'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import { usePrincipal } from '@/hooks/usePrincipal'
import type {
  AdminUser,
  CreateAPITokenRequest,
  CreateUserRequest,
  TokenPermissions,
  UpdateUserRequest,
  UserRole,
} from '@/lib/adminApi.types'

export default function UsersV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { principal, canWrite } = usePrincipal()
  const [userDialogOpen, setUserDialogOpen] = useState(false)
  const [editUserId, setEditUserId] = useState<number | null>(null)
  const [userForm, setUserForm] = useState<{ username: string; password: string; role: UserRole }>({ username: '', password: '', role: 'readonly' })
  const [tokenDialogOpen, setTokenDialogOpen] = useState(false)
  const [tokenForm, setTokenForm] = useState<CreateAPITokenRequest>({ name: '', permissions: 'readonly', ttl: '7d' })
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [tokenResultOpen, setTokenResultOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  const { data: usersData, isLoading: usersLoading } = useQuery({ queryKey: ['admin', 'users'], queryFn: () => adminApi.listUsers() })
  const { data: tokensData, isLoading: tokensLoading } = useQuery({ queryKey: ['admin', 'tokens'], queryFn: () => adminApi.listTokens() })
  const users = usersData?.data ?? []
  const tokens = tokensData?.data ?? []
  const isSelf = (user: AdminUser) => user.id === principal?.id

  const createUserMutation = useMutation({ mutationFn: (data: CreateUserRequest) => adminApi.createUser(data), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }); closeUserDialog() } })
  const updateUserMutation = useMutation({ mutationFn: ({ id, data }: { id: number; data: UpdateUserRequest }) => adminApi.updateUser(id, data), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }); closeUserDialog() } })
  const toggleUserMutation = useMutation({ mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => adminApi.updateUser(id, { enabled }), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }) } })
  const createTokenMutation = useMutation({ mutationFn: (data: CreateAPITokenRequest) => adminApi.createToken(data), onSuccess: (res) => { setCreatedToken(res.data.token); setTokenDialogOpen(false); setTokenResultOpen(true); queryClient.invalidateQueries({ queryKey: ['admin', 'tokens'] }) } })
  const deleteTokenMutation = useMutation({ mutationFn: (id: number) => adminApi.deleteToken(id), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'tokens'] }) } })

  function closeUserDialog() { setUserDialogOpen(false); setEditUserId(null); setUserForm({ username: '', password: '', role: 'readonly' }) }
  function openCreateUser() { setEditUserId(null); setUserForm({ username: '', password: '', role: 'readonly' }); setUserDialogOpen(true) }
  function openEditUser(user: AdminUser) { setEditUserId(user.id); setUserForm({ username: user.username, password: '', role: user.role }); setUserDialogOpen(true) }
  function handleUserSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!canWrite) return
    if (editUserId) {
      const data: UpdateUserRequest = {}
      if (editUserId !== principal?.id) data.role = userForm.role
      if (userForm.password) data.password = userForm.password
      updateUserMutation.mutate({ id: editUserId, data })
    } else {
      createUserMutation.mutate(userForm)
    }
  }
  function handleTokenSubmit(e: React.FormEvent) { e.preventDefault(); if (canWrite) createTokenMutation.mutate(tokenForm) }
  async function copyToken() {
    if (!createdToken) return
    if (await copyText(createdToken)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const isUserSaving = createUserMutation.isPending || updateUserMutation.isPending

  const userColumns = [
    { key: 'username', label: t('users.user'), render: (_v: unknown, row: AdminUser & Record<string, unknown>) => (<div className="flex items-center gap-3"><div className="flex h-8 w-8 items-center justify-center rounded-[6px] text-[13px] font-[500] shrink-0" style={{ background: 'var(--brand)', color: 'white' }}>{row.username?.[0]?.toUpperCase() || '?'}</div><span className="font-[500]" style={{ color: 'var(--text)' }}>{row.username}</span></div>) },
    { key: 'role', label: t('users.role'), render: (v: unknown) => <BadgeV2 variant={(v as string) === 'admin' ? 'ecosystem' : 'default'}>{v as string}</BadgeV2> },
    { key: 'enabled', label: t('status'), render: (v: unknown) => <BadgeV2 variant={v ? 'success' : 'error'}>{v ? t('users.enabled') : t('users.disabled')}</BadgeV2> },
    { key: 'last_login_at', label: t('users.lastLogin'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
    { key: 'created_at', label: t('users.createdAt'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
    { key: 'id', label: t('actions'), render: (_v: unknown, row: AdminUser & Record<string, unknown>) => canWrite ? (<div className="flex gap-1"><button onClick={(e) => { e.stopPropagation(); openEditUser(row) }} className="bg-transparent cursor-pointer p-1.5 rounded-[4px] transition-[color,background,transform] duration-150 active:scale-[0.96] hover:text-[var(--text)]" style={{ color: 'var(--text-soft)' }}><Icon name="edit" size="sm" /></button>{!isSelf(row) && <button onClick={(e) => { e.stopPropagation(); if (canWrite) toggleUserMutation.mutate({ id: row.id, enabled: !row.enabled }) }} className="bg-transparent cursor-pointer p-1.5 rounded-[4px] transition-[color,background,transform] duration-150 active:scale-[0.96] hover:text-[var(--text)]" style={{ color: 'var(--text-soft)' }}><Icon name={row.enabled ? 'person_off' : 'person'} size="sm" /></button>}</div>) : null },
  ]

  const tokenColumns = [
    { key: 'name', label: t('name'), render: (v: unknown) => <span className="font-[500]" style={{ color: 'var(--text)' }}>{v as string}</span> },
    { key: 'permissions', label: t('users.permissions'), render: (v: unknown) => <BadgeV2>{v as string}</BadgeV2> },
    { key: 'last_used_at', label: t('users.lastUsed'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{formatTime(v as string)}</span> },
    { key: 'expires_at', label: t('users.expiresAt'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--text-soft)' }}>{v ? formatTime(v as string) : t('users.neverExpires')}</span> },
    { key: 'id', label: t('actions'), render: (v: unknown) => canWrite ? <ButtonV2 variant="ghost" size="sm" className="!text-[12px]" style={{ color: 'var(--danger)' }} disabled={deleteTokenMutation.isPending} onClick={(e: React.MouseEvent) => { e.stopPropagation(); if (canWrite) deleteTokenMutation.mutate(v as number) }}>{t('users.revoke')}</ButtonV2> : null },
  ]

  return (
    <div className="space-y-12">
      {/* ── Users ─────────────────────────────────────── */}
      <section>
        <SectionHeader
          title={t('users.title')}
          action={canWrite ? <ButtonV2 onClick={openCreateUser} size="sm"><Icon name="person_add" size="sm" />{t('users.addUser')}</ButtonV2> : undefined}
        />
        {usersLoading ? (
          <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
        ) : users.length === 0 ? (
          <EmptyState icon="group" title={t('users.noUsers')} minHeight={180} />
        ) : (
          <DataTableV2
            columns={userColumns}
            data={users.map((user) => ({ ...user }))}
            rowKey={(row) => row.id as number}
            ariaLabel={t('users.title')}
          />
        )}
      </section>

      {/* ── API Tokens ───────────────────────────────── */}
      <section>
        <SectionHeader
          title="API Tokens"
          action={canWrite ? <ButtonV2 variant="secondary" size="sm" onClick={() => setTokenDialogOpen(true)}><Icon name="key" size="sm" />{t('users.generateToken')}</ButtonV2> : undefined}
        />
        {tokensLoading ? (
          <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
        ) : tokens.length === 0 ? (
          <EmptyState icon="key" title={t('users.noTokens')} minHeight={180} />
        ) : (
          <DataTableV2
            columns={tokenColumns}
            data={tokens.map((token) => ({ ...token }))}
            rowKey={(row) => row.id as number}
            ariaLabel={`${t('users.title')} API Tokens`}
          />
        )}
      </section>

      <ModalV2 open={userDialogOpen} onClose={closeUserDialog} title={editUserId ? t('users.editUser') : t('users.addUser')}>
        <form onSubmit={handleUserSubmit} className="space-y-4">
          <InputV2 label={t('login.username')} value={userForm.username} onChange={(e) => setUserForm({ ...userForm, username: e.target.value })} disabled={!!editUserId} required={!editUserId} />
          <InputV2 label={editUserId ? t('users.newPasswordHint') : t('login.password')} type="password" value={userForm.password} onChange={(e) => setUserForm({ ...userForm, password: e.target.value })} required={!editUserId} />
          <SelectV2 label={t('users.role')} value={userForm.role} disabled={editUserId === principal?.id} onChange={(e) => setUserForm({ ...userForm, role: e.target.value as UserRole })}><option value="admin">admin</option><option value="readonly">readonly</option></SelectV2>
          <div className="flex justify-end gap-3 pt-2"><ButtonV2 type="button" variant="secondary" onClick={closeUserDialog}>{t('cancel')}</ButtonV2><ButtonV2 type="submit" disabled={isUserSaving}>{isUserSaving ? t('saving') : t('save')}</ButtonV2></div>
        </form>
      </ModalV2>

      <ModalV2 open={tokenDialogOpen} onClose={() => setTokenDialogOpen(false)} title={t('users.generateToken')}>
        <form onSubmit={handleTokenSubmit} className="space-y-4">
          <InputV2 label={t('name')} value={tokenForm.name} onChange={(e) => setTokenForm({ ...tokenForm, name: e.target.value })} placeholder="e.g. CI/CD Pipeline" required />
          <SelectV2 label={t('users.permissions')} value={tokenForm.permissions} onChange={(e) => setTokenForm({ ...tokenForm, permissions: e.target.value as TokenPermissions })}><option value="readonly">{t('users.readonly')}</option><option value="readwrite">{t('users.readwrite')}</option></SelectV2>
          <SelectV2 label={t('users.validity')} value={tokenForm.ttl} onChange={(e) => setTokenForm({ ...tokenForm, ttl: e.target.value as CreateAPITokenRequest['ttl'] })}><option value="7d">{t('users.days7')}</option><option value="30d">{t('users.days30')}</option><option value="90d">{t('users.days90')}</option><option value="never">{t('users.neverExpires')}</option></SelectV2>
          <div className="flex justify-end gap-3 pt-2"><ButtonV2 type="button" variant="secondary" onClick={() => setTokenDialogOpen(false)}>{t('cancel')}</ButtonV2><ButtonV2 type="submit" disabled={createTokenMutation.isPending}>{createTokenMutation.isPending ? t('users.generating') : t('users.generate')}</ButtonV2></div>
        </form>
      </ModalV2>

      <ModalV2 open={tokenResultOpen} onClose={() => setTokenResultOpen(false)} title={t('users.tokenGenerated')}>
        <p className="text-[14px] mb-3" style={{ color: 'var(--text-soft)' }}>{t('users.tokenCopyWarning')}</p>
        <div className="flex items-center gap-2 rounded-[4px] p-3" style={{ background: 'var(--bg-soft)', border: '1px solid var(--border)' }}>
          <code className="flex-1 font-mono text-[13px] break-all" style={{ color: 'var(--text)' }}>{createdToken}</code>
          <button onClick={copyToken} className="bg-transparent cursor-pointer p-1.5 shrink-0 rounded-[4px]" style={{ color: 'var(--text-soft)' }}><Icon name={copied ? 'check' : 'content_copy'} size="sm" /></button>
        </div>
        <p className="text-[12px] mt-2" style={{ color: 'var(--danger-text)' }}>{t('users.tokenSaveWarning')}</p>
        <div className="flex justify-end mt-4"><ButtonV2 onClick={() => setTokenResultOpen(false)}>{t('confirm')}</ButtonV2></div>
      </ModalV2>
    </div>
  )
}
