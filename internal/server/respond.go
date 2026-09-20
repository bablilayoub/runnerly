package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bablilayoub/runnerly/internal/store"
)

// maxRequestBody bounds what a client can send. Agent payloads are small, and
// nothing here should ever be megabytes.
const maxRequestBody = 1 << 20 // 1 MiB

// apiError is the body of every failed response.
//
// One shape for every error means a client can always read the same fields,
// and `hint` carries the "what to do next" that the CLI already gives.
type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// writeJSON sends a value as JSON.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if body == nil || status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so there is nothing to tell the
		// client. Record it for the operator instead.
		s.log(r).Error("could not write the response body",
			"event", "response_failed", "error", err.Error())
	}
}

// fail sends an error response and logs anything the operator should see.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message, hint string) {
	if status >= http.StatusInternalServerError {
		s.log(r).Error(message, "event", "request_failed", "code", code, "status", status)
	}
	s.writeJSON(w, r, status, apiError{Error: code, Message: message, Hint: hint})
}

// failInternal reports a server-side failure without leaking its detail to
// the client, while keeping the detail in the log.
func (s *Server) failInternal(w http.ResponseWriter, r *http.Request, err error, what string) {
	s.log(r).Error(what, "event", "request_failed", "error", err.Error())
	s.fail(w, r, http.StatusInternalServerError, "internal_error",
		"Something went wrong on the server.", "Check the server logs for the detail.")
}

// failStore maps a store error onto a response, so handlers do not each
// re-derive that a missing row is a 404.
func (s *Server) failStore(w http.ResponseWriter, r *http.Request, err error, what string) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusNotFound, "not_found", what+" was not found.", "")
		return
	}
	s.failInternal(w, r, err, what)
}

// decode reads a JSON request body.
//
// Unknown fields are rejected: a client sending a field the server does not
// understand is either out of date or confused, and silently ignoring it
// produces the worst kind of bug — one where everything reports success and
// nothing happened.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(into); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.fail(w, r, http.StatusRequestEntityTooLarge, "body_too_large",
				fmt.Sprintf("The request body is larger than %d bytes.", maxRequestBody), "")
			return false
		}
		s.fail(w, r, http.StatusBadRequest, "invalid_body",
			"The request body could not be read: "+err.Error(),
			"Send JSON matching the documented fields for this endpoint.")
		return false
	}
	return true
}

// log returns a logger carrying the request's identifying fields.
func (s *Server) log(r *http.Request) *slog.Logger {
	logger := s.logger.With("component", Component)
	if r == nil {
		return logger
	}
	logger = logger.With("method", r.Method, "path", r.URL.Path)
	if runner, ok := runnerFrom(r.Context()); ok {
		logger = logger.With("runner", runner.Name, "runner_id", runner.ID)
	}
	if user, ok := userFrom(r.Context()); ok {
		logger = logger.With("user", user.Login)
	}
	return logger
}
