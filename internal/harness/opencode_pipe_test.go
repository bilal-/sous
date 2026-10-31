package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// An end hook is best effort: failure before it reads its input must not
// propagate an unhandled stream error into the hosting agent.
func TestOpencodeEndHookClosedInputDoesNotCrashHost(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "sous")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "sous.mjs")
	if _, err := (OpencodePlugin{}).Place(plugin, RoleEnd, Cmd{Exe: fake, Role: RoleEnd, Agent: "opencode"}); err != nil {
		t.Fatal(err)
	}
	drive := filepath.Join(dir, "drive.mjs")
	if err := os.WriteFile(drive, []byte(`import { Sous } from "./sous.mjs"
const h = await Sous({ directory: "/code/acme/api" })
await h.event({ event: { type: "message.updated", properties: { info: { role: "assistant", id: "m1", sessionID: "ses_1" } } } })
await h.event({ event: { type: "message.part.updated", properties: { part: { type: "text", messageID: "m1", sessionID: "ses_1", text: "x".repeat(1024 * 1024) } } } })
await h.event({ event: { type: "session.idle", properties: { sessionID: "ses_1" } } })
await new Promise(resolve => setTimeout(resolve, 300))
console.log("survived")
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, drive)
	cmd.Env = append(os.Environ(), "HOME="+dir, "SOUS_HOME="+filepath.Join(dir, "state"))
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "survived" {
		t.Fatalf("an unavailable end hook must leave the host alive: %v: %s", err, out)
	}
}
