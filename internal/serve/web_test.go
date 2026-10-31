package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWebAppActionResponses(t *testing.T) {
	runWebAppTest(t, "actions_test.cjs")
}

func runWebAppTest(t *testing.T, script string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	app, err := web.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(appPath, app, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join("testdata", script), appPath).CombinedOutput()
	if err != nil {
		t.Fatalf("web app behavior: %v\n%s", err, out)
	}
}
