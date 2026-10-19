package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// member is one key of a JSON object, kept in file order so editing hooks
// never reorders the rest of someone's settings.
type member struct {
	key   string
	value json.RawMessage
}

type object []member

func decodeObject(data []byte) (object, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("expected a JSON object")
	}
	var result object
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		result = append(result, member{key, value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected data after the JSON object")
	}
	return result, nil
}

func (o object) get(key string) json.RawMessage {
	for _, m := range o {
		if m.key == key {
			return m.value
		}
	}
	return nil
}

// set replaces key's value in place, appends it, or removes it when value is nil.
func (o object) set(key string, value json.RawMessage) object {
	for i, m := range o {
		if m.key == key {
			if value == nil {
				return append(o[:i:i], o[i+1:]...)
			}
			o[i].value = value
			return o
		}
	}
	if value == nil {
		return o
	}
	return append(o, member{key, value})
}

func (o object) encode() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// hookHandler is the part of a Claude Code or Codex hook handler bmux reads.
type hookHandler struct {
	Command string `json:"command"`
}

// editHooks rewrites the "hooks" table of a Claude Code settings.json or a
// Codex hooks.json. Every handler whose command contains marker is removed,
// along with groups and events left empty; then each event in add gets one
// group running command. Everything else keeps its content and order.
func editHooks(data []byte, marker, command string, add []string) ([]byte, error) {
	top, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	hooks, err := decodeObject(top.get("hooks"))
	if err != nil {
		return nil, fmt.Errorf("hooks: %w", err)
	}
	var events object
	for _, event := range hooks {
		var groups []json.RawMessage
		if err := json.Unmarshal(event.value, &groups); err != nil {
			return nil, fmt.Errorf("hooks.%s: %w", event.key, err)
		}
		kept := groups[:0]
		for _, raw := range groups {
			group, err := decodeObject(raw)
			if err != nil {
				return nil, fmt.Errorf("hooks.%s: %w", event.key, err)
			}
			var handlers []json.RawMessage
			if h := group.get("hooks"); h != nil {
				if err := json.Unmarshal(h, &handlers); err != nil {
					return nil, fmt.Errorf("hooks.%s: %w", event.key, err)
				}
			}
			ours := 0
			others := handlers[:0]
			for _, raw := range handlers {
				var h hookHandler
				_ = json.Unmarshal(raw, &h)
				if strings.Contains(h.Command, marker) {
					ours++
					continue
				}
				others = append(others, raw)
			}
			switch {
			case ours == 0:
				kept = append(kept, raw)
			case len(others) > 0:
				list, _ := json.Marshal(others)
				kept = append(kept, group.set("hooks", list).encode())
			}
		}
		if len(kept) > 0 {
			list, _ := json.Marshal(kept)
			events = append(events, member{event.key, list})
		}
	}
	handler, _ := json.Marshal(struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}{"command", command})
	group := object{{"hooks", json.RawMessage("[" + string(handler) + "]")}}.encode()
	for _, event := range add {
		var groups []json.RawMessage
		if existing := events.get(event); existing != nil {
			_ = json.Unmarshal(existing, &groups)
		}
		list, _ := json.Marshal(append(groups, group))
		events = events.set(event, list)
	}
	if len(events) == 0 {
		top = top.set("hooks", nil)
	} else {
		top = top.set("hooks", events.encode())
	}
	var out bytes.Buffer
	if err := json.Indent(&out, top.encode(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// hasHooks reports whether every event in want runs a command containing marker.
func hasHooks(data []byte, marker string, want []string) bool {
	var settings struct {
		Hooks map[string][]struct {
			Hooks []hookHandler `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return false
	}
	for _, event := range want {
		found := false
		for _, group := range settings.Hooks[event] {
			for _, h := range group.Hooks {
				found = found || strings.Contains(h.Command, marker)
			}
		}
		if !found {
			return false
		}
	}
	return true
}
