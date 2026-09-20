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
  # Runnerly control plane. Optional: the CLI works without one.
  # Must start with http:// or https:// when set.
  url: ""

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

executor:
  # host   - the GitHub runner executes jobs directly on this machine
  # docker - the machine runs the runner with Docker available for
  #          containerized steps and job containers
  type: %s

updates:
  # Whether Runnerly may upgrade itself and the GitHub runner unattended.
  auto: %t

security:
  # Read docs/security.md before changing these. A self-hosted runner executes
  # arbitrary workflow code; fork pull requests on a public repository are the
  # single most dangerous configuration.
  #
  # These are advisory today: Runnerly surfaces the policy, it does not yet
  # enforce it against GitHub.
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
		strings.TrimRight(labels.String(), "\n"),
		cfg.Executor.Type,
		cfg.Updates.Auto,
		cfg.Security.AllowPublicRepositories,
		cfg.Security.AllowForkWorkflows,
	)
}
