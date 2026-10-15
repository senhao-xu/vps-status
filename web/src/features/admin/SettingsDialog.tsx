import { type ReactNode, type RefObject, useEffect, useId, useRef } from 'react'

import { useI18n } from '../../i18n'

export function SettingsDialog({ title, pending, onClose, children, className = '', fallbackFocus }: {
  title: string; pending: boolean; onClose: () => void; children: ReactNode; className?: string; fallbackFocus?: RefObject<HTMLElement | null>
}) {
  const { t } = useI18n()
  const id = useId()
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const element = ref.current, previous = document.activeElement
    element?.showModal()
    element?.querySelector<HTMLElement>('[data-initial-focus]')?.focus()
    return () => { element?.close(); if (previous instanceof HTMLElement && previous.isConnected) previous.focus(); else fallbackFocus?.current?.focus() }
  }, [fallbackFocus])
  return <dialog ref={ref} className={`enrollment-dialog settings-dialog ${className}`.trim()} aria-labelledby={id} aria-busy={pending} onCancel={(event) => { event.preventDefault(); if (!pending) onClose() }}>
    <header className="dialog-heading"><h2 id={id}>{title}</h2><button type="button" disabled={pending} aria-label={t('close')} onClick={onClose}>×</button></header>{children}
  </dialog>
}
