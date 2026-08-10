package server

import (
	"strconv"
	"time"

	"github.com/lsongdev/irc-go/protocol"
)

func (s *Server) cmdMessage(p *peer, m protocol.Message, notice bool) {
	command := "PRIVMSG"
	if notice {
		command = "NOTICE"
	}
	if len(m.Params) == 0 {
		if !notice {
			p.numeric(protocol.ERRNoRecipient, "No recipient given ("+command+")")
		}
		return
	}
	if len(m.Params) < 2 || m.Params[1] == "" {
		if !notice {
			p.numeric(protocol.ERRNoTextToSend, "No text to send")
		}
		return
	}
	for _, targetName := range splitComma(m.Params[0]) {
		out := protocol.Message{Prefix: prefixPtr(p.prefix()), Command: command, Params: []string{targetName, m.Params[1]}}
		if protocol.IsChannel(targetName) {
			ch := s.channels[protocol.FoldCase(targetName)]
			if ch == nil {
				if !notice {
					p.numeric(protocol.ERRNoSuchChannel, targetName, "No such channel")
				}
				continue
			}
			membership := ch.members[p]
			if (ch.noExternal && membership == nil) || (ch.moderated && (membership == nil || (!membership.operator && !membership.voice))) {
				if !notice {
					p.numeric(protocol.ERRCannotSendToChan, ch.name, "Cannot send to channel")
				}
				continue
			}
			out.Params[0] = ch.name
			s.broadcast(ch, out, p)
		} else {
			target := s.users[protocol.FoldCase(targetName)]
			if target == nil {
				if !notice {
					p.numeric(protocol.ERRNoSuchNick, targetName, "No such nick/channel")
				}
				continue
			}
			out.Params[0] = target.nick
			target.queue(out)
			if !notice && target.away != "" {
				p.numeric(protocol.RPLAway, target.nick, target.away)
			}
		}
	}
}

func (s *Server) cmdAWAY(p *peer, m protocol.Message) {
	if len(m.Params) == 0 || m.Params[0] == "" {
		p.away = ""
		p.numeric(protocol.RPLUnAway, "You are no longer marked as being away")
		return
	}
	p.away = m.Params[0]
	p.numeric(protocol.RPLNowAway, "You have been marked as being away")
}

func (s *Server) cmdWHOIS(p *peer, m protocol.Message) {
	if len(m.Params) == 0 {
		p.numeric(protocol.ERRNeedMoreParams, "WHOIS", "Not enough parameters")
		return
	}
	target := s.users[protocol.FoldCase(m.Params[len(m.Params)-1])]
	if target == nil {
		p.numeric(protocol.ERRNoSuchNick, m.Params[len(m.Params)-1], "No such nick")
		p.numeric(protocol.RPLEndOfWhois, m.Params[len(m.Params)-1], "End of WHOIS list")
		return
	}
	p.numeric(protocol.RPLWhoisUser, target.nick, target.user, target.host, "*", target.realname)
	p.numeric(protocol.RPLWhoisServer, target.nick, s.cfg.Name, s.cfg.Network)
	var chans []string
	for ch := range target.joined {
		pre := ""
		if ch.members[target].operator {
			pre = "@"
		} else if ch.members[target].voice {
			pre = "+"
		}
		chans = append(chans, pre+ch.name)
	}
	if len(chans) > 0 {
		p.numericList(protocol.RPLWhoisChannels, []string{target.nick}, chans)
	}
	p.numeric(protocol.RPLWhoisIdle, target.nick, strconv.FormatInt(int64(time.Since(target.connected).Seconds()), 10), strconv.FormatInt(target.connected.Unix(), 10), "seconds idle, signon time")
	p.numeric(protocol.RPLEndOfWhois, target.nick, "End of WHOIS list")
}

func (s *Server) cmdWHO(p *peer, m protocol.Message) {
	mask := "0"
	if len(m.Params) > 0 {
		mask = m.Params[0]
	}
	if ch := s.channels[protocol.FoldCase(mask)]; ch != nil {
		for target, member := range ch.members {
			flags := "H"
			if target.away != "" {
				flags = "G"
			}
			if member.operator {
				flags += "@"
			} else if member.voice {
				flags += "+"
			}
			p.numeric(protocol.RPLWhoReply, ch.name, target.user, target.host, s.cfg.Name, target.nick, flags, "0 "+target.realname)
		}
	} else if target := s.users[protocol.FoldCase(mask)]; target != nil {
		flags := "H"
		if target.away != "" {
			flags = "G"
		}
		p.numeric(protocol.RPLWhoReply, "*", target.user, target.host, s.cfg.Name, target.nick, flags, "0 "+target.realname)
	}
	p.numeric(protocol.RPLEndOfWho, mask, "End of WHO list")
}
