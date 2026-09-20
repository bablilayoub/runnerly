package doctor

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/bablilayoub/runnerly/internal/ui"
)

// Render writes a human readable report.
func Render(p *ui.Printer, r Report) {
	p.Heading("Runnerly Doctor")

	for _, c := range r.Checks {
		switch c.Status {
		case StatusPass:
			p.Pass("%s", c.Name)
		case StatusWarn:
			p.Warn("%s", c.Name)
		case StatusFail:
			p.Fail("%s", c.Name)
		case StatusSkip:
			p.Skip("%s", c.Name)
		}
		// A passing check's detail is noise unless something went wrong, so
		// only failures, warnings and skips explain themselves.
		if c.Status != StatusPass {
			p.Detail(c.Detail)
			p.Remedy(c.Remedy)
		}
	}

	p.Println()
	switch {
	case r.Summary.Fail > 0:
		p.Fail("%d check(s) failed. Fix the items above, then run runnerly doctor again.", r.Summary.Fail)
	case r.Summary.Warn > 0:
		p.Warn("Everything required is in place, with %d warning(s).", r.Summary.Warn)
	default:
		p.Pass("Everything looks good.")
	}
}

// RenderJSON writes the report as a single JSON object.
func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}
