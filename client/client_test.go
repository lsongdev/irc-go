package client_test

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/lsongdev/irc-go/client"
)

func TestClientRepliesToPing(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	c := client.New(clientConn)
	defer c.Close()
	defer serverConn.Close()

	response := make(chan string, 1)
	go func() {
		_, _ = serverConn.Write([]byte("PING :token\r\n"))
		line, _ := bufio.NewReader(serverConn).ReadString('\n')
		response <- line
	}()

	select {
	case line := <-response:
		if line != "PONG token\r\n" {
			t.Fatalf("unexpected response %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PONG")
	}
}

func TestCommandHelpersValidateInput(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	c := client.New(clientConn)
	defer c.Close()
	defer serverConn.Close()

	checks := []struct {
		name string
		call func() error
	}{
		{"join", func() error { return c.Join("not-a-channel") }},
		{"part", func() error { return c.Part("", "") }},
		{"privmsg", func() error { return c.Privmsg("nick", "") }},
		{"notice", func() error { return c.Notice("", "text") }},
		{"nick", func() error { return c.NickChange("1bad") }},
		{"topic", func() error { return c.Topic("room", "topic") }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
