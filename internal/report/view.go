package report

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Viewers lists the commands that can show the page, best first. Lavish
// (lavish-axi) comes first when the person has it, so they can mark up the
// page and hand their notes to an agent; it is never required, and the
// platform opener always follows it. Lavish counts as had when lavish-axi
// is on PATH, or when its agent skill is installed and npx can run it, the
// way the skill itself does.
func Viewers(userHome, page string) [][]string {
	var out [][]string
	if _, err := exec.LookPath("lavish-axi"); err == nil {
		out = append(out, []string{"lavish-axi", page, "--reopen"})
	} else if lavishSkill(userHome) {
		if _, err := exec.LookPath("npx"); err == nil {
			out = append(out, []string{"npx", "-y", "lavish-axi", page, "--reopen"})
		}
	}
	opener := "open" // macOS
	if _, err := exec.LookPath(opener); err != nil {
		opener = "xdg-open"
	}
	return append(out, []string{opener, page})
}

// ViewerName is what a Viewers command is called for the person.
func ViewerName(argv []string) string {
	for _, a := range argv {
		if a == "lavish-axi" {
			return "Lavish"
		}
	}
	return "your browser"
}

// lavishSkill: the Lavish skill is installed for every project, in the
// shared skills folder or Claude Code's.
func lavishSkill(userHome string) bool {
	if userHome == "" {
		return false
	}
	for _, dir := range []string{".agents", ".claude"} {
		if _, err := os.Stat(filepath.Join(userHome, dir, "skills", "lavish", "SKILL.md")); err == nil {
			return true
		}
	}
	return false
}
