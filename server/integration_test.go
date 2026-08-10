package server_test

import (
	"context"
	"net"
	"testing"
	"time"

	ircclient "github.com/lsongdev/irc-go/client"
	"github.com/lsongdev/irc-go/protocol"
	"github.com/lsongdev/irc-go/server"
)

func startServer(t *testing.T) *server.Server {
	t.Helper()
	return startServerWithConfig(t, server.Config{Name: "test.local", Network: "testnet", MOTD: []string{"hello tests"}})
}

func startServerWithConfig(t *testing.T, cfg server.Config) *server.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := server.New(cfg)
	go func() {
		if err := s.Serve(ln); err != nil {
			t.Errorf("Serve: %v", err)
		}
	}()
	deadline := time.Now().Add(time.Second)
	for s.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return s
}

func connect(t *testing.T, s *server.Server, nick string) *ircclient.Client {
	t.Helper()
	c, err := ircclient.Dial(ircclient.Config{Address: s.Addr().String(), Nick: nick, RealName: nick + " Person"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	select {
	case <-c.Ready():
	case err := <-c.Errors():
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("registration timeout")
	}
	return c
}

func waitFor(t *testing.T, c *ircclient.Client, command string, match func(protocol.Message) bool) protocol.Message {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case m, ok := <-c.Messages():
			if !ok {
				t.Fatal("connection closed")
			}
			if m.Command == command && (match == nil || match(m)) {
				return m
			}
		case err := <-c.Errors():
			if err != nil {
				t.Fatal(err)
			}
		case <-timer.C:
			t.Fatalf("timeout waiting for %s", command)
		}
	}
}

func TestRegistrationChannelChatAndNickChange(t *testing.T) {
	s := startServer(t)
	alice := connect(t, s, "Alice")
	bob := connect(t, s, "Bob")
	if err := alice.Join("#go"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, alice, "JOIN", nil)
	if err := bob.Join("#go"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, bob, "JOIN", nil)
	waitFor(t, alice, "JOIN", func(m protocol.Message) bool { return m.Prefix != nil && m.Prefix.Name == "Bob" })
	if err := alice.Privmsg("#go", "hello Bob"); err != nil {
		t.Fatal(err)
	}
	msg := waitFor(t, bob, "PRIVMSG", nil)
	if msg.Prefix.Name != "Alice" || msg.Params[1] != "hello Bob" {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if err := bob.NickChange("Robert"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, alice, "NICK", func(m protocol.Message) bool { return m.Params[0] == "Robert" })
	if err := alice.Privmsg("robert", "direct"); err != nil {
		t.Fatal(err)
	}
	msg = waitFor(t, bob, "PRIVMSG", func(m protocol.Message) bool { return m.Params[0] == "Robert" })
	if msg.Params[1] != "direct" {
		t.Fatal(msg)
	}
}

func TestChannelOperatorTopicModeAndKick(t *testing.T) {
	s := startServer(t)
	op := connect(t, s, "Op")
	guest := connect(t, s, "Guest")
	_ = op.Join("#room")
	waitFor(t, op, "JOIN", nil)
	_ = guest.Join("#room")
	waitFor(t, guest, "JOIN", nil)
	waitFor(t, op, "JOIN", func(m protocol.Message) bool { return m.Prefix.Name == "Guest" })
	if err := op.Topic("#room", "A fine room"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, guest, "TOPIC", nil)
	if err := op.Mode("#room", "+m"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, guest, "MODE", nil)
	_ = guest.Privmsg("#room", "blocked")
	waitFor(t, guest, protocol.ERRCannotSendToChan, nil)
	if err := op.Mode("#room", "+v", "Guest"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, guest, "MODE", nil)
	_ = guest.Privmsg("#room", "allowed")
	waitFor(t, op, "PRIVMSG", func(m protocol.Message) bool { return m.Params[1] == "allowed" })
	_ = op.Raw("KICK", "#room", "Guest", "bye")
	waitFor(t, guest, "KICK", nil)
}

func TestDuplicateNickRejected(t *testing.T) {
	s := startServer(t)
	_ = connect(t, s, "Same")
	c, err := ircclient.Dial(ircclient.Config{Address: s.Addr().String(), Nick: "same"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitFor(t, c, protocol.ERRNicknameInUse, nil)
}

func TestPasswordFailureIsDeliveredBeforeClose(t *testing.T) {
	s := startServerWithConfig(t, server.Config{Name: "test.local", Password: "correct"})
	c, err := ircclient.Dial(ircclient.Config{Address: s.Addr().String(), Nick: "NoPass", Password: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	m := waitFor(t, c, protocol.ERRPasswdMismatch, nil)
	if got := m.Params[len(m.Params)-1]; got != "Password incorrect" {
		t.Fatalf("unexpected error: %#v", m)
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("server did not close rejected connection")
	}
}

func TestUserModeAndUnknownChannelMode(t *testing.T) {
	s := startServer(t)
	c := connect(t, s, "ModeUser")
	if err := c.Mode("ModeUser", "+i"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "MODE", func(m protocol.Message) bool { return len(m.Params) > 1 && m.Params[1] == "+i" })
	if err := c.Raw("MODE", "ModeUser"); err != nil {
		t.Fatal(err)
	}
	m := waitFor(t, c, protocol.RPLUModeIs, nil)
	if m.Params[len(m.Params)-1] != "+i" {
		t.Fatalf("unexpected user modes: %#v", m)
	}
	_ = c.Join("#modes")
	waitFor(t, c, "JOIN", nil)
	_ = c.Mode("#modes", "+z")
	waitFor(t, c, protocol.ERRUnknownMode, nil)
}
