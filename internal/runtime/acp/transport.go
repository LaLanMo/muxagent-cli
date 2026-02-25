package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Transport manages a JSON-RPC 2.0 connection over stdio to a child process.
type Transport struct {
	command string
	args    []string
	cwd     string
	env     map[string]string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *Response

	notifications chan *Notification
	requests      chan *IncomingMessage // agent→client requests (e.g. permission)

	done chan struct{}
}

// NewTransport creates a new transport for the given command.
func NewTransport(command string, args []string, cwd string, env map[string]string) *Transport {
	return &Transport{
		command:       command,
		args:          args,
		cwd:           cwd,
		env:           env,
		pending:       make(map[int64]chan *Response),
		notifications: make(chan *Notification, 64),
		requests:      make(chan *IncomingMessage, 16),
		done:          make(chan struct{}),
	}
}

// Start spawns the child process and begins reading from stdout.
func (t *Transport) Start(ctx context.Context) error {
	t.cmd = exec.CommandContext(ctx, t.command, t.args...)
	if t.cwd != "" {
		t.cmd.Dir = t.cwd
	}
	t.cmd.Env = append(t.cmd.Environ(), formatEnv(t.env)...)

	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	// Drain stderr to log
	go func() {
		scanner := bufio.NewScanner(t.stderr)
		scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
		for scanner.Scan() {
			log.Printf("[acp-stderr] %s", scanner.Text())
		}
	}()

	// Read loop for stdout
	go t.readLoop()

	return nil
}

// Stop gracefully terminates the child process.
func (t *Transport) Stop() error {
	// Signal the process to terminate
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Signal(syscall.SIGTERM)

		// Wait up to 3 seconds for graceful exit
		done := make(chan error, 1)
		go func() { done <- t.cmd.Wait() }()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = t.cmd.Process.Kill()
			<-done
		}
	}

	// Close channels
	select {
	case <-t.done:
	default:
		close(t.done)
	}

	// Fail all pending calls
	t.mu.Lock()
	for id, ch := range t.pending {
		ch <- &Response{
			ID:    id,
			Error: &RPCError{Code: -1, Message: "transport stopped"},
		}
		delete(t.pending, id)
	}
	t.mu.Unlock()

	return nil
}

// Call sends a JSON-RPC request and waits for the response.
func (t *Transport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	ch := make(chan *Response, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsBytes,
	}
	if err := t.send(req); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (t *Transport) Notify(method string, params any) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	return t.send(Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	})
}

// Respond sends a response to an agent→client request.
func (t *Transport) Respond(id int64, result any) error {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return t.send(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultBytes,
	})
}

// Notifications returns the channel for receiving agent notifications.
func (t *Transport) Notifications() <-chan *Notification {
	return t.notifications
}

// Requests returns the channel for receiving agent→client requests.
func (t *Transport) Requests() <-chan *IncomingMessage {
	return t.requests
}

func (t *Transport) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = t.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

func (t *Transport) readLoop() {
	scanner := bufio.NewScanner(t.stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg IncomingMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("[acp] failed to parse message: %v", err)
			continue
		}

		if msg.IsResponse() {
			t.mu.Lock()
			ch, ok := t.pending[*msg.ID]
			if ok {
				delete(t.pending, *msg.ID)
			}
			t.mu.Unlock()

			if ok {
				ch <- &Response{
					JSONRPC: msg.JSONRPC,
					ID:      *msg.ID,
					Result:  msg.Result,
					Error:   msg.Error,
				}
			}
		} else if msg.IsNotification() {
			select {
			case t.notifications <- &Notification{
				JSONRPC: msg.JSONRPC,
				Method:  msg.Method,
				Params:  msg.Params,
			}:
			default:
				log.Printf("[acp] notification channel full, dropping: %s", msg.Method)
			}
		} else if msg.IsRequest() {
			select {
			case t.requests <- &msg:
			default:
				log.Printf("[acp] request channel full, dropping: %s", msg.Method)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[acp] stdout scanner error: %v", err)
	}
}

func formatEnv(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}
