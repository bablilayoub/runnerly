import { useEffect, useState } from 'react'

export type Theme = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'runnerly.theme'

/**
 * The theme lives on <html> as a class, because that is what every shadcn
 * component's `dark:` variant reads. Nothing else may decide independently:
 * a component asking the media query while the page asks storage is how a
 * half-dark page happens.
 *
 * index.html applies the stored choice before this module loads, so the
 * first paint is already right.
 */
export function readTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
  } catch {
    // Storage can be refused outright in a locked-down browser. Following
    // the operating system is a fine answer and needs no permission.
  }
  return 'system'
}

export function applyTheme(theme: Theme): void {
  const dark =
    theme === 'dark' ||
    (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)

  document.documentElement.classList.toggle('dark', dark)
}

/** useTheme returns the current choice and a setter that persists it. */
export function useTheme(): [Theme, (next: Theme) => void] {
  const [theme, setTheme] = useState<Theme>(readTheme)

  useEffect(() => {
    applyTheme(theme)

    // Following the system means following it as it changes, not only at
    // load. Without this, a laptop switching to dark at sunset leaves the
    // dashboard light until it is reloaded.
    if (theme !== 'system') return
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyTheme('system')
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [theme])

  return [
    theme,
    (next: Theme) => {
      setTheme(next)
      try {
        localStorage.setItem(STORAGE_KEY, next)
      } catch {
        // The page still honors it for this session.
      }
    },
  ]
}

/** resolvedTheme is what is actually on screen right now. */
export function resolvedTheme(): 'light' | 'dark' {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

/**
 * useResolvedTheme tracks what is on screen, for the few things that need
 * to be told rather than styled — a third-party component with its own
 * theme prop, for instance.
 *
 * It watches the element rather than the preference, so it is right no
 * matter who changed it: the toggle, the operating system, or the script
 * in index.html before React existed.
 */
export function useResolvedTheme(): 'light' | 'dark' {
  const [theme, setTheme] = useState<'light' | 'dark'>(resolvedTheme)

  useEffect(() => {
    const html = document.documentElement
    const observer = new MutationObserver(() => setTheme(resolvedTheme()))
    observer.observe(html, { attributes: true, attributeFilter: ['class'] })
    setTheme(resolvedTheme())
    return () => observer.disconnect()
  }, [])

  return theme
}
