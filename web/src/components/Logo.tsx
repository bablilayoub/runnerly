import darkInk from '../../../assets/runnerly-logo-dark.png'
import lightInk from '../../../assets/runnerly-logo.png'

import { cn } from '@/lib/utils'

/**
 * The lockup, in whichever ink the current theme can see.
 *
 * The artwork is two flat PNGs — one white, one near-black — so it cannot
 * take its colour from the page. Both are rendered and one is hidden,
 * rather than swapped in JavaScript: a themed logo that arrives a frame
 * late is the flash the theme script in index.html exists to prevent.
 *
 * Only one is ever visible, so only one is described; the other is hidden
 * from assistive technology along with the eye.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <>
      <img src={darkInk} alt="Runnerly" className={cn('w-auto dark:hidden', className)} />
      <img
        src={lightInk}
        alt="Runnerly"
        aria-hidden
        className={cn('hidden w-auto dark:block', className)}
      />
    </>
  )
}
