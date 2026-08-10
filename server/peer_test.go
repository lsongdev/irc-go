package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lsongdev/irc-go/protocol"
)

func TestNumericListSplitsAtWireLimit(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	s := New(Config{Name: "test.local"})
	p := newPeer(s, clientConn)
	p.nick = "reader"
	go p.writeLoop()
	defer p.close()
	defer serverConn.Close()

	items := make([]string, 80)
	for i := range items {
		items[i] = fmt.Sprintf("member%024d", i)
	}
	p.numericList(protocol.RPLNameReply, []string{"=", "#large"}, items)

	_ = serverConn.SetReadDeadline(time.Now().Add(time.Second))
	reader := bufio.NewReader(serverConn)
	var got []string
	lines := 0
	for len(got) < len(items) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if len(line) > protocol.MaxLineBytes {
			t.Fatalf("line is %d bytes", len(line))
		}
		m, err := protocol.ParseMessage(line)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Fields(m.Params[len(m.Params)-1])...)
		lines++
	}
	if lines < 2 {
		t.Fatal("expected list to require multiple replies")
	}
	if strings.Join(got, ",") != strings.Join(items, ",") {
		t.Fatal("split replies changed list contents")
	}
}
