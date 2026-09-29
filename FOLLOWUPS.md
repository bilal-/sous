# Follow-ups

Tracked with `sous note --file`; tick a box to close it in sous too.
- [ ] pre-0.3: thread ids → stable random ids (schema v2 + migration) so FOLLOWUPS.md markers cannot collide across installs; today's dedupe is id+text <!-- sous:1:6c032f0b -->
- [x] here calls signal.ScanGit in-process, bypassing the plugin door the board uses; route through the same runner <!-- sous:2:c76ca420 -->
- [x] observed.json: prune entries not seen in 30 days; entries for vanished projects and removed plugins stay forever <!-- sous:3:d912e193 -->
- [ ] SOUS_HERE_FILE temp files leak one per `sous go`; write under $SOUS_HOME/here/ and prune, or skip for built-ins <!-- sous:4:bed3f3c4 -->
- [ ] MarkdownClose truncates before writing (crash window leaves an empty file); write then Truncate(len). Also normalizes CRLF→LF across the whole file <!-- sous:5:119c63c2 -->
- [ ] --menubar rows ignore Upstream state (the board and here show (ref missing) / status unavailable) <!-- sous:6:6e6dcb2f -->
- [ ] zsh wrapper only cd's when the project is $2 (`sous go -a codex proj` doesn't); --agent=x unsupported; `sous go .` unsupported <!-- sous:7:192aed75 -->
- [ ] skill: explain the → ref / (ref missing) / ✓ closed-upstream lines an agent sees in `here`; forbid editing sous markers by hand <!-- sous:8:026174b8 -->
- [ ] week-one cache.json has no `data`, so --menubar says "no board yet" until the next refresh after upgrading <!-- sous:9:3c975d76 -->
- [ ] sous.5m.sh hardcodes ~/.local/bin/sous; setup knows the real path and could bake it in <!-- sous:10:dff2f71e -->
- [x] cmdGo writes to os.Stdout instead of e.Stdout <!-- sous:11:59b6d1e1 -->
- [ ] hook 5s guard firing orphans a slow third-party status child in its own pgid <!-- sous:12:23d22042 -->
- [ ] signal snooze by id prefix (s:0a70) and `sous snooze 5 2 3` silently ignoring extra args <!-- sous:13:bd82ba35 -->
- [ ] projects table has no header; a non-URL remote prints a blank host column <!-- sous:14:022371f9 -->
- [ ] "as of HH:MM" beside a multi-day cache age needs a date; future timestamps render "now ago" <!-- sous:15:8dab789b -->
- [ ] ForPath walks up without bound: a $HOME that is itself a git repo resolves everything to $HOME <!-- sous:16:5b22caaf -->
- [ ] 20MB transcript line makes the SessionEnd scanner bail and record an older assistant message as the tail <!-- sous:17:3c7fbdeb -->
- [ ] snooze-until-changed keys on the summary text (e.g. "1 files uncommitted"), so editing the same dirty file stays snoozed <!-- sous:18:8fdf3c35 -->
- [x] the --json read model carries pre-formatted ages ("6d") beside timestamps; do not add more presentation fields <!-- sous:19:424a69d4 -->
- [x] note text beginning with a dash is eaten by flag parsing ("sous note \"--json …\"" fails); accept -- as a terminator and stop parsing flags after the first positional <!-- sous:21:f71deee9 -->
- [x] accounts() iterates a map: nondeterministic order of GitHub account searches and footer text <!-- sous:22:62df3972 -->
- [ ] github signal auth gate checks the active account only; a broken active account fails the whole plugin even if the org account works <!-- sous:23:043a7be5 -->
- [ ] InstallID silently regenerates on a malformed install-id file, orphaning every existing tracker marker <!-- sous:24:c9a58ec8 -->
- [ ] SSH remotes with ports (ssh://git@github.com:22/…) or ~/.ssh/config host aliases read as not-github <!-- sous:25:ab7df92d -->
- [ ] concurrent gh probes can each trigger a macOS keychain prompt after a gh upgrade <!-- sous:26:2c50a1e0 -->
- [ ] refresh_hours has two sources of truth (config.toml vs SOUS_REFRESH_HOURS in sous.zsh) <!-- sous:27:2234550f -->
