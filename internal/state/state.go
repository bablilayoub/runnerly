// Package state records the runners Runnerly has installed on this machine.
//
// It is deliberately separate from the configuration. config.yaml is what the
// operator asked for; runners.yaml is what Runnerly actually did. Keeping them
// apart means installing a runner never rewrites a hand-edited, commented
// configuration file, and it gives the agent something to read that says which
// runner lives where.
//
// Nothing here is a secret: the file records names, scopes and paths. The
// runner's own credentials live in its install directory, written by GitHub's
// config.sh, and Runnerly never copies them.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bablilayoub/runnerly/internal/github"
)

// FileName is the state file, stored beside config.yaml.
const FileName = "runners.yaml"

// ErrNotFound means no runner of that name is recorded on this machine.
var ErrNotFound = errors.New("no runner of that name is installed on this machine")

// Runner is one installed runner.
type Runner struct {
	Name  string       `yaml:"name"`
	Scope github.Scope `yaml:"scope"`
	// Host is the GitHub host the runner is registered with.
	Host string `yaml:"host"`
	// Dir is where the official runner was installed.
	Dir         string    `yaml:"dir"`
	Labels      []string  `yaml:"labels"`
	Ephemeral   bool      `yaml:"ephemeral,omitempty"`
	Release     string    `yaml:"release,omitempty"`
	InstalledAt time.Time `yaml:"installed_at"`
}

// File is the contents of runners.yaml.
type File struct {
	Runners map[string]Runner `yaml:"runners"`
}

// Path returns the state file that sits beside the given config file.
func Path(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), FileName)
}

// Load reads the state file. A missing file is not an error.
func Load(path string) (File, error) {
	f := File{Runners: map[string]Runner{}}

	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied by design
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return f, nil
		}
		return f, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return File{Runners: map[string]Runner{}}, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Runners == nil {
		f.Runners = map[string]Runner{}
	}
	// The map key is the source of truth for the name, so an entry hand-edited
	// without one still works.
	for key, r := range f.Runners {
		if r.Name == "" {
			r.Name = key
			f.Runners[key] = r
		}
	}
	return f, nil
}

// Save writes the state file, creating parent directories.
func Save(path string, f File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Put records a runner, replacing any entry with the same name.
func Put(path string, r Runner) error {
	if r.Name == "" {
		return errors.New("a recorded runner needs a name")
	}
	f, err := Load(path)
	if err != nil {
		return err
	}
	if r.InstalledAt.IsZero() {
		r.InstalledAt = time.Now().UTC().Truncate(time.Second)
	}
	f.Runners[r.Name] = r
	return Save(path, f)
}

// Get returns one recorded runner. Lookup ignores case, because GitHub treats
// runner names that way.
func Get(path, name string) (Runner, error) {
	f, err := Load(path)
	if err != nil {
		return Runner{}, err
	}
	for key, r := range f.Runners {
		if strings.EqualFold(key, name) {
			return r, nil
		}
	}
	return Runner{}, fmt.Errorf("%q: %w", name, ErrNotFound)
}

// Delete removes a runner. It reports whether anything was removed, so a
// caller can tell the operator there was nothing to do.
func Delete(path, name string) (bool, error) {
	f, err := Load(path)
	if err != nil {
		return false, err
	}
	for key := range f.Runners {
		if strings.EqualFold(key, name) {
			delete(f.Runners, key)
			if len(f.Runners) == 0 {
				// Leave nothing behind rather than an empty map.
				if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
					return false, fmt.Errorf("remove %s: %w", path, rmErr)
				}
				return true, nil
			}
			return true, Save(path, f)
		}
	}
	return false, nil
}

// List returns every recorded runner, ordered by name so output is stable.
func List(path string) ([]Runner, error) {
	f, err := Load(path)
	if err != nil {
		return nil, err
	}
	out := make([]Runner, 0, len(f.Runners))
	for _, r := range f.Runners {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Only returns the single installed runner. It is how commands avoid asking
// for a name on a machine that has exactly one, and it refuses to guess when
// there is more than one.
func Only(path string) (Runner, error) {
	runners, err := List(path)
	if err != nil {
		return Runner{}, err
	}
	switch len(runners) {
	case 0:
		return Runner{}, fmt.Errorf("no runner is installed on this machine.\n"+
			"Install one with `runnerly runner create`: %w", ErrNotFound)
	case 1:
		return runners[0], nil
	default:
		names := make([]string, 0, len(runners))
		for _, r := range runners {
			names = append(names, r.Name)
		}
		return Runner{}, fmt.Errorf("this machine has %d runners, so the name is needed: %s",
			len(runners), strings.Join(names, ", "))
	}
}
