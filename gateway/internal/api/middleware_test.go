package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestLoggingMiddleware(t *testing.T) {
	buf := captureLog(t)

	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/brew", nil))

	out := buf.String()
	assert.Contains(t, out, "POST")
	assert.Contains(t, out, "/brew")
	assert.Contains(t, out, "418")
}

func TestLoggingDefaultStatus(t *testing.T) {
	buf := captureLog(t)

	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Contains(t, buf.String(), "200")
}

func TestJSONContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	JSONContentType(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, jsonCT, rec.Header().Get("Content-Type"))
}
