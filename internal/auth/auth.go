// Package auth resolves and stores the GitHub credentials the CLI uses.
//
// # Storage
//
// Tokens live in a credentials file next to config.yaml, written with mode
// 0600. The file is NOT encrypted. A local CLI has nowhere to keep a key that
// an attacker who can read the file could not also read, so encrypting it
// beside its own key would imply a guarantee it cannot make.
//
// Operators who do not want a token on disk should set RUNNERLY_GITHUB_TOKEN
// instead; an environment token always wins over the file and is never
// written. Runnerly says which source a token came from, so this is never a
// surprise.
//
// Server-side storage is a different problem with a different answer: the
// control plane will encrypt credentials at rest, because it has somewhere to
// put a key.
package auth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileName is the credentials file, stored beside config.yaml.
const FileName = "credentials.yaml"

// Environment variables consulted for a token, in order. The Runnerly-specific
// one wins so that a GITHUB_TOKEN left over from another tool cannot silently
// take over.
var envVars = []string{
	"RUNNERLY_GITHUB_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
}

// ErrNoToken means no credential was found in any source.
var ErrNoToken = errors.New("no GitHub token")

// Source says where a token came from.
type Source string

const (
	// SourceFlag is a token passed on the command line.
	SourceFlag Source = "flag"
	// SourceEnvironment is a token from an environment variable.
	SourceEnvironment Source = "environment"
	// SourceFile is a token from the credentials file.
	SourceFile Source = "file"
)

// Token is a resolved credential and where it came from.
type Token struct {
	Value  string
	Source Source
	// Origin names the flag, environment variable or file it came from, for
	// display.
	Origin string
	// User is the GitHub login recorded when the token was stored. It is
	// empty for tokens from a flag or the environment.
	User string
}

// Host is one stored credential.
type Host struct {
	Token     string    `yaml:"token"`
	User      string    `yaml:"user,omitempty"`
	Scopes    []string  `yaml:"scopes,omitempty"`
	CreatedAt time.Time `yaml:"created_at,omitempty"`
}

// Credentials is the contents of the credentials file.
type Credentials struct {
	Hosts map[string]Host `yaml:"hosts"`
}

// Path returns the credentials file that sits beside the given config file.
func Path(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), FileName)
}

// Load reads the credentials file. A missing file is not an error.
func Load(path string) (Credentials, error) {
	creds := Credentials{Hosts: map[string]Host{}}

	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied by design
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return creds, nil
		}
		return creds, fmt.Errorf("read credentials %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return Credentials{Hosts: map[string]Host{}}, fmt.Errorf("parse credentials %s: %w", path, err)
	}
	if creds.Hosts == nil {
		creds.Hosts = map[string]Host{}
	}
	return creds, nil
}

// Save writes the credentials file with mode 0600.
func Save(path string, creds Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	data, err := yaml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials %s: %w", path, err)
	}
	return nil
}

// Store records a token for a host, replacing any existing one.
func Store(path, host string, h Host) error {
	creds, err := Load(path)
	if err != nil {
		return err
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC().Truncate(time.Second)
	}
	creds.Hosts[normalizeHost(host)] = h
	return Save(path, creds)
}

// Delete removes a host's stored token. It reports whether anything was
// removed, so the CLI can tell the operator there was nothing to do.
func Delete(path, host string) (bool, error) {
	creds, err := Load(path)
	if err != nil {
		return false, err
	}
	key := normalizeHost(host)
	if _, ok := creds.Hosts[key]; !ok {
		return false, nil
	}
	delete(creds.Hosts, key)

	// An empty file is tidier than one holding an empty map, and it leaves
	// nothing behind that looks like a credential.
	if len(creds.Hosts) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove credentials %s: %w", path, err)
		}
		return true, nil
	}
	return true, Save(path, creds)
}

// Resolve finds a token for a host. Order: explicit flag, then environment,
// then the credentials file. It returns ErrNoToken when nothing is found.
func Resolve(path, host, flagToken string, getenv func(string) string) (Token, error) {
	if flagToken = strings.TrimSpace(flagToken); flagToken != "" {
		return Token{Value: flagToken, Source: SourceFlag, Origin: "--token"}, nil
	}

	if getenv == nil {
		getenv = os.Getenv
	}
	for _, name := range envVars {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return Token{Value: v, Source: SourceEnvironment, Origin: name}, nil
		}
	}

	creds, err := Load(path)
	if err != nil {
		return Token{}, err
	}
	if h, ok := creds.Hosts[normalizeHost(host)]; ok && h.Token != "" {
		return Token{Value: h.Token, Source: SourceFile, Origin: path, User: h.User}, nil
	}

	return Token{}, fmt.Errorf("%w for %s.\nRun `runnerly login` to store one, or set %s",
		ErrNoToken, normalizeHost(host), envVars[0])
}

// EnvVarNames returns the environment variables Resolve consults, in order.
func EnvVarNames() []string { return append([]string(nil), envVars...) }

// ReadToken reads a token from r, taking the first non-empty line. It is used
// for `runnerly login --with-token`, which reads from stdin so the token never
// reaches the shell history or the process table.
func ReadToken(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	// GitHub tokens are short; a long line means the wrong input was piped in.
	scanner.Buffer(make([]byte, 0, 4096), 64<<10)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			return line, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return "", errors.New("no token was provided on standard input")
}

// Redact returns a token safe to print: the last four characters, nothing
// else. Short or empty tokens redact completely rather than leaking a large
// fraction of themselves.
func Redact(token string) string {
	if len(token) < 8 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")
	if host == "" {
		return "github.com"
	}
	return host
}
