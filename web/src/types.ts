// Types mirroring the control plane's JSON. They are hand-written rather than
// generated: the API is small, and a generator would be another build step to
// keep working for no benefit at this size.

export type RunnerStatus =
  'offline' | 'starting' | 'online' | 'busy' | 'stopping' | 'error' | 'retired'
export type Health = 'healthy' | 'stale' | 'offline'
export type Severity = 'debug' | 'info' | 'warn' | 'error'
export type CommandStatus = 'pending' | 'delivered' | 'done' | 'failed'

export interface Runner {
  id: string
  name: string
  github_host: string
  github_scope: 'repository' | 'organization'
  github_scope_id: string
  github_runner_id?: number
  status: RunnerStatus
  status_detail?: string
  health: Health
  last_heartbeat_age_seconds: number | null
  os: string
  architecture: string
  cpu_count: number
  memory_bytes: number
  disk_bytes: number
  labels: string[]
  ephemeral: boolean
  runner_version: string
  agent_version: string
  cpu_percent: number
  memory_percent: number
  disk_percent: number
  last_heartbeat?: string
  /** Set when the runner finished for good, which is how an ephemeral run ends. */
  retired_at?: string
  retired_reason?: string
  created_at: string
  updated_at: string
}

export interface Counts {
  /** Runners still in service. Retired ones are counted separately. */
  total: number
  online: number
  busy: number
  offline: number
  stale: number
  error: number
  retired: number
}

export interface RunnerEvent {
  id: number
  runner_id?: string
  event: string
  severity: Severity
  message?: string
  data?: Record<string, unknown>
  created_at: string
}

export interface Command {
  id: string
  runner_id: string
  command: string
  status: CommandStatus
  error?: string
  requested_by?: string
  created_at: string
  delivered_at?: string
  completed_at?: string
}

export interface Overview {
  runners: Counts
  recent_events: RunnerEvent[]
  thresholds: {
    stale_after_seconds: number
    offline_after_seconds: number
  }
}

export interface User {
  id: string
  github_id: number
  login: string
  name?: string
  avatar_url?: string
  created_at: string
  last_login_at: string
}

export interface EnrollmentToken {
  id: string
  description?: string
  max_uses?: number
  uses: number
  expires_at?: string
  revoked_at?: string
  created_at: string
}
