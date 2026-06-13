// Package hooks reads and analyzes the hook configuration that Claude Code
// stores in settings.json files. It powers `hookwise doctor`'s hook-safety
// checks: inventory/sprawl, missing binaries, network-dependent hooks on hot
// paths, and duplicate/overlapping hooks.
//
// The scanner is hermetic — it only reads files and never makes network calls —
// so every analysis is fully unit-testable.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
)

// HookEntry is a single command hook attached to one event type.
type HookEntry struct {
	Event      string // e.g. "PreToolUse"
	Matcher    string // tool-name matcher ("" means all)
	Type       string // hook type, usually "command"
	Command    string // the shell command string
	SourceFile string // settings file this entry came from
}

// Inventory is the flattened result of scanning one or more settings files.
type Inventory struct {
	Entries     []HookEntry
	ParseErrors []ParseError // non-fatal: malformed files are recorded, not raised
}

// ParseError records a settings file that could not be parsed.
type ParseError struct {
	File string
	Err  error
}

// settingsFile mirrors the relevant shape of a Claude Code settings.json.
//
//	{ "hooks": { "<Event>": [ { "matcher": "...", "hooks": [ {"type","command"} ] } ] } }
type settingsFile struct {
	Hooks map[string][]matcherGroup `json:"hooks"`
}

type matcherGroup struct {
	Matcher string        `json:"matcher"`
	Hooks   []commandHook `json:"hooks"`
}

type commandHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// Scan reads each settings file path in order and returns the combined hook
// inventory. Missing files are silently skipped; malformed files are recorded
// in Inventory.ParseErrors and do not abort the scan. The only error Scan
// returns is for an unreadable (but existing) file's non-IsNotExist failure —
// callers can otherwise treat the inventory as best-effort.
func Scan(paths []string) (*Inventory, error) {
	inv := &Inventory{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue // a missing settings file is normal
			}
			inv.ParseErrors = append(inv.ParseErrors, ParseError{File: p, Err: err})
			continue
		}

		var sf settingsFile
		if err := json.Unmarshal(data, &sf); err != nil {
			inv.ParseErrors = append(inv.ParseErrors, ParseError{
				File: p,
				Err:  fmt.Errorf("parse %s: %w", p, err),
			})
			continue
		}

		for event, groups := range sf.Hooks {
			for _, g := range groups {
				for _, h := range g.Hooks {
					inv.Entries = append(inv.Entries, HookEntry{
						Event:      event,
						Matcher:    g.Matcher,
						Type:       h.Type,
						Command:    h.Command,
						SourceFile: p,
					})
				}
			}
		}
	}
	return inv, nil
}

// CountByEvent returns the number of hook entries attached to each event type.
func (inv *Inventory) CountByEvent() map[string]int {
	counts := make(map[string]int)
	for _, e := range inv.Entries {
		counts[e.Event]++
	}
	return counts
}
