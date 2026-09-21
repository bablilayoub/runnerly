import {
  Activity,
  ArrowRight,
  Boxes,
  Container,
  GaugeCircle,
  ShieldCheck,
  Terminal as TerminalIcon,
} from "lucide-react"
import { Link } from "react-router-dom"

import logo from "../../assets/runnerly-logo.png"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { GithubMark } from "@/components/github-mark"
import { Lift, Reveal, Stagger, Step } from "@/components/motion"
import { Session, type Line } from "@/components/session"
import { Spotlight, SpotlightItem } from "@/components/ui/spotlight"
import { Bad, CopyLine, Out, Prompt, Terminal } from "@/components/terminal"
import { Eyebrow, Heading, Lede, Section } from "@/components/section"

const REPO = "https://github.com/bablilayoub/runnerly"
const INSTALL = "curl -fsSL https://runnerly.dev/install.sh | sh"

export default function App() {
  return (
    // No bg-background here. The backdrop is a fixed layer at z-index -10,
    // and a wrapper with its own opaque background paints straight over it
    // — which is exactly what was happening: every layer of the backdrop
    // was being drawn and then covered. The body already sets the page
    // background, so this element does not need to.
    <div className="min-h-dvh text-foreground antialiased">
      <Backdrop />
      <Nav />
      <Hero />
      <Stats />
      <TheDance />
      <Capabilities />
      <Fleet />
      <NotBuilt />
      <Docs />
      <Footer />
    </div>
  )
}

/**
 * The backdrop behind the hero.
 *
 * Five layers, all luminance, because the palette has no hue to reach for:
 * beams, a dot field, a glow, a horizon, and grain over everything.
 *
 * Nothing here runs per frame in JavaScript. The movement is CSS keyframes
 * on transform and opacity, which stay on the compositor, and all of it
 * stops for anyone who asked for reduced motion.
 */
function Backdrop() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      <Beams />

      {/* A dot field rather than a line grid: at this opacity lines read as
          a surface and dots read as depth. Masked so it is gone before it
          reaches the text. */}
      <div
        className="absolute inset-0 opacity-[0.3]"
        style={{
          backgroundImage:
            "radial-gradient(circle at center, rgb(255 255 255 / 0.22) 1px, transparent 1px)",
          backgroundSize: "26px 26px",
          maskImage: "radial-gradient(ellipse 85% 58% at 50% 0%, black 15%, transparent 72%)",
          WebkitMaskImage: "radial-gradient(ellipse 85% 58% at 50% 0%, black 15%, transparent 72%)",
        }}
      />

      <div className="absolute left-1/2 top-[-26rem] size-[56rem] -translate-x-1/2 rounded-full bg-white/[0.06] blur-[170px] motion-safe:animate-[drift-a_28s_ease-in-out_infinite]" />

      {/* The horizon: one hairline, brightest where the glow sits. */}
      <div
        className="absolute inset-x-0 top-[38rem] h-px"
        style={{
          background:
            "linear-gradient(to right, transparent, rgb(255 255 255 / 0.10) 35%, rgb(255 255 255 / 0.16) 50%, rgb(255 255 255 / 0.10) 65%, transparent)",
        }}
      />

      <Grain />
    </div>
  )
}

/**
 * Beams: soft columns of light leaning across the top of the page, each
 * drifting and breathing on its own period so the group never repeats
 * visibly.
 *
 * They are the layer doing most of the work. A glow alone is a blur, and
 * every dark landing page has one; light with a direction reads as a room
 * rather than a gradient.
 */
function Beams() {
  const beams = [
    { left: "6%", width: "18rem", tilt: -14, delay: "0s", duration: "19s", alpha: 0.16 },
    { left: "27%", width: "12rem", tilt: -9, delay: "-6s", duration: "25s", alpha: 0.11 },
    { left: "52%", width: "22rem", tilt: -17, delay: "-11s", duration: "22s", alpha: 0.19 },
    { left: "76%", width: "14rem", tilt: -7, delay: "-3s", duration: "28s", alpha: 0.12 },
  ]

  return (
    <div
      className="absolute inset-x-0 top-[-14rem] h-[64rem]"
      style={{
        maskImage: "linear-gradient(to bottom, black 8%, transparent 78%)",
        WebkitMaskImage: "linear-gradient(to bottom, black 8%, transparent 78%)",
      }}
    >
      {beams.map((b) => (
        <div
          key={b.left}
          className="absolute top-0 h-full origin-top blur-2xl motion-safe:animate-[beam_var(--dur)_ease-in-out_var(--delay)_infinite]"
          style={
            {
              left: b.left,
              width: b.width,
              // The animation sets transform wholesale, so the tilt has to
              // reach the keyframes as a variable. Setting it here as well
              // would be overwritten on the first frame.
              transform: `rotate(${b.tilt}deg)`,
              background: `linear-gradient(to bottom, rgb(255 255 255 / ${b.alpha}), transparent 72%)`,
              "--tilt": `${b.tilt}deg`,
              "--dur": b.duration,
              "--delay": b.delay,
            } as React.CSSProperties
          }
        />
      ))}
    </div>
  )
}

/**
 * Grain, as an inline turbulence filter.
 *
 * Large flat dark areas band badly on ordinary displays, and a little noise
 * is what stops it. Inline rather than an image so there is no request for
 * it and nothing to keep in sync.
 */
function Grain() {
  return (
    <svg className="absolute inset-0 size-full opacity-[0.035] mix-blend-overlay" aria-hidden>
      <filter id="grain">
        <feTurbulence
          type="fractalNoise"
          baseFrequency="0.85"
          numOctaves="3"
          stitchTiles="stitch"
        />
        <feColorMatrix type="saturate" values="0" />
      </filter>
      <rect width="100%" height="100%" filter="url(#grain)" />
    </svg>
  )
}

function Nav() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/50 bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex h-14 w-full max-w-6xl items-center justify-between px-6">
        <a href="#top" className="flex items-center">
          <img src={logo} alt="Runnerly" className="h-[22px] w-auto" />
        </a>
        <nav className="flex items-center gap-1">
          <Button variant="ghost" size="sm" asChild className="rounded-full">
            <Link to="/docs">Docs</Link>
          </Button>
          <Button size="sm" asChild className="rounded-full">
            <a href={REPO} target="_blank" rel="noreferrer">
              <GithubMark className="size-4" />
              GitHub
            </a>
          </Button>
        </nav>
      </div>
    </header>
  )
}

/** The hero session is the output of `runnerly setup`, line for line. */
const SETUP_SESSION: Line[] = [
  { kind: "cmd", text: "curl -fsSL https://runnerly.dev/install.sh | sh" },
  { kind: "ok", text: "Runnerly v0.1.0 is installed" },
  { kind: "gap" },
  { kind: "cmd", text: "runnerly setup" },
  { kind: "gap" },
  { kind: "out", text: "Setting up Runnerly", dim: true },
  { kind: "gap" },
  { kind: "ok", text: "the machine is ready, with 1 warning" },
  { kind: "ok", text: "signed in to github.com as octocat" },
  { kind: "ok", text: "runner will join acme/widgets" },
  { kind: "gap" },
  { kind: "out", text: "What this will do", dim: true },
  { kind: "gap" },
  { kind: "out", text: "  write     ~/.config/runnerly/config.yaml" },
  {
    kind: "out",
    text: "  install   GitHub's runner into ~/.local/share/runnerly/runners/build-01",
  },
  { kind: "out", text: "  register  build-01 with acme/widgets" },
  { kind: "out", text: "  labels    self-hosted,linux,x64,runnerly" },
  { kind: "gap" },
  {
    kind: "out",
    text: "  Nothing is started. The last step prints how to do that.",
    dim: true,
  },
  { kind: "gap" },
  { kind: "cmd", text: "y" },
  { kind: "ok", text: "build-01 is registered with acme/widgets" },
]

function Hero() {
  return (
    <div id="top" className="relative">
      <div className="mx-auto w-full max-w-6xl px-6 pb-16 pt-20 sm:pt-28">
        <Stagger className="flex flex-col items-center text-center">
          <Step>
            <a
              href={`${REPO}/releases`}
              target="_blank"
              rel="noreferrer"
              className="group inline-flex items-center gap-2 rounded-full border border-border bg-card/60 py-1 pl-1 pr-3 backdrop-blur transition-colors hover:border-foreground/25"
            >
              <Badge className="rounded-full px-2 py-0 font-mono text-[11px] font-normal">
                New
              </Badge>
              <span className="text-sm text-muted-foreground transition-colors group-hover:text-foreground">
                runnerly setup — one command, start to finish
              </span>
              <ArrowRight className="size-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
            </a>
          </Step>

          <Step className="mt-7">
            <h1 className="mx-auto max-w-4xl text-balance font-display text-[2.7rem] font-semibold leading-[1.02] tracking-[-0.035em] sm:text-6xl lg:text-[4.6rem]">
              Run GitHub Actions
              <br />
              <span className="text-muted-foreground/70">on your own machines.</span>
            </h1>
          </Step>

          <Step className="mt-6">
            <p className="mx-auto max-w-2xl text-pretty text-base leading-relaxed text-muted-foreground sm:text-lg">
              Runnerly installs, registers, supervises and retires self-hosted runners. GitHub keeps
              scheduling your workflows — Runnerly manages the machines that execute them.
            </p>
          </Step>

          <Step className="mt-9">
            <div className="flex flex-wrap items-center justify-center gap-3">
              <Button size="lg" asChild className="rounded-full">
                <a href="#setup">
                  Install Runnerly
                  <ArrowRight className="size-4" />
                </a>
              </Button>
              <Button size="lg" variant="secondary" asChild className="rounded-full">
                <Link to="/docs">Read the docs</Link>
              </Button>
            </div>
          </Step>

          <Step className="mt-7 w-full max-w-xl">
            <CopyLine command={INSTALL} className="rounded-full py-2.5" />
          </Step>
        </Stagger>

        <Reveal delay={0.15} className="mt-16">
          <Session title="build-01 — zsh" lines={SETUP_SESSION} minLines={22} />
        </Reveal>
      </div>
    </div>
  )
}

/**
 * Numbers that are true and checkable, not impressions. Each one is
 * something a reader could verify from the repository in a minute.
 */
// Recount before changing any of these. The test figure is
//   grep -rh "^func Test" --include '*_test.go' internal | wc -l
const STATS = [
  {
    figure: "3",
    label: "dependencies",
    note: "cobra, yaml.v3 and pgx. Nothing else.",
  },
  {
    figure: "434",
    label: "tests",
    note: "run against real Postgres and Docker in CI",
  },
  {
    figure: "0",
    label: "lines of the runner reimplemented",
    note: "it wraps GitHub's own",
  },
  {
    figure: "15",
    label: "pages of documentation",
    note: "including what is not built",
  },
] as const

function Stats() {
  return (
    // No top border or padding, so the band reads as part of the hero.
    // It does need room underneath: the next section draws a full-width
    // hairline, and with no gap that line lands exactly on the bottom edge
    // of the cards and looks like it is slicing through them.
    <Section className="border-t-0 pb-20 pt-0 sm:pb-28 sm:pt-0">
      <Reveal>
        <Spotlight className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {STATS.map(({ figure, label, note }) => (
            <SpotlightItem key={label} className="h-full">
              <div className="h-full p-6">
                <div className="font-display text-5xl font-semibold tracking-[-0.03em] tabular-nums">
                  {figure}
                </div>
                <div className="mt-3 text-sm font-medium">{label}</div>
                <div className="mt-1 text-sm leading-relaxed text-muted-foreground">{note}</div>
              </div>
            </SpotlightItem>
          ))}
        </Spotlight>
      </Reveal>
    </Section>
  )
}

function TheDance() {
  return (
    <Section id="setup">
      <Reveal className="mx-auto max-w-2xl text-center">
        <Eyebrow>Setup</Eyebrow>
        <Heading>Five commands, or one.</Heading>
        <Lede className="mx-auto">
          Standing up a runner by hand means a tarball, a registration token, <Mono>config.sh</Mono>
          , a systemd unit, and noticing three weeks later that it went offline.
        </Lede>
      </Reveal>

      <Reveal delay={0.1} className="mx-auto mt-12 max-w-4xl">
        <Tabs defaultValue="after" className="min-w-0">
          <TabsList className="mx-auto font-mono text-xs">
            <TabsTrigger value="before">By hand</TabsTrigger>
            <TabsTrigger value="after">With Runnerly</TabsTrigger>
          </TabsList>

          <TabsContent value="before" className="mt-6">
            <Terminal title="the long way">
              <Out dim># find the release, the checksum, the token…</Out>
              <Prompt>mkdir -p ~/actions-runner &amp;&amp; cd ~/actions-runner</Prompt>
              <Prompt>curl -O -L https://github.com/actions/runner/releases/…</Prompt>
              <Prompt>echo "&lt;checksum&gt; actions-runner.tar.gz" | shasum -a 256 -c</Prompt>
              <Prompt>tar xzf ./actions-runner.tar.gz</Prompt>
              <Prompt>./config.sh --url https://github.com/acme/widgets --token …</Prompt>
              <Prompt>sudo ./svc.sh install &amp;&amp; sudo ./svc.sh start</Prompt>
              {"\n"}
              <Out dim># three weeks later</Out>
              <Bad>the runner is offline and nothing told you</Bad>
            </Terminal>
          </TabsContent>

          <TabsContent value="after" className="mt-6">
            <Terminal title="the short way">
              <Prompt>curl -fsSL https://runnerly.dev/install.sh | sh</Prompt>
              <Prompt>runnerly setup</Prompt>
              {"\n"}
              <Out dim># checks the machine, signs in, registers, and prints the</Out>
              <Out dim># unit that keeps the runner alive across reboots</Out>
              {"\n"}
              <Out>and when it does fall over, the agent restarts it:</Out>
              <Out>5s, 10s, 20s, 40s, 80s — then stops, rather than hiding it</Out>
            </Terminal>
          </TabsContent>
        </Tabs>
      </Reveal>

      <Reveal delay={0.15} className="mx-auto mt-8 max-w-xl text-center">
        <p className="text-sm text-muted-foreground">
          Every step <Mono>setup</Mono> takes is also a command of its own — <Mono>doctor</Mono>,{" "}
          <Mono>login</Mono>, <Mono>runner create</Mono>, <Mono>agent systemd</Mono> — so nothing
          here is a black box.
        </p>
      </Reveal>
    </Section>
  )
}

const CAPABILITIES = [
  {
    icon: GaugeCircle,
    title: "Tells you why, not just no",
    body: "doctor checks everything that would stop a runner working — Docker, disk, network, credentials, scopes. Every failure names the command that fixes it, and it exits non-zero so it gates a provisioning script.",
  },
  {
    icon: Activity,
    title: "Keeps the runner running",
    body: "The agent restarts a failed runner on a 5s, 10s, 20s, 40s, 80s backoff, then stops rather than hiding one that cannot start — and a ceiling of ten restarts an hour catches the failures slow enough that each one looks healthy. systemd restarts the agent: two layers, each covering the other's failure.",
  },
  {
    icon: Container,
    title: "Cleans up only what the job made",
    body: "The Docker executor records what existed when a job started and removes what appeared. Not docker system prune, which would take a container belonging to something else on the machine.",
  },
  {
    icon: Boxes,
    title: "One job per runner, if you want",
    body: "ephemeral run registers, takes one job, cleans up, deregisters and destroys. Logs are kept outside the runner directory, so they outlive it. Pair it with Restart=always for a fresh runner every time.",
  },
  {
    icon: ShieldCheck,
    title: "Refuses the dangerous default",
    body: "It will not register against a public repository unless you say so explicitly, verifies every download against GitHub's published checksum, and rejects archive entries that would escape the install directory.",
  },
  {
    icon: TerminalIcon,
    title: "Never takes root quietly",
    body: "systemd subcommands print the unit and the commands to install it instead of running sudo for you. If something needs privileges, it says why and stops.",
  },
] as const

function Capabilities() {
  return (
    <Section id="capabilities">
      <Reveal className="mx-auto max-w-2xl text-center">
        <Eyebrow>What it does</Eyebrow>
        <Heading>The unglamorous parts, done properly.</Heading>
        <Lede className="mx-auto">
          Runnerly does not reimplement the Actions runner. It wraps GitHub's official one and
          manages its lifecycle.
        </Lede>
      </Reveal>

      <Spotlight className="mt-14 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {CAPABILITIES.map(({ icon: Icon, title, body }, i) => (
          <Reveal key={title} delay={(i % 3) * 0.07} className="h-full">
            <SpotlightItem className="h-full">
              <Lift className="h-full p-6">
                <Icon className="size-5 text-muted-foreground" />
                <h3 className="mt-4 font-display text-[1.05rem] font-semibold tracking-[-0.01em]">
                  {title}
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{body}</p>
              </Lift>
            </SpotlightItem>
          </Reveal>
        ))}
      </Spotlight>
    </Section>
  )
}

const FLEET_SESSION: Line[] = [
  { kind: "out", text: "Runners", dim: true },
  { kind: "out", text: "  3 total   1 online   1 busy   1 offline" },
  { kind: "gap" },
  {
    kind: "out",
    text: "build-01      ● online   acme/widgets   linux/x64    28s ago",
  },
  {
    kind: "out",
    text: "build-02      ● busy     acme/widgets   linux/x64    28s ago",
  },
  {
    kind: "out",
    text: "build-arm-01  ○ offline  acme/gadgets   linux/arm64  never",
    dim: true,
  },
]

function Fleet() {
  return (
    <Section id="fleet">
      <div className="grid min-w-0 gap-12 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] lg:items-center lg:gap-16">
        <Reveal>
          <Session title="runnerly.example.com" lines={FLEET_SESSION} minLines={6} />
        </Reveal>

        <Reveal delay={0.1}>
          <Eyebrow>More than one machine</Eyebrow>
          <Heading>An optional control plane.</Heading>
          <Lede>
            Once several machines run runners, a small server answers what exists and whether it is
            healthy, with a dashboard served from the same binary. A single machine never needs one.
          </Lede>
          <Lede className="mt-4">
            <Mono>busy</Mono> is what the runner itself reported through GitHub's job hooks — not a
            guess from the process being alive.
          </Lede>

          <div className="mt-8 space-y-2.5">
            <CopyLine command="runnerly server enrollment-token create --max-uses 1" />
            <p className="pl-1 text-sm text-muted-foreground">
              The agent trades that one-shot token for a credential of its own, which the server
              rotates daily.
            </p>
          </div>
        </Reveal>
      </div>
    </Section>
  )
}

const NOT_BUILT = [
  {
    title: "Autoscaling and cloud provisioning",
    body: "Nothing decides how many machines to have, or creates them. Ephemeral runners work on machines you already have.",
  },
  {
    title: "VM-per-job isolation",
    body: "The honest answer when a job needs a real boundary. Docker gives you a clean environment between jobs, not isolation from the host: anything that can reach the Docker socket has root on it.",
  },
  {
    title: "Unattended upgrades",
    body: "runnerly upgrade reports what is out of date and applies it on request. Doing that on a schedule needs a maintenance window to be safe, and an upgrade restarts a runner — which throws away the job it was running.",
  },
  {
    title: "A self-replacing binary",
    body: "upgrade tells you when Runnerly itself is behind and stops there. A binary that rewrites itself badly is worse than one that does not try.",
  },
  {
    title: "Windows runners",
    body: "The job hooks are shell scripts, so cleanup and busy reporting would not work. macOS is different: runners do run there, but the generated service unit is systemd, which macOS does not use.",
  },
  {
    title: "Multi-tenancy",
    body: "Everyone who can sign in to the dashboard sees every runner. The OAuth allow list is the only access control.",
  },
] as const

function NotBuilt() {
  return (
    <Section id="not-built">
      <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)] lg:gap-16">
        <Reveal>
          <Eyebrow>Not built</Eyebrow>
          <Heading>What it does not do.</Heading>
          <Lede>Listed here rather than left for you to find after you have deployed it.</Lede>
        </Reveal>

        <Reveal delay={0.1}>
          <Accordion type="single" collapsible className="w-full">
            {NOT_BUILT.map(({ title, body }) => (
              <AccordionItem key={title} value={title}>
                <AccordionTrigger className="text-left text-base">{title}</AccordionTrigger>
                <AccordionContent className="text-sm leading-relaxed text-muted-foreground">
                  {body}
                </AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>
        </Reveal>
      </div>
    </Section>
  )
}

const DOCS = [
  ["Getting started", "getting-started", "bare machine to running jobs"],
  ["Installation", "installation", "platforms, building, uninstalling"],
  ["Runners", "runners", "creating, listing, removing"],
  ["The agent", "agent", "supervision, restarts, systemd"],
  ["Docker executor", "docker", "job cleanup and its boundaries"],
  ["Ephemeral runners", "ephemeral-runners", "the one-job lifecycle"],
  ["Control plane", "server", "the server and its API"],
  ["Production", "operations", "TLS, metrics, backups, upgrades"],
  ["Security model", "security", "read this before you deploy"],
  ["Architecture", "architecture", "how it fits together, and why"],
] as const

function Docs() {
  return (
    <Section id="docs">
      <Reveal className="mx-auto max-w-2xl text-center">
        <Eyebrow>Documentation</Eyebrow>
        <Heading>Written to be read before something breaks.</Heading>
        <Lede className="mx-auto">
          Fifteen pages, including an honest account of what Runnerly does not do.
        </Lede>
      </Reveal>

      <Spotlight className="mt-14 grid gap-3 sm:grid-cols-2">
        {DOCS.map(([title, slug, blurb], i) => (
          <Reveal key={slug} delay={(i % 2) * 0.05} className="h-full">
            <SpotlightItem className="h-full">
              <Link
                to={`/docs/${slug}`}
                className="group flex h-full items-center justify-between gap-4 rounded-[calc(0.75rem-1px)] p-5 transition-colors hover:bg-card/70"
              >
                <span>
                  <span className="block font-medium tracking-tight">{title}</span>
                  <span className="mt-0.5 block text-sm text-muted-foreground">{blurb}</span>
                </span>
                <ArrowRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </Link>
            </SpotlightItem>
          </Reveal>
        ))}
      </Spotlight>

      <Reveal delay={0.1} className="mt-14">
        <div className="rounded-2xl border border-border bg-card/40 p-8 text-center sm:p-12">
          <h3 className="text-balance font-display text-2xl font-semibold tracking-[-0.02em] sm:text-3xl">
            Two commands and the machine is running jobs.
          </h3>
          <div className="mx-auto mt-7 max-w-xl">
            <CopyLine command={INSTALL} className="rounded-full py-2.5" />
          </div>
          <p className="mt-4 font-mono text-xs text-muted-foreground">then: runnerly setup</p>
        </div>
      </Reveal>
    </Section>
  )
}

/**
 * The footer's link columns.
 *
 * Every entry goes somewhere that exists. A column of plausible-looking
 * links to pages nobody has written is the easiest thing in the world to
 * add and the most annoying thing to click, so there is no Pricing, no
 * Changelog and no About here.
 */
const FOOTER_LINKS: {
  heading: string
  links: { label: string; to: string; external?: boolean }[]
}[] = [
  {
    heading: "Product",
    links: [
      { label: "What it does", to: "#capabilities" },
      { label: "Ephemeral runners", to: "/docs/ephemeral-runners" },
      { label: "Docker executor", to: "/docs/docker" },
      { label: "Control plane", to: "/docs/server" },
    ],
  },
  {
    heading: "Developers",
    links: [
      { label: "Documentation", to: "/docs" },
      { label: "Installation", to: "/docs/installation" },
      { label: "Configuration", to: "/docs/configuration" },
      { label: "Troubleshooting", to: "/docs/troubleshooting" },
    ],
  },
  {
    heading: "Project",
    links: [
      { label: "Architecture", to: "/docs/architecture" },
      { label: "Security model", to: "/docs/security" },
      {
        label: "Contributing",
        to: `${REPO}/blob/main/CONTRIBUTING.md`,
        external: true,
      },
      { label: "License", to: `${REPO}/blob/main/LICENSE`, external: true },
    ],
  },
]

function Footer() {
  return (
    <footer className="px-6 pb-6">
      <div className="relative mx-auto w-full max-w-6xl overflow-hidden rounded-3xl border border-border bg-card/30">
        <div className="relative z-10 px-6 pt-10 sm:px-10 sm:pt-12">
          <div className="grid gap-10 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1.4fr)]">
            <div>
              <img src={logo} alt="Runnerly" className="h-6 w-auto" />

              <p className="mt-4 max-w-xs text-sm leading-relaxed text-muted-foreground">
                Self-hosted GitHub Actions runners you install, supervise and retire. Free to run,
                MIT.
              </p>

              <div className="mt-6 flex flex-wrap items-center gap-2.5">
                <Button size="sm" asChild className="rounded-full">
                  <Link to="/docs/installation">Install guide</Link>
                </Button>
                <Button size="sm" variant="secondary" asChild className="rounded-full">
                  <a href={REPO} target="_blank" rel="noreferrer">
                    <GithubMark className="size-4" />
                    GitHub
                  </a>
                </Button>
              </div>
            </div>

            <nav className="grid grid-cols-2 gap-8 sm:grid-cols-3">
              {FOOTER_LINKS.map(({ heading, links }) => (
                <div key={heading}>
                  <h3 className="text-sm font-medium tracking-tight">{heading}</h3>
                  <ul className="mt-4 space-y-3">
                    {links.map(({ label, to, external }) => (
                      <li key={label}>
                        {external ? (
                          <a
                            href={to}
                            target="_blank"
                            rel="noreferrer"
                            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                          >
                            {label}
                          </a>
                        ) : to.startsWith("#") ? (
                          <a
                            href={to}
                            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                          >
                            {label}
                          </a>
                        ) : (
                          <Link
                            to={to}
                            className="text-sm text-muted-foreground transition-colors hover:text-foreground"
                          >
                            {label}
                          </Link>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </nav>
          </div>
        </div>

        <Wordmark />

        <div className="relative z-10 mx-6 border-t border-border/70 py-5 sm:mx-10">
          <div className="flex flex-col gap-2 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
            <p>© {new Date().getFullYear()} Runnerly · MIT</p>
            <p>
              Built by{" "}
              <a
                href="https://abablil.me"
                target="_blank"
                rel="noreferrer"
                className="font-medium text-foreground transition-opacity hover:opacity-70"
              >
                Ayoub Bablil
              </a>
            </p>
          </div>
        </div>
      </div>
    </footer>
  )
}

/**
 * The logo, across the bottom of the footer.
 *
 * The whole lockup rather than the word on its own, so the giant one and
 * the small one in the header are the same drawing at two sizes.
 *
 * It is not cropped. Cropping looked deliberate while it sat tight against
 * the divider, but with the room this band has it read as a mistake — all
 * the air above it and none below. Whole logo, centred, equal space either
 * side.
 */
function Wordmark() {
  return (
    <div
      aria-hidden
      className="pointer-events-none relative z-0 select-none px-6 py-10 sm:px-10 sm:py-12"
    >
      <img src={logo} alt="" className="block w-full opacity-[0.055]" />
    </div>
  )
}

function Mono({ children }: { children: React.ReactNode }) {
  return <code className="font-mono text-[0.9em] text-foreground">{children}</code>
}
