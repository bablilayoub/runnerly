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
import { Bad, CopyLine, Out, Prompt, Terminal } from "@/components/terminal"
import { Eyebrow, Heading, Lede, Section } from "@/components/section"

const REPO = "https://github.com/bablilayoub/runnerly"
const INSTALL = "curl -fsSL https://runnerly.dev/install.sh | sh"

export default function App() {
  return (
    <div className="min-h-dvh bg-background text-foreground antialiased">
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

/** Backdrop is the grid and the glow. Luminance only: the palette has no hue. */
function Backdrop() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      <div
        className="absolute inset-0 opacity-[0.04]"
        style={{
          backgroundImage:
            "linear-gradient(to right, white 1px, transparent 1px), linear-gradient(to bottom, white 1px, transparent 1px)",
          backgroundSize: "64px 64px",
          maskImage: "radial-gradient(ellipse 90% 55% at 50% 0%, black 30%, transparent 75%)",
        }}
      />
      <div className="absolute left-1/2 top-[-22rem] size-[52rem] -translate-x-1/2 rounded-full bg-white/[0.06] blur-[150px]" />
    </div>
  )
}

function Nav() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/50 bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex h-14 w-full max-w-6xl items-center justify-between px-6">
        <a href="#top" className="flex items-center gap-2.5">
          <span className="grid size-6 place-items-center rounded border border-border">
            <TerminalIcon className="size-3.5" />
          </span>
          <span className="font-medium tracking-tight">Runnerly</span>
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
  { kind: "out", text: "  install   GitHub's runner into ~/.local/share/runnerly/runners/build-01" },
  { kind: "out", text: "  register  build-01 with acme/widgets" },
  { kind: "out", text: "  labels    self-hosted,linux,x64,runnerly" },
  { kind: "gap" },
  { kind: "out", text: "  Nothing is started. The last step prints how to do that.", dim: true },
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
            <h1 className="mx-auto max-w-4xl text-balance text-[2.6rem] font-medium leading-[1.03] tracking-[-0.02em] sm:text-6xl lg:text-7xl">
              Run GitHub Actions
              <br />
              <span className="text-muted-foreground/70">on your own machines.</span>
            </h1>
          </Step>

          <Step className="mt-6">
            <p className="mx-auto max-w-2xl text-pretty text-base leading-relaxed text-muted-foreground sm:text-lg">
              Runnerly installs, registers, supervises and retires self-hosted runners. GitHub
              keeps scheduling your workflows — Runnerly manages the machines that execute them.
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
const STATS = [
  { figure: "3", label: "dependencies", note: "cobra, yaml.v3 and pgx. Nothing else." },
  { figure: "424", label: "tests", note: "run against real Postgres and Docker in CI" },
  { figure: "0", label: "lines of the runner reimplemented", note: "it wraps GitHub's own" },
  { figure: "15", label: "pages of documentation", note: "including what is not built" },
] as const

function Stats() {
  return (
    <Section className="border-t-0 py-0 sm:py-0">
      <Reveal>
        <div className="grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-4">
          {STATS.map(({ figure, label, note }) => (
            <div key={label} className="bg-background p-6">
              <div className="font-mono text-4xl font-medium tracking-tight tabular-nums">
                {figure}
              </div>
              <div className="mt-3 text-sm font-medium">{label}</div>
              <div className="mt-1 text-sm text-muted-foreground">{note}</div>
            </div>
          ))}
        </div>
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
              <Prompt>echo "&lt;checksum&gt;  actions-runner.tar.gz" | shasum -a 256 -c</Prompt>
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

      <div className="mt-14 grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
        {CAPABILITIES.map(({ icon: Icon, title, body }, i) => (
          <Reveal key={title} delay={(i % 3) * 0.07} className="bg-background">
            <Lift className="h-full bg-background p-6 transition-colors hover:bg-card/70">
              <Icon className="size-5 text-muted-foreground" />
              <h3 className="mt-4 font-medium tracking-tight">{title}</h3>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{body}</p>
            </Lift>
          </Reveal>
        ))}
      </div>
    </Section>
  )
}

const FLEET_SESSION: Line[] = [
  { kind: "out", text: "Runners", dim: true },
  { kind: "out", text: "  3 total   1 online   1 busy   1 offline" },
  { kind: "gap" },
  { kind: "out", text: "build-01      ● online   acme/widgets   linux/x64    28s ago" },
  { kind: "out", text: "build-02      ● busy     acme/widgets   linux/x64    28s ago" },
  { kind: "out", text: "build-arm-01  ○ offline  acme/gadgets   linux/arm64  never", dim: true },
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
            healthy, with a dashboard served from the same binary. A single machine never needs
            one.
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

      <div className="mt-14 grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2">
        {DOCS.map(([title, slug, blurb], i) => (
          <Reveal key={slug} delay={(i % 2) * 0.05} className="bg-background">
            <Link
              to={`/docs/${slug}`}
              className="group flex h-full items-center justify-between gap-4 bg-background p-5 transition-colors hover:bg-card/70"
            >
              <span>
                <span className="block font-medium tracking-tight">{title}</span>
                <span className="mt-0.5 block text-sm text-muted-foreground">{blurb}</span>
              </span>
              <ArrowRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
            </Link>
          </Reveal>
        ))}
      </div>

      <Reveal delay={0.1} className="mt-14">
        <div className="rounded-2xl border border-border bg-card/40 p-8 text-center sm:p-12">
          <h3 className="text-balance text-2xl font-medium tracking-tight sm:text-3xl">
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

function Footer() {
  return (
    <footer className="border-t border-border/50 py-12">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            <span className="grid size-5 place-items-center rounded border border-border">
              <TerminalIcon className="size-3" />
            </span>
            <span className="text-sm font-medium tracking-tight">Runnerly</span>
          </div>
          <p className="mt-2 max-w-md text-sm text-muted-foreground">
            MIT licensed. Self-hosted runners execute your workflow code — read the security model
            first.
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" asChild className="rounded-full">
            <Link to="/docs/security">Security</Link>
          </Button>
          <Button variant="ghost" size="sm" asChild className="rounded-full">
            <a href={REPO} target="_blank" rel="noreferrer">
              <GithubMark className="size-4" />
              GitHub
            </a>
          </Button>
        </div>
      </div>
    </footer>
  )
}

function Mono({ children }: { children: React.ReactNode }) {
  return <code className="font-mono text-[0.9em] text-foreground">{children}</code>
}
