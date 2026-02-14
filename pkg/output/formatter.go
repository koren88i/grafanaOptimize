package output

import (
	"io"

	"github.com/dashboard-advisor/pkg/rules"
)

// Formatter renders a Report to a writer.
type Formatter interface {
	Format(w io.Writer, report *rules.Report) error
}
