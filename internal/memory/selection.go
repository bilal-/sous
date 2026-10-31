package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/text"
)

type SelectionMode bool

const (
	Explicit         SelectionMode = false
	CurrentDirectory SelectionMode = true
)

// AmbiguousScope preserves existing private identities rather than guessing
// which alias the person meant. Selecting a name or an absolute path is explicit.
type AmbiguousScope struct{ Choices []string }

func (e AmbiguousScope) Error() string {
	return "ambiguous private context; choose " + strings.Join(e.Choices, " or ")
}

func ResolveScope(ctx context.Context, c *config.Config, p project.Project, mode SelectionMode) (Scope, error) {
	s := ForProject(c, p)
	if mode == Explicit || c == nil || len(c.Roots) == 0 {
		return s, nil
	}
	aliases, err := project.AliasesForPath(ctx, c.Roots, c.Ignore, p.Path)
	if err != nil {
		return Scope{}, err
	}
	choices := []string{"-p " + text.ShellArg(p.Path)}
	ambiguous := false
	for _, candidate := range aliases {
		alias := p
		alias.Path = candidate.Path
		if !s.Same(ForProject(c, alias)) {
			ambiguous = true
			if candidate.Selector != "" {
				choices = append(choices, "-p "+text.ShellArg(candidate.Selector))
			}
		}
	}
	if ambiguous {
		if len(choices) == 1 {
			choices = append(choices, "inspect named scopes with sous projects (alias names are ambiguous)")
		}
		return Scope{}, AmbiguousScope{choices}
	}
	return s, nil
}

// HistoryTarget returns an existing selector that does not reinterpret a stored
// scope through a changed or removed alias. Workspace fallback is explicitly
// broader; callers must label it as workspace history.
func HistoryTarget(c *config.Config, s Scope) (flag, value, label string) {
	p, err := project.Resolve(nil, nil, s.Project, "", "", nil)
	if err == nil && ForProject(c, p).Same(s) {
		return "-p", s.Project, "previous decisions and handoffs"
	}
	return "--workspace", s.Workspace, fmt.Sprintf("workspace history (%s)", s.Workspace)
}
