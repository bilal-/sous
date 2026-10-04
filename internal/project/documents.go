package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Document is a guide in the project, discovered without loading its text
// into sous's store. Paths are relative to the project and keep their case.
type Document struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
	Error   string `json:"error,omitempty"`
}

// Documents points to a project's own conventions. Missing guides are
// optional; unreadable guides are reported, never presented as absent.
func Documents(root string) ([]Document, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := []Document{}
	for _, guide := range []struct{ name, purpose string }{
		{"README.md", "overview"},
		{"AGENTS.md", "instructions"},
		{"CLAUDE.md", "instructions"},
		{"STATUS.md", "status"},
		{"FOLLOWUPS.md", "followups"},
	} {
		for _, entry := range entries {
			if !strings.EqualFold(entry.Name(), guide.name) || entry.IsDir() {
				continue
			}
			d := Document{Path: entry.Name(), Purpose: guide.purpose}
			path := filepath.Join(root, entry.Name())
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				continue
			}
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("not a regular file")
			}
			if err == nil {
				var f *os.File
				f, err = os.Open(path)
				if err == nil {
					f.Close()
				}
			}
			if err != nil {
				d.Error = err.Error()
			}
			out = append(out, d)
		}
	}
	return out, nil
}
