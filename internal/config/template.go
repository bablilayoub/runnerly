package config

import (
	"fmt"
	"strings"
)

// templateText is what `runnerly config init` writes. It is a commented
// document rather than a marshaled struct, because an operator reading
// config.yaml should not have to open the documentation to know what a field
// does.
//
// It must decode to exactly Default(); TestTemplateMatchesDefault enforces
// that, so the comments cannot drift away from the behavior.
const templateText = `# Runnerly configuration
#
# Every field below is set to its default. Full reference:
# https://github.com/bablilayoub/runnerly/blob/main/docs/configuration.md
#
# Check your edits with: runnerly config validate

server:
  # Runnerly control plane. Optional: the CLI and agent work without one.
  # Must start with http:// or https:// when set.
  url: ""
  # Address runnerly-server binds. Loopback by default so a fresh install is
  # not exposed before TLS is in front of it.
  listen: "%s"
  # PostgreSQL connection string for runnerly-server. Prefer
  # RUNNERLY_DATABASE_URL, which keeps the password out of this file.
  database: ""
  # 32 bytes, hex or base64, encrypting user credentials at rest.
  # Prefer RUNNERLY_SECRET_KEY. Generate one with: runnerly server keygen
  secret_key: ""
  oauth:
    # A GitHub OAuth App belongs to whoever runs the server, so Runnerly
    # ships no credentials. Until these are set, dashboard sign-in is off.
    # Prefer RUNNERLY_OAUTH_CLIENT_SECRET for the secret.
    client_id: ""
    client_secret: ""
    # Restrict who may sign in. Empty allows any GitHub account that
    # completes the flow, which is only safe on a server that is not
    # reachable from the internet.
    allowed_logins: []
  heartbeat:
    # How often an agent reports, and when the server stops believing it.
    # interval must be shorter than stale_after.
    interval: "%s"
    stale_after: "%s"
    offline_after: "%s"
  # How long the event feed keeps history. The control plane is not a log
  # platform.
  event_retention_days: %d
  # How long an agent uses a machine token before the server replaces it.
  # Rotation bounds how long a leaked credential is worth anything.
  token_lifetime: "%s"
  tls:
    # Serve HTTPS directly. Leave both empty to terminate TLS in a reverse
    # proxy, which is the more common deployment.
    cert_file: ""
    key_file: ""
  rate_limit:
    # Per client address, per minute. 0 disables limiting.
    requests_per_minute: %d
    # Enrollment and sign-in, where guessing is worth an attacker's time.
    auth_requests_per_minute: %d
    # Read the client address from X-Forwarded-For. Only turn this on
    # behind a proxy you control: anyone can send that header, so trusting
    # it lets a client choose its own rate limit bucket.
    trust_forwarded_for: false
  metrics:
    # Serve Prometheus metrics at /metrics.
    enabled: %t
    # Require this as a bearer token to scrape. Empty leaves it open, which
    # is normal on a private network.
    token: ""

ephemeral:
  # One-job runners. See docs/ephemeral-runners.md.
  #
  # Where a destroyed runner's output is kept. Empty uses a logs directory
  # beside this file. It is outside the runner's own directory on purpose:
  # the point is that the logs outlive the runner.
  log_dir: ""
  # How many runs of logs to keep. 0 keeps every one of them.
  keep_runs: %d
  # The start of each generated runner name. Empty uses the hostname.
  name_prefix: ""

agent:
  # One-shot token used to enroll with the control plane. Prefer
  # RUNNERLY_ENROLLMENT_TOKEN. It is consumed once; the machine token that
  # replaces it is stored in credentials.yaml.
  enrollment_token: ""

github:
  # GitHub hostname. Use github.com unless you run GitHub Enterprise Server.
  host: github.com
  # "repository" or "organization".
  scope: repository
  # owner/repo, when scope is repository.
  repository: ""
  # Organization login, required when scope is organization.
  organization: ""

runner:
  # Runner name as it appears in GitHub. Letters, digits, '.', '_' and '-'.
  # Empty means Runnerly generates one.
  name: ""
  # GitHub runner labels. These map directly to GitHub's own labels.
  labels:
%s
  # Where the official GitHub runner is installed. Empty uses
  # /opt/runnerly/runners for root, or ~/.local/share/runnerly/runners.
  dir: ""

executor:
  # host   - the GitHub runner executes jobs directly on this machine
  # docker - the machine runs the runner with Docker available for
  #          containerized steps and job containers
  type: %s
  docker:
    # Overrides DOCKER_HOST for the runner, for a non-default socket or a
    # rootless daemon. Empty uses Docker's own default.
    host: ""
    # What to do with containers, volumes and networks a job leaves behind:
    # after_job or never. Only what appeared during the job is removed, so
    # other containers on a shared machine are left alone.
    cleanup: "%s"
    # Also remove dangling images after a job. Off by default: it reclaims
    # disk at the cost of re-pulling layers next time.
    prune_images: %t
    # Bounds each docker command the agent runs.
    timeout: "%s"

updates:
  # Whether Runnerly may upgrade itself and the GitHub runner unattended.
  auto: %t

security:
  # Read docs/security.md before changing these. A self-hosted runner executes
  # arbitrary workflow code; fork pull requests on a public repository are the
  # single most dangerous configuration.
  #
  # allow_public_repositories is enforced: runner create refuses a public
  # repository unless it is true or --allow-public is passed.
  # allow_fork_workflows is advisory. Runnerly cannot control which workflows
  # GitHub dispatches; enforce it in GitHub's own settings.
  allow_public_repositories: %t
  allow_fork_workflows: %t
`

// Template returns a commented configuration file containing the defaults for
// this machine.
func Template() string {
	cfg := Default()

	var labels strings.Builder
	for _, l := range cfg.Runner.Labels {
		fmt.Fprintf(&labels, "    - %s\n", l)
	}

	return fmt.Sprintf(templateText,
		cfg.Server.Listen,
		cfg.Server.Heartbeat.Interval,
		cfg.Server.Heartbeat.StaleAfter,
		cfg.Server.Heartbeat.OfflineAfter,
		cfg.Server.EventRetentionDays,
		cfg.Server.TokenLifetime,
		cfg.Server.RateLimit.Requests,
		cfg.Server.RateLimit.Auth,
		cfg.Server.Metrics.Enabled,
		cfg.Ephemeral.KeepRuns,
		strings.TrimRight(labels.String(), "\n"),
		cfg.Executor.Type,
		cfg.Executor.Docker.Cleanup,
		cfg.Executor.Docker.PruneImages,
		cfg.Executor.Docker.Timeout,
		cfg.Updates.Auto,
		cfg.Security.AllowPublicRepositories,
		cfg.Security.AllowForkWorkflows,
	)
}
