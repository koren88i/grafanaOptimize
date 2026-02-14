package output

import (
	"encoding/json"
	"io"

	"github.com/dashboard-advisor/pkg/rules"
)

// JSONFormatter renders the report as JSON.
type JSONFormatter struct {
	Indent bool
}

func (f *JSONFormatter) Format(w io.Writer, report *rules.Report) error {
	enc := json.NewEncoder(w)
	if f.Indent {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(report)
}
