package configedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type jsonObject struct {
	start   int
	end     int
	close   int
	members []jsonMember
}

type jsonMember struct {
	key         string
	keyStart    int
	value       *jsonValue
	comma       int
	colonSpaces []byte
}

type jsonValue struct {
	start  int
	end    int
	object *jsonObject
}

type jsonParser struct {
	data []byte
	pos  int
}

func parseJSONConfig(data []byte) (*jsonObject, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("malformed JSON")
	}
	parser := jsonParser{data: data}
	parser.skipSpace()
	if parser.pos >= len(data) || data[parser.pos] != '{' {
		return nil, fmt.Errorf("JSON config must be one object")
	}
	root, err := parser.object()
	if err != nil {
		return nil, err
	}
	parser.skipSpace()
	if parser.pos != len(data) {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return root, nil
}

func (p *jsonParser) object() (*jsonObject, error) {
	start := p.pos
	p.pos++
	obj := &jsonObject{start: start}
	p.skipSpace()
	seen := map[string]struct{}{}
	if p.consume('}') {
		obj.close = p.pos - 1
		obj.end = p.pos
		return obj, nil
	}
	for {
		p.skipSpace()
		keyStart := p.pos
		keyEnd, err := p.stringEnd()
		if err != nil {
			return nil, err
		}
		var key string
		if err := json.Unmarshal(p.data[keyStart:keyEnd], &key); err != nil {
			return nil, fmt.Errorf("invalid JSON key: %w", err)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate JSON key %q", key)
		}
		seen[key] = struct{}{}
		p.skipSpace()
		if !p.consume(':') {
			return nil, fmt.Errorf("JSON object key %q is missing colon", key)
		}
		colonEnd := p.pos
		p.skipSpace()
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		member := jsonMember{key: key, keyStart: keyStart, value: value, comma: -1, colonSpaces: append([]byte(nil), p.data[colonEnd:value.start]...)}
		p.skipSpace()
		if p.consume(',') {
			member.comma = p.pos - 1
			obj.members = append(obj.members, member)
			continue
		}
		if p.consume('}') {
			obj.members = append(obj.members, member)
			obj.close = p.pos - 1
			obj.end = p.pos
			return obj, nil
		}
		return nil, fmt.Errorf("JSON object is missing comma or closing brace")
	}
}

func (p *jsonParser) array() error {
	p.pos++
	p.skipSpace()
	if p.consume(']') {
		return nil
	}
	for {
		p.skipSpace()
		if _, err := p.value(); err != nil {
			return err
		}
		p.skipSpace()
		if p.consume(',') {
			continue
		}
		if p.consume(']') {
			return nil
		}
		return fmt.Errorf("JSON array is missing comma or closing bracket")
	}
}

func (p *jsonParser) value() (*jsonValue, error) {
	p.skipSpace()
	start := p.pos
	if start >= len(p.data) {
		return nil, fmt.Errorf("missing JSON value")
	}
	value := &jsonValue{start: start}
	switch p.data[p.pos] {
	case '{':
		object, err := p.object()
		if err != nil {
			return nil, err
		}
		value.object = object
		value.end = object.end
	case '[':
		if err := p.array(); err != nil {
			return nil, err
		}
		value.end = p.pos
	case '"':
		end, err := p.stringEnd()
		if err != nil {
			return nil, err
		}
		value.end = end
	default:
		for p.pos < len(p.data) && !jsonDelimiter(p.data[p.pos]) {
			p.pos++
		}
		if p.pos == start {
			return nil, fmt.Errorf("invalid JSON value")
		}
		value.end = p.pos
	}
	return value, nil
}

func (p *jsonParser) stringEnd() (int, error) {
	if !p.consume('"') {
		return 0, fmt.Errorf("expected JSON string")
	}
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		p.pos++
		if c == '\\' {
			if p.pos >= len(p.data) {
				break
			}
			p.pos++
			continue
		}
		if c == '"' {
			return p.pos, nil
		}
	}
	return 0, fmt.Errorf("unterminated JSON string")
}

func (p *jsonParser) skipSpace() {
	for p.pos < len(p.data) && (p.data[p.pos] == ' ' || p.data[p.pos] == '\t' || p.data[p.pos] == '\r' || p.data[p.pos] == '\n') {
		p.pos++
	}
}
func (p *jsonParser) consume(want byte) bool {
	if p.pos < len(p.data) && p.data[p.pos] == want {
		p.pos++
		return true
	}
	return false
}
func jsonDelimiter(c byte) bool {
	return c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func jsonLookup(data []byte, path string) ([]byte, bool, error) {
	root, err := parseJSONConfig(data)
	if err != nil {
		return nil, false, err
	}
	_, member, err := jsonParent(root, path)
	if err != nil {
		return nil, false, err
	}
	if member == nil {
		return nil, false, nil
	}
	return append([]byte(nil), data[member.value.start:member.value.end]...), true, nil
}

func jsonModify(data []byte, path string, value []byte, remove bool) ([]byte, error) {
	root, err := parseJSONConfig(data)
	if err != nil {
		return nil, err
	}
	parent, member, err := jsonParent(root, path)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, fmt.Errorf("JSON parent object for %q does not exist", path)
	}
	if member != nil {
		if remove {
			index := -1
			for i := range parent.members {
				if &parent.members[i] == member {
					index = i
					break
				}
			}
			if index < 0 {
				return nil, fmt.Errorf("JSON key %q could not be located", path)
			}
			start, end := member.keyStart, member.value.end
			if index < len(parent.members)-1 {
				end = member.comma + 1
			} else if index > 0 {
				start = parent.members[index-1].comma
			}
			return splice(data, start, end, nil), nil
		}
		return splice(data, member.value.start, member.value.end, value), nil
	}
	if remove {
		return cloneBytes(data), nil
	}
	segments := strings.Split(path, ".")
	key := segments[len(segments)-1]
	quoted, _ := json.Marshal(key)
	if len(parent.members) == 0 {
		newline, indent := jsonEmptyIndent(data, parent)
		colonSpaces := []byte(nil)
		if bytes.Contains(data, []byte(": ")) {
			colonSpaces = []byte(" ")
		}
		insertion := append([]byte(nil), newline...)
		insertion = append(insertion, indent...)
		insertion = append(insertion, quoted...)
		insertion = append(insertion, ':')
		insertion = append(insertion, colonSpaces...)
		insertion = append(insertion, value...)
		return splice(data, parent.start+1, parent.start+1, insertion), nil
	}
	colonSpaces := parent.members[0].colonSpaces
	newline, indent := jsonIndent(data, parent)
	separator := append([]byte{','}, newline...)
	separator = append(separator, indent...)
	insertion := append(separator, quoted...)
	insertion = append(insertion, ':')
	insertion = append(insertion, colonSpaces...)
	insertion = append(insertion, value...)
	lastValueEnd := parent.members[len(parent.members)-1].value.end
	return splice(data, lastValueEnd, lastValueEnd, insertion), nil
}

func jsonParent(root *jsonObject, path string) (*jsonObject, *jsonMember, error) {
	segments := strings.Split(path, ".")
	current := root
	for _, segment := range segments[:len(segments)-1] {
		member := jsonFind(current, segment)
		if member == nil {
			return nil, nil, nil
		}
		if member.value.object == nil {
			return nil, nil, fmt.Errorf("JSON key %q is not an object", segment)
		}
		current = member.value.object
	}
	return current, jsonFind(current, segments[len(segments)-1]), nil
}

func jsonFind(object *jsonObject, key string) *jsonMember {
	for i := range object.members {
		if object.members[i].key == key {
			return &object.members[i]
		}
	}
	return nil
}

func jsonIndent(data []byte, object *jsonObject) ([]byte, []byte) {
	newline := []byte("\n")
	if !bytes.Contains(data, newline) {
		return []byte(" "), nil
	}
	indent := []byte("  ")
	if len(object.members) > 0 {
		lineStart := bytes.LastIndexByte(data[:object.members[0].keyStart], '\n') + 1
		candidate := data[lineStart:object.members[0].keyStart]
		if len(bytes.Trim(candidate, " \t")) == 0 {
			indent = append([]byte(nil), candidate...)
		}
	}
	return newline, indent
}

func jsonEmptyIndent(data []byte, object *jsonObject) ([]byte, []byte) {
	if !bytes.Contains(data, []byte("\n")) {
		return nil, nil
	}
	lineStart := bytes.LastIndexByte(data[:object.start], '\n') + 1
	indentEnd := lineStart
	for indentEnd < object.start && (data[indentEnd] == ' ' || data[indentEnd] == '\t') {
		indentEnd++
	}
	base := data[lineStart:indentEnd]
	indent := append(append([]byte(nil), base...), ' ', ' ')
	return []byte("\n"), indent
}

func splice(data []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(replacement))
	out = append(out, data[:start]...)
	out = append(out, replacement...)
	out = append(out, data[end:]...)
	return out
}
