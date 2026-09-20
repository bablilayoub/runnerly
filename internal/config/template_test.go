package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestTemplateMatchesDefault is the reason Template can be a hand-written
// string: if a field is added to Config, or a default changes, and the template
// is not updated, this fails.
func TestTemplateMatchesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(Template()), 0o600); err != nil {
		t.Fatal(err)
	}

	// Load rejects unknown keys, so a stale template name fails here too.
	got, found, err := Load(path)
	if err != nil {
		t.Fatalf("the generated template does not parse: %v", err)
	}
	if !found {
		t.Fatal("found = false for a file that was just written")
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("template does not decode to Default():\ngot  %+v\nwant %+v", got, Default())
	}
}

func TestTemplateIsCommented(t *testing.T) {
	tmpl := Template()
	for _, want := range []string{
		"# Runnerly configuration",
		"docs/configuration.md",
		"runnerly config validate",
		"docs/security.md",
	} {
		if !strings.Contains(tmpl, want) {
			t.Errorf("template is missing %q", want)
		}
	}
}

func TestTemplateIsValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(Template()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("the generated template is not valid: %v", err)
	}
}
