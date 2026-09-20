import { createContext, useCallback, useContext, useEffect, useRef, type ReactNode } from "react"

import { cn } from "@/lib/utils"

/**
 * Spotlight cards, from ui-layouts, rewritten.
 *
 * What was kept is the idea, which is a good one: a single gradient in
 * viewport space, drawn behind a hairline border on every card at once, so
 * the whole grid lights up coherently around the pointer instead of each
 * card reacting on its own.
 *
 * Three things were changed, all of them because the original could not
 * ship here as-is:
 *
 *   - It attached a window mousemove listener *per card* and called
 *     setState from each one. Six cards meant six React renders on every
 *     mouse move. The pointer position is now one listener on the group,
 *     written to CSS custom properties through a ref, so moving the mouse
 *     re-renders nothing at all.
 *   - Its SpotlightCard variant was hardcoded to slate and indigo. This
 *     page has no hue, so that variant is gone and the gradients here are
 *     white at low alpha.
 *   - It opened with @ts-nocheck and typed several parameters as `any`.
 *
 * Original: https://ui-layouts.com/components/spotlight-cards
 */

type SpotlightContext = {
  /** Whether the pointer should light cards it is merely near. */
  proximity: boolean
}

const Ctx = createContext<SpotlightContext>({ proximity: true })

export function Spotlight({
  children,
  className,
  proximity = true,
}: {
  children: ReactNode
  className?: string
  proximity?: boolean
}) {
  const group = useRef<HTMLDivElement>(null)
  const frame = useRef(0)

  const track = useCallback((event: PointerEvent) => {
    // Coalesced into an animation frame: pointermove fires far more often
    // than the screen refreshes, and only the last position of each frame
    // can be seen.
    if (frame.current) return
    frame.current = requestAnimationFrame(() => {
      frame.current = 0
      const el = group.current
      if (!el) return
      el.style.setProperty("--spot-x", `${event.clientX}px`)
      el.style.setProperty("--spot-y", `${event.clientY}px`)
    })
  }, [])

  useEffect(() => {
    // A coarse pointer has no hover, so there is nothing to follow and no
    // reason to listen. Same for anyone who asked for less motion.
    const fine = window.matchMedia("(hover: hover) and (pointer: fine)")
    const still = window.matchMedia("(prefers-reduced-motion: reduce)")
    if (!fine.matches || still.matches) return

    window.addEventListener("pointermove", track, { passive: true })
    return () => {
      window.removeEventListener("pointermove", track)
      if (frame.current) cancelAnimationFrame(frame.current)
    }
  }, [track])

  return (
    <Ctx.Provider value={{ proximity }}>
      <div
        ref={group}
        className={cn("group relative", className)}
        // Off-screen until the pointer arrives, so nothing is lit on load.
        style={{ "--spot-x": "-100vw", "--spot-y": "-100vh" } as React.CSSProperties}
      >
        {children}
      </div>
    </Ctx.Provider>
  )
}

/**
 * One card. The hairline border is a 1px padding box with the gradient
 * behind it; the child covers the middle, so only the edge glows.
 */
export function SpotlightItem({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  const { proximity } = useContext(Ctx)

  return (
    <div className={cn("relative overflow-hidden rounded-xl bg-border p-px", className)}>
      {proximity ? (
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 rounded-xl"
          style={{
            background:
              "radial-gradient(400px circle at var(--spot-x) var(--spot-y), rgb(255 255 255 / 0.85), rgb(255 255 255 / 0.12) 45%, transparent 70%)",
            backgroundAttachment: "fixed",
          }}
        />
      ) : null}
      <div className="relative h-full overflow-hidden rounded-[calc(0.75rem-1px)] bg-background">
        {/* A second, far fainter pass inside the card. The border alone
            reads as a rim light; this is what makes the card itself feel
            lit rather than merely outlined. */}
        {proximity ? (
          <div
            aria-hidden
            className="pointer-events-none absolute inset-0"
            style={{
              background:
                "radial-gradient(400px circle at var(--spot-x) var(--spot-y), rgb(255 255 255 / 0.07), transparent 65%)",
              backgroundAttachment: "fixed",
            }}
          />
        ) : null}
        <div className="relative h-full">{children}</div>
      </div>
    </div>
  )
}
