package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apitrc "github.com/slidebolt/sb-api/internal/trace"
	logging "github.com/slidebolt/sb-logging-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// Request is one command to send.
type Request struct {
	Plugin, Device, Entity, Action string
	Payload                        json.RawMessage
	// Words is what the person said, when the command answers a spoken or
	// typed request. With it the attempt is filed in the phrasebook.
	Words  string
	Caller string
	// Strict rejects payload fields the action does not define.
	Strict bool
	// Wait verifies the result by watching the targets' state.
	Wait bool
}

// Key is the target entity key.
func (r Request) Key() string { return r.Plugin + "." + r.Device + "." + r.Entity }

// Result is what happened.
type Result struct {
	Sent         bool
	Verification Verification
	Rejection    *Rejection
	Phrase       *Outcome
	Diagnostics  []string
	TraceID      string
}

// Text renders the result for an agent.
func (r Result) Text() string {
	var b strings.Builder
	if r.Rejection != nil {
		b.WriteString(r.Rejection.Error())
	} else if r.Verification.Status != "" {
		b.WriteString(r.Verification.Summary())
	} else {
		b.WriteString("sent")
	}
	if r.Phrase != nil {
		for _, n := range r.Phrase.Notes {
			b.WriteString("\n" + n)
		}
		e := r.Phrase.Entry
		if e.ID != "" {
			fmt.Fprintf(&b, "\nPhrasebook: %q -> %s %s is %s (entry %s).", e.Phrase, e.Target, e.Action, e.Status, e.ID)
		}
	}
	if len(r.Diagnostics) > 0 {
		b.WriteString("\nDiagnostics:\n" + strings.Join(r.Diagnostics, "\n"))
	}
	return b.String()
}

// Sender validates, publishes, verifies and records commands.
type Sender struct {
	Store    storage.Storage
	Msg      messenger.Messenger
	Logs     logging.Store
	Book     *Book
	Verifier Verifier
	Source   string // log source, e.g. "sb-api"
}

// Send runs the whole flow. A rejected command returns a Result with
// Rejection set and a nil error; err is only for publish failures.
func (s *Sender) Send(ctx context.Context, req Request) (Result, error) {
	ctx, traceID := apitrc.Ensure(ctx)
	res := Result{TraceID: traceID}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage("{}")
	}
	key := req.Key()
	rec, err := Validate(s.Store, key, req.Action, req.Payload, req.Strict)
	if err != nil {
		rej, ok := err.(*Rejection)
		if !ok {
			rej = &Rejection{Reason: err.Error()}
		}
		res.Rejection = rej
		apitrc.AppendLog(ctx, s.Logs, s.source(), "command.rejected", "warn", "command rejected before publishing", traceID, map[string]any{
			"target": key, "action": req.Action, "reason": rej.Reason, "words": req.Words,
		})
		res.Verification = Verification{Status: StatusNotSent}
		if req.Words != "" && s.Book != nil {
			// A rejected command for spoken words is filed as a known-wrong
			// mapping and counts as an ask for the repeat rules.
			if out, err := s.Book.Record(Attempt{
				Request: req.Words, Target: key, Action: req.Action, Payload: req.Payload,
				Result: res.Verification, TraceID: traceID, Caller: req.Caller,
			}); err == nil {
				res.Phrase = &out
			}
		}
		return res, nil
	}

	var before map[string]EntityRecord
	if req.Wait {
		s.Verifier.Store = s.Store
		before = s.Verifier.Snapshot(rec)
	}
	subject := key + ".command." + req.Action
	headers := apitrc.MessageHeaders(traceID, s.source(), key, req.Action)
	if err := s.Msg.PublishWithHeaders(subject, req.Payload, headers); err != nil {
		return res, fmt.Errorf("publish failed: %w", err)
	}
	res.Sent = true
	// Same event as before the phrasebook, which trace consumers rely on.
	apitrc.AppendLog(ctx, s.Logs, s.source(), "mcp.command.published", "info", "MCP published entity command", traceID, map[string]any{
		"subject": subject, "plugin": req.Plugin, "device": req.Device, "entity": req.Entity, "action": req.Action,
	})
	if req.Wait {
		res.Verification = s.Verifier.Wait(req.Action, req.Payload, before)
	}
	apitrc.AppendLog(ctx, s.Logs, s.source(), "command.result", "info", "command sent", traceID, map[string]any{
		"target": key, "action": req.Action, "status": res.Verification.Status,
		"lagging": len(res.Verification.Lagging()), "words": req.Words, "caller": req.Caller,
	})

	if req.Words != "" && s.Book != nil {
		out, err := s.Book.Record(Attempt{
			Request: req.Words, Target: key, Action: req.Action, Payload: req.Payload,
			Result: res.Verification, TraceID: traceID, Caller: req.Caller,
		})
		if err == nil {
			res.Phrase = &out
			// Diagnostics deepen with each ask; a result that did not fully work
			// gets the delivery and connectivity check at once, so the first
			// answer can already say why.
			level := out.Ask
			if (res.Verification.Status == StatusNoChange || res.Verification.Status == StatusPartial) && level < 3 {
				level = 3
			}
			if level >= 2 {
				d := Diagnoser{Store: s.Store, Logs: s.Logs}
				res.Diagnostics = d.Diagnose(ctx, level, out.Ask, res.Verification, traceID)
				if len(res.Diagnostics) > 0 {
					_ = s.Book.AttachDiagnostics(out.Entry.ID, res.Diagnostics)
					apitrc.AppendLog(ctx, s.Logs, s.source(), "phrasebook.diagnostics", "warn", "repeated request diagnostics", traceID, map[string]any{
						"words": req.Words, "ask": out.Ask, "entry": out.Entry.ID, "report": strings.Join(res.Diagnostics, "\n"),
					})
				}
			}
		}
	}
	return res, nil
}

func (s *Sender) source() string {
	if s.Source == "" {
		return "sb-api"
	}
	return s.Source
}
