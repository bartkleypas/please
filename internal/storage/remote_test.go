package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bartkleypas/please/internal/graph"
)

func TestRemoteDaemonStorage(t *testing.T) {
	var receivedSessionID string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("X-Please-Session-ID")
		switch r.URL.Path {
		case "/api/v1/nodes":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{"id":"node-123","status":"saved"}`)
				return
			}
		case "/api/v1/graph":
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"nodes":{"node-123":{"id":"node-123","role":"user","content":"hello"}}}`)
				return
			}
		case "/api/v1/gc":
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"deleted_nodes":5}`)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	storage, err := NewRemoteDaemonStorage(ts.URL, "test-token", "")
	if err != nil {
		t.Fatalf("failed to create remote storage: %v", err)
	}

	if storage.SessionID == "" {
		t.Error("expected NewRemoteDaemonStorage to initialize non-empty SessionID")
	}

	// 1. Test SaveNode
	node := &graph.Node{ID: "node-123", Role: graph.RoleUser, Content: "hello"}
	if err := storage.SaveNode(node); err != nil {
		t.Fatalf("SaveNode failed: %v", err)
	}

	// 2. Test LoadGraph
	g, latestID, err := storage.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph failed: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(g.Nodes))
	}
	if latestID != "node-123" {
		t.Errorf("expected latest ID 'node-123', got '%s'", latestID)
	}

	// 3. Test GarbageCollect
	deleted, err := storage.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect failed: %v", err)
	}
	if deleted != 5 {
		t.Errorf("expected 5 deleted nodes, got %d", deleted)
	}

	if receivedSessionID != storage.SessionID {
		t.Errorf("expected daemon to receive session ID '%s', got '%s'", storage.SessionID, receivedSessionID)
	}
}

func TestRemoteDaemonStorage_PurgedRecordsAndStores(t *testing.T) {
	memories := make(map[string]Memory)
	blobs := make(map[string]ObservationBlob)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/gc" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status":"success","purged_records":7}`)
		case r.URL.Path == "/api/v1/memories" && r.Method == http.MethodPost:
			var mem Memory
			if err := json.NewDecoder(r.Body).Decode(&mem); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			memories[string(mem.Scope)+":"+mem.SessionID+":"+mem.Key] = mem
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(&mem)
		case r.URL.Path == "/api/v1/memories" && r.Method == http.MethodGet:
			q := r.URL.Query()
			if q.Get("exact") == "true" {
				mapKey := q.Get("scope") + ":" + q.Get("session_id") + ":" + q.Get("key")
				mem, ok := memories[mapKey]
				if !ok {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(&mem)
				return
			}
			var out []Memory
			for _, m := range memories {
				out = append(out, m)
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/api/v1/memories" && r.Method == http.MethodDelete:
			q := r.URL.Query()
			mapKey := q.Get("scope") + ":" + q.Get("session_id") + ":" + q.Get("key")
			if _, ok := memories[mapKey]; !ok {
				http.NotFound(w, r)
				return
			}
			delete(memories, mapKey)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		case r.URL.Path == "/api/v1/memories/diagnose" && r.Method == http.MethodGet:
			diag := MemoryDiagnostics{
				TotalMemories: len(memories),
				ByScope:       map[MemoryScope]int{ScopeWorkspace: len(memories)},
				ByCategory:    map[MemoryCategory]int{CategoryFact: len(memories)},
			}
			_ = json.NewEncoder(w).Encode(diag)
		case r.URL.Path == "/api/v1/observations" && r.Method == http.MethodPost:
			var blob ObservationBlob
			if err := json.NewDecoder(r.Body).Decode(&blob); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			blobs[blob.ReceiptID] = blob
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"status":"saved","receipt_id":%q}`, blob.ReceiptID)
		case r.URL.Path == "/api/v1/observations/obs_abc12345" && r.Method == http.MethodGet:
			blob, ok := blobs["obs_abc12345"]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(&blob)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	strg, err := NewRemoteDaemonStorage(ts.URL, "token", "")
	if err != nil {
		t.Fatalf("NewRemoteDaemonStorage failed: %v", err)
	}

	purged, err := strg.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect failed: %v", err)
	}
	if purged != 7 {
		t.Errorf("expected purged_records 7, got %d", purged)
	}

	mem := &Memory{
		Key:      "go_version",
		Content:  "1.25",
		Category: CategoryFact,
		Scope:    ScopeWorkspace,
		Tags:     []string{"env", "go"},
	}
	if err := strg.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	gotMem, err := strg.GetMemory(ScopeWorkspace, "", "go_version")
	if err != nil || gotMem == nil || gotMem.Content != "1.25" {
		t.Fatalf("GetMemory mismatch: got=%+v, err=%v", gotMem, err)
	}

	mems, err := strg.QueryMemories(MemoryFilter{Scope: ScopeWorkspace, Limit: 5})
	if err != nil || len(mems) != 1 {
		t.Fatalf("QueryMemories mismatch: len=%d, err=%v", len(mems), err)
	}

	diag, err := strg.DiagnoseMemories(ScopeWorkspace, "")
	if err != nil || diag.TotalMemories != 1 {
		t.Fatalf("DiagnoseMemories mismatch: diag=%+v, err=%v", diag, err)
	}

	if err := strg.DeleteMemory(ScopeWorkspace, "", "go_version"); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}
	if err := strg.DeleteMemory(ScopeWorkspace, "", "go_version"); !errors.Is(err, ErrMemoryNotFound) {
		t.Errorf("expected ErrMemoryNotFound on second delete, got %v", err)
	}

	blob := &ObservationBlob{
		ReceiptID: "obs_abc12345",
		NodeID:    "node-1",
		CreatedAt: time.Now(),
		Tool:      "read_file",
		Payload:   []byte("payload contents"),
	}
	if err := strg.SaveObservationBlob(blob); err != nil {
		t.Fatalf("SaveObservationBlob failed: %v", err)
	}
	gotBlob, err := strg.GetObservationBlob("obs_abc12345")
	if err != nil || string(gotBlob.Payload) != "payload contents" {
		t.Fatalf("GetObservationBlob mismatch: got=%+v, err=%v", gotBlob, err)
	}
}
