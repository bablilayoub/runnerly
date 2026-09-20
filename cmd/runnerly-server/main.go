// Command runnerly-server is the Runnerly control plane.
//
// It keeps track of the machines that run GitHub Actions jobs: which exist,
// whether they are alive, and what has happened to them. It does not schedule
// anything; GitHub still does that.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/secret"
	"github.com/bablilayoub/runnerly/internal/server"
	"github.com/bablilayoub/runnerly/internal/store"
	"github.com/bablilayoub/runnerly/internal/version"
	"github.com/bablilayoub/runnerly/internal/web"
)

// Environment variables that override the configuration file, so a
// deployment can keep secrets out of it.
const (
	envDatabaseURL = "RUNNERLY_DATABASE_URL"
	envSecretKey   = "RUNNERLY_SECRET_KEY"
	envClientID    = "RUNNERLY_OAUTH_CLIENT_ID"
	// This is the name of an environment variable, not a secret.
	envClientSecret = "RUNNERLY_OAUTH_CLIENT_SECRET" //nolint:gosec // G101 false positive
	envListen       = "RUNNERLY_LISTEN"
)

// shutdownGrace is how long in-flight requests get to finish on stop.
const shutdownGrace = 20 * time.Second

// staleCommandAge is how long a command may wait before it is given up on.
// An agent that has not collected one within an hour is not coming back for
// it, and a dashboard showing "pending" for ever helps nobody.
const staleCommandAge = time.Hour

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath  = flag.String("config", "", "path to config.yaml (default: $RUNNERLY_CONFIG, else the user or system config)")
		listen      = flag.String("listen", "", "address to bind (default: server.listen from the configuration)")
		logFormat   = flag.String("log-format", string(agent.FormatJSON), "log format: json or text")
		logLevel    = flag.String("log-level", "info", "log level: debug, info, warn or error")
		migrateOnly = flag.Bool("migrate-only", false, "apply database migrations and exit")
		noMigrate   = flag.Bool("no-migrate", false, "do not apply migrations on start")
		showVersion = flag.Bool("version", false, "print build information and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Get().String())
		return 0
	}

	logger, err := agent.NewLogger(os.Stdout, agent.LogFormat(*logFormat), *logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	log := logger.With("component", server.Component)

	cfg, _, err := config.Load(configPathOrDefault(*configPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: the configuration is not valid.\n%v\n", err)
		return 1
	}

	databaseURL := firstNonEmpty(os.Getenv(envDatabaseURL), cfg.Server.Database)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, store.Options{URL: databaseURL})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer db.Close()

	if !*noMigrate || *migrateOnly {
		applied, err := db.Migrate(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		if len(applied) > 0 {
			log.Info("applied database migrations",
				"event", "migrations_applied", "migrations", strings.Join(applied, ","))
		}
	}
	if *migrateOnly {
		return 0
	}

	key, err := loadKey(firstNonEmpty(os.Getenv(envSecretKey), cfg.Server.SecretKey))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if key == nil {
		log.Warn("no secret key is set, so signed-in users' GitHub tokens will not be stored",
			"event", "no_secret_key",
			"hint", "generate one with `runnerly server keygen` if the server should hold them")
	}

	oauth := server.OAuthConfig{
		ClientID:      firstNonEmpty(os.Getenv(envClientID), cfg.Server.OAuth.ClientID),
		ClientSecret:  firstNonEmpty(os.Getenv(envClientSecret), cfg.Server.OAuth.ClientSecret),
		AllowedLogins: cfg.Server.OAuth.AllowedLogins,
		GitHubHost:    cfg.GitHub.Host,
	}
	if oauth.ClientID == "" || oauth.ClientSecret == "" || cfg.Server.URL == "" {
		log.Warn("dashboard sign-in is disabled until OAuth is configured",
			"event", "oauth_not_configured",
			"hint", "set server.url, server.oauth.client_id and "+envClientSecret+
				"; agent endpoints work regardless")
	}
	if len(oauth.AllowedLogins) == 0 && oauth.ClientID != "" {
		log.Warn("any GitHub account that completes sign-in will be allowed",
			"event", "no_allow_list",
			"hint", "set server.oauth.allowed_logins unless this server is on a private network")
	}

	srv, err := server.New(server.Options{
		Store:             db,
		Logger:            logger,
		Key:               key,
		OAuth:             oauth,
		PublicURL:         cfg.Server.URL,
		Thresholds:        thresholds(cfg),
		HeartbeatInterval: cfg.Server.Heartbeat.Interval.Duration(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	address := firstNonEmpty(*listen, os.Getenv(envListen), cfg.Server.Listen, "127.0.0.1:8080")
	httpServer := &http.Server{
		Addr:    address,
		Handler: srv,
		// Generous enough for a slow link, bounded so a stuck client cannot
		// hold a connection forever.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	if web.Available() {
		log.Info("serving the dashboard", "event", "dashboard_available")
	} else {
		log.Info("this binary has no dashboard; the API is unaffected",
			"event", "dashboard_missing",
			"hint", "run `make web` before `make build`, or use a release binary")
	}

	go housekeeping(ctx, db, log, cfg.Server.EventRetentionDays)

	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "event", "server_started", "address", address, "version", version.Get().Short())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	case <-ctx.Done():
		log.Info("shutting down", "event", "server_stopping")
	}

	// Stop accepting new requests but let the ones in flight finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("some requests were still running at shutdown",
			"event", "server_stop_forced", "error", err.Error())
		return 1
	}
	log.Info("stopped", "event", "server_stopped")
	return 0
}

// housekeeping prunes what would otherwise grow without limit.
func housekeeping(ctx context.Context, db *store.Store, log *slog.Logger, retentionDays int) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := db.PruneSessions(ctx); err != nil {
				log.Warn("could not prune sessions", "event", "prune_failed", "error", err.Error())
			} else if n > 0 {
				log.Info("pruned expired sessions", "event", "sessions_pruned", "count", n)
			}

			if n, err := db.ExpireStaleCommands(ctx, staleCommandAge); err != nil {
				log.Warn("could not expire stale commands", "event", "prune_failed", "error", err.Error())
			} else if n > 0 {
				log.Info("expired commands no agent collected",
					"event", "commands_expired", "count", n)
			}

			if retentionDays > 0 {
				if n, err := db.PruneEvents(ctx, retentionDays); err != nil {
					log.Warn("could not prune events", "event", "prune_failed", "error", err.Error())
				} else if n > 0 {
					log.Info("pruned old events", "event", "events_pruned", "count", n)
				}
			}
		}
	}
}

func thresholds(cfg config.Config) store.Thresholds {
	t := store.DefaultThresholds()
	if d := cfg.Server.Heartbeat.StaleAfter.Duration(); d > 0 {
		t.Stale = d
	}
	if d := cfg.Server.Heartbeat.OfflineAfter.Duration(); d > 0 {
		t.Offline = d
	}
	return t
}

// loadKey parses the secret key, treating "not set" as a choice rather than
// an error.
func loadKey(encoded string) (*secret.Key, error) {
	key, err := secret.ParseKey(encoded)
	if errors.Is(err, secret.ErrNoKey) {
		return nil, nil
	}
	return key, err
}

func configPathOrDefault(path string) string {
	if path != "" {
		return path
	}
	return config.Path()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
