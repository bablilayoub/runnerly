import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import { Button } from './primitives'

interface ConfirmProps {
  title: string
  body: ReactNode
  confirmLabel: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}

/**
 * Confirm is the gate in front of anything that cannot be undone from here.
 *
 * It is a <dialog>, so the browser handles focus trapping, Escape and the
 * backdrop rather than this file reimplementing them badly.
 */
export function Confirm({ title, body, confirmLabel, danger, onConfirm, onCancel }: ConfirmProps) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const dialog = ref.current
    if (dialog && !dialog.open) {
      dialog.showModal()
    }
  }, [])

  return (
    <dialog
      ref={ref}
      onCancel={(e) => {
        e.preventDefault()
        onCancel()
      }}
      className="m-auto w-[min(32rem,calc(100vw-2rem))] rounded-md border p-0 backdrop:bg-black/40"
      style={{
        borderColor: 'var(--border-strong)',
        background: 'var(--bg-raised)',
        color: 'var(--text)',
      }}
    >
      <div className="px-5 py-4">
        <h2 className="text-base font-medium">{title}</h2>
        <div className="mt-2 text-sm" style={{ color: 'var(--text-muted)' }}>
          {body}
        </div>
      </div>
      <div
        className="flex justify-end gap-2 border-t px-5 py-3"
        style={{ borderColor: 'var(--border)' }}
      >
        <Button onClick={onCancel}>Cancel</Button>
        <Button onClick={onConfirm} variant={danger ? 'danger' : 'default'}>
          {confirmLabel}
        </Button>
      </div>
    </dialog>
  )
}
