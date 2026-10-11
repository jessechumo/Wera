package h1b

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ReadFile streams a DOL LCA disclosure workbook (.xlsx), calling fn for
// every certified H-1B case. It returns how many rows were read and kept.
// Rows are streamed, so large files (250 MB) need little memory.
func ReadFile(path string, fn func(Case) error) (read, kept int, err error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return 0, 0, fmt.Errorf("%s has no sheets", path)
	}
	rows, err := f.Rows(sheets[0])
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close() //nolint:errcheck // read-only
	var h Header
	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return read, kept, err
		}
		if h == nil {
			var missing []string
			if h, missing = ParseHeader(row); len(missing) > 0 {
				return 0, 0, fmt.Errorf("%s is not an LCA disclosure file (missing %s)", path, strings.Join(missing, ", "))
			}
			continue
		}
		if len(row) == 0 {
			continue // the files end with many blank rows
		}
		read++
		c, ok := h.ParseRow(row)
		if !ok {
			continue
		}
		kept++
		if err := fn(c); err != nil {
			return read, kept, err
		}
	}
	return read, kept, rows.Error()
}
