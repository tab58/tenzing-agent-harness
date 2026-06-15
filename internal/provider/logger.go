package provider

// Logger receives provider diagnostics (request/response traces, skipped
// malformed data). Nil disables them.
type Logger interface {
	Logf(format string, args ...any)
}

// logf writes through l when one is set — the single nil-check for
// provider-internal call sites.
func logf(l Logger, format string, args ...any) {
	if l != nil {
		l.Logf(format, args...)
	}
}
