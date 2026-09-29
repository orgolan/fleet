// Package claudetrust records Claude Code's "do you trust this folder" answer for
// a path, so crewmates started in a project the captain registered do not stop at
// that dialog. Claude Code keeps the answer as projects[<path>].hasTrustDialogAccepted
// in its config file; only that one key is ever touched.
package claudetrust

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const key = "hasTrustDialogAccepted"

// ConfigPath is $CLAUDE_CONFIG_DIR/.claude.json, else ~/.claude.json.
func ConfigPath() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude.json"), nil
}

func canon(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// load returns the top-level document and its projects map, keeping every other
// value byte-for-byte (json.RawMessage) so numbers and strings survive a rewrite.
func load(cfg string) (top, projects map[string]json.RawMessage, err error) {
	top, projects = map[string]json.RawMessage{}, map[string]json.RawMessage{}
	b, err := os.ReadFile(cfg)
	if errors.Is(err, os.ErrNotExist) {
		return top, projects, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, nil, err
	}
	if raw, ok := top["projects"]; ok {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return nil, nil, err
		}
	}
	return top, projects, nil
}

// IsTrusted reports whether Claude Code already trusts path.
func IsTrusted(path string) (bool, error) {
	cfg, err := ConfigPath()
	if err != nil {
		return false, err
	}
	_, projects, err := load(cfg)
	if err != nil {
		return false, err
	}
	var entry map[string]json.RawMessage
	if raw, ok := projects[canon(path)]; ok {
		if json.Unmarshal(raw, &entry) != nil {
			return false, nil
		}
	}
	return string(entry[key]) == "true", nil
}

// Trust marks path as trusted. The file is re-read right before the write and
// replaced atomically with its permissions kept.
func Trust(path string) error {
	cfg, err := ConfigPath()
	if err != nil {
		return err
	}
	top, projects, err := load(cfg)
	if err != nil {
		return err
	}
	p := canon(path)
	entry := map[string]json.RawMessage{}
	if raw, ok := projects[p]; ok {
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
	}
	if string(entry[key]) == "true" {
		return nil
	}
	entry[key] = json.RawMessage("true")
	if projects[p], err = marshal(entry); err != nil {
		return err
	}
	if top["projects"], err = marshal(projects); err != nil {
		return err
	}
	out, err := marshal(top)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(cfg); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := cfg + ".fleet.tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), mode); err != nil {
		return err
	}
	return os.Rename(tmp, cfg)
}

// marshal encodes without HTML escaping, so text in untouched values is not altered.
func marshal(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
