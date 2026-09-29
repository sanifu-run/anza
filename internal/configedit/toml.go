package configedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type tomlDocument struct {
	entries  map[string]tomlEntry
	sections map[string]tomlSection
}

type tomlEntry struct {
	valueStart int
	valueEnd   int
	lineStart  int
	lineEnd    int
	section    string
}

type tomlSection struct {
	headerEnd int
	end       int
	lastLine  int
}

var (
	tomlDecimalInteger = regexp.MustCompile(`^[+-]?(?:0|[1-9](?:_?[0-9])*)$`)
	tomlHexInteger     = regexp.MustCompile(`^0x[0-9A-Fa-f](?:_?[0-9A-Fa-f])*$`)
	tomlOctInteger     = regexp.MustCompile(`^0o[0-7](?:_?[0-7])*$`)
	tomlBinInteger     = regexp.MustCompile(`^0b[01](?:_?[01])*$`)
	tomlFloat          = regexp.MustCompile(`^[+-]?(?:0|[1-9](?:_?[0-9])*)(?:\.[0-9](?:_?[0-9])*)?(?:[eE][+-]?[0-9](?:_?[0-9])*)?$`)
)

func parseTOMLConfig(data []byte) (*tomlDocument, error) {
	doc := &tomlDocument{entries: make(map[string]tomlEntry), sections: map[string]tomlSection{"": {headerEnd: 0, end: len(data), lastLine: 0}}}
	section := ""
	sectionHeaderSeen := map[string]struct{}{}
	offset := 0
	for offset < len(data) {
		lineStart := offset
		relEnd := bytes.IndexByte(data[offset:], '\n')
		lineEnd := len(data)
		if relEnd >= 0 {
			lineEnd = offset + relEnd + 1
			offset = lineEnd
		} else {
			offset = len(data)
		}
		contentEnd := lineEnd
		if contentEnd > lineStart && data[contentEnd-1] == '\n' {
			contentEnd--
		}
		if contentEnd > lineStart && data[contentEnd-1] == '\r' {
			contentEnd--
		}
		line := data[lineStart:contentEnd]
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		if trimmed[0] == '[' {
			header := bytes.TrimSpace(trimmed[:findTOMLComment(trimmed)])
			if bytes.HasPrefix(header, []byte("[[")) || len(header) < 3 || header[len(header)-1] != ']' {
				return nil, fmt.Errorf("unsupported or malformed table header on line %d", lineNumber(data, lineStart))
			}
			name := strings.TrimSpace(string(header[1 : len(header)-1]))
			if err := validatePath(name); err != nil {
				return nil, fmt.Errorf("unsupported table name on line %d: %w", lineNumber(data, lineStart), err)
			}
			if _, ok := sectionHeaderSeen[name]; ok {
				return nil, fmt.Errorf("duplicate TOML table %q", name)
			}
			sectionHeaderSeen[name] = struct{}{}
			old := doc.sections[section]
			old.end = lineStart
			doc.sections[section] = old
			section = name
			doc.sections[section] = tomlSection{headerEnd: lineEnd, end: len(data), lastLine: lineEnd}
			continue
		}
		eq := findTOMLEquals(line)
		if eq < 0 {
			return nil, fmt.Errorf("unsupported TOML syntax on line %d", lineNumber(data, lineStart))
		}
		keyText := strings.TrimSpace(string(line[:eq]))
		if err := validatePath(keyText); err != nil {
			return nil, fmt.Errorf("unsupported TOML key on line %d: %w", lineNumber(data, lineStart), err)
		}
		key := keyText
		if section != "" {
			key = section + "." + keyText
		}
		valueStartLine := eq + 1
		for valueStartLine < len(line) && (line[valueStartLine] == ' ' || line[valueStartLine] == '\t') {
			valueStartLine++
		}
		valueEndLine := findTOMLComment(line[valueStartLine:]) + valueStartLine
		for valueEndLine > valueStartLine && (line[valueEndLine-1] == ' ' || line[valueEndLine-1] == '\t') {
			valueEndLine--
		}
		if _, err := parseTOMLValue(line[valueStartLine:valueEndLine]); err != nil {
			return nil, fmt.Errorf("invalid or unsupported TOML value for %q on line %d: %w", key, lineNumber(data, lineStart), err)
		}
		if _, exists := doc.entries[key]; exists {
			return nil, fmt.Errorf("duplicate TOML key %q", key)
		}
		doc.entries[key] = tomlEntry{valueStart: lineStart + valueStartLine, valueEnd: lineStart + valueEndLine, lineStart: lineStart, lineEnd: lineEnd, section: section}
		sec := doc.sections[section]
		sec.lastLine = lineEnd
		doc.sections[section] = sec
	}
	return doc, nil
}

func tomlLookup(data []byte, key string) ([]byte, bool, error) {
	doc, err := parseTOMLConfig(data)
	if err != nil {
		return nil, false, err
	}
	entry, ok := doc.entries[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), data[entry.valueStart:entry.valueEnd]...), true, nil
}

func tomlModify(data []byte, key string, value []byte, remove bool) ([]byte, error) {
	doc, err := parseTOMLConfig(data)
	if err != nil {
		return nil, err
	}
	entry, exists := doc.entries[key]
	if exists {
		if !remove {
			return splice(data, entry.valueStart, entry.valueEnd, value), nil
		}
		return splice(data, entry.lineStart, entry.lineEnd, nil), nil
	}
	if remove {
		return cloneBytes(data), nil
	}
	segments := strings.Split(key, ".")
	keyName := segments[len(segments)-1]
	sectionName := strings.Join(segments[:len(segments)-1], ".")
	if sectionName == "" {
		insert := doc.sections[""].end
		if insert < 0 || insert > len(data) {
			insert = len(data)
		}
		if len(data) == 0 {
			return append(append([]byte(nil), []byte(keyName+" = ")...), append(value, '\n')...), nil
		}
		prefix := []byte("")
		if insert > 0 && data[insert-1] != '\n' {
			prefix = []byte("\n")
		}
		line := append(prefix, []byte(keyName+" = ")...)
		line = append(line, value...)
		line = append(line, '\n')
		return splice(data, insert, insert, line), nil
	}
	section, exists := doc.sections[sectionName]
	if !exists {
		if hasTOMLScalarPrefix(doc, sectionName) {
			return nil, fmt.Errorf("TOML key conflicts with table %q", sectionName)
		}
		separator := ""
		if len(data) > 0 {
			separator = "\n"
			if data[len(data)-1] != '\n' {
				separator += "\n"
			} else if len(data) > 1 && data[len(data)-2] != '\n' {
				separator += "\n"
			}
		}
		addition := separator + "[" + sectionName + "]\n" + keyName + " = " + string(value) + "\n"
		return append(append([]byte(nil), data...), []byte(addition)...), nil
	}
	insert := section.lastLine
	if insert < section.headerEnd {
		insert = section.headerEnd
	}
	prefix := []byte("")
	if insert > 0 && data[insert-1] != '\n' {
		prefix = []byte("\n")
	}
	line := append(prefix, []byte(keyName+" = ")...)
	line = append(line, value...)
	line = append(line, '\n')
	return splice(data, insert, insert, line), nil
}

func hasTOMLScalarPrefix(doc *tomlDocument, path string) bool {
	segments := strings.Split(path, ".")
	for i := range segments {
		prefix := strings.Join(segments[:i+1], ".")
		if _, exists := doc.entries[prefix]; exists {
			return true
		}
	}
	return false
}

func encodeTOMLValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case string:
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	case bool:
		return []byte(strconv.FormatBool(v)), nil
	case int:
		return []byte(strconv.Itoa(v)), nil
	case int8:
		return []byte(strconv.FormatInt(int64(v), 10)), nil
	case int16:
		return []byte(strconv.FormatInt(int64(v), 10)), nil
	case int32:
		return []byte(strconv.FormatInt(int64(v), 10)), nil
	case int64:
		return []byte(strconv.FormatInt(v, 10)), nil
	case uint:
		return []byte(strconv.FormatUint(uint64(v), 10)), nil
	case uint8:
		return []byte(strconv.FormatUint(uint64(v), 10)), nil
	case uint16:
		return []byte(strconv.FormatUint(uint64(v), 10)), nil
	case uint32:
		return []byte(strconv.FormatUint(uint64(v), 10)), nil
	case uint64:
		return []byte(strconv.FormatUint(v, 10)), nil
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("non-finite float is unsupported")
		}
		return []byte(strconv.FormatFloat(float64(v), 'g', -1, 32)), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("non-finite float is unsupported")
		}
		return []byte(strconv.FormatFloat(v, 'g', -1, 64)), nil
	case []string:
		items := make([]any, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return encodeTOMLValue(items)
	case []any:
		var out []byte
		out = append(out, '[')
		for i, item := range v {
			if i > 0 {
				out = append(out, []byte(", ")...)
			}
			encoded, err := encodeTOMLValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, encoded...)
		}
		out = append(out, ']')
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported TOML value type %T", value)
	}
}

func parseTOMLValue(raw []byte) (any, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, fmt.Errorf("empty value")
	}
	if strings.HasPrefix(s, "\"\"\"") || strings.HasPrefix(s, "'''") {
		return nil, fmt.Errorf("multiline strings are unsupported")
	}
	if s[0] == '"' {
		var value string
		if err := json.Unmarshal([]byte(s), &value); err != nil {
			return nil, err
		}
		if strings.Contains(s, `\/`) {
			return nil, fmt.Errorf("non-TOML JSON string escape")
		}
		return value, nil
	}
	if s[0] == '\'' {
		if len(s) < 2 || s[len(s)-1] != '\'' || strings.Contains(s[1:len(s)-1], "'") {
			return nil, fmt.Errorf("invalid literal string")
		}
		return s[1 : len(s)-1], nil
	}
	if s == "true" || s == "false" {
		return s == "true", nil
	}
	if strings.HasPrefix(s, "[") {
		if s[len(s)-1] != ']' {
			return nil, fmt.Errorf("multiline or unclosed array is unsupported")
		}
		body := strings.TrimSpace(s[1 : len(s)-1])
		if body == "" {
			return []any{}, nil
		}
		parts, err := splitTOMLArray(body)
		if err != nil {
			return nil, err
		}
		values := make([]any, 0, len(parts))
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			value, err := parseTOMLValue([]byte(part))
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	}
	numeric := strings.ReplaceAll(s, "_", "")
	if tomlDecimalInteger.MatchString(s) {
		i, err := strconv.ParseInt(numeric, 10, 64)
		if err == nil {
			return i, nil
		}
		return nil, err
	}
	for _, base := range []struct {
		pattern *regexp.Regexp
		radix   int
		prefix  string
	}{{tomlHexInteger, 16, "0x"}, {tomlOctInteger, 8, "0o"}, {tomlBinInteger, 2, "0b"}} {
		if base.pattern.MatchString(s) {
			i, err := strconv.ParseInt(strings.TrimPrefix(numeric, base.prefix), base.radix, 64)
			if err == nil {
				return i, nil
			}
			return nil, err
		}
	}
	if tomlFloat.MatchString(s) && (strings.ContainsAny(s, ".eE")) {
		f, err := strconv.ParseFloat(numeric, 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("unsupported TOML scalar %q", s)
}

func splitTOMLArray(s string) ([]string, error) {
	var parts []string
	start := 0
	depth := 0
	quote := byte(0)
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '[' {
			depth++
			continue
		}
		if c == ']' {
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced array")
			}
			continue
		}
		if c == ',' && depth == 0 {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if quote != 0 || depth != 0 {
		return nil, fmt.Errorf("unbalanced array value")
	}
	parts = append(parts, s[start:])
	return parts, nil
}

func findTOMLEquals(line []byte) int {
	quote := byte(0)
	escaped := false
	for i, c := range line {
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '=' {
			return i
		}
		if c == '#' {
			break
		}
	}
	return -1
}
func findTOMLComment(value []byte) int {
	quote := byte(0)
	escaped := false
	depth := 0
	for i, c := range value {
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '[' {
			depth++
			continue
		}
		if c == ']' {
			depth--
			continue
		}
		if c == '#' && depth == 0 {
			return i
		}
	}
	return len(value)
}
func lineNumber(data []byte, start int) int { return bytes.Count(data[:start], []byte{'\n'}) + 1 }
