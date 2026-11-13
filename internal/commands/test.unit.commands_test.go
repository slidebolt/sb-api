package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	logcfg "github.com/slidebolt/sb-logging"
	logging "github.com/slidebolt/sb-logging-sdk"
	logserver "github.com/slidebolt/sb-logging/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ---------------------------------------------------------------------------
// memStore: just enough storage for command checks and the phrasebook.
// ---------------------------------------------------------------------------

type memStore struct {
	mu       sync.Mutex
	entities map[string]map[string]any
	files    map[string]json.RawMessage
}

func newMemStore() *memStore {
	return &memStore{entities: map[string]map[string]any{}, files: map[string]json.RawMessage{}}
}

func (m *memStore) put(key string, e map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entities[key] = e
}

func (m *memStore) setState(key string, state map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entities[key]; ok {
		e["state"] = state
	}
}

func (m *memStore) Get(key storage.Keyed) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entities[key.Key()]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return json.Marshal(e)
}

func match(pattern, key string) bool {
	if strings.HasSuffix(pattern, ".*") || strings.HasSuffix(pattern, ".>") {
		return strings.HasPrefix(key, pattern[:len(pattern)-1])
	}
	return pattern == key
}

func (m *memStore) Search(pattern string) ([]storage.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []storage.Entry
	for k, e := range m.entities {
		if match(pattern, k) {
			data, _ := json.Marshal(e)
			out = append(out, storage.Entry{Key: k, Data: data})
		}
	}
	return out, nil
}

// Query supports the group targets used here: labels.Group eq <value>.
func (m *memStore) Query(q storage.Query) ([]storage.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []storage.Entry
	for k, e := range m.entities {
		labels, _ := e["labels"].(map[string][]string)
		ok := true
		for _, f := range q.Where {
			want := fmt.Sprint(f.Value)
			found := false
			for _, v := range labels[strings.TrimPrefix(f.Field, "labels.")] {
				if v == want {
					found = true
				}
			}
			ok = ok && found
		}
		if ok && len(q.Where) > 0 {
			data, _ := json.Marshal(e)
			out = append(out, storage.Entry{Key: k, Data: data})
		}
	}
	return out, nil
}

func (m *memStore) SearchFiles(target storage.StorageTarget, pattern string) ([]storage.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []storage.Entry
	for k, v := range m.files {
		if match(pattern, k) {
			out = append(out, storage.Entry{Key: k, Data: v})
		}
	}
	return out, nil
}

func (m *memStore) WriteFile(target storage.StorageTarget, key storage.Keyed, data json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key.Key()] = data
	return nil
}

func (m *memStore) ReadFile(target storage.StorageTarget, key storage.Keyed) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.files[key.Key()]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *memStore) DeleteFile(target storage.StorageTarget, key storage.Keyed) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key.Key())
	return nil
}

func (m *memStore) Save(v storage.Keyed) error                               { return nil }
func (m *memStore) Delete(key storage.Keyed) error                           { return nil }
func (m *memStore) SetPrivate(key storage.Keyed, data json.RawMessage) error { return nil }
func (m *memStore) GetPrivate(key storage.Keyed) (json.RawMessage, error)    { return nil, nil }
func (m *memStore) DeletePrivate(key storage.Keyed) error                    { return nil }
func (m *memStore) SetInternal(key storage.Keyed, data json.RawMessage) error {
	return nil
}
func (m *memStore) GetInternal(key storage.Keyed) (json.RawMessage, error) { return nil, nil }
func (m *memStore) DeleteInternal(key storage.Keyed) error                 { return nil }
func (m *memStore) SetProfile(key storage.Keyed, data json.RawMessage) error {
	return nil
}
func (m *memStore) Close() {}

var _ storage.Storage = (*memStore)(nil)

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

var lightCommands = []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp"}

func light(name string, group string, power bool, brightness int) map[string]any {
	return map[string]any{
		"type": "light", "name": name, "commands": lightCommands,
		"labels": map[string][]string{"Group": {group}},
		"state":  map[string]any{"power": power, "brightness": brightness},
	}
}

type house struct {
	store  *memStore
	msg    messenger.Messenger
	logs   logging.Store
	sender *Sender
	broken map[string]bool // members the fake plugin never updates
}

// newHouse has a basement group with three bulbs and a fake plugin that
// applies light commands to the members' state (except broken ones).
func newHouse(t *testing.T) *house {
	t.Helper()
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { msg.Close() })
	logSvc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	h := &house{store: newMemStore(), msg: msg, logs: logSvc.Store(), broken: map[string]bool{}}
	h.store.put("plugin-automation.group.basement", map[string]any{
		"type": "light", "name": "Basement", "commands": lightCommands,
		"target": map[string]any{"pattern": "", "where": []map[string]any{{"field": "labels.Group", "op": "eq", "value": "basement"}}},
		"state":  map[string]any{"power": true, "brightness": 100},
	})
	for _, n := range []string{"bar-01", "bar-02", "tv-01"} {
		h.store.put("plugin-esphome."+n+"."+n+"_1", light(n, "basement", true, 100))
	}
	sub, err := msg.Subscribe("plugin-automation.group.basement.command.>", func(m *messenger.Message) {
		action := m.Subject[strings.LastIndex(m.Subject, ".")+1:]
		var p map[string]any
		_ = json.Unmarshal(m.Data, &p)
		for _, n := range []string{"bar-01", "bar-02", "tv-01"} {
			if h.broken[n] {
				continue
			}
			key := "plugin-esphome." + n + "." + n + "_1"
			switch action {
			case "light_turn_on":
				h.store.setState(key, map[string]any{"power": true, "brightness": 100})
			case "light_turn_off":
				h.store.setState(key, map[string]any{"power": false, "brightness": 100})
			case "light_set_brightness":
				b, _ := p["brightness"].(float64)
				h.store.setState(key, map[string]any{"power": b > 0, "brightness": b})
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sub.Unsubscribe() })
	if err := msg.Flush(); err != nil {
		t.Fatal(err)
	}
	h.sender = &Sender{
		Store: h.store, Msg: msg, Logs: h.logs, Book: &Book{Store: h.store},
		Verifier: Verifier{Timeout: 800 * time.Millisecond, Poll: 20 * time.Millisecond},
	}
	return h
}

func (h *house) send(t *testing.T, words, action, payload string) Result {
	t.Helper()
	res, err := h.sender.Send(context.Background(), Request{
		Plugin: "plugin-automation", Device: "group", Entity: "basement",
		Action: action, Payload: json.RawMessage(payload), Words: words, Strict: true, Wait: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (h *house) entry(t *testing.T, id string) Entry {
	t.Helper()
	e, err := h.sender.Book.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// ---------------------------------------------------------------------------
// before sending
// ---------------------------------------------------------------------------

func TestSend_UnknownActionIsRejectedWithTheActionsItTakes(t *testing.T) {
	h := newHouse(t)
	res := h.send(t, "set the basement lights to 50%", "set_brightness", `{"brightness":127}`)
	if res.Sent || res.Rejection == nil {
		t.Fatalf("want rejection, got %+v", res)
	}
	text := res.Text()
	for _, want := range []string{`does not take the action "set_brightness"`, "light_set_brightness {brightness: int 0-254, transition?: int seconds}"} {
		if !strings.Contains(text, want) {
			t.Errorf("rejection text missing %q:\n%s", want, text)
		}
	}
	if res.Phrase == nil || res.Phrase.Entry.Status != EntryWrong {
		t.Fatalf("rejected attempt for spoken words should be filed as wrong, got %+v", res.Phrase)
	}
}

func TestValidate_PayloadChecks(t *testing.T) {
	h := newHouse(t)
	key := "plugin-automation.group.basement"
	if _, err := Validate(h.store, key, "light_set_brightness", json.RawMessage(`{"brightness_pct":50}`), true); err == nil {
		t.Error("strict: an undefined field (which would decode as brightness 0) must be rejected")
	}
	if _, err := Validate(h.store, key, "light_set_brightness", json.RawMessage(`{"brightness":127,"extra":1}`), false); err != nil {
		t.Errorf("lenient: extra fields are tolerated, got %v", err)
	}
	if _, err := Validate(h.store, key, "light_set_brightness", json.RawMessage(`{"brightness":300}`), false); err == nil {
		t.Error("out-of-range brightness must be rejected")
	}
	if _, err := Validate(h.store, "plugin-x.nope.nope", "light_turn_on", nil, false); err == nil {
		t.Error("a missing entity must be rejected")
	}
}

// ---------------------------------------------------------------------------
// after sending
// ---------------------------------------------------------------------------

func TestSend_VerifiedWhenEveryMemberReachesTheState(t *testing.T) {
	h := newHouse(t)
	res := h.send(t, "set the basement lights to 50%", "light_set_brightness", `{"brightness":127}`)
	if res.Verification.Status != StatusVerified {
		t.Fatalf("want verified, got %s: %s", res.Verification.Status, res.Text())
	}
	if len(res.Verification.Checked) != 3 {
		t.Fatalf("a group is checked through its members, got %d", len(res.Verification.Checked))
	}
	if res.Phrase.Entry.Status != EntryVerified {
		t.Fatalf("entry should be verified, got %s", res.Phrase.Entry.Status)
	}
}

func TestSend_PartialNamesTheMembersThatDidNotChange(t *testing.T) {
	h := newHouse(t)
	h.broken["tv-01"] = true
	res := h.send(t, "turn off the basement lights", "light_turn_off", `{}`)
	if res.Verification.Status != StatusPartial {
		t.Fatalf("want partial, got %s", res.Verification.Status)
	}
	if !strings.Contains(res.Text(), "2 of 3") || !strings.Contains(res.Text(), "tv-01") {
		t.Fatalf("summary should name the lagging member: %s", res.Text())
	}
}

func TestSend_NoChangeIsNotConfirmed(t *testing.T) {
	h := newHouse(t)
	for _, n := range []string{"bar-01", "bar-02", "tv-01"} {
		h.broken[n] = true
	}
	res := h.send(t, "turn off the basement lights", "light_turn_off", `{}`)
	if res.Verification.Status != StatusNoChange || !strings.Contains(res.Text(), "NOT confirmed") {
		t.Fatalf("want no_change / NOT confirmed, got %s: %s", res.Verification.Status, res.Text())
	}
	// A valid command that changed nothing says nothing about the mapping:
	// usually the devices are offline. It stays unverified, with diagnostics.
	if res.Phrase.Entry.Status != EntryUnverified {
		t.Fatalf("a valid command that changed nothing is unverified, got %s", res.Phrase.Entry.Status)
	}
	if len(res.Diagnostics) == 0 {
		t.Fatal("a command that changed nothing gets diagnostics on the first ask")
	}
}

func TestSend_RejectedThenFixedIsOneAskAndLinksTheFix(t *testing.T) {
	h := newHouse(t)
	words := "set the basement lights to 50%"
	bad := h.send(t, words, "set_brightness", `{"brightness":127}`)
	good := h.send(t, words, "light_set_brightness", `{"brightness":127}`)
	if good.Phrase.Ask != 1 || len(good.Diagnostics) != 0 {
		t.Fatalf("the agent fixing its own rejected command is not a second ask: ask=%d diag=%v", good.Phrase.Ask, good.Diagnostics)
	}
	old := h.entry(t, bad.Phrase.Entry.ID)
	if old.Status != EntryWrong || old.SupersededBy != good.Phrase.Entry.ID {
		t.Fatalf("rejected entry should be wrong and point at the fix, got %+v", old)
	}
	if good.Phrase.Entry.Status != EntryVerified {
		t.Fatalf("the fix is verified, got %s", good.Phrase.Entry.Status)
	}
}

func TestRepeat_NothingChangedTwiceWithDifferentCommandsMarksNeitherWrong(t *testing.T) {
	b := &Book{Store: newMemStore()}
	first := record(t, b, attempt("turn off the basement lights", "light_turn_off", `{}`, StatusNoChange, 0))
	b2 := attempt("turn off the basement lights", "light_turn_off", `{}`, StatusNoChange, 10)
	b2.Target = "plugin-esphome.bar-01.bar-01_1"
	record(t, b, b2)
	if e, _ := b.Get(first.Entry.ID); e.Status == EntryWrong {
		t.Fatal("when nothing works either way, the first mapping is not proven wrong")
	}
}

// ---------------------------------------------------------------------------
// phrasebook repeat rules (driven through Book.Record with set times)
// ---------------------------------------------------------------------------

func at(sec int) time.Time { return time.Date(2026, 11, 9, 12, 0, sec, 0, time.UTC) }

func attempt(words, action, payload, status string, sec int) Attempt {
	return Attempt{Request: words, Target: "plugin-automation.group.basement", Action: action,
		Payload: json.RawMessage(payload), Result: Verification{Status: status}, At: at(sec)}
}

func record(t *testing.T, b *Book, a Attempt) Outcome {
	t.Helper()
	out, err := b.Record(a)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRepeat_FailedThenDifferentCommandMarksWrongAndLinks(t *testing.T) {
	b := &Book{Store: newMemStore()}
	first := record(t, b, attempt("set the basement lights to 50%", "light_set_brightness", `{"brightness":20}`, StatusNoChange, 0))
	second := record(t, b, attempt("Set the basement lights to 50%.", "light_set_brightness", `{"brightness":127}`, StatusVerified, 20))
	old, _ := b.Get(first.Entry.ID)
	if old.Status != EntryWrong || old.SupersededBy != second.Entry.ID {
		t.Fatalf("first entry: want wrong superseded by %s, got %+v", second.Entry.ID, old)
	}
	if second.Entry.Supersedes != first.Entry.ID || second.Entry.Status != EntryVerified {
		t.Fatalf("second entry: want verified superseding %s, got %+v", first.Entry.ID, second.Entry)
	}
	if second.Ask != 2 || len(second.Notes) == 0 {
		t.Fatalf("want ask 2 with a note, got ask=%d notes=%v", second.Ask, second.Notes)
	}
}

func TestRepeat_VerifiedThenDifferentCommandMarksWrong(t *testing.T) {
	b := &Book{Store: newMemStore()}
	first := record(t, b, attempt("turn off the basement lights", "light_turn_off", `{}`, StatusVerified, 0))
	b2 := attempt("turn the basement lights off", "light_turn_off", `{}`, StatusVerified, 30)
	b2.Target = "plugin-automation.group.everything"
	record(t, b, b2)
	if e, _ := b.Get(first.Entry.ID); e.Status != EntryWrong {
		t.Fatalf("a verified command replaced on a repeat is wrong, got %s", e.Status)
	}
}

func TestRepeat_IdenticalCommandAfterVerifiedIsSuspect(t *testing.T) {
	b := &Book{Store: newMemStore()}
	record(t, b, attempt("turn the basement lights off", "light_turn_off", `{}`, StatusVerified, 0))
	out := record(t, b, attempt("turn the basement lights off", "light_turn_off", `{}`, StatusVerified, 15))
	if out.Entry.Status != EntrySuspect || out.Entry.Retries != 1 || out.Entry.Uses != 2 {
		t.Fatalf("want suspect with 1 retry and 2 uses, got %+v", out.Entry)
	}
}

func TestRepeat_RelativeRequestsAndSlowRepeatsAreNotFailures(t *testing.T) {
	b := &Book{Store: newMemStore()}
	first := record(t, b, attempt("turn the basement lights up", "light_set_brightness", `{"brightness":150}`, StatusVerified, 0))
	record(t, b, attempt("turn the basement lights up", "light_set_brightness", `{"brightness":200}`, StatusVerified, 5))
	if e, _ := b.Get(first.Entry.ID); e.Status != EntryVerified {
		t.Fatalf("relative request repeated: want verified, got %s", e.Status)
	}

	slow := record(t, b, attempt("turn on the bar lights", "light_turn_on", `{}`, StatusVerified, 100))
	record(t, b, attempt("turn on the bar lights", "light_turn_on", `{}`, StatusVerified, 100+90))
	if e, _ := b.Get(slow.Entry.ID); e.Status != EntryVerified {
		t.Fatalf("repeat after more than a minute: want verified, got %s", e.Status)
	}
}

func TestRepeat_DifferentNumbersAreDifferentRequests(t *testing.T) {
	b := &Book{Store: newMemStore()}
	first := record(t, b, attempt("set the basement lights to 50%", "light_set_brightness", `{"brightness":127}`, StatusVerified, 0))
	out := record(t, b, attempt("set the basement lights to 100%", "light_set_brightness", `{"brightness":254}`, StatusVerified, 20))
	if e, _ := b.Get(first.Entry.ID); e.Status != EntryVerified || out.Ask != 1 {
		t.Fatalf("50%% then 100%% is two requests: first=%s ask=%d", e.Status, out.Ask)
	}
}

func TestCorrect_MarksTheMostRecentCommandWrong(t *testing.T) {
	b := &Book{Store: newMemStore(), Now: func() time.Time { return at(30) }}
	record(t, b, attempt("turn on the bar lights", "light_turn_on", `{}`, StatusVerified, 0))
	last := record(t, b, attempt("set the basement lights to 50%", "light_set_brightness", `{"brightness":127}`, StatusUnchecked, 20))
	e, err := b.Correct("", "", "I didn't see any light changes", false)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != last.Entry.ID || e.Status != EntryWrong || !strings.Contains(e.Reason, "didn't see") {
		t.Fatalf("want the last entry marked wrong with the note, got %+v", e)
	}
}

// ---------------------------------------------------------------------------
// lookup
// ---------------------------------------------------------------------------

func TestLookup_IgnoresNumbersAndRanksVerifiedFirst(t *testing.T) {
	b := &Book{Store: newMemStore()}
	record(t, b, attempt("set the basement lights to 50%", "set_brightness", `{"brightness":127}`, StatusNotSent, 0))
	record(t, b, attempt("set the basement lights to 50%", "light_set_brightness", `{"brightness":127}`, StatusVerified, 100))
	record(t, b, attempt("turn on the garage door light", "light_turn_on", `{}`, StatusVerified, 200))

	matches, err := b.Lookup("set the basement lights to 30%", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("want the verified example and the close known-wrong one, got %d: %+v", len(matches), matches)
	}
	if matches[0].Entry.Action != "light_set_brightness" || matches[0].Entry.Status != EntryVerified {
		t.Fatalf("verified example should rank first, got %+v", matches[0].Entry)
	}
	if matches[1].Entry.Status != EntryWrong {
		t.Fatalf("the known-wrong attempt should be shown so it is avoided, got %+v", matches[1].Entry)
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("Set the Basement lights to 50%."); got != "set the basement lights to 50 %" {
		t.Fatalf("normalize: %q", got)
	}
	if IsRelative(Normalize("dim the basement lights to 30%")) {
		t.Error("a request with a number is absolute")
	}
	if !IsRelative(Normalize("make it brighter")) {
		t.Error("brighter is relative")
	}
}

// ---------------------------------------------------------------------------
// escalating diagnostics
// ---------------------------------------------------------------------------

func TestDiagnostics_EscalateWithEachAsk(t *testing.T) {
	h := newHouse(t)
	h.broken["tv-01"] = true
	// tv-01 has a connectivity sensor that is off, and its plugin dropped commands.
	h.store.put("plugin-esphome.tv-01.status", map[string]any{
		"type": "binary_sensor", "name": "Connectivity",
		"state": map[string]any{"on": false, "deviceClass": "connectivity"},
	})
	h.store.put("sb-script.instances.abc", map[string]any{
		"name": "PartyTime", "status": "running", "resolvedTargets": []string{"plugin-esphome.tv-01.tv-01_1"},
	})
	// The fake plugin logs a drop for tv-01 under whatever trace it receives.
	sub, err := h.msg.Subscribe("plugin-automation.group.basement.command.>", func(m *messenger.Message) {
		_ = h.logs.Append(context.Background(), logging.Event{
			ID: fmt.Sprintf("drop-%d", time.Now().UnixNano()), TS: time.Now().UTC(), Source: "plugin-esphome",
			Kind: "device.command.dropped", Level: "warn", Message: "command dropped before ESPHome send",
			Plugin: "plugin-esphome", Device: "tv-01", TraceID: messenger.TraceID(m.Headers),
			Data: map[string]any{"reason": "no active ESPHome connection"},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	_ = h.msg.Flush()

	words := "turn off the basement lights"
	// Partly worked on the first ask: it explains itself at once (delivery and connectivity).
	r1 := h.send(t, words, "light_turn_off", `{}`)
	d1 := strings.Join(r1.Diagnostics, "\n")
	if !strings.Contains(d1, "Ask #1") || !strings.Contains(d1, "OFFLINE") || !strings.Contains(d1, "command dropped by plugin-esphome") ||
		!strings.Contains(d1, "Likely cause: 1 of 1 devices offline") || strings.Contains(d1, "PartyTime") {
		t.Fatalf("ask 1 with a partial result: lagging, delivery, connectivity, likely cause:\n%s", d1)
	}
	r2 := h.send(t, words, "light_turn_off", `{}`)
	if r2.Phrase.Entry.Status != EntrySuspect || !strings.Contains(strings.Join(r2.Diagnostics, "\n"), "Ask #2") {
		t.Fatalf("identical repeat should be suspect with ask-2 diagnostics, got %s %v", r2.Phrase.Entry.Status, r2.Diagnostics)
	}
	h.send(t, words, "light_turn_off", `{}`)
	r4 := h.send(t, words, "light_turn_off", `{}`)
	d4 := strings.Join(r4.Diagnostics, "\n")
	if !strings.Contains(d4, "PartyTime") || !strings.Contains(d4, "Conclusion: tv-01 offline") {
		t.Fatalf("ask 4 adds scripts and a conclusion:\n%s", d4)
	}
	if e := h.entry(t, r4.Phrase.Entry.ID); len(e.Diagnostics) == 0 {
		t.Fatal("the report is saved on the entry")
	}
}

func TestDiagnostics_AllReachedMeansTheyMayMeanOtherLights(t *testing.T) {
	h := newHouse(t)
	words := "turn off the basement lights"
	h.send(t, words, "light_turn_off", `{}`)
	r2 := h.send(t, words, "light_turn_off", `{}`)
	if !strings.Contains(strings.Join(r2.Diagnostics, "\n"), "may mean different lights") {
		t.Fatalf("want the 'different lights' hint, got %v", r2.Diagnostics)
	}
}

func TestExpectation_RGBUsesTheDomainFields(t *testing.T) {
	ok, checkable := Expectation("light_set_rgb", json.RawMessage(`{"r":255,"g":0,"b":10}`))
	if !checkable {
		t.Fatal("light_set_rgb with r,g,b is checkable")
	}
	if !ok(nil, map[string]any{"rgb": []any{250.0, 4.0, 12.0}}) || ok(nil, map[string]any{"rgb": []any{0.0, 0.0, 255.0}}) {
		t.Fatal("rgb check compares state.rgb with r,g,b")
	}
}

func TestDiagnostics_GroupDevicesThatShareACondition(t *testing.T) {
	store := newMemStore()
	var lagging []TargetState
	for i := 0; i < 30; i++ {
		dev := fmt.Sprintf("bulb-%02d", i)
		store.put("plugin-esphome."+dev+".connection", map[string]any{
			"type":  "diagnostic",
			"state": map[string]any{"connected": false, "lastError": fmt.Sprintf("dial tcp 10.0.0.%d:6053: connect: network is unreachable", i)},
		})
		lagging = append(lagging, TargetState{Key: "plugin-esphome." + dev + "." + dev + "_1", Name: dev})
	}
	lines := Diagnoser{Store: store}.Diagnose(context.Background(), 3, 1, Verification{Status: StatusNoChange, Checked: lagging}, "")
	if len(lines) != 3 {
		t.Fatalf("want summary, one grouped line and the likely cause, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[1], "30 devices (bulb-00, bulb-01, bulb-02, bulb-03 and 26 more): OFFLINE (network is unreachable)") {
		t.Fatalf("grouped line: %s", lines[1])
	}
	if !strings.Contains(lines[2], "30 of 30 devices offline") {
		t.Fatalf("likely cause: %s", lines[2])
	}
}
