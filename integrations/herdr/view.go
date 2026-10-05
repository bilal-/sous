package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
)

type View struct {
	Scope, Project, SelectedKey, Message, Prompt string
	Remote                                       *string
	PromptKey, PromptProject                     string
	Input                                        []rune
	Details                                      *integration.Item
}

func (v *View) beginPrompt(mode string, item integration.Item) {
	v.Prompt, v.PromptKey, v.PromptProject = mode, item.Key, item.Project
	if v.PromptProject == "" {
		v.PromptProject = v.Project
	}
	v.Input = nil
}

func (v *View) items(state bridgeState) []integration.Item {
	var out []integration.Item
	if state.Snapshot == nil {
		return out
	}
	for _, item := range state.Snapshot.Items {
		keep := item.Section != "ideas" && item.Section != "snoozed"
		switch v.Scope {
		case "space":
			keep = keep && projectMatches(state.Snapshot, project.Project{Path: v.Project, Remote: v.Remote}, item.Project)
		case "ideas":
			keep = item.Section == "ideas"
		case "snoozed":
			keep = item.Section == "snoozed"
		}
		if keep {
			out = append(out, item)
		}
	}
	return out
}

func (v *View) selected(state bridgeState) (integration.Item, bool) {
	items := v.items(state)
	if len(items) == 0 {
		return integration.Item{}, false
	}
	for _, item := range items {
		if item.Key == v.SelectedKey {
			return item, true
		}
	}
	v.SelectedKey = items[0].Key
	return items[0], true
}

func (v *View) move(state bridgeState, delta int) {
	current, ok := v.selected(state)
	if !ok {
		return
	}
	items := v.items(state)
	index := slices.IndexFunc(items, func(item integration.Item) bool { return item.Key == current.Key })
	v.SelectedKey = items[max(0, min(len(items)-1, index+delta))].Key
}

func kindLabel(item integration.Item) string {
	if item.Run != nil {
		switch item.Run.State {
		case "starting", "running":
			return "Working"
		case "needs_you":
			return "Needs you"
		case "done":
			return "Finished"
		case "failed":
			return "Failed"
		}
	}
	return map[string]string{"on_you": "On you", "on_others": "On others", "unfinished": "Unfinished", "ideas": "Idea", "snoozed": "Snoozed"}[item.Section]
}

func (v *View) Render(state bridgeState, width, height int) string {
	width, height = max(1, width), max(1, height)
	scope := map[string]string{"all": "all projects", "space": "this Space", "ideas": "ideas", "snoozed": "snoozed"}[v.Scope]
	lines := []string{"Tasks · " + scope, ""}
	if !state.Available {
		lines[1] = "? " + state.Error + " · keeping the last known tasks"
	} else if state.Snapshot != nil {
		if !state.Snapshot.Complete {
			lines[1] = "? " + strings.Join(state.Snapshot.Problems, " · ")
		} else if state.Snapshot.AsOf != nil {
			lines[1] = "Remote snapshot " + state.Snapshot.AsOf.Local().Format("Jan 2 15:04") + " · local tasks update live"
		}
	}
	lines = append(lines, strings.Repeat("─", width))
	room := max(1, height-7)
	if v.Details != nil {
		item := v.Details
		body := []string{item.Name + " · " + kindLabel(*item), item.Text}
		if item.Ref != nil {
			body = append(body, "Reference: "+*item.Ref)
		}
		if item.Upstream != nil {
			body = append(body, "Tracker: "+item.Upstream.State+" "+item.Upstream.Error)
		}
		if item.Run != nil {
			body = append(body, "Run: "+string(item.Run.State)+" · "+item.Run.Text, item.Run.Brief, item.Run.Error)
			body = append(body, item.Run.LogTail...)
		}
		for _, text := range body {
			runes := []rune(clean(text))
			for len(runes) > 0 && len(lines) < room+3 {
				n := min(width, len(runes))
				lines = append(lines, string(runes[:n]))
				runes = runes[n:]
			}
		}
	} else {
		items := v.items(state)
		current, hasSelection := v.selected(state)
		index := slices.IndexFunc(items, func(item integration.Item) bool { return item.Key == current.Key })
		start := max(0, index-room+1)
		if len(items) == 0 {
			message := "Nothing waiting in this view"
			if !state.Available || (state.Snapshot != nil && !state.Snapshot.Complete) {
				message = "No tasks available in this view · check the status above"
			}
			lines = append(lines, message)
		}
		for _, item := range items[start:min(len(items), start+room)] {
			prefix := " "
			if hasSelection && item.Key == current.Key {
				prefix = "›"
			}
			line := fmt.Sprintf("%s %-12s %-12s %s", prefix, clip(item.Name, 12), kindLabel(item), item.Text)
			if item.Stale {
				line += " (stale)"
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < height-4 {
		lines = append(lines, "")
	}
	message := v.Message
	if message == "" {
		message = state.Warning
	}
	lines = append(lines, message, strings.Repeat("─", width))
	if v.Prompt != "" {
		lines = append(lines, v.Prompt+": "+string(v.Input), "Enter saves · Ctrl+C cancels")
	} else if v.Details != nil {
		lines = append(lines, "i returns to the task list · q closes Tasks", "")
	} else {
		lines = append(lines, "↑↓ select · Enter open · i details · n note · z snooze · d close · a answer", "Tab scope · r refresh sources · q close")
	}
	for i, line := range lines {
		lines[i] = clip(line, width)
	}
	return strings.Join(lines[:min(height, len(lines))], "\r\n")
}

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func terminalSize() (int, int) {
	rows, columns := 24, 90
	if size, err := stty("size"); err == nil {
		_, _ = fmt.Sscan(size, &rows, &columns)
	}
	return max(1, columns), max(1, rows)
}

type actionResult struct {
	Message string
	Details *integration.Item
}

func boardAction(ctx context.Context, o options, action, key, project, text string) actionResult {
	if action == "refresh" {
		if err := o.Provider.Refresh(ctx); err != nil {
			return actionResult{Message: "Refresh failed: " + err.Error()}
		}
		return actionResult{Message: "Sources refreshed"}
	}
	var item integration.Item
	if action != "note" {
		fresh, err := selected(ctx, o.Provider, key)
		if err != nil {
			return actionResult{Message: err.Error()}
		}
		item = fresh
		if !slices.Contains(item.Actions, action) {
			return actionResult{Message: "This action is no longer available for the task"}
		}
		project = item.Project
	}
	if action == "open" {
		if err := o.bridge().Open(ctx, item); err != nil {
			return actionResult{Message: err.Error()}
		}
		return actionResult{Message: "Project opened"}
	}
	d, err := o.Provider.Describe(ctx)
	if err != nil {
		return actionResult{Message: err.Error()}
	}
	for _, entry := range d.Actions {
		if entry.ID != action {
			continue
		}
		result, err := o.Provider.Invoke(ctx, entry, map[string]string{"id": item.ID, "project": project, "text": text, "answer": text})
		if err != nil {
			return actionResult{Message: err.Error()}
		}
		if action == "show" {
			var details board.NoteJSON
			if err := json.Unmarshal(result, &details); err != nil {
				return actionResult{Message: err.Error()}
			}
			item.Item = details.Item
			return actionResult{Details: &item}
		}
		return actionResult{Message: map[string]string{"note": "Note saved in Ideas · press Tab to switch views", "done": "Note closed", "snooze": "Task snoozed", "reply": "Answer sent"}[action]}
	}
	return actionResult{Message: "Sous does not advertise this action"}
}

func boardPane(ctx context.Context, o options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	previous, err := stty("-g")
	if err != nil {
		return errors.New("tasks needs a terminal; open the tasks pane with herdr plugin pane open")
	}
	if _, err := stty("raw", "-echo"); err != nil {
		return err
	}
	defer func() { _, _ = stty(previous); fmt.Print("\x1b[?25h\x1b[?1049l") }()
	fmt.Print("\x1b[?1049h\x1b[?25l")
	view := View{Scope: "all"}
	if o.CallerCwd != "" {
		identity, _ := o.Provider.Project(ctx, o.CallerCwd)
		view.Project, view.Remote = identity.Path, identity.Remote
	}
	width, height := terminalSize()
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	keys := make(chan rune, 16)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			key, _, err := reader.ReadRune()
			if err != nil {
				cancel()
				return
			}
			select {
			case keys <- key:
			case <-ctx.Done():
				return
			}
		}
	}()
	results := make(chan actionResult, 1)
	busy := false
	defer func() {
		cancel()
		if busy {
			<-results
		}
	}()
	dispatch := func(action, key, project, text string) {
		busy, view.Message = true, "Working…"
		go func() { results <- boardAction(ctx, o, action, key, project, text) }()
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	lastRender, escape := "", 0
	state := bridgeState{Error: "waiting for sous"}
	for {
		if fresh, err := readState(o.bridge().State); err == nil {
			state = fresh
		} else {
			state.Available, state.Error = false, err.Error()
		}
		rendered := view.Render(state, width, height)
		if rendered != lastRender {
			fmt.Print("\x1b[H\x1b[2J" + rendered)
			lastRender = rendered
		}
		select {
		case <-ctx.Done():
			return nil
		case <-resize:
			width, height = terminalSize()
		case <-ticker.C:
		case result := <-results:
			busy, view.Message, view.Details = false, result.Message, result.Details
		case key := <-keys:
			if key == 27 {
				escape = 1
				continue
			}
			if escape > 0 {
				if escape == 1 && key == '[' {
					escape = 2
					continue
				}
				if escape == 2 && view.Prompt == "" {
					if key == 'A' {
						view.move(state, -1)
					}
					if key == 'B' {
						view.move(state, 1)
					}
				}
				escape = 0
				continue
			}
			item, hasItem := view.selected(state)
			if view.Prompt != "" {
				switch key {
				case 3:
					view.Prompt, view.Input = "", nil
				case 8, 127:
					if len(view.Input) > 0 {
						view.Input = view.Input[:len(view.Input)-1]
					}
				case '\r', '\n':
					text := string(view.Input)
					mode := view.Prompt
					view.Prompt, view.Input = "", nil
					project, key := view.PromptProject, view.PromptKey
					if text == "" {
						continue
					}
					switch mode {
					case "Remember a note":
						dispatch("note", "", project, text)
					case "Answer the run":
						dispatch("reply", key, project, text)
					case "Close this note? Type yes":
						if text == "yes" {
							dispatch("done", key, project, "")
						}
					}
				default:
					if key >= 32 && key != 127 && len(view.Input) < 8192 {
						view.Input = append(view.Input, key)
					}
				}
				continue
			}
			if key == 'q' || key == 3 {
				return nil
			}
			if key == 'i' && view.Details != nil {
				view.Details = nil
				continue
			}
			if view.Details != nil {
				continue
			}
			if busy {
				continue
			}
			switch key {
			case 'j':
				view.move(state, 1)
			case 'k':
				view.move(state, -1)
			case '\t':
				scopes := []string{"all", "space", "ideas", "snoozed"}
				view.Scope = scopes[(slices.Index(scopes, view.Scope)+1)%len(scopes)]
			case 'r':
				dispatch("refresh", "", "", "")
			case 'n':
				view.beginPrompt("Remember a note", item)
			case '\r', '\n':
				if hasItem {
					dispatch("open", item.Key, item.Project, "")
				}
			case 'i':
				if hasItem && slices.Contains(item.Actions, "show") {
					dispatch("show", item.Key, item.Project, "")
				}
			case 'z':
				if hasItem {
					dispatch("snooze", item.Key, item.Project, "")
				}
			case 'd':
				if hasItem && slices.Contains(item.Actions, "done") {
					view.beginPrompt("Close this note? Type yes", item)
				}
			case 'a':
				if hasItem && slices.Contains(item.Actions, "reply") {
					view.beginPrompt("Answer the run", item)
				}
			}
		}
	}
}
