// Package client provides a concurrent, embeddable IRC client.
package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lsongdev/irc-go/protocol"
)

type Config struct {
	Address  string
	Password string
	Nick     string
	Username string
	RealName string
	Timeout  time.Duration
}

type Client struct {
	conn      net.Conn
	mu        sync.Mutex
	stateMu   sync.RWMutex
	nick      string
	messages  chan protocol.Message
	errs      chan error
	ready     chan struct{}
	done      chan struct{}
	readyOnce sync.Once
	closeOnce sync.Once
}

// DialContext connects, starts the read loop, and sends IRC registration.
func DialContext(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Address == "" {
		cfg.Address = "localhost:6667"
	}
	if !protocol.IsNickname(cfg.Nick) {
		return nil, fmt.Errorf("irc client: invalid nickname %q", cfg.Nick)
	}
	if cfg.Username == "" {
		cfg.Username = cfg.Nick
	}
	if cfg.RealName == "" {
		cfg.RealName = cfg.Nick
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", cfg.Address)
	if err != nil {
		return nil, err
	}
	c := New(conn)
	c.stateMu.Lock()
	c.nick = cfg.Nick
	c.stateMu.Unlock()
	if cfg.Password != "" {
		if err = c.Send(protocol.Message{Command: "PASS", Params: []string{cfg.Password}}); err != nil {
			c.Close()
			return nil, err
		}
	}
	if err = c.Send(protocol.Message{Command: "NICK", Params: []string{cfg.Nick}}); err != nil {
		c.Close()
		return nil, err
	}
	if err = c.Send(protocol.Message{Command: "USER", Params: []string{cfg.Username, "0", "*", cfg.RealName}}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func Dial(cfg Config) (*Client, error) { return DialContext(context.Background(), cfg) }

// New wraps an established connection and immediately starts reading it.
func New(conn net.Conn) *Client {
	c := &Client{conn: conn, messages: make(chan protocol.Message, 256), errs: make(chan error, 1), ready: make(chan struct{}), done: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *Client) Messages() <-chan protocol.Message { return c.messages }
func (c *Client) Errors() <-chan error              { return c.errs }
func (c *Client) Ready() <-chan struct{}            { return c.ready }
func (c *Client) Done() <-chan struct{}             { return c.done }

// WaitReady waits until the server accepts registration.
func (c *Client) WaitReady(ctx context.Context) error {
	select {
	case <-c.ready:
		return nil
	case err := <-c.errs:
		if err == nil {
			return net.ErrClosed
		}
		return err
	case <-c.done:
		return net.ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) Nick() string { c.stateMu.RLock(); defer c.stateMu.RUnlock(); return c.nick }

func (c *Client) Send(m protocol.Message) error {
	line, err := m.Encode()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return net.ErrClosed
	default:
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err = c.conn.Write([]byte(line))
	return err
}

func (c *Client) readLoop() {
	defer func() { c.Close(); close(c.messages); close(c.errs) }()
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 512), protocol.MaxLineBytes)
	for scanner.Scan() {
		m, err := protocol.ParseMessage(scanner.Text())
		if err != nil {
			c.report(err)
			continue
		}
		if m.Command == "PING" && len(m.Params) > 0 {
			_ = c.Send(protocol.Message{Command: "PONG", Params: m.Params})
		}
		c.observe(m)
		select {
		case c.messages <- m:
		case <-c.done:
			return
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		c.report(err)
	}
}

func (c *Client) observe(m protocol.Message) {
	if m.Command == protocol.RPLWelcome {
		c.readyOnce.Do(func() { close(c.ready) })
	}
	if m.Command == "NICK" && m.Prefix != nil && protocol.FoldCase(m.Prefix.Name) == protocol.FoldCase(c.Nick()) && len(m.Params) > 0 {
		c.stateMu.Lock()
		c.nick = m.Params[0]
		c.stateMu.Unlock()
	}
}
func (c *Client) report(err error) {
	select {
	case c.errs <- err:
	default:
	}
}

// Close closes the transport. Use Quit to send a graceful IRC QUIT first.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() { close(c.done); err = c.conn.Close() })
	return err
}
