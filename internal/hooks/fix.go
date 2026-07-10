package hooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// This file implements `hookwise audit --fix`: two-phase removal of EXACT
// duplicate hook entries (issue #40, amended design v1.1).
//
// Phase 1 — PlanDuplicateRemovals: pure, read-only. Groups hook entries by
// canonical full-object deep-equality (every field, including timeout/type),
// keyed WITHIN a single settings file only. The first occurrence in document
// order is always kept; later occurrences become removal candidates.
// Anything weaker than full-object intra-file equality — (matcher,command)
// matches with differing other fields, or the same hook appearing across
// files (layering) — is surfaced as a recommendation, never a removal.
//
// Phase 2 — ApplyRemovals: mutates ONLY the caller-accepted candidate IDs,
// guarded by a plan-time (mtime,size,sha256) fingerprint (TOCTOU: a live
// Claude Code session may write settings.json at any moment). The mutation
// is a byte-range splice of the original file, so every byte the user did
// not agree to remove — unknown JSON keys included — survives verbatim.
// Safety chain: timestamped backup before any write, atomic temp+rename
// write, then a re-scan gate (parseable, exact removed count, no hook event
// category vanished); any gate failure restores the backup automatically.

// ---------------------------------------------------------------------------
// Plan types
// ---------------------------------------------------------------------------

// FileGuard fingerprints a settings file at plan time so ApplyRemovals can
// detect (and refuse) concurrent modification.
type FileGuard struct {
	ModTime time.Time
	Size    int64
	SHA256  string
}

// RemovalCandidate is one exact-duplicate hook entry that may be removed.
// ID is deterministic for a given file content, so a plan ID resolves to the
// same entry at apply time once the FileGuard has verified the bytes are
// unchanged.
type RemovalCandidate struct {
	ID      string // "<event>#g<group>#h<hook>#<sha8 of canonical object>"
	File    string // settings file path
	Event   string // e.g. "PreToolUse"
	Matcher string // matcher of the enclosing group
	Command string // command string, for display
}

// Recommendation is a finding the fixer refuses to act on automatically.
type Recommendation struct {
	Kind    string // "near-duplicate" | "cross-file"
	Message string
}

// FixPlan is the output of PlanDuplicateRemovals.
type FixPlan struct {
	Removals        []RemovalCandidate
	Recommendations []Recommendation
	Guards          map[string]FileGuard // keyed by settings file path
	SkippedFiles    []ParseError         // unparseable files: never planned against
}

// ---------------------------------------------------------------------------
// File layout walk (byte-exact)
// ---------------------------------------------------------------------------

// elemRange is the byte range [Start,End) of one array element in the
// original file.
type elemRange struct {
	Start int
	End   int
}

// hookSite is one hook object's location within a settings file.
type hookSite struct {
	Event     string
	Matcher   string
	GroupIdx  int
	HookIdx   int
	ArrayID   int // Start offset of the enclosing "hooks" array's first element region; unique per array
	Range     elemRange
	Canonical string // canonical JSON of the full hook object
	Command   string
}

// fileLayout is the byte-exact map of every hook entry in one settings file.
type fileLayout struct {
	sites  []hookSite          // document order
	arrays map[int][]elemRange // ArrayID → all element ranges of that hooks array
}

// canonicalJSON re-marshals a raw JSON value with sorted object keys and no
// insignificant whitespace, giving a deep-equality key for full objects.
func canonicalJSON(raw []byte) (string, error) {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v) // maps marshal with sorted keys
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// decodeRawWithOffsets decodes the next JSON value from dec into a
// RawMessage and returns its exact byte range within data (base-relative).
// The decoder guarantees RawMessage holds the verbatim input bytes of the
// value; the invariant is re-checked against data to make any offset-math
// bug loud instead of silently splicing the wrong bytes.
func decodeRawWithOffsets(dec *json.Decoder, data []byte, base int) (json.RawMessage, elemRange, error) {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, elemRange{}, err
	}
	end := base + int(dec.InputOffset())
	start := end - len(raw)
	if start < 0 || end > len(data) || !bytes.Equal(data[start:end], raw) {
		return nil, elemRange{}, fmt.Errorf("hooks: internal offset mismatch while walking settings JSON")
	}
	return raw, elemRange{Start: start, End: end}, nil
}

// expectDelim consumes one token and verifies it is the given delimiter.
func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("hooks: expected %q in settings JSON, got %v", want, tok)
	}
	return nil
}

// walkSettings maps every hook entry in a settings file to its exact byte
// range. Non-conforming shapes (hooks value not an object, event value not an
// array, group not an object) are tolerated and yield no sites — the fixer
// simply has nothing it can safely remove there.
func walkSettings(data []byte) (*fileLayout, error) {
	layout := &fileLayout{arrays: map[int][]elemRange{}}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		raw, r, err := decodeRawWithOffsets(dec, data, 0)
		if err != nil {
			return nil, err
		}
		if key != "hooks" {
			continue
		}
		if err := walkHooksObject(raw, r.Start, data, layout); err != nil {
			return nil, err
		}
	}
	return layout, nil
}

// walkHooksObject walks the value of the top-level "hooks" key.
func walkHooksObject(hooksRaw []byte, base int, data []byte, layout *fileLayout) error {
	trimmed := bytes.TrimSpace(hooksRaw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil // "hooks" is not an object — nothing to plan
	}
	dec := json.NewDecoder(bytes.NewReader(hooksRaw))
	if err := expectDelim(dec, '{'); err != nil {
		return err
	}
	for dec.More() {
		evTok, err := dec.Token()
		if err != nil {
			return err
		}
		event, _ := evTok.(string)
		raw, r, err := decodeRawWithOffsets(dec, data, base)
		if err != nil {
			return err
		}
		if err := walkEventGroups(raw, r.Start, event, data, layout); err != nil {
			return err
		}
	}
	return nil
}

// walkEventGroups walks one event's matcher-group array.
func walkEventGroups(groupsRaw []byte, base int, event string, data []byte, layout *fileLayout) error {
	trimmed := bytes.TrimSpace(groupsRaw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil // event value is not an array — nothing to plan
	}
	dec := json.NewDecoder(bytes.NewReader(groupsRaw))
	if err := expectDelim(dec, '['); err != nil {
		return err
	}
	for gi := 0; dec.More(); gi++ {
		raw, r, err := decodeRawWithOffsets(dec, data, base)
		if err != nil {
			return err
		}
		if err := walkGroup(raw, r.Start, event, gi, data, layout); err != nil {
			return err
		}
	}
	return nil
}

// walkGroup walks one matcher group's "hooks" array, recording a hookSite
// for each hook object.
func walkGroup(groupRaw []byte, base int, event string, gi int, data []byte, layout *fileLayout) error {
	trimmed := bytes.TrimSpace(groupRaw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil // group is not an object — nothing to plan
	}

	// Field order inside the group is arbitrary, so read the matcher via a
	// plain unmarshal before walking for offsets.
	var meta struct {
		Matcher string `json:"matcher"`
	}
	_ = json.Unmarshal(groupRaw, &meta) // tolerated: non-string matcher → ""

	dec := json.NewDecoder(bytes.NewReader(groupRaw))
	if err := expectDelim(dec, '{'); err != nil {
		return err
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		raw, r, err := decodeRawWithOffsets(dec, data, base)
		if err != nil {
			return err
		}
		if key != "hooks" {
			continue
		}
		inner := bytes.TrimSpace(raw)
		if len(inner) == 0 || inner[0] != '[' {
			continue // group "hooks" is not an array — nothing to plan
		}
		hdec := json.NewDecoder(bytes.NewReader(raw))
		if err := expectDelim(hdec, '['); err != nil {
			return err
		}
		arrayID := r.Start
		for hi := 0; hdec.More(); hi++ {
			hraw, hr, err := decodeRawWithOffsets(hdec, data, r.Start)
			if err != nil {
				return err
			}
			canon, err := canonicalJSON(hraw)
			if err != nil {
				return err
			}
			var ch commandHook
			_ = json.Unmarshal(hraw, &ch)
			layout.arrays[arrayID] = append(layout.arrays[arrayID], hr)
			layout.sites = append(layout.sites, hookSite{
				Event:     event,
				Matcher:   meta.Matcher,
				GroupIdx:  gi,
				HookIdx:   hi,
				ArrayID:   arrayID,
				Range:     hr,
				Canonical: canon,
				Command:   ch.Command,
			})
		}
	}
	return nil
}

// siteID builds the deterministic removal-candidate ID for a site.
func siteID(s hookSite) string {
	sum := sha256.Sum256([]byte(s.Canonical))
	return fmt.Sprintf("%s#g%d#h%d#%s", s.Event, s.GroupIdx, s.HookIdx, hex.EncodeToString(sum[:])[:8])
}

// planFile computes the per-file layout and the removable-duplicate set.
// Duplicate identity is (event, matcher, canonical full hook object), seen
// within this file only; the first occurrence in document order is kept.
func planFile(path string, data []byte) (*fileLayout, []RemovalCandidate, error) {
	layout, err := walkSettings(data)
	if err != nil {
		return nil, nil, err
	}
	type dupKey struct{ event, matcher, canonical string }
	seen := map[dupKey]bool{}
	var removals []RemovalCandidate
	for _, s := range layout.sites {
		k := dupKey{s.Event, s.Matcher, s.Canonical}
		if seen[k] {
			removals = append(removals, RemovalCandidate{
				ID:      siteID(s),
				File:    path,
				Event:   s.Event,
				Matcher: s.Matcher,
				Command: s.Command,
			})
			continue
		}
		seen[k] = true
	}
	return layout, removals, nil
}

// captureGuard fingerprints the file for the TOCTOU check.
func captureGuard(path string, data []byte) (FileGuard, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileGuard{}, err
	}
	sum := sha256.Sum256(data)
	return FileGuard{
		ModTime: info.ModTime(),
		Size:    info.Size(),
		SHA256:  hex.EncodeToString(sum[:]),
	}, nil
}

// PlanDuplicateRemovals scans the given settings files and returns the fix
// plan: exact intra-file duplicate removals, plus recommendations for
// everything the fixer must not touch (near-duplicates and cross-file
// layering). It performs no writes.
func PlanDuplicateRemovals(paths []string) (*FixPlan, error) {
	plan := &FixPlan{Guards: map[string]FileGuard{}}

	type xfKey struct{ event, matcher, canonical string }
	xfFirst := map[xfKey]string{} // key → first file it appeared in
	xfFlagged := map[xfKey]bool{}
	type nearKey struct{ event, matcher, command string }
	nearFirst := map[nearKey]string{} // key → first canonical seen
	nearFlagged := map[nearKey]bool{}

	visited := map[string]bool{}
	for _, path := range paths {
		if visited[path] {
			continue
		}
		visited[path] = true

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // a missing settings file is normal
			}
			plan.SkippedFiles = append(plan.SkippedFiles, ParseError{File: path, Err: err})
			continue
		}
		layout, removals, err := planFile(path, data)
		if err != nil {
			plan.SkippedFiles = append(plan.SkippedFiles, ParseError{
				File: path,
				Err:  fmt.Errorf("parse %s: %w", path, err),
			})
			continue
		}
		guard, err := captureGuard(path, data)
		if err != nil {
			plan.SkippedFiles = append(plan.SkippedFiles, ParseError{File: path, Err: err})
			continue
		}
		plan.Guards[path] = guard
		plan.Removals = append(plan.Removals, removals...)

		for _, s := range layout.sites {
			// Near-duplicate: same (event, matcher, command) but the full
			// object differs (e.g. timeout). Recommendation only.
			nk := nearKey{s.Event, s.Matcher, s.Command}
			if first, ok := nearFirst[nk]; !ok {
				nearFirst[nk] = s.Canonical
			} else if first != s.Canonical && !nearFlagged[nk] {
				nearFlagged[nk] = true
				plan.Recommendations = append(plan.Recommendations, Recommendation{
					Kind: "near-duplicate",
					Message: fmt.Sprintf("%q on %s (matcher %q) appears with differing fields (e.g. timeout/type) — review manually, not auto-removed",
						s.Command, s.Event, s.Matcher),
				})
			}

			// Cross-file repeat: identical full object in another file is
			// layering (shared vs local settings). Recommendation only.
			xk := xfKey{s.Event, s.Matcher, s.Canonical}
			if first, ok := xfFirst[xk]; !ok {
				xfFirst[xk] = path
			} else if first != path && !xfFlagged[xk] {
				xfFlagged[xk] = true
				plan.Recommendations = append(plan.Recommendations, Recommendation{
					Kind: "cross-file",
					Message: fmt.Sprintf("%q on %s (matcher %q) appears in both %s and %s — settings layering, review manually, not auto-removed",
						s.Command, s.Event, s.Matcher, first, path),
				})
			}
		}
	}
	return plan, nil
}

// ---------------------------------------------------------------------------
// Apply
// ---------------------------------------------------------------------------

// applyWriteFile is the write seam for ApplyRemovals' main mutation. Tests
// override it to simulate a corrupted write; backup restore deliberately
// bypasses it.
var applyWriteFile = atomicWriteFile

// atomicWriteFile writes data to path via a temp file + rename in the same
// directory, so readers never observe a partially-written settings file.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hookwise-fix-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// spliceRemovals removes the given sites from data, returning the new file
// content. Removal is by byte range: for each maximal run of removed
// elements within one array the cut extends to the next kept element (or
// swallows the preceding separator when the run reaches the array tail), so
// commas stay balanced and every byte outside the cuts survives verbatim.
func spliceRemovals(data []byte, layout *fileLayout, remove []hookSite) []byte {
	byArray := map[int][]int{}
	for _, s := range remove {
		byArray[s.ArrayID] = append(byArray[s.ArrayID], s.HookIdx)
	}

	var cuts []elemRange
	for arrayID, idxs := range byArray {
		elems := layout.arrays[arrayID]
		sort.Ints(idxs)
		for i := 0; i < len(idxs); {
			j := i
			for j+1 < len(idxs) && idxs[j+1] == idxs[j]+1 {
				j++
			}
			a, b := idxs[i], idxs[j]
			var cut elemRange
			switch {
			case b < len(elems)-1:
				cut = elemRange{Start: elems[a].Start, End: elems[b+1].Start}
			case a > 0:
				cut = elemRange{Start: elems[a-1].End, End: elems[b].End}
			default:
				cut = elemRange{Start: elems[a].Start, End: elems[b].End}
			}
			cuts = append(cuts, cut)
			i = j + 1
		}
	}

	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Start > cuts[j].Start })
	out := append([]byte(nil), data...)
	for _, c := range cuts {
		out = append(out[:c.Start], out[c.End:]...)
	}
	return out
}

// scanBytes parses settings content the same way Scan does, without touching
// the filesystem — used for the before/after validation gate.
func scanBytes(data []byte) (*settingsFile, error) {
	var sf settingsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	return &sf, nil
}

// entryCountAndEvents returns the flattened hook-entry count and the set of
// event categories present.
func entryCountAndEvents(sf *settingsFile) (int, map[string]bool) {
	count := 0
	events := map[string]bool{}
	for event, groups := range sf.Hooks {
		for _, g := range groups {
			if len(g.Hooks) > 0 {
				events[event] = true
			}
			count += len(g.Hooks)
		}
	}
	return count, events
}

// ApplyRemovals removes exactly the accepted duplicate candidates from the
// settings file at path. guard must be the FileGuard captured for path by
// PlanDuplicateRemovals; if the file changed since plan time the apply is
// refused. Returns the number of entries removed and the backup file path.
//
// Safety chain: TOCTOU re-check → timestamped backup → byte-splice → atomic
// write → re-scan gate (parseable, removed count matches, no event category
// vanished). Any post-write gate failure restores the backup automatically.
func ApplyRemovals(path string, acceptedIDs []string, guard FileGuard) (removed int, backupPath string, err error) {
	if len(acceptedIDs) == 0 {
		return 0, "", nil
	}

	// TOCTOU gate: refuse if the file changed since the plan was made. A live
	// Claude Code session can rewrite settings.json at any moment.
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", fmt.Errorf("hooks: stat %s: %w", path, err)
	}
	if !info.ModTime().Equal(guard.ModTime) || info.Size() != guard.Size {
		return 0, "", fmt.Errorf("hooks: %s changed since the plan was made (mtime/size differ) — re-run `hookwise audit --fix`", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", fmt.Errorf("hooks: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != guard.SHA256 {
		return 0, "", fmt.Errorf("hooks: %s changed since the plan was made (content differs) — re-run `hookwise audit --fix`", path)
	}

	// Re-derive the removable set from the (verified-identical) bytes and
	// resolve the accepted IDs. Only IDs in the removable set are reachable:
	// a keeper (first occurrence) or unknown ID is refused outright.
	layout, removals, err := planFile(path, data)
	if err != nil {
		return 0, "", fmt.Errorf("hooks: re-plan %s: %w", path, err)
	}
	removable := map[string]bool{}
	for _, r := range removals {
		removable[r.ID] = true
	}
	siteByID := map[string]hookSite{}
	for _, s := range layout.sites {
		siteByID[siteID(s)] = s
	}
	var toRemove []hookSite
	for _, id := range acceptedIDs {
		if !removable[id] {
			return 0, "", fmt.Errorf("hooks: %q is not a removable duplicate in %s — refusing to apply", id, path)
		}
		toRemove = append(toRemove, siteByID[id])
	}

	// Backup BEFORE any mutation, preserving the original file's mode.
	backupPath, err = writeBackup(path, data, info.Mode().Perm())
	if err != nil {
		return 0, "", fmt.Errorf("hooks: backup %s: %w", path, err)
	}

	newData := spliceRemovals(data, layout, toRemove)

	if err := applyWriteFile(path, newData, info.Mode().Perm()); err != nil {
		return 0, backupPath, fmt.Errorf("hooks: write %s: %w", path, err)
	}

	// Post-write gate. Scan is fail-open (nil error on parse failure), so
	// err==nil is NOT validation — the gates below are.
	if gateErr := validateAfterWrite(path, data, len(toRemove)); gateErr != nil {
		if restoreErr := atomicWriteFile(path, data, info.Mode().Perm()); restoreErr != nil {
			return 0, backupPath, fmt.Errorf("hooks: %v; AND restoring backup failed: %v (backup preserved at %s)", gateErr, restoreErr, backupPath)
		}
		return 0, backupPath, fmt.Errorf("hooks: %w — original restored from backup", gateErr)
	}

	return len(toRemove), backupPath, nil
}

// writeBackup writes data to "<path>.bak-<RFC3339>" (suffixed on collision)
// with O_EXCL so an existing backup is never clobbered.
func writeBackup(path string, data []byte, perm os.FileMode) (string, error) {
	stamp := time.Now().UTC().Format(time.RFC3339)
	for i := 0; ; i++ {
		candidate := fmt.Sprintf("%s.bak-%s", path, stamp)
		if i > 0 {
			candidate = fmt.Sprintf("%s.bak-%s.%d", path, stamp, i)
		}
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return candidate, nil
	}
}

// validateAfterWrite is the post-write gate: the rewritten file must parse
// cleanly via the same scanner the rest of hookwise uses, the flattened
// entry count must have dropped by exactly the accepted amount, and no hook
// event category may have vanished.
func validateAfterWrite(path string, before []byte, expectedRemoved int) error {
	inv, scanErr := Scan([]string{path})
	if scanErr != nil || len(inv.ParseErrors) != 0 {
		return fmt.Errorf("rewritten %s does not scan cleanly", path)
	}
	beforeSF, err := scanBytes(before)
	if err != nil {
		return fmt.Errorf("original %s no longer parses: %v", path, err)
	}
	beforeCount, beforeEvents := entryCountAndEvents(beforeSF)

	afterData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read %s: %v", path, err)
	}
	afterSF, err := scanBytes(afterData)
	if err != nil {
		return fmt.Errorf("rewritten %s does not parse: %v", path, err)
	}
	afterCount, afterEvents := entryCountAndEvents(afterSF)

	if beforeCount-afterCount != expectedRemoved {
		return fmt.Errorf("rewritten %s removed %d entries, expected %d", path, beforeCount-afterCount, expectedRemoved)
	}
	for ev := range beforeEvents {
		if !afterEvents[ev] {
			return fmt.Errorf("rewritten %s lost hook event category %s", path, ev)
		}
	}
	return nil
}
