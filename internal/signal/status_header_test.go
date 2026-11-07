package signal

import "testing"

func TestParseStatusHeader(t *testing.T) {
	for _, c := range []struct {
		in   string
		want branchStatus
	}{
		{"## main", branchStatus{branch: "main"}},
		{"## main...origin/main", branchStatus{branch: "main", upstream: true, ahead: "0"}},
		{"## main...origin/main [ahead 3]", branchStatus{branch: "main", upstream: true, ahead: "3"}},
		{"## main...origin/main [behind 2]", branchStatus{branch: "main", upstream: true, ahead: "0"}},
		{"## feat/x...origin/feat/x [ahead 1, behind 4]", branchStatus{branch: "feat/x", upstream: true, ahead: "1"}},
		{"## main...origin/main [gone]", branchStatus{branch: "main"}},
		{"## main...origin/main [different]", branchStatus{branch: "main", upstream: true}},
		{"## HEAD (no branch)", branchStatus{branch: "(detached)"}},
		{"## No commits yet on main", branchStatus{branch: "main"}},
		{"## Initial commit on main", branchStatus{branch: "main"}},
	} {
		if got := parseStatusHeader(c.in); got != c.want {
			t.Errorf("parseStatusHeader(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
