import type { ReactNode } from 'react'

/** Table keeps every listing in the dashboard looking the same. */
export function Table({ head, children }: { head: ReactNode[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b" style={{ borderColor: 'var(--border)' }}>
            {head.map((cell, i) => (
              <th
                key={i}
                scope="col"
                className="px-4 py-2 text-left text-xs font-medium tracking-wide whitespace-nowrap uppercase"
                style={{ color: 'var(--text-muted)' }}
              >
                {cell}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

export function Row({ children, onClick }: { children: ReactNode; onClick?: () => void }) {
  return (
    <tr
      onClick={onClick}
      className={`border-b last:border-0 ${onClick ? 'cursor-pointer' : ''}`}
      style={{ borderColor: 'var(--border)' }}
    >
      {children}
    </tr>
  )
}

export function Cell({
  children,
  mono,
  muted,
}: {
  children: ReactNode
  mono?: boolean
  muted?: boolean
}) {
  return (
    <td
      className={`px-4 py-2 align-middle ${mono ? 'font-mono text-xs' : ''}`}
      style={muted ? { color: 'var(--text-muted)' } : undefined}
    >
      {children}
    </td>
  )
}
