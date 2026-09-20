import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import type { User } from '../types'
import { api } from '../api'

const NAV = [
  { to: '/', label: 'Overview', end: true },
  { to: '/runners', label: 'Runners', end: false },
  { to: '/events', label: 'Events', end: false },
  { to: '/settings', label: 'Settings', end: false },
]

export function Layout({ user, onSignOut }: { user: User; onSignOut: () => void }) {
  const navigate = useNavigate()

  async function signOut() {
    try {
      await api.logout()
    } finally {
      // Whether or not the server acknowledged it, this browser is done.
      onSignOut()
      navigate('/')
    }
  }

  return (
    <div className="min-h-full">
      <header className="border-b" style={{ borderColor: 'var(--border)' }}>
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <span className="font-mono text-sm font-semibold tracking-tight">runnerly</span>

          <nav className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
            {NAV.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className="py-1"
                style={({ isActive }) => ({
                  color: isActive ? 'var(--text)' : 'var(--text-muted)',
                  borderBottom: isActive ? '1px solid var(--text)' : '1px solid transparent',
                })}
              >
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="ml-auto flex items-center gap-3 text-sm">
            <span style={{ color: 'var(--text-muted)' }}>{user.login}</span>
            <button
              onClick={() => void signOut()}
              className="cursor-pointer"
              style={{ color: 'var(--text-muted)' }}
            >
              Sign out
            </button>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
