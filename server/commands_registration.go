package server

import (
	"strings"

	"github.com/lsongdev/irc-go/protocol"
)

func (s *Server) cmdCAP(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "CAP", "Not enough parameters")
		return
	}
	sub := strings.ToUpper(m.Params[0])
	switch sub {
	case "LS":
		p.capNegotiating = true
		p.queue(protocol.Message{Prefix: &protocol.Prefix{Name: s.cfg.Name}, Command: "CAP", Params: []string{"*", "LS", ""}})
	case "LIST":
		p.queue(protocol.Message{Prefix: &protocol.Prefix{Name: s.cfg.Name}, Command: "CAP", Params: []string{"*", "LIST", ""}})
	case "REQ":
		req := lastParam(m)
		p.queue(protocol.Message{Prefix: &protocol.Prefix{Name: s.cfg.Name}, Command: "CAP", Params: []string{"*", "NAK", req}})
	case "END":
		p.capNegotiating = false
	}
}

func (s *Server) cmdPASS(p *peer, m protocol.Message) {
	if p.registered {
		p.numeric(protocol.ERRAlreadyRegistered, "You may not reregister")
		return
	}
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "PASS", "Not enough parameters")
		return
	}
	p.password = m.Params[0]
}

func (s *Server) cmdNICK(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNoNicknameGiven, "No nickname given")
		return
	}
	nick := m.Params[0]
	if !protocol.IsNickname(nick) {
		p.numeric(protocol.ERRErroneousNickname, nick, "Erroneous nickname")
		return
	}
	if existing := s.users[protocol.FoldCase(nick)]; existing != nil && existing != p {
		p.numeric(protocol.ERRNicknameInUse, nick, "Nickname is already in use")
		return
	}
	old := p.nick
	if p.registered {
		delete(s.users, protocol.FoldCase(old))
		s.users[protocol.FoldCase(nick)] = p
		msg := protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "NICK", Params: []string{nick}}
		recipients := map[*peer]bool{p: true}
		for ch := range p.joined {
			for member := range ch.members {
				recipients[member] = true
			}
		}
		for member := range recipients {
			member.queue(msg)
		}
	}
	p.nick = nick
}

func (s *Server) cmdUSER(p *peer, m protocol.Message) {
	if p.registered {
		p.numeric(protocol.ERRAlreadyRegistered, "You may not reregister")
		return
	}
	if len(m.Params) < 4 {
		p.numeric(protocol.ERRNeedMoreParams, "USER", "Not enough parameters")
		return
	}
	p.user, p.realname = m.Params[0], m.Params[3]
}

func (s *Server) cmdPING(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric("409", "No origin specified")
		return
	}
	p.queue(protocol.Message{Prefix: &protocol.Prefix{Name: s.cfg.Name}, Command: "PONG", Params: []string{s.cfg.Name, m.Params[0]}})
}

func (s *Server) cmdQUIT(p *peer, m protocol.Message) {
	reason := "Client Quit"
	if len(m.Params) > 0 {
		reason = m.Params[0]
	}
	s.removePeer(p, reason)
}
