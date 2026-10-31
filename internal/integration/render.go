package integration

import (
	"fmt"
	"io"
	"strings"

	"github.com/bilal-/sous/internal/text"
)

func RenderDescription(w io.Writer, protocol string, v int, version string) {
	fmt.Fprintf(w, "sous integration · %s v%d · %s\n", protocol, v, version)
	fmt.Fprintln(w, "sous integration snapshot --json · current notes and local runs, last remote snapshot")
	fmt.Fprintln(w, "sous integration watch --json · baseline and changes as newline-delimited JSON")
	fmt.Fprintln(w, "sous integration --json · discovery and task actions")
}

func RenderSnapshot(w io.Writer, s Snapshot) {
	fmt.Fprintf(w, "sous integration · %d items · %s\n", len(s.Items), s.Revision)
	if len(s.Problems) > 0 {
		fmt.Fprintf(w, "? %s\n", strings.Join(s.Problems, "; "))
	}
	for _, item := range s.Items {
		fmt.Fprintf(w, "%s · %s · %s · %s\n", item.ID, item.Name, item.Section, item.Text)
	}
}

func RenderEvent(w io.Writer, e Event) error {
	if e.Type == "unavailable" {
		_, err := fmt.Fprintf(w, "sous integration · unavailable · %s\n", e.Error)
		return err
	}
	if e.Snapshot == nil {
		_, err := fmt.Fprintf(w, "sous integration · %s · %s · %s\n", e.Type, text.Plural(len(e.Changes), "change"), e.Revision)
		return err
	}
	_, err := fmt.Fprintf(w, "sous integration · %s · %d items · %s\n", e.Type, len(e.Snapshot.Items), e.Revision)
	return err
}
