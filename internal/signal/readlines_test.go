package signal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// readLines is the strict reader tests use: any bad line fails, so a
// built-in that prints junk is caught here rather than counted by the
// lenient runner.
func readLines(r io.Reader) ([]Signal, error) {
	var out []Signal
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s Signal
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("bad signal line %q: %w", line, err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}
