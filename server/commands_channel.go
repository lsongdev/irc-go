package server

import (
	"sort"
	"strconv"
	"time"

	"github.com/lsongdev/irc-go/protocol"
)

func (s *Server) cmdJOIN(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "JOIN", "Not enough parameters")
		return
	}
	if m.Params[0] == "0" {
		for ch := range p.joined {
			s.part(p, ch, "Leaving")
		}
		return
	}
	names := splitComma(m.Params[0])
	var keys []string
	if len(m.Params) > 1 {
		keys = splitComma(m.Params[1])
	}
	for i, name := range names {
		if !protocol.IsChannel(name) {
			p.numeric(protocol.ERRNoSuchChannel, name, "No such channel")
			continue
		}
		folded := protocol.FoldCase(name)
		ch := s.channels[folded]
		if ch == nil {
			ch = &channel{name: name, members: make(map[*peer]*membership), invited: make(map[string]bool), noExternal: true}
			ch.topicProtected = true
			s.channels[folded] = ch
		}
		if _, ok := ch.members[p]; ok {
			continue
		}
		if ch.inviteOnly && !ch.invited[protocol.FoldCase(p.nick)] {
			p.numeric(protocol.ERRInviteOnlyChan, name, "Cannot join channel (+i)")
			continue
		}
		if ch.limit > 0 && len(ch.members) >= ch.limit {
			p.numeric(protocol.ERRChannelIsFull, name, "Cannot join channel (+l)")
			continue
		}
		key := ""
		if i < len(keys) {
			key = keys[i]
		}
		if ch.key != "" && ch.key != key {
			p.numeric(protocol.ERRBadChannelKey, name, "Cannot join channel (+k)")
			continue
		}
		first := len(ch.members) == 0
		ch.members[p] = &membership{operator: first}
		p.joined[ch] = struct{}{}
		delete(ch.invited, protocol.FoldCase(p.nick))
		join := protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "JOIN", Params: []string{ch.name}}
		s.broadcast(ch, join, nil)
		if ch.topic == "" {
			p.numeric(protocol.RPLNoTopic, ch.name, "No topic is set")
		} else {
			p.numeric(protocol.RPLTopic, ch.name, ch.topic)
		}
		s.sendNames(p, ch)
	}
}

func (s *Server) cmdPART(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "PART", "Not enough parameters")
		return
	}
	reason := ""
	if len(m.Params) > 1 {
		reason = m.Params[1]
	}
	for _, name := range splitComma(m.Params[0]) {
		ch := s.channels[protocol.FoldCase(name)]
		if ch == nil {
			p.numeric(protocol.ERRNoSuchChannel, name, "No such channel")
			continue
		}
		if _, ok := ch.members[p]; !ok {
			p.numeric(protocol.ERRNotOnChannel, name, "You're not on that channel")
			continue
		}
		s.part(p, ch, reason)
	}
}

func (s *Server) part(p *peer, ch *channel, reason string) {
	params := []string{ch.name}
	if reason != "" {
		params = append(params, reason)
	}
	s.broadcast(ch, protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "PART", Params: params}, nil)
	s.removeFromChannel(p, ch)
}

func (s *Server) cmdNAMES(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		for _, ch := range s.channels {
			s.sendNames(p, ch)
		}
		return
	}
	for _, name := range splitComma(m.Params[0]) {
		if ch := s.channels[protocol.FoldCase(name)]; ch != nil {
			s.sendNames(p, ch)
		} else {
			p.numeric(protocol.RPLEndOfNames, name, "End of NAMES list")
		}
	}
}

func (s *Server) sendNames(p *peer, ch *channel) {
	names := make([]string, 0, len(ch.members))
	for member, mode := range ch.members {
		prefix := ""
		if mode.operator {
			prefix = "@"
		} else if mode.voice {
			prefix = "+"
		}
		names = append(names, prefix+member.nick)
	}
	sort.Strings(names)
	p.numericList(protocol.RPLNameReply, []string{"=", ch.name}, names)
	p.numeric(protocol.RPLEndOfNames, ch.name, "End of NAMES list")
}

func (s *Server) cmdLIST(p *peer, m protocol.Message) {
	p.numeric(protocol.RPLListStart, "Channel", "Users  Name")
	if len(m.Params) > 0 {
		for _, name := range splitComma(m.Params[0]) {
			if ch := s.channels[protocol.FoldCase(name)]; ch != nil {
				p.numeric(protocol.RPLList, ch.name, strconv.Itoa(len(ch.members)), ch.topic)
			}
		}
	} else {
		for _, ch := range s.channels {
			p.numeric(protocol.RPLList, ch.name, strconv.Itoa(len(ch.members)), ch.topic)
		}
	}
	p.numeric(protocol.RPLListEnd, "End of LIST")
}

func (s *Server) cmdTOPIC(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "TOPIC", "Not enough parameters")
		return
	}
	ch := s.channels[protocol.FoldCase(m.Params[0])]
	if ch == nil {
		p.numeric(protocol.ERRNoSuchChannel, m.Params[0], "No such channel")
		return
	}
	membership := ch.members[p]
	if membership == nil {
		p.numeric(protocol.ERRNotOnChannel, ch.name, "You're not on that channel")
		return
	}
	if len(m.Params) == 1 {
		if ch.topic == "" {
			p.numeric(protocol.RPLNoTopic, ch.name, "No topic is set")
		} else {
			p.numeric(protocol.RPLTopic, ch.name, ch.topic)
		}
		return
	}
	if ch.topicProtected && !membership.operator {
		p.numeric(protocol.ERRChanOPrivsNeeded, ch.name, "You're not channel operator")
		return
	}
	ch.topic = m.Params[1]
	ch.topicBy = p.nick
	ch.topicAt = time.Now()
	s.broadcast(ch, protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "TOPIC", Params: []string{ch.name, ch.topic}}, nil)
}

func (s *Server) cmdKICK(p *peer, m protocol.Message) {
	if len(m.Params) < 2 {
		p.numeric(protocol.ERRNeedMoreParams, "KICK", "Not enough parameters")
		return
	}
	ch := s.channels[protocol.FoldCase(m.Params[0])]
	if ch == nil {
		p.numeric(protocol.ERRNoSuchChannel, m.Params[0], "No such channel")
		return
	}
	if member := ch.members[p]; member == nil || !member.operator {
		p.numeric(protocol.ERRChanOPrivsNeeded, ch.name, "You're not channel operator")
		return
	}
	target := s.users[protocol.FoldCase(m.Params[1])]
	if target == nil || ch.members[target] == nil {
		p.numeric(protocol.ERRUserNotInChannel, m.Params[1], ch.name, "They aren't on that channel")
		return
	}
	reason := p.nick
	if len(m.Params) > 2 {
		reason = m.Params[2]
	}
	s.broadcast(ch, protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "KICK", Params: []string{ch.name, target.nick, reason}}, nil)
	s.removeFromChannel(target, ch)
}

func (s *Server) cmdINVITE(p *peer, m protocol.Message) {
	if len(m.Params) < 2 {
		p.numeric(protocol.ERRNeedMoreParams, "INVITE", "Not enough parameters")
		return
	}
	target := s.users[protocol.FoldCase(m.Params[0])]
	if target == nil {
		p.numeric(protocol.ERRNoSuchNick, m.Params[0], "No such nick")
		return
	}
	ch := s.channels[protocol.FoldCase(m.Params[1])]
	if ch == nil {
		p.numeric(protocol.ERRNoSuchChannel, m.Params[1], "No such channel")
		return
	}
	member := ch.members[p]
	if member == nil {
		p.numeric(protocol.ERRNotOnChannel, ch.name, "You're not on that channel")
		return
	}
	if ch.inviteOnly && !member.operator {
		p.numeric(protocol.ERRChanOPrivsNeeded, ch.name, "You're not channel operator")
		return
	}
	if ch.members[target] != nil {
		p.numeric(protocol.ERRUserOnChannel, target.nick, ch.name, "is already on channel")
		return
	}
	ch.invited[protocol.FoldCase(target.nick)] = true
	p.numeric(protocol.RPLInviting, target.nick, ch.name)
	target.queue(protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "INVITE", Params: []string{target.nick, ch.name}})
}

func (s *Server) broadcast(ch *channel, m protocol.Message, except *peer) {
	for member := range ch.members {
		if member != except {
			member.queue(m)
		}
	}
}
