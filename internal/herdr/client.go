// Package herdr is a thin client for the herdr socket API: newline-delimited
// JSON requests ({id, method, params}) answered by {id, result} or {id, error}.
package herdr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
)

// SocketPath resolves the API socket: HERDR_SOCKET_PATH, else the default location.
func SocketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "herdr", "herdr.sock")
}

// Error is an error response from the server.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("herdr: %s: %s", e.Code, e.Message) }

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

var seq atomic.Uint64

func nextID() string { return "fleet-" + strconv.FormatUint(seq.Add(1), 10) }

// Client issues one-shot requests, each on its own connection.
type Client struct{ Socket string }

func New() *Client { return &Client{Socket: SocketPath()} }

// Call sends a request and decodes the result into out (may be nil).
func (c *Client) Call(method string, params, out any) error {
	if params == nil {
		params = struct{}{}
	}
	conn, err := net.Dial("unix", c.Socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{nextID(), method, params}); err != nil {
		return err
	}
	var resp response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}
