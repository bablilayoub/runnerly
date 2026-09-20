/**
 * Login is deliberately plain: one button, and an explanation when the
 * button cannot work yet.
 *
 * Sign-in is refused with 501 until an operator configures a GitHub OAuth
 * App, so the page shows that message rather than bouncing the visitor to a
 * dead redirect.
 */
export function Login({ reason }: { reason?: string }) {
  return (
    <div className="mx-auto flex min-h-full max-w-md flex-col justify-center px-4 py-16">
      <h1 className="font-mono text-lg font-semibold tracking-tight">runnerly</h1>
      <p className="mt-1 text-sm" style={{ color: 'var(--text-muted)' }}>
        Run GitHub Actions on your own infrastructure.
      </p>

      {reason ? (
        <div
          className="mt-6 rounded-md border px-4 py-3 text-sm"
          style={{ borderColor: 'var(--border-strong)' }}
        >
          <p className="font-medium">Sign-in is not available</p>
          <p className="mt-1 whitespace-pre-line" style={{ color: 'var(--text-muted)' }}>
            {reason}
          </p>
        </div>
      ) : (
        <a
          href="/api/v1/auth/github"
          className="mt-6 inline-block rounded-sm border px-4 py-2 text-center text-sm"
          style={{
            borderColor: 'var(--border-strong)',
            background: 'var(--bg-raised)',
            color: 'var(--text)',
          }}
        >
          Sign in with GitHub
        </a>
      )}

      <p className="mt-6 text-xs" style={{ color: 'var(--text-faint)' }}>
        Agents do not sign in. They enroll with a token and keep reporting whether or not anyone is
        watching this page.
      </p>
    </div>
  )
}
