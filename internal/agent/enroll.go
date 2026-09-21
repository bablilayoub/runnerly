package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/controlplane"
	"github.com/bablilayoub/runnerly/internal/machine"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/version"
)

// EnrollOptions describes how to reach a control plane.
type EnrollOptions struct {
	// ServerURL is the control plane. Empty means the agent reports nowhere.
	ServerURL string
	// EnrollmentToken is the one-shot token, needed only the first time.
	EnrollmentToken string
	// CredentialsPath is where the machine token is kept.
	CredentialsPath string
	// Runner is the runner being supervised.
	Runner state.Runner
	Logger *slog.Logger
	// HTTPClient is injectable for tests.
	HTTPClient controlplane.Option
}

// Enrollment is the result of connecting to a control plane.
type Enrollment struct {
	Client   *controlplane.Client
	RunnerID string
	// Interval is the heartbeat interval the server asked for.
	Interval time.Duration
	// Enrolled is true when this call performed the enrollment, rather than
	// reusing a credential the machine already had.
	Enrolled bool
}

// ErrNeedEnrollmentToken means the machine has no credential and was given
// nothing to get one with.
var ErrNeedEnrollmentToken = errors.New("this machine is not enrolled with the control plane")

// EnrollmentTokenEnvVar keeps a one-shot token out of the configuration file.
const EnrollmentTokenEnvVar = "RUNNERLY_ENROLLMENT_TOKEN"

// ServerURLEnvVar overrides the configured control plane.
const ServerURLEnvVar = "RUNNERLY_SERVER_URL"

// PersistMachineToken returns a function that stores a rotated credential
// where enrollment put the first one.
//
// Every entry point uses it, so none of them can forget and quietly leave a
// machine re-enrolling on each start.
func PersistMachineToken(credentialsPath, serverURL, runnerID string) func(string) error {
	return func(token string) error {
		return auth.StoreMachine(credentialsPath, serverURL, auth.Machine{
			RunnerID: runnerID,
			Token:    token,
		})
	}
}

// EnrollmentTokenFrom prefers the environment over the configuration, the
// same way the GitHub token does, so a provisioning script can inject one
// without editing a file.
func EnrollmentTokenFrom(configured string) string {
	if token := strings.TrimSpace(os.Getenv(EnrollmentTokenEnvVar)); token != "" {
		return token
	}
	return strings.TrimSpace(configured)
}

// ServerURLFrom prefers the environment over the configuration.
func ServerURLFrom(configured string) string {
	if url := strings.TrimSpace(os.Getenv(ServerURLEnvVar)); url != "" {
		return url
	}
	return strings.TrimSpace(configured)
}

// Enroll connects to the control plane, enrolling the machine if it has no
// credential yet.
//
// Enrollment is a one-time exchange: a token an operator issues is traded for
// a credential belonging to this machine. The enrollment token is never
// stored, and the machine token replaces it.
func Enroll(ctx context.Context, opts EnrollOptions) (*Enrollment, error) {
	if opts.ServerURL == "" {
		return nil, nil
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(nopWriter{}, nil))
	}

	clientOpts := []controlplane.Option{}
	if opts.HTTPClient != nil {
		clientOpts = append(clientOpts, opts.HTTPClient)
	}

	existing, found, err := auth.ResolveMachine(opts.CredentialsPath, opts.ServerURL, nil)
	if err != nil {
		return nil, err
	}
	if found {
		return &Enrollment{
			Client:   controlplane.New(opts.ServerURL, existing.Token, clientOpts...),
			RunnerID: existing.RunnerID,
		}, nil
	}

	if opts.EnrollmentToken == "" {
		return nil, fmt.Errorf("%w at %s.\n"+
			"Create a token on the server with `runnerly server enrollment-token create`, then set\n"+
			"agent.enrollment_token or RUNNERLY_ENROLLMENT_TOKEN on this machine",
			ErrNeedEnrollmentToken, opts.ServerURL)
	}

	client := controlplane.New(opts.ServerURL, "", clientOpts...)
	registered, err := client.Register(ctx, opts.EnrollmentToken, registrationFor(opts.Runner))
	if err != nil {
		return nil, fmt.Errorf("enroll with %s: %w", opts.ServerURL, err)
	}

	if err := auth.StoreMachine(opts.CredentialsPath, opts.ServerURL, auth.Machine{
		RunnerID: registered.Runner.ID,
		Token:    registered.MachineToken,
	}); err != nil {
		// The machine is enrolled but cannot remember it, so the next start
		// would burn another enrollment token. That is worth failing on.
		return nil, fmt.Errorf("enrolled with %s but could not store the machine token: %w",
			opts.ServerURL, err)
	}

	opts.Logger.Info("enrolled with the control plane",
		"component", Component,
		"event", "agent_enrolled",
		"server", opts.ServerURL,
		"runner_id", registered.Runner.ID,
	)

	return &Enrollment{
		Client:   controlplane.New(opts.ServerURL, registered.MachineToken, clientOpts...),
		RunnerID: registered.Runner.ID,
		Interval: registered.Config.Interval(),
		Enrolled: true,
	}, nil
}

// registrationFor describes this machine to the control plane.
//
// CPU count comes from the runtime; memory and disk need per-platform calls
// and come from internal/machine. Anything that cannot be measured on this
// platform is reported as zero, which every surface renders as a dash:
// unknown rather than a number Runnerly did not actually take.
func registrationFor(r state.Runner) controlplane.RegisterRequest {
	scope := string(r.Scope.Kind)
	if scope == "" {
		scope = "repository"
	}
	return controlplane.RegisterRequest{
		Name:          r.Name,
		GitHubHost:    defaultString(r.Host, "github.com"),
		GitHubScope:   scope,
		GitHubScopeID: r.Scope.String(),
		OS:            runtime.GOOS,
		Architecture:  runtime.GOARCH,
		CPUCount:      runtime.NumCPU(),
		MemoryBytes:   machine.MemoryTotal(),
		DiskBytes:     machine.DiskTotal(r.Dir),
		Labels:        r.Labels,
		Ephemeral:     r.Ephemeral,
		AgentVersion:  version.Get().Short(),
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// nopWriter discards log output for a nil logger.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
