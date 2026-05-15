package httpx

import "net/http"

// StatusWriter wraps http.ResponseWriter to capture the status code and
// the total number of bytes written, for access logging.
type StatusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

// NewStatusWriter wraps w with a StatusWriter pre-initialised to 200.
func NewStatusWriter(w http.ResponseWriter) *StatusWriter {
	return &StatusWriter{ResponseWriter: w, status: http.StatusOK}
}

// Status returns the captured status code (200 if none was explicitly set).
func (w *StatusWriter) Status() int { return w.status }

// Bytes returns the number of bytes successfully written to the body.
func (w *StatusWriter) Bytes() int64 { return w.bytes }

// WriteHeader captures the status code and forwards it.
func (w *StatusWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the byte count and forwards the write. If WriteHeader was
// not called yet, http.ResponseWriter semantics require an implicit 200,
// which we record locally as well.
func (w *StatusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}
