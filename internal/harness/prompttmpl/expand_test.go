package prompttmpl

import (
	"reflect"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name string
		body string
		args string
		want string
	}{
		{"positional", "review $1 for $2", "foo.go bugs", "review foo.go for bugs"},
		{"missing positional empty", "a=$1 b=$2", "only", "a=only b="},
		{"all args dollar-at", "run: $@", "a b c", "run: a b c"},
		{"all args ARGUMENTS", "run: $ARGUMENTS", "a b c", "run: a b c"},
		{"no args", "run: [$@]", "", "run: []"},
		{"quoted token groups", "first=$1 second=$2", `"a b" c`, "first=a b second=c"},
		{"quoted empty token", "v=[$1]", `""`, "v=[]"},
		{"unterminated quote runs to end", "v=[$1]", `"a b`, "v=[a b]"},
		{"default used when missing", "x=${1:-fallback}", "", "x=fallback"},
		{"default skipped when present", "x=${1:-fallback}", "real", "x=real"},
		{"default used when quoted empty", "x=${1:-fallback}", `""`, "x=fallback"},
		{"braced positional", "x=${2}", "a b", "x=b"},
		{"braced positional above nine", "x=${10}", "1 2 3 4 5 6 7 8 9 ten", "x=ten"},
		{"slice from N", "rest: ${@:2}", "a b c d", "rest: b c d"},
		{"slice with length", "mid: ${@:2:2}", "a b c d", "mid: b c"},
		{"slice length past end", "tail: ${@:3:9}", "a b c d", "tail: c d"},
		{"slice start past end", "none: [${@:9}]", "a b", "none: []"},
		{"braced all args", "x=${@} y=${ARGUMENTS}", "a b", "x=a b y=a b"},
		{"digit after positional", "file $12", "a b", "file a2"},
		{"positional inside word", "cost: $1x", "5", "cost: 5x"},
		{"literal dollar", "cost: $USD and $ end", "", "cost: $USD and $ end"},
		{"unknown brace literal", "keep ${foo} as-is", "a", "keep ${foo} as-is"},
		{"unclosed brace literal", "keep ${1", "a", "keep ${1"},
		{"dollar at end", "x=$", "a", "x=$"},
		{"zero invalid", "x=${0:-d} $0", "a", "x=${0:-d} $0"},
		{"multiple occurrences", "$1 then $1", "hi", "hi then hi"},
		{"tabs and newlines split", "a=$1 b=$2", "x\t\ny", "a=x b=y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Expand(tt.body, tt.args)
			if got != tt.want {
				t.Errorf("Expand(%q, %q) = %q, want %q", tt.body, tt.args, got, tt.want)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"spaces only", "   ", nil},
		{"plain", "a b c", []string{"a", "b", "c"}},
		{"quoted group", `a "b c" d`, []string{"a", "b c", "d"}},
		{"adjacent quote", `a"b c"d`, []string{"ab cd"}},
		{"quoted empty", `""`, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tokenize(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
