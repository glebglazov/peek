package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// request and response are one JSON line each, exchanged over the control
// socket and then the connection closes. The CLI prints Lines as they come.
type request struct {
	Command string `json:"command"`
	Path    string `json:"path,omitempty"`
	Alias   string `json:"alias,omitempty"`
}

type response struct {
	Error string   `json:"error,omitempty"`
	Lines []string `json:"lines,omitempty"`
}

// Control is the daemon half: it turns socket requests into registry changes
// and answers with the URLs a person can paste into a browser.
type Control struct {
	registry *Registry
	baseURL  string
}

// Listen binds the control socket before the daemon announces itself, so a
// second daemon fails while the first still owns the socket.
func (c *Control) Listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(socket), err)
	}
	if err := clearStaleSocket(socket); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socket, err)
	}
	return listener, nil
}

func (c *Control) Accept(listener net.Listener) error {
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("accept on %s: %w", listener.Addr(), err)
		}
		go c.answer(conn)
	}
}

func (c *Control) answer(conn net.Conn) {
	defer conn.Close()

	var req request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(response{Error: "unreadable request: " + err.Error()})
		return
	}
	json.NewEncoder(conn).Encode(c.handle(req))
}

func (c *Control) handle(req request) response {
	switch req.Command {
	case "add":
		share, err := c.registry.Add(req.Path, req.Alias)
		if err != nil {
			return response{Error: err.Error()}
		}
		return response{Lines: []string{shareURL(c.baseURL, share.Alias)}}

	case "rm":
		share, err := c.registry.Remove(req.Alias)
		if err != nil {
			return response{Error: err.Error()}
		}
		return response{Lines: []string{"removed " + share.Alias}}

	case "list":
		shares := c.registry.ListByNewest()
		if len(shares) == 0 {
			return response{Lines: []string{"no shares"}}
		}
		lines := make([]string, 0, len(shares))
		for _, share := range shares {
			lines = append(lines, fmt.Sprintf("%-24s %-16s %s", shareURL(c.baseURL, share.Alias), modifiedAt(share.Modified), share.Path))
		}
		return response{Lines: lines}

	default:
		return response{Error: fmt.Sprintf("unknown command %q", req.Command)}
	}
}

// ask is the CLI half: one round trip to a daemon that must already run.
func ask(socket string, req request) ([]string, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, errors.New("peek is not running; start it with: peek serve")
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read answer: %w", err)
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Lines, nil
}

// A socket file outlives a daemon that was killed. Only a socket nothing
// answers on may be removed, so a running daemon is never displaced.
func clearStaleSocket(socket string) error {
	if _, err := os.Stat(socket); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if conn, err := net.Dial("unix", socket); err == nil {
		conn.Close()
		return fmt.Errorf("peek already runs on %s", socket)
	}
	if err := os.Remove(socket); err != nil {
		return fmt.Errorf("remove stale socket %s: %w", socket, err)
	}
	return nil
}
