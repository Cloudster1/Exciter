package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/observability"
)

// maxRequestIDLength bounds a client-supplied request id, which is echoed back
// and written to every log line.
const maxRequestIDLength = 64

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxLogger
)

// RequestIDFrom returns the id assigned to this request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

func loggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// requestID assigns every request an id and echoes it back, so one identifier
// a user can quote ties a support request to the exact log lines.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > maxRequestIDLength {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush and Unwrap keep server-sent events and http.ResponseController working
// through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logging attaches a request-scoped logger and writes one access line per request.
func logging(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			log := base.With("request_id", RequestIDFrom(r.Context()))
			if traceID := observability.TraceIDFrom(r.Context()); traceID != "" {
				log = log.With("trace_id", traceID)
			}
			ctx := context.WithValue(r.Context(), ctxLogger, log)

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", rec.bytes,
			)
		})
	}
}

// recovery turns a panic into a 500 in the error envelope rather than a
// dropped connection, and keeps the process alive.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				// http.ErrAbortHandler is a deliberate abort, not a bug.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				loggerFrom(r.Context()).Error("panic in handler", "panic", v, "path", r.URL.Path)
				respondError(w, r, ErrInternal(nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
