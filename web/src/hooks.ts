import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from './api'

export interface Loadable<T> {
  data: T | undefined
  error: ApiError | Error | undefined
  loading: boolean
  /** True only for the first load, so a poll does not blank the page. */
  initial: boolean
  reload: () => void
}

/**
 * useLoad fetches once and optionally keeps polling.
 *
 * Polling replaces the data in place rather than clearing it first: a
 * dashboard that flashes empty every few seconds is harder to read than one
 * that updates quietly.
 *
 * `deps` are the inputs the request depends on — a filter, a route
 * parameter. Changing one fetches again at once instead of waiting for the
 * next tick. Without that, picking "Error" on the events page highlighted
 * the button and then showed the unfiltered list for another five seconds,
 * which reads as a filter that does not work rather than one that is
 * slow. The loader itself deliberately stays out of the dependency list:
 * it is an inline arrow function, new on every render, and depending on it
 * would restart the poll continuously.
 */
export function useLoad<T>(
  load: () => Promise<T>,
  intervalMs = 0,
  deps: readonly unknown[] = [],
): Loadable<T> {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<ApiError | Error>()
  const [loading, setLoading] = useState(true)
  const [initial, setInitial] = useState(true)

  // Keeping the loader in a ref lets callers pass an inline arrow function
  // without restarting the poll on every render.
  const loader = useRef(load)
  loader.current = load

  const run = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const result = await loader.current()
      if (signal?.aborted) return
      setData(result)
      setError(undefined)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      if (!signal?.aborted) {
        setLoading(false)
        setInitial(false)
      }
    }
  }, [])

  // Serialised, so a caller can pass an inline array without the effect
  // re-running on identity alone.
  const key = JSON.stringify(deps)

  useEffect(() => {
    const controller = new AbortController()
    void run(controller.signal)

    if (intervalMs <= 0) {
      return () => controller.abort()
    }

    const timer = setInterval(() => {
      // A hidden tab does not need fresh numbers, and polling one wastes
      // the server's time and the laptop's battery.
      if (document.visibilityState === 'visible') {
        void run(controller.signal)
      }
    }, intervalMs)

    return () => {
      controller.abort()
      clearInterval(timer)
    }
  }, [run, intervalMs, key])

  return { data, error, loading, initial, reload: () => void run() }
}

/** useNow re-renders on a timer so relative times stay honest. */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])
  return now
}
