import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldLabel, FieldError } from '@/components/ui/field'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'

import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import type {
  AdminCascadeConfigState,
  AdminCascadeConfigUpdateResponse,
} from '@/lib/adminApi.types'
import {
  buildCascadePatch,
  cascadeDraftHasErrors,
  draftFromState,
  newPeerDraft,
  validateCascadeDraft,
  type CascadeConfigDraft,
  type CascadeDraftErrors,
  type CascadePeerDraft,
} from '@/admin/cascade/configDraft'

interface CascadeConfigFormProps {
  state: AdminCascadeConfigState
  canWrite: boolean
  onSaved: (result: AdminCascadeConfigUpdateResponse) => void
}

const peerRowGrid = 'grid gap-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,1fr)_auto_auto] lg:items-center'

/**
 * Inline editor for the [cascade] section. It lives directly on the Cascade
 * page instead of a dialog so changing a value is one level deep, and the
 * sticky save bar only appears once something actually changed.
 */
export default function CascadeConfigForm({ state, canWrite, onSaved }: CascadeConfigFormProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<CascadeConfigDraft>(() => draftFromState(state))
  const [errors, setErrors] = useState<CascadeDraftErrors>({ peers: {} })
  const [dirty, setDirty] = useState(false)

  const saveMutation = useMutation({
    mutationFn: (next: CascadeConfigDraft) => adminApi.updateCascadeConfig(buildCascadePatch(next)),
    onSuccess: ({ data }) => {
      setDirty(false)
      onSaved(data)
    },
  })

  function update(update: Partial<CascadeConfigDraft>) {
    setDraft(current => ({ ...current, ...update }))
    setDirty(true)
  }

  function updatePeer(key: string, peerUpdate: Partial<CascadePeerDraft>) {
    setDraft(current => ({
      ...current,
      peers: current.peers.map(peer => peer.key === key ? { ...peer, ...peerUpdate } : peer),
    }))
    setDirty(true)
  }

  function discard() {
    setDraft(draftFromState(state))
    setErrors({ peers: {} })
    saveMutation.reset()
    setDirty(false)
  }

  function submit() {
    const nextErrors = validateCascadeDraft(draft)
    setErrors(nextErrors)
    if (cascadeDraftHasErrors(nextErrors)) return
    saveMutation.mutate(draft)
  }

  const saveError = saveMutation.error ? getApiError(saveMutation.error) : null
  const disabled = !canWrite || !state.config_writable || saveMutation.isPending
  const sharedTokenPlaceholder = draft.clearSharedToken
    ? t('cascade.config.tokenClearedPlaceholder')
    : draft.sharedTokenSet
      ? t('cascade.config.tokenKeepPlaceholder')
      : t('cascade.config.tokenNewPlaceholder')

  return (
    <form
      data-cascade-config-form
      className="space-y-4"
      onSubmit={event => {
        event.preventDefault()
        submit()
      }}
    >
      <fieldset className="space-y-4" disabled={disabled}>
        <div className="flex flex-wrap gap-x-8 gap-y-2">
          <Field orientation="horizontal">
            <Switch
              checked={draft.enabled}
              onCheckedChange={checked => update({ enabled: checked })}
            />
            <FieldLabel>{t('cascade.config.enabled')}</FieldLabel>
          </Field>
          <Field orientation="horizontal">
            <Switch
              checked={draft.allowInsecureHTTP}
              onCheckedChange={checked => update({ allowInsecureHTTP: checked })}
            />
            <FieldLabel>{t('cascade.config.allowInsecureHTTP')}</FieldLabel>
          </Field>
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Field invalid={Boolean(errors.maxHops ? t(errors.maxHops) : undefined)}>
            <FieldLabel>{t('cascade.config.maxHops')}</FieldLabel>
            <Input aria-invalid={Boolean(errors.maxHops ? t(errors.maxHops) : undefined) || undefined}
              type="number"
              min={1}
              max={16}
              value={draft.maxHops}
              onChange={event => update({ maxHops: event.target.value })}
            />
            {(errors.maxHops ? t(errors.maxHops) : undefined) && <FieldError match>{errors.maxHops ? t(errors.maxHops) : undefined}</FieldError>}
          </Field>
          <Field invalid={Boolean(errors.maxTTL ? t(errors.maxTTL) : undefined)}>
            <FieldLabel>{t('cascade.config.maxTTL')}</FieldLabel>
            <Input aria-invalid={Boolean(errors.maxTTL ? t(errors.maxTTL) : undefined) || undefined} className="font-mono"
              value={draft.maxTTL}
              onChange={event => update({ maxTTL: event.target.value })}
            />
            {(errors.maxTTL ? t(errors.maxTTL) : undefined) && <FieldError match>{errors.maxTTL ? t(errors.maxTTL) : undefined}</FieldError>}
          </Field>
          <div className="flex items-end gap-1">
            <Field invalid={Boolean(errors.sharedToken ? t(errors.sharedToken) : undefined)}>
              <FieldLabel>{t('cascade.config.sharedToken')}</FieldLabel>
              <Input aria-invalid={Boolean(errors.sharedToken ? t(errors.sharedToken) : undefined) || undefined}
                className="min-w-0 flex-1"
                type="password"
                autoComplete="new-password"
                value={draft.sharedToken}
                placeholder={sharedTokenPlaceholder}
                onChange={event => update({ sharedToken: event.target.value, clearSharedToken: false })}
              />
              {(errors.sharedToken ? t(errors.sharedToken) : undefined) && <FieldError match>{errors.sharedToken ? t(errors.sharedToken) : undefined}</FieldError>}
            </Field>
            {draft.sharedTokenSet && draft.sharedToken === '' && (
              <Tooltip>
                <TooltipTrigger render={
                  <Button
                    onClick={() => update({ clearSharedToken: !draft.clearSharedToken })}
                    variant="ghost"
                    size="icon"
                    data-icon-button
                    aria-label={t(draft.clearSharedToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}
                    className="mb-0.5"
                    style={{ color: 'var(--text-soft)' }}
                  />
                }>
                  <Icon name={draft.clearSharedToken ? 'undo' : 'key'} size="sm" />
                </TooltipTrigger>
                <TooltipContent>{t(draft.clearSharedToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}</TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
      </fieldset>

      <fieldset className="space-y-2" disabled={disabled}>
        <div className="flex items-center justify-between gap-2">
          <legend className="text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
            {t('cascade.config.peersSection')}
          </legend>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              setDraft(current => ({ ...current, peers: [...current.peers, newPeerDraft()] }))
              setDirty(true)
            }}
          >
            <Icon name="add" size="sm" />
            {t('cascade.config.addPeer')}
          </Button>
        </div>
        {draft.peers.length === 0 ? (
          <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
            {t('cascade.config.peersEmpty')}
          </p>
        ) : (
          <>
            <div className={`${peerRowGrid} hidden px-1 text-[10px] font-[600] uppercase tracking-[0.06em] lg:grid`} style={{ color: 'var(--text-subtle)' }}>
              <span>{t('cascade.config.peerName')}</span>
              <span>{t('cascade.config.peerURL')}</span>
              <span>{t('cascade.config.peerTokenShort')}</span>
              <span className="text-center">{t('cascade.config.peerForwardShort')}</span>
              <span />
            </div>
            <div className="max-h-[300px] space-y-2 overflow-y-auto pr-1">
            {draft.peers.map(peer => {
              const peerErrors = errors.peers[peer.key]
              const tokenPlaceholder = peer.clearToken
                ? t('cascade.config.peerTokenClearedPlaceholder')
                : peer.tokenSet
                  ? t('cascade.config.peerTokenKeepPlaceholder')
                  : t('cascade.config.peerTokenInheritPlaceholder')
              return (
                <div
                  key={peer.key}
                  data-cascade-peer-editor={peer.key}
                  className={`${peerRowGrid} rounded-md border p-2 lg:border-0 lg:p-0`}
                  style={{ borderColor: 'var(--border)' }}
                >
                  <Field invalid={Boolean(peerErrors?.name ? t(peerErrors.name) : undefined)}>
                    <Input aria-invalid={Boolean(peerErrors?.name ? t(peerErrors.name) : undefined) || undefined}
                      aria-label={t('cascade.config.peerName')}
                      placeholder={t('cascade.config.peerName')}
                      value={peer.name}
                      maxLength={64}
                      autoComplete="off"
                      onChange={event => updatePeer(peer.key, { name: event.target.value })}
                    />
                    {(peerErrors?.name ? t(peerErrors.name) : undefined) && <FieldError match>{peerErrors?.name ? t(peerErrors.name) : undefined}</FieldError>}
                  </Field>
                  <Field invalid={Boolean(peerErrors?.url ? t(peerErrors.url) : undefined)}>
                    <Input aria-invalid={Boolean(peerErrors?.url ? t(peerErrors.url) : undefined) || undefined} className="font-mono"
                      aria-label={t('cascade.config.peerURL')}
                      placeholder="https://cache.example:23333"
                      autoComplete="off"
                      spellCheck={false}
                      value={peer.url}
                      onChange={event => updatePeer(peer.key, { url: event.target.value })}
                    />
                    {(peerErrors?.url ? t(peerErrors.url) : undefined) && <FieldError match>{peerErrors?.url ? t(peerErrors.url) : undefined}</FieldError>}
                  </Field>
                  <div className="flex items-center gap-1">
                    <Field invalid={Boolean(peerErrors?.token ? t(peerErrors.token) : undefined)}>
                      <Input aria-invalid={Boolean(peerErrors?.token ? t(peerErrors.token) : undefined) || undefined}
                        className="min-w-0 flex-1"
                        aria-label={t('cascade.config.peerToken')}
                        placeholder={tokenPlaceholder}
                        type="password"
                        autoComplete="new-password"
                        value={peer.token}
                        onChange={event => updatePeer(peer.key, { token: event.target.value, clearToken: false })}
                      />
                      {(peerErrors?.token ? t(peerErrors.token) : undefined) && <FieldError match>{peerErrors?.token ? t(peerErrors.token) : undefined}</FieldError>}
                    </Field>
                    {peer.tokenSet && peer.token === '' && (
                      <Tooltip>
                        <TooltipTrigger render={
                          <Button
                            onClick={() => updatePeer(peer.key, { clearToken: !peer.clearToken })}
                            variant="ghost"
                            size="icon"
                            data-icon-button
                            aria-label={t(peer.clearToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}
                            style={{ color: 'var(--text-soft)' }}
                          />
                        }>
                          <Icon name={peer.clearToken ? 'undo' : 'key'} size="sm" />
                        </TooltipTrigger>
                        <TooltipContent>{t(peer.clearToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}</TooltipContent>
                      </Tooltip>
                    )}
                  </div>
                  <div className="flex justify-start lg:justify-center">
                    <Switch
                      aria-label={t('cascade.config.peerForwardCredentials')}
                      checked={peer.forwardCredentials}
                      onCheckedChange={checked => updatePeer(peer.key, { forwardCredentials: checked })}
                    />
                  </div>
                  <Tooltip>
                    <TooltipTrigger render={
                      <Button
                        onClick={() => {
                          setDraft(current => ({
                            ...current,
                            peers: current.peers.filter(candidate => candidate.key !== peer.key),
                          }))
                          setDirty(true)
                        }}
                        variant="ghost"
                        size="icon"
                        data-icon-button
                        aria-label={t('cascade.config.removePeer', {
                          name: peer.name || t('cascade.config.peerName'),
                        })}
                        style={{ color: 'var(--danger-text)' }}
                      />
                    }>
                      <Icon name={"delete"} size="sm" />
                    </TooltipTrigger>
                    <TooltipContent>{t('cascade.config.removePeer', {
                      name: peer.name || t('cascade.config.peerName'),
                    })}</TooltipContent>
                  </Tooltip>
                </div>
              )
            })}
            </div>
          </>
        )}
      </fieldset>

      {saveError && (
        <Alert variant={"destructive"}><AlertDescription>
          {saveError.code === 'CONFIG_READ_ONLY'
            ? t('cascade.config.readOnlyError')
            : t('cascade.config.saveError', { reason: saveError.message })}
        </AlertDescription></Alert>
      )}

      {dirty && (
        <div
          data-cascade-save-bar
          className="sticky bottom-4 z-10 flex flex-wrap items-center justify-between gap-3 rounded-md border px-4 py-3"
          style={{
            borderColor: 'var(--border)',
            background: 'var(--bg-card)',
            boxShadow: '0 4px 16px color-mix(in oklab, var(--text) 8%, transparent)',
          }}
        >
          <span className="text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
            {t('cascade.config.unsaved')}
          </span>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={saveMutation.isPending}
              onClick={discard}
            >
              {t('cascade.config.discard')}
            </Button>
            <Button
              type="submit"
              size="sm"
              aria-busy={saveMutation.isPending || undefined}
              disabled={saveMutation.isPending || disabled}
            >
              {saveMutation.isPending ? t('saving') : t('save')}
            </Button>
          </div>
        </div>
      )}
    </form>
  )
}
