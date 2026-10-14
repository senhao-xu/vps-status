import { type ReactNode, useEffect, useId, useRef } from 'react'

import { useI18n } from '../../i18n'

export function SettingsDialog({ title, pending, onClose, children }: { title: string; pending: boolean; onClose: () => void; children: ReactNode }) {
  const { t } = useI18n()
  const id = useId()
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const element = ref.current, previous = document.activeElement
    element?.showModal()
    element?.querySelector<HTMLElement>('[data-initial-focus]')?.focus()
    return () => { element?.close(); if (previous instanceof HTMLElement && previous.isConnected) previous.focus() }
  }, [])
  return <dialog ref={ref} className="enrollment-dialog settings-dialog" aria-labelledby={id} aria-busy={pending} onCancel={(event) => { event.preventDefault(); if (!pending) onClose() }}>
    <header className="dialog-heading"><h2 id={id}>{title}</h2><button type="button" disabled={pending} aria-label={t('close')} onClick={onClose}>×</button></header>{children}
  </dialog>
}
