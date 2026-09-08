package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-agent-scaffold/internal/app/config"
	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/infrastructure/ai"
)

// A local HTTP model exercises the real prompts, factory, runtime and model client.
// Its canned replies verify recording behavior; they are never saved as live evidence.
func TestOriginalPipelineRecordsUnmodifiedStageInputs(t *testing.T) {
	const analysis = "登录流程：输入、判断、成功与失败回路"
	const draft = "<mxfile>draft deliberately not validated</mxfile>"
	const final = "<mxfile>reviewer output kept exactly</mxfile>"
	replies := []string{analysis, draft, final}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string } `json:"messages"`
			Tools    json.RawMessage                  `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Tools) != 0 {
			t.Error("no-tools profile must not send tools")
		}
		if len(request.Messages) != 2 {
			t.Error("original pipeline should send system + current user")
		}
		if calls >= len(replies) {
			t.Error("unexpected extra call")
			http.Error(w, "unexpected call", 500)
			return
		}
		if calls == 1 && !strings.Contains(request.Messages[0].Content, analysis) {
			t.Error("drawer did not receive analysis_result")
		}
		if calls == 2 && !strings.Contains(request.Messages[0].Content, draft) {
			t.Error("reviewer did not receive draft_diagram")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": replies[calls]}}}})
		calls++
	}))
	defer server.Close()
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "test-key-not-real")
	t.Setenv("BAIDU_SEARCH_MCP_BASE_URI", "http://127.0.0.1:1")
	t.Setenv("BAIDU_SEARCH_MCP_SSE_ENDPOINT", "/sse")
	tables, err := config.LoadAgentTablesFile("../../configs/agent/agent-draw-io.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registered, recorder, err := assembleOriginal(tables["drawIoAgent"], ai.NewEinoProvider().WithRequestTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := registered.Runner.CreateSession("test-user")
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := registered.Runner.Run("test-user", sessionID, model.ChatContent{Texts: []model.TextPart{{Message: "画登录流程"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || outputs[0] != final {
		t.Fatalf("recorder changed output: %#v", outputs)
	}
	if len(recorder.calls) != 3 {
		t.Fatalf("expected three stages, got %d", len(recorder.calls))
	}
	for i, stage := range []string{"agent_analyst", "agent_drawer", "agent_reviewer"} {
		if recorder.calls[i].Stage != stage || recorder.calls[i].Reply.Content != replies[i] {
			t.Fatalf("stage %d recording mismatch: %#v", i, recorder.calls[i])
		}
	}
}

func TestDatasetRejectsDuplicateOrUnsafeIDs(t *testing.T) {
	for _, input := range []string{
		`{"dataset_id":"dev","cases":[{"id":"same","prompt":"a"},{"id":"same","prompt":"b"}]}`,
		`{"dataset_id":"dev","cases":[{"id":"../escape","prompt":"a"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "cases.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readDataset(path); err == nil {
			t.Fatalf("unsafe dataset accepted: %s", input)
		}
	}
}
