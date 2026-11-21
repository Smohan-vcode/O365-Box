package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// usageError is a user mistake: printed, exit 2.
type usageErr struct{ msg string }

func (e usageErr) Error() string { return e.msg }

func usageError(msg string) error { return usageErr{msg} }

// validationError reports an invalid configuration: printed, exit 2.
func validationError(msg string) error { return usageErr{msg} }

func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
