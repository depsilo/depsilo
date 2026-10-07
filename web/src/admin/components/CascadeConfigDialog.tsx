import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import IconButton from '@/components/IconButton'
import InlineNotice from '@/components/InlineNotice'
import InputV2 from '@/components/Input'
import ModalV2 from '@/components/Modal'
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

interface CascadeConfigDialogProps {
  state: AdminCascadeConfigState
  onClose: () => void
  onSaved: (result: AdminCascadeConfigUpdateResponse) => void
}

export default function CascadeConfigDialog({
  state,
  onClose,
  onSaved,
}: CascadeConfigDialogProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<CascadeConfigDraft>(() => draftFromState(state))
  const [errors, setErrors] = useState<CascadeDraftErrors>({ peers: {} })

  const saveMutation = useMutation({
    mutationFn: (next: CascadeConfigDraft) => adminApi.updateCascadeConfig(buildCascadePatch(next)),
    onSuccess: ({ data }) => onSaved(data),
  })

  function updatePeer(key: string, update: Partial<CascadePeerDraft>) {
    setDraft(current => ({
      ...current,
      peers: current.peers.map(peer => peer.key === key ? { ...peer, ...update } : peer),
    }))
  }

  function submit() {
    const nextErrors = validateCascadeDraft(draft)
    setErrors(nextErrors)
    if (cascadeDraftHasErrors(nextErrors)) return
    saveMutation.mutate(draft)
  }

  const saveError = saveMutation.error ? getApiError(saveMutation.error) : null

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
    <ModalV2
      open
      onClose={onClose}
      title={t('cascade.config.title')}
      width={720}
      closeDisabled={saveMutation.isPending}
    >
      <form
        className="space-y-6"
        onSubmit={event => {
          event.preventDefault()
          submit()
        }}
      >
        <p className="text-[12px] leading-5" style={{ color: 'var(--text-soft)' }}>
          {t('cascade.config.description')}
        </p>
        <fieldset className="space-y-4" disabled={saveMutation.isPending}>
          <legend className="mb-2 text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
            {t('cascade.config.generalSection')}
          </legend>
          <div className="flex flex-wrap gap-x-8 gap-y-2">
            <SwitchV2
              label={t('cascade.config.enabled')}
              checked={draft.enabled}
              onCheckedChange={checked => setDraft({ ...draft, enabled: checked })}
            />
            <SwitchV2
              label={t('cascade.config.allowInsecureHTTP')}
              checked={draft.allowInsecureHTTP}
              onCheckedChange={checked => setDraft({ ...draft, allowInsecureHTTP: checked })}
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
              onChange={event => setDraft({ ...draft, maxHops: event.target.value })}
            />
            <InputV2
              label={t('cascade.config.maxTTL')}
              mono
              value={draft.maxTTL}
              error={errors.maxTTL ? t(errors.maxTTL) : undefined}
              hint={t('cascade.config.maxTTLHint')}
              onChange={event => setDraft({ ...draft, maxTTL: event.target.value })}
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
              onChange={event => setDraft({
                ...draft,
                sharedToken: event.target.value,
                clearSharedToken: false,
              })}
            />
            {draft.sharedTokenSet && draft.sharedToken === '' && (
              <ButtonV2
                type="button"
                variant="ghost"
                size="sm"
                className="mt-1"
                onClick={() => setDraft({ ...draft, clearSharedToken: !draft.clearSharedToken })}
              >
                {t(draft.clearSharedToken ? 'cascade.config.undoClear' : 'cascade.config.clearToken')}
              </ButtonV2>
            )}
          </div>
        </fieldset>

        <fieldset className="space-y-3 border-t border-[var(--border)] pt-4" disabled={saveMutation.isPending}>
          <legend className="px-1 text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
            {t('cascade.config.peersSection')}
          </legend>
          {draft.peers.length === 0 ? (
            <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
              {t('cascade.config.peersEmpty')}
            </p>
          ) : (
            <div className="max-h-[340px] space-y-3 overflow-y-auto pr-1">
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
                      label={t('cascade.config.removePeer', { name: peer.name || t('cascade.config.peerLabel', { index: index + 1 }) })}
                      tone="danger"
                      onClick={() => setDraft({
                        ...draft,
                        peers: draft.peers.filter(candidate => candidate.key !== peer.key),
                      })}
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
            onClick={() => setDraft({ ...draft, peers: [...draft.peers, newPeerDraft()] })}
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

        <div className="flex justify-end gap-3 pt-1">
          <ButtonV2
            type="button"
            variant="secondary"
            className="min-h-[40px]"
            disabled={saveMutation.isPending}
            onClick={onClose}
          >
            {t('cancel')}
          </ButtonV2>
          <ButtonV2
            type="submit"
            className="min-h-[40px]"
            aria-busy={saveMutation.isPending || undefined}
            disabled={saveMutation.isPending}
          >
            {saveMutation.isPending ? t('saving') : t('save')}
          </ButtonV2>
        </div>
      </form>
    </ModalV2>
  )
}
