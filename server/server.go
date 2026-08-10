// Package server provides an embeddable IRC server.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/lsongdev/irc-go/protocol"
)

type Config struct {
	Name       string
	Network    string
	Address    string
	Password   string
	MOTD       []string
	MaxClients int
	Logger     *slog.Logger
}

type Server struct {
	cfg       Config
	created   time.Time
	mu        sync.Mutex
	listener  net.Listener
	peers     map[*peer]struct{}
	users     map[string]*peer
	channels  map[string]*channel
	closed    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

type channel struct {
	name           string
	topic          string
	topicBy        string
	topicAt        time.Time
	members        map[*peer]*membership
	invited        map[string]bool
	inviteOnly     bool
	moderated      bool
	noExternal     bool
	topicProtected bool
	key            string
	limit          int
}

type membership struct{ operator, voice bool }

func New(cfg Config) *Server {
	if cfg.Name == "" {
		cfg.Name = "irc.local"
	}
	if cfg.Network == "" {
		cfg.Network = "irc-go"
	}
	if cfg.Address == "" {
		cfg.Address = ":6667"
	}
	if cfg.MaxClients <= 0 {
		cfg.MaxClients = 1024
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{cfg: cfg, created: time.Now(), peers: make(map[*peer]struct{}), users: make(map[string]*peer), channels: make(map[string]*channel), closed: make(chan struct{})}
}

func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return errors.New("irc server: already serving")
	}
	s.listener = ln
	s.mu.Unlock()
	s.cfg.Logger.Info("IRC server listening", "address", ln.Addr())
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		s.mu.Lock()
		if len(s.peers) >= s.cfg.MaxClients {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		p := newPeer(s, conn)
		s.peers[p] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() { defer s.wg.Done(); p.run() }()
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.mu.Lock()
		if s.listener != nil {
			_ = s.listener.Close()
		}
		for p := range s.peers {
			p.close()
		}
		s.mu.Unlock()
	})
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) dispatch(p *peer, m protocol.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.gone || p.closing {
		return
	}
	s.cfg.Logger.Debug("IRC command", "nick", p.nick, "command", m.Command)

	s.dispatchLocked(p, m)
	if !p.registered {
		s.tryRegister(p)
	}
}

func (s *Server) dispatchLocked(p *peer, m protocol.Message) {
	switch m.Command {
	case "CAP":
		s.cmdCAP(p, m)
	case "PASS":
		s.cmdPASS(p, m)
	case "NICK":
		s.cmdNICK(p, m)
	case "USER":
		s.cmdUSER(p, m)
	case "PING":
		s.cmdPING(p, m)
	case "PONG":
	case "QUIT":
		s.cmdQUIT(p, m)
	default:
		if !p.registered {
			p.numeric(protocol.ERRNotRegistered, "You have not registered")
			return
		}
		switch m.Command {
		case "JOIN":
			s.cmdJOIN(p, m)
		case "PART":
			s.cmdPART(p, m)
		case "PRIVMSG":
			s.cmdMessage(p, m, false)
		case "NOTICE":
			s.cmdMessage(p, m, true)
		case "NAMES":
			s.cmdNAMES(p, m)
		case "LIST":
			s.cmdLIST(p, m)
		case "TOPIC":
			s.cmdTOPIC(p, m)
		case "MODE":
			s.cmdMODE(p, m)
		case "KICK":
			s.cmdKICK(p, m)
		case "INVITE":
			s.cmdINVITE(p, m)
		case "WHO":
			s.cmdWHO(p, m)
		case "WHOIS":
			s.cmdWHOIS(p, m)
		case "AWAY":
			s.cmdAWAY(p, m)
		case "MOTD":
			s.sendMOTD(p)
		case "VERSION":
			p.numeric(protocol.RPLVersion, "irc-go 1.0 "+s.cfg.Name+" :lightweight IRC server")
		default:
			p.numeric(protocol.ERRUnknownCommand, m.Command, "Unknown command")
		}
	}
}

func (s *Server) tryRegister(p *peer) {
	if p.registered || p.nick == "" || p.user == "" || p.capNegotiating {
		return
	}
	if s.cfg.Password != "" && p.password != s.cfg.Password {
		nick := p.nick
		if nick == "" {
			nick = "*"
		}
		p.queueFinal(protocol.Message{Prefix: &protocol.Prefix{Name: s.cfg.Name}, Command: protocol.ERRPasswdMismatch, Params: []string{nick, "Password incorrect"}})
		return
	}
	if existing := s.users[protocol.FoldCase(p.nick)]; existing != nil && existing != p {
		p.numeric(protocol.ERRNicknameInUse, p.nick, "Nickname is already in use")
		p.nick = ""
		return
	}
	p.registered = true
	s.users[protocol.FoldCase(p.nick)] = p
	p.numeric(protocol.RPLWelcome, fmt.Sprintf("Welcome to the %s IRC Network %s", s.cfg.Network, p.prefix().String()))
	p.numeric(protocol.RPLYourHost, "Your host is "+s.cfg.Name+", running version irc-go-1.0")
	p.numeric(protocol.RPLCreated, "This server was created "+s.created.UTC().Format(time.RFC1123))
	p.numeric(protocol.RPLMyInfo, s.cfg.Name, "irc-go-1.0", "i", "imntklov")
	p.numeric(protocol.RPLISupport, "CASEMAPPING=rfc1459", "CHANTYPES=#&", "CHANMODES=,k,l,imnt", "PREFIX=(ov)@+", "NICKLEN=30", "are supported by this server")
	s.sendMOTD(p)
}

func (s *Server) sendMOTD(p *peer) {
	if len(s.cfg.MOTD) == 0 {
		p.numeric(protocol.ERRNoMotd, "MOTD File is missing")
		return
	}
	p.numeric(protocol.RPLMotdStart, "- "+s.cfg.Name+" Message of the day -")
	for _, line := range s.cfg.MOTD {
		p.numeric(protocol.RPLMotd, "- "+line)
	}
	p.numeric(protocol.RPLEndOfMotd, "End of MOTD command")
}
