/** Formatting helpers shared by the pages. */

/** relativeTime turns a timestamp into "3s ago", "4m ago", "2d ago". */
export function relativeTime(iso: string | undefined, now: number): string {
  if (!iso) return 'never'
  const then = Date.parse(iso)
  if (Number.isNaN(then)) return 'unknown'
  return `${duration((now - then) / 1000)} ago`
}

/** duration renders seconds compactly: 45s, 3m, 2h, 5d. */
export function duration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  return `${Math.floor(s / 86400)}d`
}

/** bytes renders a byte count, or a dash when it was never measured. */
export function bytes(value: number): string {
  if (!value) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = value
  let unit = 0
  while (n >= 1024 && unit < units.length - 1) {
    n /= 1024
    unit++
  }
  return `${n < 10 && unit > 0 ? n.toFixed(1) : Math.round(n)} ${units[unit]}`
}

/** percent renders a 0-100 reading, or a dash when nothing was reported. */
export function percent(value: number): string {
  if (!value) return '—'
  return `${Math.round(value)}%`
}

/** orDash keeps empty cells visibly empty rather than blank. */
export function orDash(value: string | undefined | null): string {
  return value && value.length > 0 ? value : '—'
}
