package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	// ConnectTimeout is the maximum time allowed to establish a connection
	ConnectTimeout = 5 * time.Second

	// DefaultSendTimeout is the default timeout for sending messages
	DefaultSendTimeout = 10 * time.Second
)

// BaseClient provides common client functionality used by both
// Unix socket and Windows named pipe implementations.
type BaseClient struct {
	conn          net.Conn
	encoder       *json.Encoder
	reader        *messageReader
	handler       func(msg Message)
	pendingReplys map[string]chan Message
	mu            sync.RWMutex
	encodeMu      sync.Mutex // Protects encoder writes
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	closed        bool
}

// NewBaseClient creates a new BaseClient.
func NewBaseClient() *BaseClient {
	return &BaseClient{
		pendingReplys: make(map[string]chan Message),
	}
}

// OnMessage registers a handler for incoming messages from the server.
func (c *BaseClient) OnMessage(handler func(msg Message)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

// Send sends a message to the server.
func (c *BaseClient) Send(msg Message) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.RUnlock()

	c.encodeMu.Lock()
	defer c.encodeMu.Unlock()
	return c.encoder.Encode(msg)
}

// SendAndWait sends a message and waits for a response with matching ReplyTo.
func (c *BaseClient) SendAndWait(ctx context.Context, msg Message) (*Message, error) {
	// Create channel for response
	replyChan := make(chan Message, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.pendingReplys[msg.ID] = replyChan
	c.mu.Unlock()

	// Clean up on exit
	defer func() {
		c.mu.Lock()
		delete(c.pendingReplys, msg.ID)
		c.mu.Unlock()
	}()

	// Send the message
	if err := c.Send(msg); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case reply := <-replyChan:
		return &reply, nil
	}
}

// Close gracefully closes the connection.
func (c *BaseClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.conn != nil {
		c.conn.Close()
	}

	c.wg.Wait()
	return nil
}

// SetConnection sets the network connection.
// Called by platform-specific implementations after establishing the connection.
func (c *BaseClient) SetConnection(conn net.Conn) {
	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.reader = &messageReader{reader: bufio.NewReaderSize(conn, ReadBufferSize)}
}

// Authenticate sends the authentication message and waits for a response.
func (c *BaseClient) Authenticate(ctx context.Context, token string) error {
	authMsg, err := NewMessage(TypeAuth, AuthPayload{Token: token})
	if err != nil {
		return fmt.Errorf("failed to create auth message: %w", err)
	}

	// Send auth message
	if err := c.Send(authMsg); err != nil {
		return fmt.Errorf("failed to send auth message: %w", err)
	}

	// Read auth response with timeout
	authCtx, cancel := context.WithTimeout(ctx, AuthTimeout)
	defer cancel()

	decoder := c.reader

	// Set read deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(AuthTimeout)); err != nil {
		return fmt.Errorf("set IPC auth deadline: %w", err)
	}
	defer func() {
		if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
			slog.Warn("Failed to clear IPC auth deadline", "error", err)
		}
	}()

	responseChan := make(chan struct {
		msg Message
		err error
	}, 1)

	go func() {
		var msg Message
		err := decoder.Decode(&msg)
		responseChan <- struct {
			msg Message
			err error
		}{msg, err}
	}()

	select {
	case <-authCtx.Done():
		c.conn.Close()
		<-responseChan
		return fmt.Errorf("authentication timeout")
	case result := <-responseChan:
		if result.err != nil {
			return fmt.Errorf("failed to read auth response: %w", result.err)
		}

		if result.msg.Type != TypeAuthResponse {
			return fmt.Errorf("unexpected response type: %s", result.msg.Type)
		}

		var payload AuthResponsePayload
		if err := result.msg.ParsePayload(&payload); err != nil {
			return fmt.Errorf("failed to parse auth response: %w", err)
		}

		if !payload.Success {
			return fmt.Errorf("authentication failed: %s", payload.Error)
		}

		return nil
	}
}

// StartReadLoop starts the message reading loop in a goroutine.
func (c *BaseClient) StartReadLoop(ctx context.Context) {
	c.ctx, c.cancel = context.WithCancel(ctx)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.readLoop()
	}()
}

// readLoop reads messages from the server until the connection is closed.
func (c *BaseClient) readLoop() {
	decoder := c.reader

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				slog.Debug("server disconnected")
				return
			}
			select {
			case <-c.ctx.Done():
				return // Expected during shutdown
			default:
				slog.Debug("failed to read message", "error", err)
				return
			}
		}

		// Check if this is a response to a pending request
		if msg.ReplyTo != "" {
			c.mu.RLock()
			replyChan, ok := c.pendingReplys[msg.ReplyTo]
			c.mu.RUnlock()

			if ok {
				select {
				case replyChan <- msg:
				default:
					// Channel full, drop message
					slog.Warn("dropped reply message, channel full", "reply_to", msg.ReplyTo)
				}
				continue
			}
		}

		// Dispatch to handler
		c.mu.RLock()
		handler := c.handler
		c.mu.RUnlock()

		if handler != nil {
			handler(msg)
		}
	}
}
