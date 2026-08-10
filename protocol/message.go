// Package protocol implements IRC wire messages and protocol helpers.
package protocol

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxLineBytes = 512

var (
	ErrEmptyMessage = errors.New("irc: empty message")
	ErrLineTooLong  = errors.New("irc: message exceeds 512 bytes")
	ErrBadMessage   = errors.New("irc: malformed message")
)

// Prefix identifies the origin of a message. Servers usually set Name only;
// users use the nick!user@host form.
type Prefix struct {
	Name string
	User string
	Host string
}

func (p Prefix) String() string {
	s := p.Name
	if p.User != "" {
		s += "!" + p.User
	}
	if p.Host != "" {
		s += "@" + p.Host
	}
	return s
}

// Message is one IRC protocol line without its terminating CRLF.
type Message struct {
	Tags    map[string]string
	Prefix  *Prefix
	Command string
	Params  []string
}

// ParseMessage parses a single IRC line. Both CRLF-terminated lines and bare
// lines are accepted; the encoded message must not exceed the IRC 512-byte
// limit including CRLF.
func ParseMessage(line string) (Message, error) {
	if strings.HasSuffix(line, "\n") {
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
	}
	if len(line)+2 > MaxLineBytes {
		return Message{}, ErrLineTooLong
	}
	if line == "" || strings.ContainsAny(line, "\r\n\x00") {
		return Message{}, ErrEmptyMessage
	}

	var m Message
	rest := line
	if rest[0] == '@' {
		word, tail, ok := cutWord(rest)
		if !ok || len(word) == 1 {
			return Message{}, ErrBadMessage
		}
		var err error
		m.Tags, err = parseTags(word[1:])
		if err != nil {
			return Message{}, err
		}
		rest = tail
	}
	if rest != "" && rest[0] == ':' {
		word, tail, ok := cutWord(rest)
		if !ok || len(word) == 1 {
			return Message{}, ErrBadMessage
		}
		rawPrefix := word[1:]
		if strings.Contains(rawPrefix, "!@") || strings.HasSuffix(rawPrefix, "!") || strings.HasSuffix(rawPrefix, "@") {
			return Message{}, ErrBadMessage
		}
		p := parsePrefix(rawPrefix)
		if p.Name == "" {
			return Message{}, ErrBadMessage
		}
		m.Prefix = &p
		rest = tail
	}
	command, rest, _ := cutWord(rest)
	if !validCommand(command) {
		return Message{}, ErrBadMessage
	}
	m.Command = strings.ToUpper(command)

	for rest != "" {
		if rest[0] == ':' {
			m.Params = append(m.Params, rest[1:])
			break
		}
		param, tail, _ := cutWord(rest)
		if param == "" {
			return Message{}, ErrBadMessage
		}
		m.Params = append(m.Params, param)
		rest = tail
	}
	if len(m.Params) > 15 {
		return Message{}, ErrBadMessage
	}
	return m, nil
}

// String returns the message without CRLF. Invalid messages return an empty
// string; use Encode when an error is required.
func (m Message) String() string {
	s, _ := m.Encode()
	return strings.TrimSuffix(s, "\r\n")
}

// Encode serializes a message with its mandatory CRLF terminator.
func (m Message) Encode() (string, error) {
	if !validCommand(m.Command) || len(m.Params) > 15 {
		return "", ErrBadMessage
	}
	var b strings.Builder
	if len(m.Tags) > 0 {
		keys := make([]string, 0, len(m.Tags))
		for k := range m.Tags {
			if k == "" || strings.ContainsAny(k, " ;\r\n\x00") {
				return "", ErrBadMessage
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('@')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(k)
			if m.Tags[k] != "" {
				b.WriteByte('=')
				b.WriteString(escapeTag(m.Tags[k]))
			}
		}
		b.WriteByte(' ')
	}
	if m.Prefix != nil {
		if m.Prefix.Name == "" || strings.ContainsAny(m.Prefix.String(), " \r\n\x00") {
			return "", ErrBadMessage
		}
		b.WriteByte(':')
		b.WriteString(m.Prefix.String())
		b.WriteByte(' ')
	}
	b.WriteString(strings.ToUpper(m.Command))
	for i, p := range m.Params {
		if strings.ContainsAny(p, "\r\n\x00") {
			return "", ErrBadMessage
		}
		b.WriteByte(' ')
		last := i == len(m.Params)-1
		if last && (p == "" || strings.ContainsRune(p, ' ') || strings.HasPrefix(p, ":")) {
			b.WriteByte(':')
			b.WriteString(p)
		} else {
			if p == "" || strings.ContainsRune(p, ' ') || strings.HasPrefix(p, ":") {
				return "", ErrBadMessage
			}
			b.WriteString(p)
		}
	}
	b.WriteString("\r\n")
	if b.Len() > MaxLineBytes {
		return "", fmt.Errorf("%w: %d bytes", ErrLineTooLong, b.Len())
	}
	return b.String(), nil
}

// EncodeTruncated encodes m and, if necessary, shortens only its final
// parameter so the result fits the IRC line limit. It reports whether the
// final parameter was shortened. This is useful for servers that add a prefix
// to an already received message.
func (m Message) EncodeTruncated() (wire string, truncated bool, err error) {
	wire, err = m.Encode()
	if err == nil || !errors.Is(err, ErrLineTooLong) || len(m.Params) == 0 {
		return wire, false, err
	}
	params := append([]string(nil), m.Params...)
	original := params[len(params)-1]
	params[len(params)-1] = ""
	m.Params = params
	base, baseErr := m.Encode()
	if baseErr != nil {
		return "", false, err
	}
	available := MaxLineBytes - len(base)
	if available < 0 {
		return "", false, err
	}
	if available > len(original) {
		available = len(original)
	}
	if utf8.ValidString(original) {
		for available > 0 && !utf8.ValidString(original[:available]) {
			available--
		}
	}
	params[len(params)-1] = original[:available]
	wire, fitErr := m.Encode()
	if fitErr != nil {
		return "", false, fitErr
	}
	return wire, true, nil
}

func cutWord(s string) (word, rest string, more bool) {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return "", "", false
	}
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], strings.TrimLeft(s[i+1:], " "), true
	}
	return s, "", false
}

func parsePrefix(s string) Prefix {
	var p Prefix
	if bang := strings.IndexByte(s, '!'); bang >= 0 {
		p.Name, s = s[:bang], s[bang+1:]
		if at := strings.IndexByte(s, '@'); at >= 0 {
			p.User, p.Host = s[:at], s[at+1:]
		} else {
			p.User = s
		}
	} else if at := strings.IndexByte(s, '@'); at >= 0 {
		p.Name, p.Host = s[:at], s[at+1:]
	} else {
		p.Name = s
	}
	return p
}

func parseTags(s string) (map[string]string, error) {
	tags := make(map[string]string)
	for _, item := range strings.Split(s, ";") {
		k, v, found := strings.Cut(item, "=")
		if k == "" || strings.ContainsAny(k, " ;\r\n\x00") {
			return nil, ErrBadMessage
		}
		if !found {
			v = ""
		}
		tags[k] = unescapeTag(v)
	}
	return tags, nil
}

func validCommand(command string) bool {
	if len(command) == 3 && command[0] >= '0' && command[0] <= '9' && command[1] >= '0' && command[1] <= '9' && command[2] >= '0' && command[2] <= '9' {
		return true
	}
	if command == "" {
		return false
	}
	for i := range len(command) {
		c := command[i]
		if c < 'A' || c > 'Z' {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	return true
}

func escapeTag(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\:", " ", "\\s", "\r", "\\r", "\n", "\\n")
	return r.Replace(s)
}

func unescapeTag(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case ':':
			b.WriteByte(';')
		case 's':
			b.WriteByte(' ')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
