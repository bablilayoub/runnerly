import { Logo } from '@/components/Logo'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

/**
 * lucide dropped brand marks, and the GitHub one is the single place on
 * this page where a logo means something: it says which account you are
 * about to sign in with. Inlining one path is cheaper than a second icon
 * library.
 */
function GitHubMark() {
  return (
    <svg viewBox="0 0 16 16" fill="currentColor" aria-hidden className="size-4">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  )
}

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
      <h1>
        <Logo className="h-7" />
      </h1>
      <p className="mt-3 text-sm text-muted-foreground">
        Run GitHub Actions on your own infrastructure.
      </p>

      {reason ? (
        <Alert className="mt-6">
          <AlertTitle>Sign-in is not available</AlertTitle>
          <AlertDescription className="whitespace-pre-line">{reason}</AlertDescription>
        </Alert>
      ) : (
        <Button asChild size="lg" className="mt-6">
          <a href="/api/v1/auth/github">
            <GitHubMark />
            Sign in with GitHub
          </a>
        </Button>
      )}

      <p className="mt-6 text-xs text-muted-foreground">
        Agents do not sign in. They enroll with a token and keep reporting whether or not anyone is
        watching this page.
      </p>
    </div>
  )
}
