package protocol

import "strings"

// FoldCase applies the RFC 1459 casemapping used for nicknames and channels.
func FoldCase(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z':
			return r + ('a' - 'A')
		case '[':
			return '{'
		case ']':
			return '}'
		case '\\':
			return '|'
		case '^':
			return '~'
		default:
			return r
		}
	}, s)
}

func IsChannel(name string) bool {
	return len(name) >= 2 && (name[0] == '#' || name[0] == '&') && !strings.ContainsAny(name, " \a,:")
}

func IsNickname(nick string) bool {
	if nick == "" || len(nick) > 30 {
		return false
	}
	first := nick[0]
	if !isLetter(first) && !strings.ContainsRune("[]\\`_^{|}", rune(first)) {
		return false
	}
	for i := 1; i < len(nick); i++ {
		c := nick[i]
		if !isLetter(c) && (c < '0' || c > '9') && !strings.ContainsRune("-[]\\`_^{|}", rune(c)) {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
