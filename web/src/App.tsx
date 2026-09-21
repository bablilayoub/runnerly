import { useEffect, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { ApiError, api } from '@/api'
import { Layout } from '@/components/Layout'
import { Failure, PageLoading } from '@/components/states'
import { Events } from '@/pages/Events'
import { Login } from '@/pages/Login'
import { Overview } from '@/pages/Overview'
import { RunnerDetail } from '@/pages/RunnerDetail'
import { Runners } from '@/pages/Runners'
import { Settings } from '@/pages/Settings'
import type { User } from '@/types'

type Auth =
  | { state: 'loading' }
  | { state: 'signed-in'; user: User }
  | { state: 'signed-out'; reason?: string }
  | { state: 'broken'; error: Error }

export function App() {
  const [auth, setAuth] = useState<Auth>({ state: 'loading' })

  useEffect(() => {
    let cancelled = false

    api
      .session()
      .then(({ user }) => {
        if (!cancelled) setAuth({ state: 'signed-in', user })
      })
      .catch(async (err: unknown) => {
        if (cancelled) return
        if (!(err instanceof ApiError) || !err.isUnauthenticated) {
          setAuth({
            state: 'broken',
            error: err instanceof Error ? err : new Error(String(err)),
          })
          return
        }

        // Not signed in. Whether that is fixable depends on the server:
        // with no GitHub OAuth App there is nothing to sign in with, and a
        // button that leads to a 501 helps nobody.
        try {
          const config = await api.authConfig()
          if (cancelled) return
          setAuth({
            state: 'signed-out',
            reason: config.sign_in_available
              ? undefined
              : [config.reason, config.hint].filter(Boolean).join('\n\n'),
          })
        } catch {
          if (!cancelled) setAuth({ state: 'signed-out' })
        }
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (auth.state === 'loading') {
    return (
      <div className="mx-auto max-w-6xl px-4 py-6">
        <PageLoading />
      </div>
    )
  }

  if (auth.state === 'broken') {
    return (
      <div className="mx-auto max-w-2xl px-4 py-16">
        <Failure error={auth.error} />
      </div>
    )
  }

  if (auth.state === 'signed-out') {
    return <Login reason={auth.reason} />
  }

  return (
    <Routes>
      <Route
        element={<Layout user={auth.user} onSignOut={() => setAuth({ state: 'signed-out' })} />}
      >
        <Route index element={<Overview />} />
        <Route path="runners" element={<Runners />} />
        <Route path="runners/:id" element={<RunnerDetail />} />
        <Route path="events" element={<Events />} />
        <Route path="settings" element={<Settings user={auth.user} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
