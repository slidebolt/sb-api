package commands

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	storage "github.com/slidebolt/sb-storage-sdk"
)

// Phrasebook entry statuses.
const (
	EntryVerified   = "verified"   // the command was seen to work for these words
	EntryUnverified = "unverified" // sent, but the result could not be checked
	EntrySuspect    = "suspect"    // worked, but the person asked again right after
	EntryWrong      = "wrong"      // did not work, or was replaced by a different command
)

const (
	// RepeatWindow is how soon an identical request counts as "that didn't work".
	RepeatWindow = 60 * time.Second
	// EscalationWindow is how long repeated asks keep raising the diagnostics level.
	EscalationWindow = 10 * time.Minute
	// repeatSimilarity is how close two requests must be to count as the same ask.
	repeatSimilarity = 0.8
)

// Entry is one learned mapping from words to a command.
type Entry struct {
	ID           string          `json:"id"`
	Phrase       string          `json:"phrase"`
	Normalized   string          `json:"normalized"`
	Target       string          `json:"target"`
	Action       string          `json:"action"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Status       string          `json:"status"`
	Reason       string          `json:"reason,omitempty"`
	Uses         int             `json:"uses"`
	Retries      int             `json:"retries,omitempty"`
	FirstSeen    time.Time       `json:"firstSeen"`
	LastUsed     time.Time       `json:"lastUsed"`
	LastResult   string          `json:"lastResult,omitempty"`
	Lagging      []string        `json:"lagging,omitempty"`
	Supersedes   string          `json:"supersedes,omitempty"`
	SupersededBy string          `json:"supersededBy,omitempty"`
	Caller       string          `json:"caller,omitempty"`
	Diagnostics  []string        `json:"diagnostics,omitempty"`
}

// Attempt is one command sent for a spoken request.
type Attempt struct {
	Request string
	Target  string
	Action  string
	Payload json.RawMessage
	Result  Verification
	TraceID string
	Caller  string
	At      time.Time

	normalized string
	entryID    string
}

// Outcome is what recording an attempt did, for the tool result.
type Outcome struct {
	Entry    Entry
	Notes    []string
	Ask      int      // how many times this request was made within EscalationWindow (1 = first)
	Previous *Attempt // the previous ask of the same request, if any
}

// Book stores entries in storage (internal target: never indexed, never in
// the inventory) and remembers recent attempts in memory for the repeat rules.
type Book struct {
	Store storage.Storage
	Now   func() time.Time

	mu     sync.Mutex
	recent []Attempt
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// EntryID identifies a (words, command) pair.
func EntryID(normalized, target, action string, payload json.RawMessage) string {
	h := sha1.Sum([]byte(normalized + "|" + target + "|" + action + "|" + canonicalJSON(payload)))
	return hex.EncodeToString(h[:8])
}

func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v) // map keys are sorted by encoding/json
	return string(out)
}

func sameCommand(a, b Attempt) bool {
	return a.Target == b.Target && a.Action == b.Action && canonicalJSON(a.Payload) == canonicalJSON(b.Payload)
}

// Record files an attempt: it applies the repeat rules to the previous ask of
// the same request, then updates the entry for this one.
func (b *Book) Record(a Attempt) (Outcome, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if a.At.IsZero() {
		a.At = b.now()
	}
	a.normalized = Normalize(a.Request)
	a.entryID = EntryID(a.normalized, a.Target, a.Action, a.Payload)
	tokens := Tokens(a.normalized, true)

	// Drop attempts older than the escalation window, then count earlier asks.
	kept := b.recent[:0]
	for _, r := range b.recent {
		if a.At.Sub(r.At) <= EscalationWindow {
			kept = append(kept, r)
		}
	}
	b.recent = kept
	// Rejected attempts are the agent fixing its own command within one
	// answer, not the person asking again, so they do not raise the ask count
	// (but a rejected attempt is still superseded by the fix below).
	out := Outcome{Ask: 1}
	if a.Result.Status == StatusNotSent {
		out.Ask = 0
	}
	for i := len(b.recent) - 1; i >= 0; i-- {
		r := b.recent[i]
		if Similarity(tokens, Tokens(r.normalized, true)) >= repeatSimilarity {
			if r.Result.Status != StatusNotSent && a.Result.Status != StatusNotSent {
				out.Ask++
			}
			if out.Previous == nil {
				prev := r
				out.Previous = &prev
			}
		}
	}
	if out.Ask < 1 {
		out.Ask = 1
	}

	identicalRepeat := false
	if prev := out.Previous; prev != nil && a.At.Sub(prev.At) <= RepeatWindow && !IsRelative(a.normalized) {
		ago := a.At.Sub(prev.At).Round(time.Second)
		worked := a.Result.Status == StatusVerified || a.Result.Status == StatusPartial
		switch {
		case prev.Result.Status == StatusNotSent && !sameCommand(*prev, a):
			// The agent's rejected command, fixed: link the fix to it.
			if err := b.mark(prev.entryID, EntryWrong, "rejected: not a command the entity takes", a.entryID); err != nil {
				return out, err
			}
		case prev.Result.Status == StatusNoChange && !sameCommand(*prev, a):
			if worked {
				if err := b.mark(prev.entryID, EntryWrong, fmt.Sprintf("nothing changed; a different command worked %s later", ago), a.entryID); err != nil {
					return out, err
				}
				out.Notes = append(out.Notes, fmt.Sprintf("The command tried %s ago changed nothing and this one worked; the old mapping is marked wrong.", ago))
			}
		case prev.Result.Status == StatusNoChange || prev.Result.Status == StatusNotSent:
			identicalRepeat = prev.Result.Status == StatusNoChange
		case !sameCommand(*prev, a):
			if err := b.mark(prev.entryID, EntryWrong, fmt.Sprintf("superseded by a different command when asked again %s later", ago), a.entryID); err != nil {
				return out, err
			}
			out.Notes = append(out.Notes, fmt.Sprintf("The same request %s ago used %s on %s; it is marked wrong and replaced by this command.", ago, prev.Action, prev.Target))
		default:
			identicalRepeat = true
		}
	}

	entry, err := b.load(a.entryID)
	if err != nil {
		return out, err
	}
	now := a.At
	if entry.ID == "" {
		entry = Entry{
			ID: a.entryID, Phrase: strings.TrimSpace(a.Request), Normalized: a.normalized,
			Target: a.Target, Action: a.Action, Payload: a.Payload, FirstSeen: now, Caller: a.Caller,
		}
		if out.Previous != nil && !identicalRepeat && a.At.Sub(out.Previous.At) <= RepeatWindow && out.Previous.entryID != a.entryID {
			entry.Supersedes = out.Previous.entryID
		}
	}
	entry.Uses++
	entry.LastUsed = now
	entry.LastResult = a.Result.Status
	entry.Lagging = nil
	for _, t := range a.Result.Lagging() {
		entry.Lagging = append(entry.Lagging, t.Name)
	}
	switch a.Result.Status {
	case StatusVerified, StatusPartial:
		entry.Status, entry.Reason = EntryVerified, ""
		if a.Result.Status == StatusPartial {
			entry.Reason = "last use only partly worked"
		}
	case StatusNotSent:
		entry.Status, entry.Reason = EntryWrong, "rejected: not a command the entity takes"
	case StatusNoChange:
		// The command was valid but nothing changed: usually the devices are
		// offline, which says nothing about whether the mapping is right.
		if entry.Status != EntryVerified && entry.Status != EntrySuspect {
			entry.Status = EntryUnverified
		}
		entry.Reason = "last use changed nothing (devices may be offline)"
	default:
		if entry.Status == "" || entry.Status == EntryWrong {
			entry.Status = EntryUnverified
		}
	}
	if identicalRepeat {
		entry.Retries++
		if entry.Status != EntryWrong {
			entry.Status = EntrySuspect
			entry.Reason = fmt.Sprintf("asked again right after it was used (%d times)", entry.Retries)
		}
		out.Notes = append(out.Notes, "The person asked for this again right after the same command; see the diagnostics for why it may not be taking effect.")
	}
	if err := b.save(entry); err != nil {
		return out, err
	}
	out.Entry = entry
	b.recent = append(b.recent, a)
	return out, nil
}

// AttachDiagnostics saves a diagnostics report on an entry.
func (b *Book) AttachDiagnostics(id string, lines []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, err := b.load(id)
	if err != nil || e.ID == "" {
		return err
	}
	e.Diagnostics = lines
	return b.save(e)
}

// Correct marks an entry wrong with the person's note. With no id it picks
// the most recent attempt (within EscalationWindow), optionally the one whose
// request best matches requestText.
func (b *Book) Correct(id, requestText, note string) (Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == "" {
		cutoff := b.now().Add(-EscalationWindow)
		best, bestScore := "", -1.0
		want := Tokens(Normalize(requestText), true)
		for i := len(b.recent) - 1; i >= 0; i-- {
			r := b.recent[i]
			if r.At.Before(cutoff) {
				break
			}
			score := 0.0
			if requestText != "" {
				score = Similarity(want, Tokens(r.normalized, true))
			}
			if score > bestScore {
				best, bestScore = r.entryID, score
			}
		}
		id = best
	}
	if id == "" {
		return Entry{}, fmt.Errorf("no recent command to correct; pass entry")
	}
	if err := b.mark(id, EntryWrong, "person said it did not work: "+strings.TrimSpace(note), ""); err != nil {
		return Entry{}, err
	}
	return b.load(id)
}

// Match is a lookup result.
type Match struct {
	Entry Entry
	Score float64
}

// Lookup returns the closest entries for a request. Numbers are ignored for
// matching so "30%" finds the "50%" example. Wrong entries are included only
// when they are very close, so the agent can avoid repeating a known mistake.
func (b *Book) Lookup(text string, limit int) ([]Match, error) {
	if limit <= 0 {
		limit = 5
	}
	entries, err := b.List()
	if err != nil {
		return nil, err
	}
	want := Tokens(Normalize(text), false)
	var out []Match
	for _, e := range entries {
		score := Similarity(want, Tokens(e.Normalized, false))
		if score < 0.3 || (e.Status == EntryWrong && score < 0.6) {
			continue
		}
		out = append(out, Match{Entry: e, Score: score})
	}
	rank := map[string]int{EntryVerified: 0, EntrySuspect: 1, EntryUnverified: 2, EntryWrong: 3}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if rank[out[i].Entry.Status] != rank[out[j].Entry.Status] {
			return rank[out[i].Entry.Status] < rank[out[j].Entry.Status]
		}
		return out[i].Entry.Uses > out[j].Entry.Uses
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// List returns every entry, most recently used first.
func (b *Book) List() ([]Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	all, err := b.readAll()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(all))
	for _, e := range all {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out, nil
}

// Get returns one entry.
func (b *Book) Get(id string) (Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, err := b.load(id)
	if err == nil && e.ID == "" {
		err = fmt.Errorf("no phrasebook entry %s", id)
	}
	return e, err
}

// Update sets an entry's status and reason by hand.
func (b *Book) Update(id, status, reason string) (Entry, error) {
	switch status {
	case EntryVerified, EntryUnverified, EntrySuspect, EntryWrong:
	default:
		return Entry{}, fmt.Errorf("status must be verified, unverified, suspect or wrong")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, err := b.load(id)
	if err != nil {
		return e, err
	}
	if e.ID == "" {
		return e, fmt.Errorf("no phrasebook entry %s", id)
	}
	e.Status, e.Reason = status, reason
	return e, b.save(e)
}

// Delete removes an entry.
func (b *Book) Delete(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	all, err := b.readAll()
	if err != nil {
		return err
	}
	delete(all, id)
	return b.writeAll(all)
}

// Prune deletes entries never verified and unused for maxAge.
func (b *Book) Prune(maxAge time.Duration) (int, error) {
	entries, err := b.List()
	if err != nil {
		return 0, err
	}
	n := 0
	cutoff := b.now().Add(-maxAge)
	for _, e := range entries {
		if e.Status != EntryVerified && e.LastUsed.Before(cutoff) {
			if err := b.Delete(e.ID); err == nil {
				n++
			}
		}
	}
	return n, nil
}

func (b *Book) mark(id, status, reason, supersededBy string) error {
	if id == "" {
		return nil
	}
	e, err := b.load(id)
	if err != nil || e.ID == "" {
		return err
	}
	e.Status, e.Reason = status, reason
	if supersededBy != "" && supersededBy != id {
		e.SupersededBy = supersededBy
	}
	return b.save(e)
}

// The whole phrasebook is one internal file: it stays small (one entry per
// distinct request and command), reads in one call, and needs no search.
const bookFile = rawKey("sb-phrasebook.entries")

func (b *Book) readAll() (map[string]Entry, error) {
	data, err := b.Store.ReadFile(storage.Internal, bookFile)
	all := map[string]Entry{}
	if err != nil || len(data) == 0 || string(data) == "null" {
		return all, nil // not written yet
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("phrasebook: %w", err)
	}
	return all, nil
}

func (b *Book) writeAll(all map[string]Entry) error {
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return b.Store.WriteFile(storage.Internal, bookFile, data)
}

func (b *Book) load(id string) (Entry, error) {
	all, err := b.readAll()
	if err != nil {
		return Entry{}, err
	}
	return all[id], nil
}

func (b *Book) save(e Entry) error {
	all, err := b.readAll()
	if err != nil {
		return err
	}
	all[e.ID] = e
	return b.writeAll(all)
}
