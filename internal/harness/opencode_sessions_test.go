package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Assistant messages belong to a session even before their first text part.
// Older payloads omit that owner until a part arrives. Cleanup must support
// both forms without discarding another session's messages or accepting a
// late part from a message whose session has already been cleaned up.
func TestOpencodeSessionCleanupKeepsOtherSessionMessages(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	for _, owner := range []string{"info", "part"} {
		for _, cleanup := range []string{"idle", "deleted"} {
			t.Run(owner+"/"+cleanup, func(t *testing.T) {
				dir := t.TempDir()
				got := filepath.Join(dir, "ends.jsonl")
				fake := filepath.Join(dir, "sous")
				script := "#!/bin/sh\nin=$(cat)\nprintf '%s\\n' \"$in\" >> \"$SOUS_TEST_HOOK_LOG\"\n"
				if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				plugin := filepath.Join(dir, "sous.mjs")
				for _, role := range []string{RoleStart, RoleEnd} {
					if _, err := (OpencodePlugin{}).Place(plugin, role, Cmd{Exe: fake, Role: role, Agent: "opencode"}); err != nil {
						t.Fatal(err)
					}
				}
				drive := filepath.Join(dir, "drive.mjs")
				if err := os.WriteFile(drive, []byte(`import { Sous } from "./sous.mjs"
const h = await Sous({ directory: "/code/acme/api" })
const event = (type, properties) => h.event({ event: { type, properties } })
const fromInfo = process.argv[2] === "info"
const cleanup = process.argv[3]
const info = (id, sessionID) => ({ role: "assistant", id, ...(fromInfo ? { sessionID } : {}) })
const part = (messageID, sessionID, text) => ({ type: "text", messageID, sessionID, text })
await event("message.updated", { info: info("mA", "A") })
await event("message.part.updated", { part: part("mA", "A", "A final") })
await event("message.updated", { info: { role: "assistant", id: "mA" } })
if (fromInfo) {
  await event("message.updated", { info: info("mA-unwritten", "A") })
} else {
  await event("message.updated", { info: { role: "assistant", id: "mA-tool" } })
  await event("message.part.updated", { part: { type: "tool", messageID: "mA-tool", sessionID: "A", tool: "shell", state: { status: "completed" } } })
}
await event("message.updated", { info: info("mB", "B") })
if (!fromInfo) {
  await event("message.part.updated", { part: part("mB", "B", "B partial") })
}
// An info update that omits the session must retain an already learned owner.
await event("message.updated", { info: { role: "assistant", id: "mB" } })
if (cleanup === "idle") {
  await event("session.idle", { sessionID: "A" })
} else {
  await event("session.deleted", { info: { id: "A" } })
}
// No new message.updated announces mA after cleanup, so this part is obsolete.
await event("message.part.updated", { part: part("mA", "A", "late A text") })
if (fromInfo) {
  await event("message.part.updated", { part: part("mA-unwritten", "A", "late unwritten A text") })
} else {
  await event("message.part.updated", { part: part("mA-tool", "A", "late tool A text") })
}
await event("message.part.updated", { part: part("mB", "B", "B final") })
await event("session.idle", { sessionID: "A" })
await event("session.idle", { sessionID: "B" })
`), 0o600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(node, drive, owner, cleanup)
				cmd.Env = append(os.Environ(), "HOME="+dir, "SOUS_HOME="+filepath.Join(dir, "state"), "SOUS_TEST_HOOK_LOG="+got)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("node: %v: %s", err, out)
				}
				wantA, wantTextA := 1, ""
				if cleanup == "idle" {
					wantA, wantTextA = 2, "A final"
				}
				var ends map[string][]string
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					ends = map[string][]string{}
					data, _ := os.ReadFile(got)
					for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
						var in struct {
							SessionID   string `json:"session_id"`
							LastMessage string `json:"last_message"`
						}
						if json.Unmarshal([]byte(line), &in) == nil && in.SessionID != "" {
							ends[in.SessionID] = append(ends[in.SessionID], in.LastMessage)
						}
					}
					if len(ends["A"]) == wantA && len(ends["B"]) == 1 {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				if len(ends) != 2 || len(ends["A"]) != wantA || len(ends["B"]) != 1 {
					t.Fatalf("want %d end hooks for A and one for B, got %#v", wantA, ends)
				}
				for _, last := range ends["A"] {
					if last != wantTextA {
						t.Errorf("cleanup must ignore A's late unannounced part: got %q, want %q", last, wantTextA)
					}
				}
				if ends["B"][0] != "B final" {
					t.Errorf("A's cleanup must retain B's assistant tracking: got %q", ends["B"][0])
				}
			})
		}
	}
}
