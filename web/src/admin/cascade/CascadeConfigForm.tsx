import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import IconButton from '@/components/IconButton'
import InlineNotice from '@/components/InlineNotice'
import InputV2 from '@/components/Input'
import SwitchV2 from '@/components/Switch'
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

  function sharedTokenHint(): string {
    if (draft.clearSharedToken) return t('cascade.config.tokenClearedHint')
    if (draft.sharedTokenSet) return t('cascade.config.tokenKeepHint')
    return t('cascade.config.tokenNewHint')
  }

  function peerTokenHint(peer: CascadePeerDraft): string {
    if (peer.clearToken) return t('cascade.config.peerTokenClearedHint')
    if (peer.tokenSet) return t('cascade.config.peerTokenKeepHint')
    return t('cascade.config.peerTokenInheritHint')
  }

  return (
    <form
      data-cascade-config-form
      className="space-y-5"
      onSubmit={event => {
        event.preventDefault()
        submit()
      }}
    >
      <fieldset className="space-y-4" disabled={disabled}>
        <div className="flex flex-wrap gap-x-8 gap-y-2">
          <SwitchV2
            label={t('cascade.config.enabled')}
            checked={draft.enabled}
            onCheckedChange={checked => update({ enabled: checked })}
          />
          <SwitchV2
            label={t('cascade.config.allowInsecureHTTP')}
            checked={draft.allowInsecureHTTP}
            onCheckedChange={checked => update({ allowInsecureHTTP: checked })}
          />
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <InputV2
            label={t('cascade.config.maxHops')}
            type="number"
            min={1}
            max={16}
            value={draft.maxHops}
            error={errors.maxHops ? t(errors.maxHops) : undefined}
            onChange={event => update({ maxHops: event.target.value })}
          />
          <InputV2
            label={t('cascade.config.maxTTL')}
            mono
            value={draft.maxTTL}
            error={errors.maxTTL ? t(errors.maxTTL) : undefined}
            hint={t('cascade.config.maxTTLHint')}
            onChange={event => update({ maxTTL: event.target.value })}
          />
        </div>
        <div>
          <InputV2
            label={t('cascade.config.sharedToken')}
            type="password"
            autoComplete="new-password"
            value={draft.sharedToken}
            hint={sharedTokenHint()}
            error={errors.sharedToken ? t(errors.sharedToken) : undefined}
            onChange={event => update({ sharedToken: event.target.value, clearSharedToken: false })}
          />
          {draft.sharedTokenSet && draft.sharedToken === '' && (
            <ButtonV2
              type="button"
              variant="ghost"
              size="sm"
              className="mt-1"
              onClick={() => update({ clearSharedToken: !draft.clearSharedToken })}
            >
              {t(draft.clearSharedToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}
            </ButtonV2>
          )}
        </div>
      </fieldset>

      <fieldset className="space-y-3" disabled={disabled}>
        <legend className="text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
          {t('cascade.config.peersSection')}
        </legend>
        {draft.peers.length === 0 ? (
          <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
            {t('cascade.config.peersEmpty')}
          </p>
        ) : (
          <div className="max-h-[420px] space-y-3 overflow-y-auto pr-1">
            {draft.peers.map((peer, index) => {
              const peerErrors = errors.peers[peer.key]
              return (
                <div
                  key={peer.key}
                  data-cascade-peer-editor={peer.key}
                  className="space-y-3 rounded-md border p-3"
                  style={{ borderColor: 'var(--border)' }}
                >
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
                      {t('cascade.config.peerLabel', { index: index + 1 })}
                    </span>
                    <IconButton
                      icon="delete"
                      label={t('cascade.config.removePeer', {
                        name: peer.name || t('cascade.config.peerLabel', { index: index + 1 }),
                      })}
                      tone="danger"
                      onClick={() => {
                        setDraft(current => ({
                          ...current,
                          peers: current.peers.filter(candidate => candidate.key !== peer.key),
                        }))
                        setDirty(true)
                      }}
                    />
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <InputV2
                      label={t('cascade.config.peerName')}
                      value={peer.name}
                      maxLength={64}
                      autoComplete="off"
                      error={peerErrors?.name ? t(peerErrors.name) : undefined}
                      onChange={event => updatePeer(peer.key, { name: event.target.value })}
                    />
                    <InputV2
                      label={t('cascade.config.peerURL')}
                      mono
                      autoComplete="off"
                      spellCheck={false}
                      value={peer.url}
                      error={peerErrors?.url ? t(peerErrors.url) : undefined}
                      onChange={event => updatePeer(peer.key, { url: event.target.value })}
                    />
                  </div>
                  <InputV2
                    label={t('cascade.config.peerToken')}
                    type="password"
                    autoComplete="new-password"
                    value={peer.token}
                    hint={peerTokenHint(peer)}
                    error={peerErrors?.token ? t(peerErrors.token) : undefined}
                    onChange={event => updatePeer(peer.key, { token: event.target.value, clearToken: false })}
                  />
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                    {peer.tokenSet && peer.token === '' && (
                      <ButtonV2
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => updatePeer(peer.key, { clearToken: !peer.clearToken })}
                      >
                        {t(peer.clearToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}
                      </ButtonV2>
                    )}
                    <SwitchV2
                      label={t('cascade.config.peerForwardCredentials')}
                      checked={peer.forwardCredentials}
                      onCheckedChange={checked => updatePeer(peer.key, { forwardCredentials: checked })}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        )}
        <ButtonV2
          type="button"
          variant="secondary"
          size="sm"
          onClick={() => {
            setDraft(current => ({ ...current, peers: [...current.peers, newPeerDraft()] }))
            setDirty(true)
          }}
        >
          <Icon name="add" size="sm" />
          {t('cascade.config.addPeer')}
        </ButtonV2>
      </fieldset>

      {saveError && (
        <InlineNotice tone="danger">
          {saveError.code === 'CONFIG_READ_ONLY'
            ? t('cascade.config.readOnlyError')
            : t('cascade.config.saveError', { reason: saveError.message })}
        </InlineNotice>
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
            <ButtonV2
              type="button"
              variant="ghost"
              size="sm"
              disabled={saveMutation.isPending}
              onClick={discard}
            >
              {t('cascade.config.discard')}
            </ButtonV2>
            <ButtonV2
              type="submit"
              size="sm"
              aria-busy={saveMutation.isPending || undefined}
              disabled={saveMutation.isPending || disabled}
            >
              {saveMutation.isPending ? t('saving') : t('save')}
            </ButtonV2>
          </div>
        </div>
      )}
    </form>
  )
}
