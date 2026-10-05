package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

const pluginID = "sous.tasks"

type Host interface {
	Call(context.Context, string, any, any) error
	Monitor(context.Context) error
}

type Workspace struct {
	ID string `json:"workspace_id"`
}

type Pane struct {
	ID        string            `json:"pane_id"`
	Workspace string            `json:"workspace_id"`
	Cwd       string            `json:"cwd"`
	Tokens    map[string]string `json:"tokens"`
}

type Session struct {
	Workspaces []Workspace `json:"workspaces"`
	Panes      []Pane      `json:"panes"`
}

type SocketHost struct{ Path string }

type hostError struct{ Code, Message string }

func (e *hostError) Error() string { return "herdr " + e.Code + ": " + e.Message }

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r response) err() error {
	if r.Error == nil {
		return nil
	}
	return &hostError{r.Error.Code, r.Error.Message}
}

func (h SocketHost) connect(ctx context.Context) (net.Conn, error) {
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", h.Path)
}

func request(w io.Writer, method string, params any) error {
	return json.NewEncoder(w).Encode(map[string]any{"id": "sous", "method": method, "params": params})
}

func (h SocketHost) Call(ctx context.Context, method string, params any, out any) error {
	conn, err := h.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := request(conn, method, params); err != nil {
		return err
	}
	var reply response
	if err := json.NewDecoder(io.LimitReader(conn, 16<<20)).Decode(&reply); err != nil {
		return err
	}
	if reply.ID != "sous" {
		return errors.New("herdr response has an unexpected request ID")
	}
	if err := reply.err(); err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(reply.Result, out)
	}
	return nil
}

// A subscription binds the observer to this server's lifetime. It never
// changes focus and does not treat event payloads as authoritative state.
func (h SocketHost) Monitor(ctx context.Context) error {
	for ctx.Err() == nil {
		err := h.monitor(ctx)
		var failure *hostError
		if errors.As(err, &failure) && failure.Code == "events_lost" {
			continue // the heartbeat reconciles through authoritative snapshots
		}
		return err
	}
	return ctx.Err()
}

func (h SocketHost) monitor(ctx context.Context) error {
	conn, err := h.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := request(conn, "events.subscribe", map[string]any{"subscriptions": []map[string]string{{"type": "workspace.created"}}}); err != nil {
		return err
	}
	decoder := json.NewDecoder(conn)
	var reply response
	if err := decoder.Decode(&reply); err != nil {
		return err
	}
	if err := reply.err(); err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Time{})
	for {
		if err := decoder.Decode(&reply); err != nil {
			return err
		}
		if err := reply.err(); err != nil {
			return err
		}
	}
}

func session(ctx context.Context, host Host) (Session, error) {
	var result struct {
		Session
		Snapshot *Session `json:"snapshot"`
	}
	if err := host.Call(ctx, "session.snapshot", map[string]any{}, &result); err != nil {
		return Session{}, err
	}
	if result.Snapshot != nil {
		return *result.Snapshot, nil
	}
	return result.Session, nil
}
