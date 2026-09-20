import {
  Activity,
  ArrowUpRight,
  Boxes,
  Container,
  GaugeCircle,
  ShieldCheck,
  Sparkles,
  Terminal as TerminalIcon,
} from "lucide-react"

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
import { Bad, CopyLine, Ok, Out, Prompt, Terminal } from "@/components/terminal"
import { Eyebrow, Heading, Lede, Section } from "@/components/section"

const REPO = "https://github.com/bablilayoub/runnerly"
const INSTALL = "curl -fsSL https://runnerly.dev/install.sh | sh"

export default function App() {
  return (
    <div className="min-h-dvh bg-background text-foreground antialiased">
      <Grid />
      <Nav />
      <Hero />
      <TheDance />
      <Capabilities />
      <Fleet />
      <NotBuilt />
      <Docs />
      <Footer />
    </div>
  )
}

/** Grid is the faint backdrop. Luminance only: the palette has no hue. */
function Grid() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      <div
        className="absolute inset-0 opacity-[0.045]"
        style={{
          backgroundImage:
            "linear-gradient(to right, white 1px, transparent 1px), linear-gradient(to bottom, white 1px, transparent 1px)",
          backgroundSize: "72px 72px",
        }}
      />
      <div className="absolute left-1/2 top-[-18rem] size-[46rem] -translate-x-1/2 rounded-full bg-white/[0.05] blur-[140px]" />
    </div>
  )
}

function Nav() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/60 bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex h-14 w-full max-w-6xl items-center justify-between px-6">
        <a href="#top" className="flex items-center gap-2.5">
          <span className="grid size-6 place-items-center rounded border border-border">
            <TerminalIcon className="size-3.5" />
          </span>
          <span className="font-medium tracking-tight">Runnerly</span>
        </a>
        <nav className="flex items-center gap-1">
          <Button variant="ghost" size="sm" asChild>
            <a href="#docs">Docs</a>
          </Button>
          <Button variant="ghost" size="sm" asChild>
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

function Hero() {
  return (
    <div id="top" className="relative">
      <div className="mx-auto w-full max-w-6xl px-6 pb-20 pt-20 sm:pb-28 sm:pt-28">
        <Badge
          variant="outline"
          className="mb-6 h-auto max-w-full gap-1.5 whitespace-normal py-1 text-left font-mono text-xs font-normal"
        >
          <Sparkles className="size-3" />
          one command from bare machine to running jobs
        </Badge>

        <h1 className="max-w-3xl text-balance text-4xl font-medium leading-[1.05] tracking-tight sm:text-6xl">
          Run GitHub Actions on your own infrastructure.
        </h1>

        <p className="mt-6 max-w-2xl text-pretty text-lg leading-relaxed text-muted-foreground">
          Runnerly installs, registers, supervises and retires self-hosted runners. GitHub keeps
          scheduling your workflows — Runnerly manages the machines that execute them.
        </p>

        <div className="mt-9 max-w-xl">
          <CopyLine command={INSTALL} />
          <p className="mt-2.5 pl-1 font-mono text-xs text-muted-foreground/80">
            then: runnerly setup
          </p>
        </div>

        <div className="mt-8 flex flex-wrap items-center gap-3">
          <Button asChild>
            <a href="#docs">
              Read the docs
              <ArrowUpRight className="size-4" />
            </a>
          </Button>
          <Button variant="outline" asChild>
            <a href={REPO} target="_blank" rel="noreferrer">
              <GithubMark className="size-4" />
              Source
            </a>
          </Button>
        </div>

        <div className="mt-16">
          <Terminal title="runnerly setup">
            <Prompt>runnerly setup</Prompt>
            {"\n"}
            <Out dim>Setting up Runnerly</Out>
            {"\n"}
            <Ok>the machine is ready, with 1 warning</Ok>
            <Ok>signed in to github.com as octocat</Ok>
            <Ok>runner will join acme/widgets</Ok>
            {"\n"}
            <Out dim>What this will do</Out>
            {"\n"}
            <Out> write /home/me/.config/runnerly/config.yaml</Out>
            <Out> install GitHub's runner into ~/.local/share/runnerly/runners/build-01</Out>
            <Out> register build-01 with acme/widgets</Out>
            <Out> labels self-hosted,linux,x64,runnerly</Out>
            {"\n"}
            <Out dim> Nothing is started. The last step prints how to do that.</Out>
            {"\n"}
            <Prompt>
              Go ahead? [y/N]: <span className="text-muted-foreground">y</span>
            </Prompt>
            <Ok>build-01 is registered with acme/widgets</Ok>
          </Terminal>
        </div>
      </div>
    </div>
  )
}

function TheDance() {
  return (
    <Section id="setup">
      <div className="grid min-w-0 gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] lg:items-center lg:gap-16">
        <div>
          <Eyebrow>Setup</Eyebrow>
          <Heading>Five commands, or one.</Heading>
          <Lede>
            Standing up a self-hosted runner by hand means downloading a tarball, finding a
            registration token, running <Mono>config.sh</Mono>, writing a systemd unit, and then
            noticing three weeks later that it went offline.
          </Lede>
          <Lede className="mt-4">
            <Mono>runnerly setup</Mono> does all of it, and shows you the whole plan before it
            changes anything. Every step it takes is also a command of its own, so nothing is a
            black box.
          </Lede>
        </div>

        <Tabs defaultValue="after" className="min-w-0">
          <TabsList className="font-mono text-xs">
            <TabsTrigger value="before">By hand</TabsTrigger>
            <TabsTrigger value="after">With Runnerly</TabsTrigger>
          </TabsList>

          <TabsContent value="before" className="mt-4">
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

          <TabsContent value="after" className="mt-4">
            <Terminal title="the short way">
              <Prompt>curl -fsSL https://runnerly.dev/install.sh | sh</Prompt>
              <Prompt>runnerly setup</Prompt>
              {"\n"}
              <Out dim># it checks the machine, signs in, registers, and prints</Out>
              <Out dim># the unit that keeps the runner alive across reboots</Out>
              {"\n"}
              <Ok>build-01 is registered with acme/widgets</Ok>
              {"\n"}
              <Out dim># and when it does fall over</Out>
              <Out>the agent restarts it: 5s, 10s, 20s, 40s, 80s</Out>
            </Terminal>
          </TabsContent>
        </Tabs>
      </div>
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
    body: "The agent restarts a failed runner on a 5s, 10s, 20s, 40s, 80s backoff, then stops rather than hiding one that cannot start. systemd restarts the agent — two layers, each covering the other's failure.",
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
      <Eyebrow>What it does</Eyebrow>
      <Heading>The unglamorous parts, done properly.</Heading>
      <Lede>
        Runnerly is not a replacement for GitHub Actions and does not reimplement the runner. It
        wraps GitHub's official one and manages its lifecycle.
      </Lede>

      <div className="mt-12 grid gap-px overflow-hidden rounded-xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
        {CAPABILITIES.map(({ icon: Icon, title, body }) => (
          <div key={title} className="bg-background p-6 transition-colors hover:bg-card/60">
            <Icon className="size-5 text-muted-foreground" />
            <h3 className="mt-4 font-medium tracking-tight">{title}</h3>
            <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{body}</p>
          </div>
        ))}
      </div>
    </Section>
  )
}

function Fleet() {
  return (
    <Section id="fleet">
      <div className="grid min-w-0 gap-12 lg:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)] lg:items-center lg:gap-16">
        <Terminal title="runnerly.example.com">
          <Out dim>Runners</Out>
          <Out> 3 total 1 online 1 busy 1 offline</Out>
          {"\n"}
          <Out>build-01 ● online acme/widgets linux/x64 28s ago</Out>
          <Out>build-02 ● busy acme/widgets linux/x64 28s ago</Out>
          <Out dim>build-arm-01 ○ offline acme/gadgets linux/arm64 never</Out>
        </Terminal>

        <div>
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

          <div className="mt-8 space-y-2">
            <CopyLine command="runnerly server enrollment-token create --max-uses 1" />
            <p className="pl-1 text-sm text-muted-foreground">
              The agent trades that one-shot token for a credential of its own, which the server
              rotates daily.
            </p>
          </div>
        </div>
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
      <div className="grid gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)] lg:gap-16">
        <div>
          <Eyebrow>Not built</Eyebrow>
          <Heading>What it does not do.</Heading>
          <Lede>
            Listed here rather than left for you to discover after you have deployed it.
          </Lede>
        </div>

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
      </div>
    </Section>
  )
}

const DOCS = [
  ["Getting started", "docs/getting-started.md", "bare machine to running jobs"],
  ["Installation", "docs/installation.md", "platforms, building, uninstalling"],
  ["Runners", "docs/runners.md", "creating, listing, removing"],
  ["The agent", "docs/agent.md", "supervision, restarts, systemd"],
  ["Docker executor", "docs/docker.md", "job cleanup and its boundaries"],
  ["Ephemeral runners", "docs/ephemeral-runners.md", "the one-job lifecycle"],
  ["Control plane", "docs/server.md", "the server and its API"],
  ["Production", "docs/operations.md", "TLS, metrics, backups, upgrades"],
  ["Security model", "docs/security.md", "read this before you deploy"],
  ["Architecture", "docs/architecture.md", "how it fits together, and why"],
] as const

function Docs() {
  return (
    <Section id="docs">
      <Eyebrow>Documentation</Eyebrow>
      <Heading>Written to be read before something breaks.</Heading>

      <div className="mt-12 grid gap-px overflow-hidden rounded-xl border border-border bg-border sm:grid-cols-2">
        {DOCS.map(([title, path, blurb]) => (
          <a
            key={path}
            href={`${REPO}/blob/main/${path}`}
            target="_blank"
            rel="noreferrer"
            className="group flex items-center justify-between gap-4 bg-background p-5 transition-colors hover:bg-card/60"
          >
            <span>
              <span className="block font-medium tracking-tight">{title}</span>
              <span className="mt-0.5 block text-sm text-muted-foreground">{blurb}</span>
            </span>
            <ArrowUpRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:-translate-y-0.5 group-hover:translate-x-0.5" />
          </a>
        ))}
      </div>
    </Section>
  )
}

function Footer() {
  return (
    <footer className="border-t border-border/60 py-12">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="flex items-center gap-2.5">
            <span className="grid size-5 place-items-center rounded border border-border">
              <TerminalIcon className="size-3" />
            </span>
            <span className="text-sm font-medium tracking-tight">Runnerly</span>
          </div>
          <p className="mt-2 text-sm text-muted-foreground">
            MIT licensed. Self-hosted runners execute your workflow code — read the security model
            first.
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" asChild>
            <a href={`${REPO}/blob/main/docs/security.md`} target="_blank" rel="noreferrer">
              Security
            </a>
          </Button>
          <Button variant="ghost" size="sm" asChild>
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
