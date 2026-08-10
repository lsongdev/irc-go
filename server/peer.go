package server

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/lsongdev/irc-go/protocol"
)

type peer struct {
	server                                     *Server
	conn                                       net.Conn
	send                                       chan outbound
	done                                       chan struct{}
	closeOnce                                  sync.Once
	gone                                       bool
	nick, user, realname, password, host, away string
	registered, capNegotiating, closing        bool
	invisible                                  bool
	joined                                     map[*channel]struct{}
	connected                                  time.Time
}

type outbound struct {
	message protocol.Message
	final   bool
}

func newPeer(s *Server, conn net.Conn) *peer {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		host = conn.RemoteAddr().String()
	}
	return &peer{server: s, conn: conn, send: make(chan outbound, 128), done: make(chan struct{}), host: host, joined: make(map[*channel]struct{}), connected: time.Now()}
}

func (p *peer) run() {
	defer func() {
		p.server.mu.Lock()
		p.server.removePeer(p, "Connection closed")
		p.server.mu.Unlock()
	}()
	go p.writeLoop()
	scanner := bufio.NewScanner(p.conn)
	scanner.Buffer(make([]byte, 512), protocol.MaxLineBytes)
	for scanner.Scan() {
		m, err := protocol.ParseMessage(scanner.Text())
		if err != nil {
			continue
		}
		p.server.dispatch(p, m)
		select {
		case <-p.done:
			return
		default:
		}
	}
}

func (p *peer) writeLoop() {
	for {
		select {
		case out := <-p.send:
			line, _, err := out.message.EncodeTruncated()
			if err != nil {
				continue
			}
			_ = p.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err = p.conn.Write([]byte(line)); err != nil {
				p.close()
				return
			}
			if out.final {
				p.close()
				return
			}
		case <-p.done:
			return
		}
	}
}

func (p *peer) close() { p.closeOnce.Do(func() { close(p.done); _ = p.conn.Close() }) }

func (p *peer) queue(m protocol.Message) {
	select {
	case p.send <- outbound{message: m}:
	default:
		p.close()
	}
}

func (p *peer) queueFinal(m protocol.Message) {
	p.closing = true
	select {
	case p.send <- outbound{message: m, final: true}:
	default:
		p.close()
	}
}

func (p *peer) prefix() protocol.Prefix {
	return protocol.Prefix{Name: p.nick, User: p.user, Host: p.host}
}

func (p *peer) numeric(code string, params ...string) {
	nick := p.nick
	if nick == "" {
		nick = "*"
	}
	p.queue(protocol.Message{Prefix: &protocol.Prefix{Name: p.server.cfg.Name}, Command: code, Params: append([]string{nick}, params...)})
}

func (p *peer) numericList(code string, fixed []string, items []string) {
	nick := p.nick
	if nick == "" {
		nick = "*"
	}
	base := append([]string{nick}, fixed...)
	var chunk []string
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		params := append(append([]string(nil), base...), strings.Join(chunk, " "))
		p.queue(protocol.Message{Prefix: &protocol.Prefix{Name: p.server.cfg.Name}, Command: code, Params: params})
		chunk = nil
	}
	for _, item := range items {
		candidate := append(append([]string(nil), chunk...), item)
		params := append(append([]string(nil), base...), strings.Join(candidate, " "))
		m := protocol.Message{Prefix: &protocol.Prefix{Name: p.server.cfg.Name}, Command: code, Params: params}
		if _, err := m.Encode(); err != nil && len(chunk) > 0 {
			flush()
		}
		chunk = append(chunk, item)
	}
	flush()
}

func (s *Server) removePeer(p *peer, reason string) {
	if p.gone {
		return
	}
	p.gone = true
	if p.registered {
		quit := protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "QUIT", Params: []string{reason}}
		recipients := make(map[*peer]bool)
		for ch := range p.joined {
			for member := range ch.members {
				if member != p {
					recipients[member] = true
				}
			}
			s.removeFromChannel(p, ch)
		}
		for other := range recipients {
			other.queue(quit)
		}
		delete(s.users, protocol.FoldCase(p.nick))
	}
	delete(s.peers, p)
	p.close()
}

func (s *Server) removeFromChannel(p *peer, ch *channel) {
	delete(ch.members, p)
	delete(p.joined, ch)
	if len(ch.members) == 0 {
		delete(s.channels, protocol.FoldCase(ch.name))
		return
	}
	for _, membership := range ch.members {
		if membership.operator {
			return
		}
	}
	for member, membership := range ch.members {
		membership.operator = true
		s.broadcast(ch, protocol.Message{Prefix: &protocol.Prefix{Name: s.cfg.Name}, Command: "MODE", Params: []string{ch.name, "+o", member.nick}}, nil)
		return
	}
}

func prefixPtr(p protocol.Prefix) *protocol.Prefix { return &p }
func lastParam(m protocol.Message) string {
	if len(m.Params) == 0 {
		return ""
	}
	return m.Params[len(m.Params)-1]
}
func splitComma(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
