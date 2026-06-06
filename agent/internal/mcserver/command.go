package mcserver

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ParseCommand splits a user-provided launch command without invoking a shell.
func ParseCommand(command string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false
	started := false

	flush := func() {
		if started {
			args = append(args, current.String())
			current.Reset()
			started = false
		}
	}

	runes := []rune(command)
	for index, r := range runes {
		if escaped {
			current.WriteRune(r)
			started = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			if index+1 < len(runes) {
				next := runes[index+1]
				if next == '\\' || next == '"' || next == '\'' || unicode.IsSpace(next) {
					escaped = true
					started = true
					continue
				}
			}
			current.WriteRune(r)
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			started = true
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
			started = true
		}
	}

	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in command", quote)
	}
	flush()
	if len(args) == 0 {
		return nil, errors.New("server command is required")
	}
	return args, nil
}
