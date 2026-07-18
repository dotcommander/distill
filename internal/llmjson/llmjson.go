// Package llmjson contains small helpers for recovering JSON payloads from
// model responses that may include prose or fenced blocks around the object.
package llmjson

// ExtractObject salvages the last balanced top-level JSON object from a
// response that may wrap it in prose, ```json fences, or trailing chatter. It
// forward-scans tracking string-literal state with backslash escapes, so braces
// inside quoted values do not corrupt brace depth. Returns "" when no balanced
// object is present.
func ExtractObject(s string) string {
	var (
		inStr bool
		esc   bool
		depth int
		start = -1
		last  string
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					last = s[start : i+1]
				}
			}
		}
	}
	return last
}
