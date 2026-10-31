// Package memory keeps private, sourced handoffs, decisions and reusable lessons.
// It never infers that a commitment is complete or reads a referenced document.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

type Kind string

const (
	Handoff    Kind = "handoff"
	Decision   Kind = "decision"
	Constraint Kind = "constraint"
	Lesson     Kind = "lesson"
	Question   Kind = "question"
)

func (k Kind) Valid() bool {
	return slices.Contains([]Kind{Handoff, Decision, Constraint, Lesson, Question}, k)
}

type Scope struct {
	Workspace string `json:"workspace"`
	Project   string `json:"project"`
	Remote    string `json:"remote,omitempty"`
}

// Same requires a workspace match even when two clones have the same remote.
func (s Scope) Same(other Scope) bool {
	return s.Workspace == other.Workspace && (s.Project == other.Project || s.Remote != "" && s.Remote == other.Remote)
}

// ForProject uses a named per-project workspace, otherwise the most specific
// discovery root. An unconfigured project has its own private scope.
func ForProject(c *config.Config, p project.Project) Scope {
	s := Scope{Project: p.Path, Workspace: p.Path}
	if p.Remote != nil {
		s.Remote = *p.Remote
	}
	if c == nil {
		return s
	}
	if name := c.Project(project.OrgName(p.Path)).Workspace; name != "" {
		s.Workspace = name
		return s
	}
	best := ""
	for _, root := range c.Roots {
		root = filepath.Clean(root)
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		rel, err := filepath.Rel(root, p.Path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && len(root) > len(best) {
			best = root
		}
	}
	if best != "" {
		s.Workspace = best
	}
	return s
}

type Record struct {
	ID                 string     `json:"id"`
	Scope              Scope      `json:"scope"`
	Kind               Kind       `json:"kind"`
	Text               string     `json:"text"`
	Source             string     `json:"source"`
	Author             string     `json:"author"`
	CreatedAt          time.Time  `json:"created_at"`
	VerifiedAt         *time.Time `json:"verified_at"`
	VerificationSource string     `json:"verification_source,omitempty"`
	Supersedes         string     `json:"supersedes,omitempty"`
	SupersededBy       string     `json:"superseded_by,omitempty"`
	ForgottenAt        *time.Time `json:"forgotten_at,omitempty"`
	SharedWith         []Scope    `json:"shared_with"`
	Key                string     `json:"key"`
	Fingerprint        string     `json:"fingerprint"`
}

func (r Record) VisibleTo(s Scope) bool {
	return r.Scope.Same(s) || slices.ContainsFunc(r.SharedWith, s.Same)
}

type Doc struct {
	Version int      `json:"version"`
	NextID  int      `json:"next_id"`
	Records []Record `json:"records"`
}

var file = store.V1(`{"version":1,"next_id":1,"records":[]}`)
var ErrNotFound = errors.New("no memory")
var ErrConflict = errors.New("memory conflict")

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type Input struct {
	Scope                                 Scope
	Kind                                  Kind
	Text, Source, Author, Key, Supersedes string
	Verified                              bool
}

func All(s *store.Store) ([]Record, error) {
	d, err := store.Load[Doc](s, "memory", file)
	if err != nil {
		return nil, err
	}
	if d.Records == nil {
		return []Record{}, nil
	}
	return d.Records, nil
}
func Get(s *store.Store, id string) (Record, error) {
	records, err := All(s)
	if err != nil {
		return Record{}, err
	}
	for _, r := range records {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, fmt.Errorf("%w %s", ErrNotFound, id)
}
func find(d *Doc, id string) (*Record, error) {
	for i := range d.Records {
		if d.Records[i].ID == id {
			return &d.Records[i], nil
		}
	}
	return nil, fmt.Errorf("%w %s", ErrNotFound, id)
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func Add(s *store.Store, in Input, now time.Time) (Record, bool, error) {
	in.Text, in.Source = strings.TrimSpace(in.Text), strings.TrimSpace(in.Source)
	if !in.Kind.Valid() {
		return Record{}, false, ValidationError("memory kind must be handoff, decision, constraint, lesson or question")
	}
	if in.Text == "" || len(in.Text) > 32768 {
		return Record{}, false, ValidationError("memory text must contain 1 to 32768 bytes")
	}
	if in.Scope.Workspace == "" || !filepath.IsAbs(in.Scope.Project) {
		return Record{}, false, ValidationError("memory needs a workspace and project path")
	}
	if len(in.Source) > 2048 || strings.ContainsAny(in.Source, "\r\n") {
		return Record{}, false, ValidationError("source must be one line, at most 2048 bytes")
	}
	if in.Verified && in.Source == "" {
		return Record{}, false, ValidationError("verified memory needs --source describing the evidence")
	}
	if in.Author == "" {
		in.Author = "human"
	}
	identity := in.Scope.Project
	if in.Scope.Remote != "" {
		identity = in.Scope.Remote
	}
	fingerprint := digest([]any{in.Scope.Workspace, identity, in.Kind, in.Text, in.Source, in.Supersedes, in.Verified})
	key := fingerprint
	if in.Key != "" {
		key = "key:" + digest([]string{in.Scope.Workspace, identity, in.Key})
	}
	var result Record
	already := false
	_, err := store.Modify[Doc](s, "memory", file, func(d *Doc) error {
		for _, r := range d.Records {
			if r.Key != key {
				continue
			}
			if r.Fingerprint != fingerprint {
				return fmt.Errorf("%w: key already records different content", ErrConflict)
			}
			result, already = r, true
			return nil
		}
		id := fmt.Sprintf("m:%d", d.NextID)
		if in.Supersedes != "" {
			old, err := find(d, in.Supersedes)
			if err != nil {
				return err
			}
			if !old.Scope.Same(in.Scope) {
				return ValidationError("a memory can replace only one from the same project and workspace")
			}
			if old.ForgottenAt != nil || old.SupersededBy != "" {
				return fmt.Errorf("%w: %s is already forgotten or superseded", ErrConflict, old.ID)
			}
			old.SupersededBy = id
		}
		result = Record{ID: id, Scope: in.Scope, Kind: in.Kind, Text: in.Text, Source: in.Source, Author: in.Author,
			CreatedAt: now.UTC(), Supersedes: in.Supersedes, SharedWith: []Scope{}, Key: key, Fingerprint: fingerprint}
		if in.Verified {
			at := now.UTC()
			result.VerifiedAt, result.VerificationSource = &at, in.Source
		}
		d.NextID++
		d.Records = append(d.Records, result)
		return nil
	})
	return result, already, err
}

func change(s *store.Store, id string, fn func(*Record) error) error {
	_, err := store.Modify[Doc](s, "memory", file, func(d *Doc) error {
		r, err := find(d, id)
		if err != nil {
			return err
		}
		return fn(r)
	})
	return err
}
func Verify(s *store.Store, id, source string, now time.Time) error {
	source = strings.TrimSpace(source)
	if source == "" || len(source) > 2048 || strings.ContainsAny(source, "\r\n") {
		return ValidationError("verification needs a one-line --source describing the evidence")
	}
	return change(s, id, func(r *Record) error {
		if r.ForgottenAt != nil || r.SupersededBy != "" {
			return fmt.Errorf("%w: verify the current memory", ErrConflict)
		}
		if r.VerifiedAt != nil && r.VerificationSource == source {
			return nil
		}
		at := now.UTC()
		r.VerifiedAt, r.VerificationSource = &at, source
		return nil
	})
}
func Forget(s *store.Store, id string, now time.Time) error {
	return change(s, id, func(r *Record) error {
		if r.ForgottenAt != nil {
			return nil
		}
		at := now.UTC()
		r.ForgottenAt = &at
		r.Text, r.Source, r.VerificationSource = "", "", ""
		r.VerifiedAt, r.SharedWith = nil, []Scope{}
		return nil
	})
}
func Share(s *store.Store, id string, target Scope, add bool) error {
	if target.Workspace == "" || !filepath.IsAbs(target.Project) {
		return ValidationError("sharing needs a target project and workspace")
	}
	return change(s, id, func(r *Record) error {
		if r.ForgottenAt != nil {
			return fmt.Errorf("%w: memory was forgotten", ErrConflict)
		}
		if add {
			if !r.Scope.Same(target) && !slices.ContainsFunc(r.SharedWith, target.Same) {
				r.SharedWith = append(r.SharedWith, target)
			}
		} else {
			r.SharedWith = slices.DeleteFunc(r.SharedWith, target.Same)
		}
		return nil
	})
}

type Query struct {
	Scope        Scope
	Workspace    string
	All, History bool
	Kind         Kind
	Text         string
	Limit        int
	IDs          []string
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

func Search(s *store.Store, q Query) ([]Record, error) {
	r, err := Find(s, q)
	return r.Records, err
}

// Result retains the full matching count even when only some records are shown.
type Result struct {
	Records    []Record `json:"records"`
	TotalCount int      `json:"total_count"`
	Truncated  bool     `json:"truncated"`
}

func Find(s *store.Store, q Query) (Result, error) {
	if q.Kind != "" && !q.Kind.Valid() {
		return Result{}, ValidationError("unknown memory kind")
	}
	if q.Limit < 0 || q.Limit > 1000 {
		return Result{}, ValidationError("limit must be between 1 and 1000")
	}
	records, err := All(s)
	if err != nil {
		return Result{}, err
	}
	out := []Record{}
	scores := map[string]int{}
	terms := words(q.Text)
	for _, r := range records {
		if r.ForgottenAt != nil || !q.History && r.SupersededBy != "" || q.Kind != "" && r.Kind != q.Kind {
			continue
		}
		allowed := q.All || q.Workspace != "" && r.Scope.Workspace == q.Workspace || q.Scope.Project != "" && r.VisibleTo(q.Scope)
		if !allowed || q.IDs != nil && !slices.Contains(q.IDs, r.ID) {
			continue
		}
		found := map[string]bool{}
		for _, w := range words(r.Text + " " + r.Source) {
			found[w] = true
		}
		for _, term := range terms {
			if found[term] {
				scores[r.ID]++
			}
		}
		if len(terms) > 0 && scores[r.ID] == 0 {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if scores[out[i].ID] != scores[out[j].ID] {
			return scores[out[i].ID] > scores[out[j].ID]
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	total := len(out)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return Result{Records: out, TotalCount: total, Truncated: len(out) < total}, nil
}
