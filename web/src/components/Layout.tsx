import { LogOutIcon } from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { Logo } from '@/components/Logo'
import { ThemeToggle } from '@/components/ThemeToggle'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { api } from '@/api'
import { cn } from '@/lib/utils'
import type { User } from '@/types'

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
      {/* Sticky, because the nav is how you leave a page that is taller
          than the window — and the events feed always is. */}
      <header className="sticky top-0 z-40 border-b border-border bg-background/80 backdrop-blur">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-2.5">
          <NavLink to="/" aria-label="Runnerly" className="shrink-0">
            <Logo className="h-[18px]" />
          </NavLink>

          <nav className="flex flex-wrap gap-x-1 text-sm">
            {NAV.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cn(
                    'rounded-md px-2.5 py-1 transition-colors',
                    isActive
                      ? 'bg-muted text-foreground'
                      : 'text-muted-foreground hover:text-foreground',
                  )
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="ml-auto flex items-center gap-1">
            <ThemeToggle />

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  className="gap-2 pl-1"
                  aria-label={`Signed in as ${user.login}`}
                >
                  <Avatar className="size-5">
                    {user.avatar_url && <AvatarImage src={user.avatar_url} alt="" />}
                    <AvatarFallback className="text-[10px]">
                      {user.login.slice(0, 2).toUpperCase()}
                    </AvatarFallback>
                  </Avatar>
                  <span className="hidden sm:inline">{user.login}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel className="font-normal">
                  <span className="block text-xs text-muted-foreground">Signed in as</span>
                  <span className="font-mono">{user.login}</span>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => void signOut()}>
                  <LogOutIcon />
                  Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
