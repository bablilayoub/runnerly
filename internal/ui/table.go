package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// columnGap separates columns. Two spaces reads as a column boundary without
// drawing box characters, which survive copy-paste badly.
const columnGap = "  "

// Table is a left-aligned text table. Columns are sized to their widest cell,
// so output stays aligned without a fixed layout.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable returns a table with the given column headers.
func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

// Row appends a row. Rows shorter than the header are padded; longer rows keep
// their extra cells, which widens the table rather than losing data.
func (t *Table) Row(cells ...string) {
	t.rows = append(t.rows, cells)
}

// Len returns the number of rows.
func (t *Table) Len() int { return len(t.rows) }

// widths returns the rendered width of each column.
func (t *Table) widths() []int {
	count := len(t.headers)
	for _, r := range t.rows {
		if len(r) > count {
			count = len(r)
		}
	}

	w := make([]int, count)
	for i, h := range t.headers {
		w[i] = utf8.RuneCountInString(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if n := utf8.RuneCountInString(c); n > w[i] {
				w[i] = n
			}
		}
	}
	return w
}

// Table writes the table. An empty table writes nothing, so callers handle the
// "no results" message themselves rather than printing a bare header.
func (p *Printer) Table(t *Table) {
	if t == nil || len(t.rows) == 0 {
		return
	}
	w := t.widths()

	if len(t.headers) > 0 {
		fmt.Fprintln(p.w, p.paint(dim, renderRow(t.headers, w)))
	}
	for _, r := range t.rows {
		fmt.Fprintln(p.w, renderRow(r, w))
	}
}

func renderRow(cells []string, widths []int) string {
	var b strings.Builder
	for i, c := range cells {
		if i > 0 {
			b.WriteString(columnGap)
		}
		b.WriteString(c)
		// The last column is not padded, so lines have no trailing spaces.
		if i < len(cells)-1 && i < len(widths) {
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c)))
		}
	}
	return b.String()
}
