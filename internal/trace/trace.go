package trace

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	logging "github.com/slidebolt/sb-logging-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
)

const HeaderTraceID = "X-Sb-Trace-Id"

type contextKey string

const traceIDKey contextKey = "sb_api_trace_id"

var sequence uint64

func NewID() string {
	return fmt.Sprintf("api-%d-%d", time.Now().UTC().UnixNano(), atomic.AddUint64(&sequence, 1))
}

func WithContext(ctx context.Context, traceID string) context.Context {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey, traceID)
}

func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func Ensure(ctx context.Context) (context.Context, string) {
	if traceID := FromContext(ctx); traceID != "" {
		return ctx, traceID
	}
	traceID := NewID()
	return WithContext(ctx, traceID), traceID
}

func FromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(HeaderTraceID))
}

func WithRequestTrace(r *http.Request) (*http.Request, string) {
	if r == nil {
		return r, ""
	}
	traceID := FromRequest(r)
	if traceID == "" {
		traceID = NewID()
	}
	return r.WithContext(WithContext(r.Context(), traceID)), traceID
}

func MessageHeaders(traceID, originService, originEntity, originAction string) messenger.Headers {
	headers := messenger.WithTraceID(nil, strings.TrimSpace(traceID))
	return messenger.WithOrigin(headers, originService, originEntity, originAction)
}

func AppendLog(ctx context.Context, logger logging.Store, source, kind, level, message, traceID string, data map[string]any) {
	if logger == nil {
		return
	}
	if traceID == "" {
		traceID = FromContext(ctx)
	}
	event := logging.Event{
		ID:      NewID(),
		TS:      time.Now().UTC(),
		Source:  strings.TrimSpace(source),
		Kind:    strings.TrimSpace(kind),
		Level:   strings.TrimSpace(level),
		Message: strings.TrimSpace(message),
		TraceID: strings.TrimSpace(traceID),
		Data:    data,
	}
	_ = logger.Append(ctx, event)
}
