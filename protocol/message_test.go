package protocol_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/lsongdev/irc-go/protocol"
)

func TestMessageRoundTrip(t *testing.T) {
	input := "@aaa=bbb;example.com/ddd=hello\\sworld :nick!user@host PRIVMSG #chan :hello there\r\n"
	m, err := protocol.ParseMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	if m.Tags["example.com/ddd"] != "hello world" || m.Prefix.Name != "nick" || m.Prefix.User != "user" || m.Prefix.Host != "host" {
		t.Fatalf("unexpected parse: %#v", m)
	}
	if m.Command != "PRIVMSG" || len(m.Params) != 2 || m.Params[1] != "hello there" {
		t.Fatalf("unexpected message: %#v", m)
	}
	encoded, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := protocol.ParseMessage(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if again.String() != m.String() {
		t.Fatalf("round trip mismatch:\n%s\n%s", again.String(), m.String())
	}
}

func TestMessageValidation(t *testing.T) {
	for _, line := range []string{"", ":only-prefix", "@=bad PING x", ":nick!@host PING x", "BAD1 x", "PRIVMSG one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen", "PING bad\nline"} {
		if _, err := protocol.ParseMessage(line); err == nil {
			t.Errorf("ParseMessage(%q) unexpectedly succeeded", line)
		}
	}
	long := "PRIVMSG #x :" + strings.Repeat("x", 500)
	if _, err := protocol.ParseMessage(long); !errors.Is(err, protocol.ErrLineTooLong) {
		t.Fatalf("expected line too long, got %v", err)
	}
}

func TestRFC1459CaseMapping(t *testing.T) {
	if protocol.FoldCase("Nick[\\^") != "nick{|~" {
		t.Fatal(protocol.FoldCase("Nick[\\^"))
	}
	for _, nick := range []string{"alice", "[bot]", "A-1"} {
		if !protocol.IsNickname(nick) {
			t.Errorf("valid nick rejected: %s", nick)
		}
	}
	for _, nick := range []string{"", "1alice", "bad nick"} {
		if protocol.IsNickname(nick) {
			t.Errorf("invalid nick accepted: %s", nick)
		}
	}
}

func TestEncodeTruncatedPreservesUTF8AndLimit(t *testing.T) {
	m := protocol.Message{Prefix: &protocol.Prefix{Name: "nick", User: "user", Host: "host"}, Command: "PRIVMSG", Params: []string{"#room", strings.Repeat("聊天", 100)}}
	wire, truncated, err := m.EncodeTruncated()
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(wire) > protocol.MaxLineBytes {
		t.Fatalf("truncated=%v length=%d", truncated, len(wire))
	}
	parsed, err := protocol.ParseMessage(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.Params[1], parsed.Params[1]) {
		t.Fatal("truncated text is not a prefix of original")
	}
}
