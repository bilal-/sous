package integration

import (
	"fmt"
	"io"
	"strings"
)

func RenderDescription(w io.Writer, d Description) {
	fmt.Fprintf(w, "sous integration · %s v%d · %s\n", d.Protocol, d.V, d.Version)
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
	_, err := fmt.Fprintf(w, "sous integration · %s · %d items · %s\n", e.Type, len(e.Snapshot.Items), e.Revision)
	return err
}
