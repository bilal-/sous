# Sous Tasks for herdr

This directory contains the complete herdr plugin: its manifest, observer,
interactive Tasks pane, task actions and tests. It uses herdr's published plugin
and socket APIs and needs no changes to herdr.

Tasks appears in a managed terminal pane inside herdr. Project counts and task
labels appear in herdr's own sidebar when you add the rows below; alerts use
herdr's configured notification delivery. Herdr's plugin API does not provide
custom native widgets or clickable task destinations in notifications.

## Requirements

* macOS or Linux, with herdr 0.8.2 or newer.
* Go matching the repository's `go.mod` (currently 1.27.1 or newer) to build or
  install this source plugin. The compiled plugin needs no Go runtime.
* `sous` with the `sous.integration` v0 interface, on herdr's `PATH`. Check it
  with `sous integration --json`. Build sous from this checkout if your installed
  release does not yet have that command.
* `stty`, normally supplied by the operating system, for the interactive pane.

Configure sous's project folders using the [main guide](../../README.md), then
run `sous --refresh` once to create the first full snapshot. The plugin never
sets up agent hooks, files notes publicly, or installs other programs for you.

## Install from a checkout

From the repository root:

```sh
make install  # build and install this checkout's sous, if needed
make herdr
herdr plugin link "$PWD/integrations/herdr"
```

Linking registers the plugin for the current user. It does not build it; run
`make herdr` again after changing plugin code. If you previously installed it
from GitHub, uninstall that copy before linking a local one.

## Install from GitHub

Once these files are published in the repository:

```sh
herdr plugin install bilal-/sous/integrations/herdr
```

Herdr clones the repository, previews the manifest, builds `sous-herdr` with Go,
and registers `sous.tasks`. Sous is installed separately. You can pin a published
revision with herdr's `--ref` option. Reinstall from the same source to update;
herdr has no separate plugin-update command.

## Open Tasks

Run these inside the herdr session you want to use:

```sh
herdr plugin action invoke sous.tasks.start
herdr plugin pane open --plugin sous.tasks --entrypoint tasks
```

Startup hooks start the observer on future herdr server starts. Linking or
enabling a plugin in an already running server does not run startup hooks, so
use the `start` action then. Opening Tasks also ensures an observer is started.
Closing the Tasks pane leaves notifications and sidebar reporting active.

Optional shortcut in herdr's configuration:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "sous.tasks.tasks"
description = "Sous Tasks"
```

Choose an unused key in your setup and reload herdr's configuration. The action
opens an overlay, keeping the underlying project's pane intact. You can also
open Tasks in a tab with `--placement tab` or in a split with `--placement split`.

## Use the task pane

| Key | Action |
|---|---|
| Up/Down or `k`/`j` | Select a task. |
| Enter | Focus its linked pane, or open its project in an agent tab. |
| `i` | Show a note's details; press `i` again to return. |
| `n` | Capture a private note for the selected task's project, or the originating Space's project. |
| `z` | Snooze the selected task. |
| `d` | Close a note after typing `yes`. Closing a run also asks its runner to stop. |
| `a` | Answer a run when sous says it needs you. |
| `r` | Refresh sous's remote sources. |
| Tab | Switch among all projects, this Space, ideas and snoozed tasks. |
| `q` | Close Tasks. |

Inputs use Enter to save, Backspace to edit and Ctrl+C to cancel. Ctrl+C outside
an input closes Tasks. New notes use sous's normal `idea` kind and appear in
Ideas; use Tab to switch to that view. Actions use literal argument values and the advertised
working directory; user text is never assembled into a shell command. The
plugin looks up a task's stable key again before acting, so a changing list
cannot redirect a pending action to another task.

Opening a task reuses a pane linked to the same sous instance and task key.
Otherwise it finds or creates the project's Space, launches the registered
`project` pane entrypoint and runs sous's advertised `open` action there. That
starts your configured agent with sous's project handoff. It does not submit a
new background run automatically. The agent needs to be configured normally in
sous. A run needing a reply is answered through sous's runner, using `a`.

## Add sidebar summaries

Merge these rows into your existing herdr configuration; keep the other rows
you already use:

```toml
[ui.sidebar.spaces]
rows = [
  ["state_icon", "workspace"],
  ["branch", "git_status"],
  ["$sous_tasks"],
  ["$sous_freshness"],
]

[ui.sidebar.agents]
rows = [
  ["state_icon", "machine", "workspace", "tab"],
  ["agent"],
  ["$sous_task_title"],
]
```

Space counts come from the same task snapshot and resolve worktree paths with
`sous here`. `?` means the picture has known gaps; an unavailable provider says
so explicitly. The remote snapshot's time remains visible. Metadata expires
after 30 seconds without reporting, so a stopped observer cannot leave permanent
healthy counts behind.

Spaces without indexed project data show `sous project not tracked`.

## Notifications and refresh

The observer loads its first snapshot quietly and alerts on meaningful task
changes: a run needs you, finishes or fails, or new non-human work lands on you.
Polling timestamps, repeated revisions, recovery baselines and removals do not
trigger alerts. Delivery uses herdr's notification configuration. If delivery
fails, the task stays in the list and the pane shows a warning. Alerts identify
the project and task; open Tasks to navigate to it.

Find the plugin's configuration directory:

```sh
herdr plugin config-dir sous.tasks
```

Create `config.toml` in that directory if you want different defaults:

```toml
notifications = true
refresh_interval_seconds = 0
# sous_path = "/path/to/sous"
```

The default `0` leaves remote refresh manual. An interval from 30 to 86400
seconds opts into periodic `sous --refresh`; for example, `300` checks every
five minutes. The first scheduled refresh waits for its interval. Discovery,
subscription and session startup stay offline. Config changes take effect when
you stop and start the observer:

```sh
herdr plugin action invoke sous.tasks.stop
# Wait up to five seconds for the observer to stop, then:
herdr plugin action invoke sous.tasks.start
```

Set `notifications = false` to keep the pane and summaries while muting alerts.

## Lifecycle, privacy and removal

The startup command exits after starting a plugin-owned observer. That process
holds one lock per herdr socket, sous home and executable. It consumes sous's
offline change feed, checks herdr's connection and enabled plugin state, and
stops its provider child when herdr disconnects or the plugin is disabled or
uninstalled. Overlapping starts do not create duplicate observers. A server
handoff can replace the observer; the new baseline remains quiet.

The plugin stores its last snapshot and delivery warning under herdr's
`HERDR_PLUGIN_STATE_DIR`, in a session-specific subdirectory, with versioned,
locked, whole-file updates. Its `observer.log` is in that same subdirectory.
These files can contain private task text. They are not written into the
checkout. State from a newer plugin is refused rather than overwritten.

To remove it:

```sh
herdr plugin action invoke sous.tasks.stop
herdr plugin uninstall sous.tasks
```

Use `herdr plugin unlink sous.tasks` instead for a local checkout. Herdr's
configuration and state directories may remain; remove the plugin's own
directories separately if you want to discard its cache. Sidebar custom tokens
disappear once their metadata expires. Remove the optional rows and shortcut
from your configuration if you no longer want them.

## Troubleshooting and validation

* Missing integration command: build/install sous from a checkout containing
  the provider, then verify `sous integration --json`.
* Missing snapshot: run `sous --refresh` or press `r` in Tasks.
* No summaries: check that the observer is started and the sidebar rows are
  configured. Task updates never make old remote data fresh.
* No agent opens: run `sous doctor` and configure a launcher; project opening
  uses the same agent as `sous go`.
* No alerts: initial and recovery baselines are deliberately quiet. Check
  plugin notification settings and herdr's toast delivery settings.
* Plugin startup errors: `herdr plugin log list --plugin sous.tasks` shows the
  startup command; ongoing observer errors are in `observer.log`.

`make ci` includes the plugin's race tests. They use fake provider processes,
throwaway homes and a fake herdr socket: task transitions, failure recovery,
notification delivery failure, literal arguments, stable prompt identity,
private state versions, pane reuse and shutdown with the host. No test changes
your real herdr session or starts a real tracker or agent.

The host-neutral protocol is documented in
[docs/integrations.md](../../docs/integrations.md). Herdr's supported surfaces
are described in its [socket API](https://herdr.dev/docs/socket-api/) and
[configuration guide](https://herdr.dev/docs/configuration/).
