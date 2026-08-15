package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alpha-omega-corp/_core/app"
	"github.com/uptrace/bunrouter"
)

// Slot and slotHandler stand in for the consumer types this form exists for:
// a handler returning a value slice, taking an argument past the context.
type Slot struct {
	StartsAt time.Time `json:"startsAt"`
}

type slotHandler struct {
	err error
}

func (h *slotHandler) Available(_ context.Context, from time.Time) ([]Slot, error) {
	if h.err != nil {
		return nil, h.err
	}

	return []Slot{{StartsAt: from}}, nil
}

func serve(t *testing.T, h bunrouter.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()

	router := bunrouter.New()
	router.GET("/slots", h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slots", nil))

	return rec
}

// A value slice reaches the client as a JSON array, with no pointer dance in
// the controller.
func TestHttpResponseEncodesValues(t *testing.T) {
	handler := &slotHandler{}
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)

	rec := serve(t, app.HttpResponse(func(req bunrouter.Request) ([]Slot, error) {
		return handler.Available(req.Context(), now)
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var slots []Slot
	if err := json.Unmarshal(rec.Body.Bytes(), &slots); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}

	if len(slots) != 1 || !slots[0].StartsAt.Equal(now) {
		t.Fatalf("slots = %+v, want one slot at %s", slots, now)
	}
}

// An untagged error is the server's fault, and nothing follows the error body.
func TestHttpResponseUntaggedErrorIs500(t *testing.T) {
	handler := &slotHandler{err: errors.New("query failed")}

	rec := serve(t, app.HttpResponse(func(req bunrouter.Request) ([]Slot, error) {
		return handler.Available(req.Context(), time.Now())
	}))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	if body := rec.Body.String(); body != "query failed" {
		t.Fatalf("body = %q, want the error alone with no JSON appended", body)
	}
}

// A tagged error keeps its status even after being wrapped on the way up.
func TestHttpResponseTaggedErrorKeepsStatus(t *testing.T) {
	handler := &slotHandler{
		err: fmt.Errorf("available: %w", app.HttpFail(http.StatusNotFound, errors.New("no such shop"))),
	}

	rec := serve(t, app.HttpResponse(func(req bunrouter.Request) ([]Slot, error) {
		return handler.Available(req.Context(), time.Now())
	}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// The request is in scope, so a route keyed by something other than :id stays
// in this form.
func TestHttpResponseReadsRequest(t *testing.T) {
	router := bunrouter.New()
	router.GET("/orders/:reference", app.HttpResponse(func(req bunrouter.Request) (string, error) {
		return req.Param("reference"), nil
	}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders/AB-1234", nil))

	if got := rec.Body.String(); got != "\"AB-1234\"\n" {
		t.Fatalf("body = %q, want the reference", got)
	}
}
