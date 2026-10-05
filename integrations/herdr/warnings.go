package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/bilal-/sous/internal/store"
)

var warningLabels = map[string]string{
	"notification": "Notification could not be delivered",
	"sidebar":      "Sidebar could not be updated",
	"refresh":      "Remote refresh failed",
}

type bridgeFormat struct{}

func (bridgeFormat) Current() int { return 2 }
func (bridgeFormat) Empty() []byte {
	return []byte(`{"version":2,"available":false,"snapshot":null,"error":"waiting for sous","warnings":{}}`)
}
func (bridgeFormat) Migrate(from int, raw []byte) ([]byte, error) {
	if from != 1 {
		return nil, fmt.Errorf("no migration from v%d", from)
	}
	var prior struct {
		bridgeState
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal(raw, &prior); err != nil {
		return nil, err
	}
	prior.Version, prior.Warnings = 2, map[string]string{}
	if prior.Warning != "" {
		source := "legacy"
		for name, label := range warningLabels {
			if strings.HasPrefix(prior.Warning, label+": ") {
				source = name
				break
			}
		}
		prior.Warnings[source] = prior.Warning
	}
	return json.Marshal(prior.bridgeState)
}

func (s bridgeState) Warning() string {
	messages := make([]string, 0, len(s.Warnings))
	for _, message := range s.Warnings {
		messages = append(messages, message)
	}
	slices.Sort(messages)
	return strings.Join(messages, "; ")
}

func setWarning(s *bridgeState, source string, err error) {
	if s.Warnings == nil {
		s.Warnings = map[string]string{}
	}
	if err == nil {
		delete(s.Warnings, source)
	} else {
		s.Warnings[source] = warningLabels[source] + ": " + err.Error()
	}
}

func (b *Bridge) warning(source string, err error) {
	_, _ = store.Modify[bridgeState](b.State, "bridge", stateFile, func(s *bridgeState) error {
		setWarning(s, source, err)
		return nil
	})
}

func (b *Bridge) Refresh(ctx context.Context) error {
	p, ok := b.Provider.(interface{ Refresh(context.Context) error })
	if !ok {
		return fmt.Errorf("task provider cannot refresh")
	}
	err := p.Refresh(ctx)
	if ctx.Err() == nil {
		b.warning("refresh", err)
	}
	return err
}
