import type { ReactNode } from 'react'
import type { Health, RunnerStatus, Severity } from '../types'

/** StatusDot is the only place color carries meaning in the interface. */
export function StatusDot({ status }: { status: RunnerStatus }) {
  const color =
    status === 'online' || status === 'starting'
      ? 'var(--ok)'
      : status === 'busy'
        ? 'var(--busy)'
        : status === 'error'
          ? 'var(--bad)'
          : 'var(--text-faint)'

  return (
    <span
      aria-hidden="true"
      className="inline-block size-2 shrink-0 rounded-full"
      style={{ background: color }}
    />
  )
}

/** Status pairs the dot with its word, so the meaning does not rely on color. */
export function Status({ status, health }: { status: RunnerStatus; health?: Health }) {
  return (
    <span className="inline-flex items-center gap-2 whitespace-nowrap">
      <StatusDot status={status} />
      <span>{status}</span>
      {health === 'stale' && (
        <span className="text-xs" style={{ color: 'var(--warn)' }}>
          stale
        </span>
      )}
    </span>
  )
}

export function SeverityTag({ severity }: { severity: Severity }) {
  const color =
    severity === 'error' ? 'var(--bad)' : severity === 'warn' ? 'var(--warn)' : 'var(--text-faint)'
  return (
    <span className="font-mono text-xs uppercase" style={{ color }}>
      {severity}
    </span>
  )
}

export function Label({ children }: { children: ReactNode }) {
  return (
    <span
      className="rounded-sm border px-1.5 py-0.5 font-mono text-xs"
      style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
    >
      {children}
    </span>
  )
}

type ButtonProps = {
  children: ReactNode
  onClick?: () => void
  type?: 'button' | 'submit'
  variant?: 'default' | 'danger'
  disabled?: boolean
  title?: string
}

export function Button({
  children,
  onClick,
  type = 'button',
  variant = 'default',
  disabled,
  title,
}: ButtonProps) {
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      title={title}
      className="cursor-pointer rounded-sm border px-3 py-1.5 text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-50"
      style={{
        borderColor: variant === 'danger' ? 'var(--bad)' : 'var(--border-strong)',
        color: variant === 'danger' ? 'var(--bad)' : 'var(--text)',
        background: 'var(--bg-raised)',
      }}
    >
      {children}
    </button>
  )
}

export function Card({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section
      className="rounded-md border"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-raised)' }}
    >
      {title && (
        <h2
          className="border-b px-4 py-2.5 text-xs font-medium tracking-wide uppercase"
          style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
        >
          {title}
        </h2>
      )}
      {children}
    </section>
  )
}

/** Stat is one number on the overview. */
export function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <div
      className="rounded-md border px-4 py-3"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-raised)' }}
    >
      <div className="text-2xl" style={{ color: 'var(--text)' }}>
        {value}
      </div>
      <div
        className="mt-0.5 text-xs tracking-wide uppercase"
        style={{ color: 'var(--text-muted)' }}
      >
        {label}
      </div>
    </div>
  )
}

/** Empty states say what to do, not just that there is nothing. */
export function Empty({ title, hint }: { title: string; hint?: ReactNode }) {
  return (
    <div className="px-4 py-10 text-center">
      <p style={{ color: 'var(--text-muted)' }}>{title}</p>
      {hint && (
        <p className="mt-2 text-sm" style={{ color: 'var(--text-faint)' }}>
          {hint}
        </p>
      )}
    </div>
  )
}

/** Failure shows a server error together with the hint it came with. */
export function Failure({ error }: { error: Error }) {
  const hint = 'hint' in error ? String(error.hint) : ''
  return (
    <div
      className="rounded-md border px-4 py-3 text-sm"
      style={{ borderColor: 'var(--bad)', color: 'var(--text)' }}
      role="alert"
    >
      <p>{error.message}</p>
      {hint && (
        <p className="mt-1" style={{ color: 'var(--text-muted)' }}>
          {hint}
        </p>
      )}
    </div>
  )
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <div className="px-4 py-10 text-center text-sm" style={{ color: 'var(--text-faint)' }}>
      {label}…
    </div>
  )
}
