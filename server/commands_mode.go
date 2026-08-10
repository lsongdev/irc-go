package server

import (
	"strconv"
	"strings"

	"github.com/lsongdev/irc-go/protocol"
)

func (s *Server) cmdMODE(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "MODE", "Not enough parameters")
		return
	}
	target := m.Params[0]
	if !protocol.IsChannel(target) {
		s.cmdUserMode(p, m)
		return
	}
	ch := s.channels[protocol.FoldCase(target)]
	if ch == nil {
		p.numeric(protocol.ERRNoSuchChannel, target, "No such channel")
		return
	}
	if len(m.Params) == 1 {
		p.numeric(protocol.RPLChannelModeIs, append([]string{ch.name, ch.modeString()}, ch.modeParams()...)...)
		return
	}
	member := ch.members[p]
	if member == nil || !member.operator {
		p.numeric(protocol.ERRChanOPrivsNeeded, ch.name, "You're not channel operator")
		return
	}
	adding := true
	arg := 2
	var applied strings.Builder
	var appliedSign byte
	var args []string
	for _, mode := range m.Params[1] {
		if mode == '+' {
			adding = true
			continue
		}
		if mode == '-' {
			adding = false
			continue
		}
		switch mode {
		case 'i':
			ch.inviteOnly = adding
		case 'm':
			ch.moderated = adding
		case 'n':
			ch.noExternal = adding
		case 't':
			ch.topicProtected = adding
		case 'k':
			if adding {
				if arg >= len(m.Params) {
					continue
				}
				ch.key = m.Params[arg]
				args = append(args, m.Params[arg])
				arg++
			} else {
				ch.key = ""
			}
		case 'l':
			if adding {
				if arg >= len(m.Params) {
					continue
				}
				n, err := strconv.Atoi(m.Params[arg])
				if err != nil || n <= 0 {
					continue
				}
				ch.limit = n
				args = append(args, m.Params[arg])
				arg++
			} else {
				ch.limit = 0
			}
		case 'o', 'v':
			if arg >= len(m.Params) {
				continue
			}
			targetPeer := s.users[protocol.FoldCase(m.Params[arg])]
			name := m.Params[arg]
			arg++
			if targetPeer == nil || ch.members[targetPeer] == nil {
				p.numeric(protocol.ERRUserNotInChannel, name, ch.name, "They aren't on that channel")
				continue
			}
			if mode == 'o' {
				ch.members[targetPeer].operator = adding
			} else {
				ch.members[targetPeer].voice = adding
			}
			args = append(args, targetPeer.nick)
		default:
			p.numeric(protocol.ERRUnknownMode, string(mode), "is unknown mode char to me")
			continue
		}
		sign := byte('-')
		if adding {
			sign = '+'
		}
		if sign != appliedSign {
			applied.WriteByte(sign)
			appliedSign = sign
		}
		applied.WriteRune(mode)
	}
	if applied.Len() > 0 {
		s.broadcast(ch, protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "MODE", Params: append([]string{ch.name, applied.String()}, args...)}, nil)
	}
}

func (s *Server) cmdUserMode(p *peer, m protocol.Message) {
	if protocol.FoldCase(m.Params[0]) != protocol.FoldCase(p.nick) {
		p.numeric(protocol.ERRUsersDontMatch, "Cannot change mode for other users")
		return
	}
	if len(m.Params) == 1 {
		modes := "+"
		if p.invisible {
			modes += "i"
		}
		p.numeric(protocol.RPLUModeIs, modes)
		return
	}
	adding := true
	changed := false
	for _, mode := range m.Params[1] {
		switch mode {
		case '+':
			adding = true
		case '-':
			adding = false
		case 'i':
			p.invisible = adding
			changed = true
		default:
			p.numeric(protocol.ERRUModeUnknownFlag, "Unknown MODE flag")
		}
	}
	if changed {
		p.queue(protocol.Message{Prefix: prefixPtr(p.prefix()), Command: "MODE", Params: []string{p.nick, m.Params[1]}})
	}
}

func (ch *channel) modeString() string {
	var b strings.Builder
	b.WriteByte('+')
	if ch.inviteOnly {
		b.WriteByte('i')
	}
	if ch.moderated {
		b.WriteByte('m')
	}
	if ch.noExternal {
		b.WriteByte('n')
	}
	if ch.topicProtected {
		b.WriteByte('t')
	}
	if ch.key != "" {
		b.WriteByte('k')
	}
	if ch.limit > 0 {
		b.WriteByte('l')
	}
	return b.String()
}

func (ch *channel) modeParams() []string {
	var params []string
	if ch.key != "" {
		params = append(params, ch.key)
	}
	if ch.limit > 0 {
		params = append(params, strconv.Itoa(ch.limit))
	}
	return params
}
