package factoryapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	attractor "dark-factory/internal/factory"
)

func TestHealth(t *testing.T) {
	s := NewServer(nil)
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestCreateRunValidation(t *testing.T) {
	s := NewServer(func(cfg attractor.RunConfig) error { return nil })
	r := httptest.NewRequest(http.MethodPost, "/runs", bytes.NewBufferString(`{"pipeline_path":"x"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", w.Code)
	}
}

func TestCreateAndGetRunSuccess(t *testing.T) {
	done := make(chan struct{}, 1)
	s := NewServer(func(cfg attractor.RunConfig) error {
		done <- struct{}{}
		return nil
	})
	body := `{"pipeline_path":"p.dot","workdir":"/w","runsdir":"/r","run_id":"abc"}`
	r := httptest.NewRequest(http.MethodPost, "/runs", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d body=%s", w.Code, w.Body.String())
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/runs/abc", nil)
	getW := httptest.NewRecorder()
	s.Handler().ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", getW.Code)
	}
	b, _ := io.ReadAll(getW.Body)
	var rec RunRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if rec.ID != "abc" {
		t.Fatalf("unexpected id: %s", rec.ID)
	}
	if rec.Status != RunSuccess {
		t.Fatalf("expected success got %s", rec.Status)
	}
}

func TestCreateRunFailure(t *testing.T) {
	s := NewServer(func(cfg attractor.RunConfig) error { return io.EOF })
	body := `{"pipeline_path":"p.dot","workdir":"/w","runsdir":"/r","run_id":"fail1"}`
	r := httptest.NewRequest(http.MethodPost, "/runs", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d", w.Code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		getReq := httptest.NewRequest(http.MethodGet, "/runs/fail1", nil)
		getW := httptest.NewRecorder()
		s.Handler().ServeHTTP(getW, getReq)
		if getW.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", getW.Code)
		}
		var rec RunRecord
		if err := json.Unmarshal(getW.Body.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Status == RunFailed {
			if rec.Error == "" {
				t.Fatal("expected error message")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for failed status")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
