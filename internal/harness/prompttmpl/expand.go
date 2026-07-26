package prompttmpl

import (
	"strconv"
	"strings"
	"unicode"
)

// Expand substitutes a tokenized argument string into a template body with
// bash-style syntax: $1..$9, $@ / $ARGUMENTS (all args joined by spaces),
// ${N}, ${N:-default}, ${@:N} and ${@:N:L} (1-based token slices). Arguments
// are whitespace-split, double-quote aware; quotes group and are dropped.
// Unrecognized $-sequences are left literal. No shell is involved.
func Expand(body, argStr string) string {
	args := tokenize(argStr)
	var sb strings.Builder
	for i := 0; i < len(body); {
		if body[i] != '$' || i+1 >= len(body) {
			sb.WriteByte(body[i])
			i++
			continue
		}
		next := body[i+1]
		switch {
		case next >= '1' && next <= '9':
			sb.WriteString(argN(args, int(next-'0')))
			i += 2
		case next == '@':
			sb.WriteString(strings.Join(args, " "))
			i += 2
		case strings.HasPrefix(body[i+1:], "ARGUMENTS"):
			sb.WriteString(strings.Join(args, " "))
			i += 1 + len("ARGUMENTS")
		case next == '{':
			end := strings.IndexByte(body[i:], '}')
			if end == -1 {
				sb.WriteByte('$')
				i++
				continue
			}
			if out, ok := evalBrace(body[i+2:i+end], args); ok {
				sb.WriteString(out)
				i += end + 1
			} else {
				sb.WriteByte('$')
				i++
			}
		default:
			sb.WriteByte('$')
			i++
		}
	}
	return sb.String()
}

// argN returns the 1-based Nth token, or "" when out of range.
func argN(args []string, n int) string {
	if n < 1 || n > len(args) {
		return ""
	}
	return args[n-1]
}

// evalBrace evaluates the inside of a ${...} expression. ok=false means the
// expression is not one of ours and the "$" should stay literal.
func evalBrace(expr string, args []string) (string, bool) {
	if expr == "@" || expr == "ARGUMENTS" {
		return strings.Join(args, " "), true
	}
	if rest, isSlice := strings.CutPrefix(expr, "@:"); isSlice {
		nStr, lStr, hasLen := strings.Cut(rest, ":")
		n, err := strconv.Atoi(nStr)
		if err != nil || n < 1 {
			return "", false
		}
		if n > len(args) {
			return "", true
		}
		slice := args[n-1:]
		if hasLen {
			l, err := strconv.Atoi(lStr)
			if err != nil || l < 0 {
				return "", false
			}
			if l < len(slice) {
				slice = slice[:l]
			}
		}
		return strings.Join(slice, " "), true
	}
	nStr, def, hasDef := strings.Cut(expr, ":-")
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		return "", false
	}
	v := argN(args, n)
	if v == "" && hasDef {
		return def, true
	}
	return v, true
}

// tokenize splits an argument string on whitespace, honoring double quotes:
// `a "b c"` → ["a", "b c"]. Quotes are dropped; a quoted empty string is an
// empty token; an unterminated quote runs to the end of input. No escapes.
func tokenize(s string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			started = true
		case !inQuote && unicode.IsSpace(r):
			if started {
				tokens = append(tokens, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		tokens = append(tokens, cur.String())
	}
	return tokens
}
