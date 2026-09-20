import { useEffect, useRef, useState, type ReactNode } from "react"
import { motion, useInView, useReducedMotion, type Variants } from "motion/react"

import { cn } from "@/lib/utils"

/**
 * Motion primitives for the page.
 *
 * Two rules hold everywhere here:
 *
 *  1. Reduced motion means no movement. The content still appears, it just
 *     arrives instead of sliding.
 *  2. An animation can only ever add to the page. Nothing is allowed to
 *     leave content permanently invisible, because a fade that never fires
 *     is worse than no fade at all — the section is simply gone, and it is
 *     gone silently, with the text sitting in the DOM.
 */

const EASE = [0.16, 1, 0.3, 1] as const

/**
 * SAFETY_MS is the backstop for rule 2. If a block has not been revealed by
 * then it is shown anyway, whatever the observer did or did not do. It is
 * long enough that a normal scroll reveal wins the race and the fallback is
 * never seen.
 */
const SAFETY_MS = 2500

/** Reveal fades and lifts its children in when they scroll into view. */
export function Reveal({
  children,
  delay = 0,
  y = 16,
  className,
}: {
  children: ReactNode
  delay?: number
  y?: number
  className?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const inView = useInView(ref, { once: true, margin: "-80px" })
  const still = useReducedMotion()
  const [forced, setForced] = useState(false)

  useEffect(() => {
    if (inView || forced) return
    const t = window.setTimeout(() => setForced(true), SAFETY_MS)
    return () => window.clearTimeout(t)
  }, [inView, forced])

  const shown = inView || forced || still

  return (
    <motion.div
      ref={ref}
      className={className}
      initial={{ opacity: 0, y: still ? 0 : y }}
      animate={shown ? { opacity: 1, y: 0 } : undefined}
      transition={{ duration: still ? 0 : 0.6, delay: still ? 0 : delay, ease: EASE }}
    >
      {children}
    </motion.div>
  )
}

/**
 * Stagger animates its children in sequence, for the hero, where the order
 * things appear in is the order you read them.
 *
 * It needs no backstop: it animates on mount rather than on scroll, so there
 * is no observer to miss.
 */
export function Stagger({
  children,
  className,
  delay = 0,
  gap = 0.08,
}: {
  children: ReactNode
  className?: string
  delay?: number
  gap?: number
}) {
  const still = useReducedMotion()

  const variants: Variants = {
    hidden: {},
    shown: {
      transition: { staggerChildren: still ? 0 : gap, delayChildren: still ? 0 : delay },
    },
  }

  return (
    <motion.div className={className} variants={variants} initial="hidden" animate="shown">
      {children}
    </motion.div>
  )
}

/** Step is one child of a Stagger. */
export function Step({
  children,
  className,
  y = 14,
}: {
  children: ReactNode
  className?: string
  y?: number
}) {
  const still = useReducedMotion()

  const variants: Variants = {
    hidden: { opacity: 0, y: still ? 0 : y },
    shown: {
      opacity: 1,
      y: 0,
      transition: { duration: still ? 0 : 0.65, ease: EASE },
    },
  }

  return (
    <motion.div className={className} variants={variants}>
      {children}
    </motion.div>
  )
}

/**
 * Lift raises a card slightly on hover. Subtle on purpose: the page has no
 * hue to signal with, so movement and border brightness do that work.
 */
export function Lift({ children, className }: { children: ReactNode; className?: string }) {
  const still = useReducedMotion()

  return (
    <motion.div
      className={cn("h-full", className)}
      whileHover={still ? undefined : { y: -3 }}
      transition={{ duration: 0.2, ease: EASE }}
    >
      {children}
    </motion.div>
  )
}
