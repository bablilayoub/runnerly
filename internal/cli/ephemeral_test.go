package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/state"
)

// withAgent replaces the supervisor, so no process is started, and records
// what the agent was asked to run.
func withAgent(seen *[]agent.Options, outcome agent.Outcome, err error) option {
	return func(e *env) {
		e.runAgent = func(_ context.Context, opts agent.Options) (agent.Outcome, error) {
			*seen = append(*seen, opts)
			// The runner's output goes through here, so a test can check it
			// reaches the log file.
			if opts.Output != nil {
				_, _ = opts.Output.Write([]byte("a line from the runner\n"))
			}
			return outcome, err
		}
	}
}

func completedJob() agent.Outcome {
	started := time.Now().Add(-90 * time.Second)
	return agent.Outcome{
		Completed: true,
		Job: jobstate.State{
			Status:     jobstate.StatusIdle,
			Repository: "acme/widgets",
			Workflow:   "test",
			StartedAt:  started,
			FinishedAt: started.Add(90 * time.Second),
		},
	}
}

// ephemeralHandler answers everything the lifecycle asks GitHub for.
//
// deregistered records whether GitHub was asked to remove the runner, and
// stillRegistered decides whether the lookup afterwards finds one — which is
// how a runner that never took a job differs from one that did.
func ephemeralHandler(t *testing.T, stillRegistered *bool, deregistered *bool, registeredName func() string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete:
			*deregistered = true
			w.WriteHeader(http.StatusNoContent)

		case strings.HasSuffix(r.URL.Path, "/registration-token"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"AREGTOKEN","expires_at":"2099-01-01T00:00:00Z"}`))

		case strings.HasSuffix(r.URL.Path, "/downloads"):
			_, _ = w.Write([]byte(`[{"os":"linux","architecture":"x64",` +
				`"filename":"actions-runner-linux-x64.tar.gz",` +
				`"download_url":"http://127.0.0.1:1/unused.tar.gz","sha256_checksum":"deadbeef"}]`))

		case strings.HasSuffix(r.URL.Path, "/actions/runners"):
			// The lookup filters by name on the client, so a runner that is
			// still registered has to come back under the name this run
			// actually used.
			name := ""
			if registeredName != nil {
				name = registeredName()
			}
			if *stillRegistered && name != "" {
				_, _ = w.Write([]byte(`{"total_count":1,"runners":[{"id":7,"name":"` + name + `"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"total_count":0,"runners":[]}`))
			}

		case r.URL.Path == "/repos/acme/widgets":
			_, _ = w.Write([]byte(`{"full_name":"acme/widgets","name":"widgets","private":true,` +
				`"owner":{"login":"acme"},"permissions":{"admin":true}}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// ephemeralConfig writes a config and a pre-unpacked runner directory.
func ephemeralConfig(t *testing.T, extra string) (configPath, runnerDir string) {
	t.Helper()
	runnerDir = preUnpacked(t)
	configPath = configIn(t, "github:\n  repository: acme/widgets\nexecutor:\n  type: host\n"+extra)
	return configPath, runnerDir
}

func TestEphemeralRunCompletesTheWholeLifecycle(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "")
	registered, deregistered := false, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, nil)),
		withRunnerEnv(&commands),
		withAgent(&runs, completedJob(), nil),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"ephemeral", "run", "--name-prefix", "eph", "--dir", dir)
	if err != nil {
		t.Fatalf("ephemeral run: %v", err)
	}

	if len(runs) != 1 {
		t.Fatalf("the agent ran %d times, want once", len(runs))
	}
	if !runs[0].Runner.Ephemeral {
		t.Error("the runner was not registered as ephemeral")
	}
	if !strings.HasPrefix(runs[0].Runner.Name, "eph-") {
		t.Errorf("name = %q, want the prefix", runs[0].Runner.Name)
	}
	if !strings.Contains(commands[0], "--ephemeral") {
		t.Errorf("config.sh was not told the runner is ephemeral:\n%s", commands[0])
	}

	// Ran a job, so the output says what it was.
	for _, want := range []string{"acme/widgets / test", "ran"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Destroyed: identity gone, binaries kept so the next run is not a
	// fresh download.
	for _, gone := range []string{".runner", "_work"} {
		if _, statErr := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(statErr) {
			t.Errorf("%s survived teardown", gone)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, "config.sh")); statErr != nil {
		t.Error("the unpacked runner was deleted; the next run would re-download it")
	}

	// Forgotten locally.
	if _, getErr := state.Get(state.Path(cfg), runs[0].Runner.Name); getErr == nil {
		t.Error("the runner is still recorded on this machine")
	}
}

func TestEphemeralDeregistersARunnerThatNeverTookAJob(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "")
	// GitHub still has it, which is what happens when the runner stopped
	// before doing any work: nothing auto-removed it.
	registered, deregistered := true, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, func() string {
			return nameFromConfigSh(commands)
		})),
		withRunnerEnv(&commands),
		withAgent(&runs, agent.Outcome{}, nil),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"ephemeral", "run", "--name-prefix", "eph", "--dir", dir)
	if err != nil {
		t.Fatalf("ephemeral run: %v", err)
	}

	if !deregistered {
		t.Error("a runner that never took a job was left registered with GitHub")
	}
	if !strings.Contains(out, "without running a job") {
		t.Errorf("the output should say no job ran:\n%s", out)
	}
}

func TestEphemeralDoesNotDeregisterWhatGitHubAlreadyRemoved(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "")
	// GitHub removes an ephemeral runner itself once it has run a job.
	registered, deregistered := false, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, nil)),
		withRunnerEnv(&commands),
		withAgent(&runs, completedJob(), nil),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"ephemeral", "run", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if deregistered {
		t.Error("Runnerly tried to delete a registration GitHub had already removed")
	}
	if !strings.Contains(out, "already removed") {
		t.Errorf("output should say so:\n%s", out)
	}
}

func TestEphemeralNamesAreUniquePerRun(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "")
	registered, deregistered := false, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, nil)),
		withRunnerEnv(&commands),
		withAgent(&runs, completedJob(), nil),
	}

	for range 3 {
		if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
			"ephemeral", "run", "--name-prefix", "eph", "--dir", dir); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	for _, r := range runs {
		if seen[r.Runner.Name] {
			t.Fatalf("the name %q was reused; it would collide with a registration "+
				"GitHub has not finished removing", r.Runner.Name)
		}
		seen[r.Runner.Name] = true
	}
}

func TestEphemeralKeepsTheRunnersOutput(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "")
	registered, deregistered := false, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, nil)),
		withRunnerEnv(&commands),
		withAgent(&runs, completedJob(), nil),
	}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"ephemeral", "run", "--dir", dir); err != nil {
		t.Fatal(err)
	}

	// The log must be outside the runner directory: that directory is taken
	// apart, and a log inside it would go with it.
	logDir := filepath.Join(filepath.Dir(cfg), "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("no log directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d logs, want 1", len(entries))
	}
	if strings.HasPrefix(logDir, dir) {
		t.Error("logs are kept inside the runner directory, which is destroyed")
	}

	body, err := os.ReadFile(filepath.Join(logDir, entries[0].Name())) //nolint:gosec // a path this test made
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "a line from the runner") {
		t.Errorf("the runner's output was not kept:\n%s", body)
	}
}

func TestEphemeralPrunesOldLogs(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "ephemeral:\n  keep_runs: 2\n")
	registered, deregistered := false, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, nil)),
		withRunnerEnv(&commands),
		withAgent(&runs, completedJob(), nil),
	}

	logDir := filepath.Join(filepath.Dir(cfg), "logs")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// Older logs, named so they sort before anything this run writes.
	for _, name := range []string{"20200101-000000-old-a.log", "20200102-000000-old-b.log"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"ephemeral", "run", "--dir", dir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d logs, want keep_runs = 2", len(entries))
	}
	// The oldest went first.
	for _, entry := range entries {
		if entry.Name() == "20200101-000000-old-a.log" {
			t.Error("the oldest log was kept and a newer one removed")
		}
	}
}

func TestEphemeralPurgeRemovesEverything(t *testing.T) {
	cfg, dir := ephemeralConfig(t, "")
	registered, deregistered := false, false

	var runs []agent.Options
	var commands []string
	opts := []option{
		withGitHub(t, ephemeralHandler(t, &registered, &deregistered, nil)),
		withRunnerEnv(&commands),
		withAgent(&runs, completedJob(), nil),
	}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"ephemeral", "run", "--dir", dir, "--purge"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("--purge left the runner directory behind")
	}
}

func TestEphemeralRefusesAPublicRepositoryLikeRunnerCreate(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)

	public := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/acme/widgets" {
			_, _ = w.Write([]byte(`{"full_name":"acme/widgets","name":"widgets","private":false,` +
				`"owner":{"login":"acme"},"permissions":{"admin":true}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}

	var runs []agent.Options
	var commands []string
	opts := []option{withGitHub(t, public), withRunnerEnv(&commands), withAgent(&runs, completedJob(), nil)}

	_, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "ephemeral", "run", "--dir", dir)
	if err == nil {
		t.Fatal("ephemeral run registered against a public repository")
	}
	if !strings.Contains(err.Error(), "--allow-public") {
		t.Errorf("error = %v", err)
	}
	if len(runs) != 0 {
		t.Error("the agent ran despite the policy refusal")
	}
}

func TestEphemeralLogsListing(t *testing.T) {
	cfg := configIn(t, "")
	logDir := filepath.Join(filepath.Dir(cfg), "logs")

	out, _, err := runCLI(t, nil, "--config", cfg, "ephemeral", "logs")
	if err != nil {
		t.Fatalf("ephemeral logs: %v", err)
	}
	if !strings.Contains(out, "no ephemeral runs have been logged") {
		t.Errorf("output = %q", out)
	}

	if err := os.MkdirAll(logDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"20240101-000000-eph-aaaa.log", "20240102-000000-eph-bbbb.log"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out, _, err = runCLI(t, nil, "--config", cfg, "ephemeral", "logs")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"eph-aaaa", "eph-bbbb"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}

	out, _, err = runCLI(t, nil, "--config", cfg, "ephemeral", "logs", "bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "eph-aaaa") {
		t.Errorf("the filter did not narrow the listing:\n%s", out)
	}
}

// nameFromConfigSh reads the runner name out of the recorded config.sh
// invocation, which is the only place the generated name appears.
func nameFromConfigSh(commands []string) string {
	for _, c := range commands {
		fields := strings.Fields(c)
		for i, f := range fields {
			if f == "--name" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}
