package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/integration"
)

type Provider interface {
	Describe(context.Context) (integration.Description, error)
	Read(context.Context) (integration.Snapshot, error)
	Watch(context.Context, func(integration.Event) error) error
	Project(context.Context, string) (string, error)
}

type CLIProvider struct {
	Binary string
	Env    []string
}

func (p *CLIProvider) command(ctx context.Context, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, p.Binary, args...)
	cmd.Env = p.Env
	cmd.WaitDelay = time.Second
	return cmd
}

func (p *CLIProvider) call(ctx context.Context, args []string, cwd string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := p.command(ctx, args)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(out, &failure) == nil && failure.Error != "" {
			return nil, errors.New(failure.Error)
		}
		return nil, fmt.Errorf("sous: %s (%w)", strings.TrimSpace(stderr.String()), err)
	}
	return out, nil
}

func (p *CLIProvider) Describe(ctx context.Context) (integration.Description, error) {
	b, err := p.call(ctx, []string{"integration", "--json"}, "")
	var d integration.Description
	if err == nil {
		err = json.Unmarshal(b, &d)
	}
	if err == nil && (d.V != integration.Version || d.Protocol != integration.Protocol || d.Provider != "sous") {
		err = errors.New("unsupported sous integration protocol; update sous and the herdr plugin together")
	}
	return d, err
}

func (p *CLIProvider) Read(ctx context.Context) (integration.Snapshot, error) {
	b, err := p.call(ctx, []string{"integration", "snapshot", "--json"}, "")
	var s integration.Snapshot
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	return s, err
}

func (p *CLIProvider) Watch(ctx context.Context, emit func(integration.Event) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := p.command(ctx, []string{"integration", "watch", "--json"})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var event integration.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			cancel()
			_ = cmd.Wait()
			return fmt.Errorf("invalid sous subscription event: %w", err)
		}
		if err := emit(event); err != nil {
			cancel()
			_ = cmd.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		cancel()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("sous subscription ended: %s (%w)", strings.TrimSpace(stderr.String()), err)
	}
	return ctx.Err()
}

func (p *CLIProvider) Project(ctx context.Context, cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := p.call(ctx, []string{"here", "--json", "--", cwd}, "")
	var result struct {
		Project string `json:"project"`
	}
	if err == nil {
		err = json.Unmarshal(b, &result)
	}
	if err == nil && result.Project == "" {
		err = errors.New("sous did not identify a project")
	}
	return result.Project, err
}

func bindAction(binary string, action integration.Action, values map[string]string) ([]string, string, error) {
	if len(action.Argv) == 0 || action.Argv[0] != "sous" {
		return nil, "", errors.New("action does not invoke sous")
	}
	for _, parameter := range action.Parameters {
		if values[parameter] == "" {
			return nil, "", fmt.Errorf("missing %s for %s", parameter, action.Title)
		}
	}
	substitute := func(arg string) (string, error) {
		if strings.HasPrefix(arg, "{") && strings.HasSuffix(arg, "}") {
			key := strings.TrimSuffix(strings.TrimPrefix(arg, "{"), "}")
			if value, ok := values[key]; ok {
				return value, nil
			}
			return "", fmt.Errorf("unknown action parameter %s", key)
		}
		return arg, nil
	}
	args := append([]string{}, action.Argv...)
	args[0] = binary
	for i := 1; i < len(args); i++ {
		value, err := substitute(args[i])
		if err != nil {
			return nil, "", err
		}
		args[i] = value
	}
	cwd, err := substitute(action.Cwd)
	return args, cwd, err
}

func (p *CLIProvider) Invoke(ctx context.Context, action integration.Action, values map[string]string) (json.RawMessage, error) {
	if action.Terminal {
		return nil, errors.New("this action requires a herdr terminal pane")
	}
	args, cwd, err := bindAction(p.Binary, action, values)
	if err != nil {
		return nil, err
	}
	return p.call(ctx, args[1:], cwd)
}

func (p *CLIProvider) Refresh(ctx context.Context) error {
	_, err := p.call(ctx, []string{"--refresh"}, "")
	return err
}
