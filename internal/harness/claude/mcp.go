package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const defaultProtocol = "2025-06-18"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// channelServer is the small MCP surface Claude needs for its one-way channel.
type channelServer struct {
	version string
	in      io.Reader
	out     io.Writer
	mu      sync.Mutex
}

func newChannelServer(version string, in io.Reader, out io.Writer) *channelServer {
	return &channelServer{version: version, in: in, out: out}
}

func (s *channelServer) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode notification: %w", err)
	}
	return s.write(message{JSONRPC: "2.0", Method: method, Params: raw})
}

func (s *channelServer) Serve(ctx context.Context) error {
	lines := bufio.NewScanner(s.in)
	lines.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for lines.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		var req message
		if err := json.Unmarshal(lines.Bytes(), &req); err != nil {
			continue
		}
		if err := s.handle(req); err != nil {
			return err
		}
	}
	if err := lines.Err(); err != nil {
		return fmt.Errorf("read stdio: %w", err)
	}
	return nil
}

func (s *channelServer) handle(req message) error {
	if len(req.ID) == 0 {
		return nil
	}
	switch req.Method {
	case "initialize":
		return s.write(message{JSONRPC: "2.0", ID: req.ID, Result: s.initializeResult(req.Params)})
	case "ping":
		return s.write(message{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	default:
		return s.write(message{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
	}
}

func (s *channelServer) initializeResult(params json.RawMessage) map[string]any {
	return map[string]any{
		"protocolVersion": s.negotiate(params),
		"capabilities": map[string]any{
			"experimental": map[string]any{capability: map[string]any{}},
		},
		"serverInfo":   map[string]any{"name": Name, "version": s.version},
		"instructions": instructions,
	}
}

func (s *channelServer) negotiate(params json.RawMessage) string {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ProtocolVersion == "" || p.ProtocolVersion == rejectedProtocol {
		return defaultProtocol
	}
	return p.ProtocolVersion
}

func (s *channelServer) write(m message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.out.Write(append(raw, '\n'))
	return err
}
