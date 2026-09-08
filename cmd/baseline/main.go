// baseline collects Day 1 evidence using the existing Agent/Runner implementation.
// It does not score, repair, or change the production drawing configuration.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"ai-agent-scaffold/internal/app/config"
	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/agent/service/armory"
	armoryfactory "ai-agent-scaffold/internal/domain/agent/service/armory/factory"
	"ai-agent-scaffold/internal/infrastructure/adk"
	"ai-agent-scaffold/internal/infrastructure/ai"

	"github.com/joho/godotenv"
)

type dataset struct {
	ID    string     `json:"dataset_id"`
	Cases []testCase `json:"cases"`
}

type testCase struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

type callRecord struct {
	Stage     string              `json:"stage"`
	Messages  []ports.ChatMessage `json:"messages"`
	Reply     ports.ChatReply     `json:"reply"`
	ElapsedMS int64               `json:"elapsed_ms"`
	Error     string              `json:"error,omitempty"`
}

// A wrapper observes each real model call while the original runtime still controls execution.
type recordingFactory struct {
	*adk.Factory
	calls  []callRecord
	secret string
}

func (f *recordingFactory) NewLLMAgent(ctx context.Context, cfg model.AgentConfig, chatModel ports.ChatModel) (ports.Agent, error) {
	return f.Factory.NewLLMAgent(ctx, cfg, &recordingModel{ChatModel: chatModel, owner: f, stage: cfg.Name})
}

type recordingModel struct {
	ports.ChatModel
	owner *recordingFactory
	stage string
}

func (m *recordingModel) Generate(ctx context.Context, messages []ports.ChatMessage) (ports.ChatReply, error) {
	started := time.Now()
	reply, err := m.ChatModel.Generate(ctx, messages)
	m.owner.calls = append(m.owner.calls, callRecord{
		Stage: m.stage, Messages: append([]ports.ChatMessage(nil), messages...),
		Reply: reply, ElapsedMS: time.Since(started).Milliseconds(), Error: safeError(err, m.owner.secret),
	})
	return reply, err
}

func main() {
	if err := run(); err != nil {
		// run only returns local/configuration errors; provider errors are sanitized before returning.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "configs/agent/agent-draw-io.yaml", "original drawing YAML")
	casesPath := flag.String("cases", "evals/cases/dev-v1.json", "development cases")
	caseID := flag.String("case", "dev-01-login", "one case id, or all (10 sequential runs)")
	modelOverride := flag.String("model", "", "explicit model override for this collection only; does not edit YAML")
	output := flag.String("out", "", "new evidence directory; existing directories are never overwritten")
	probe := flag.Bool("probe", false, "check configured model with one short request and MCP TCP reachability")
	models := flag.Bool("models", false, "list model ids advertised by the configured gateway (not proof they are usable)")
	check := flag.Bool("check", false, "validate inputs and report metadata without network calls or file writes")
	timeout := flag.Duration("timeout", 5*time.Minute, "per model request timeout; default matches application config")
	flag.Parse()
	if *timeout <= 0 || (*check && (*probe || *models)) || (*probe && *models) {
		return fmt.Errorf("timeout must be positive; choose at most one of -check, -probe and -models")
	}
	// Same precedence as the server: already-set environment values win over .env.
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load .env: %w", err)
	}
	tables, err := config.LoadAgentTablesFile(*configPath)
	if err != nil {
		return err
	}
	table, ok := tables["drawIoAgent"]
	if !ok {
		return fmt.Errorf("drawIoAgent table is missing")
	}
	configuredModel := table.Module.ChatModel.Model
	if strings.TrimSpace(*modelOverride) != "" {
		table.Module.ChatModel.Model = strings.TrimSpace(*modelOverride)
	}
	data, err := readDataset(*casesPath)
	if err != nil {
		return err
	}
	selected := make([]testCase, 0, len(data.Cases))
	for _, item := range data.Cases {
		if *caseID == "all" || item.ID == *caseID {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("unknown case %q", *caseID)
	}
	apiHost, err := url.Parse(table.Module.AiAPI.BaseURL)
	if err != nil || apiHost.Hostname() == "" {
		return fmt.Errorf("configured model base URL has no valid host")
	}
	meta := map[string]any{
		"started_at": time.Now().UTC().Format(time.RFC3339Nano), "go_version": runtime.Version(),
		"model": table.Module.ChatModel.Model, "configured_model": configuredModel,
		"model_override": *modelOverride, "provider_host": apiHost.Hostname(),
		"profile": "original-no-tools", "dataset_id": data.ID,
		"configured_mcp_count":   len(table.Module.ChatModel.ToolMCPList),
		"configured_skill_count": len(table.Module.ChatModel.ToolSkillsList),
		"active_tool_count":      0, "request_timeout": timeout.String(),
		"usage": nil, "usage_note": "The current model port discards provider usage; unknown, not zero.",
		"quality_evaluated": false, "selected_case_count": len(selected),
		"limitations": "Original prompts/Agent/Runner; tools disabled explicitly in this collector only. No HTTP/browser, XML validation, repair or semantic grading. Not the full configured MCP profile.",
	}
	// Record exact local inputs rather than relying only on the command's name.
	for label, path := range map[string]string{
		"config": *configPath, "dataset": *casesPath, "runtime": "internal/infrastructure/adk/adapter.go",
		"model_client": "internal/infrastructure/ai/openai_client.go", "dependencies": "go.mod",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		meta[label+"_sha256"] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	if *check {
		return json.NewEncoder(os.Stdout).Encode(meta)
	}
	if *output == "" {
		return fmt.Errorf("-out is required for a probe or collection")
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(*output, 0755); err != nil {
		return fmt.Errorf("create new evidence directory: %w", err)
	}
	if err := writeJSON(filepath.Join(*output, "metadata.json"), meta); err != nil {
		return err
	}
	if *models {
		return listModels(table.Module.AiAPI, *timeout, *output)
	}
	provider := ai.NewEinoProvider().WithRequestTimeout(*timeout)
	if *probe {
		return probeConnections(table, provider, *timeout, *output)
	}

	registered, recorder, err := assembleOriginal(table, provider)
	if err != nil {
		return fmt.Errorf("assemble original workflow: %s", safeError(err, recorder.secret))
	}
	for _, item := range selected {
		recorder.calls = nil
		sessionID, err := registered.Runner.CreateSession("baseline-dev")
		if err != nil {
			return err
		}
		started := time.Now()
		outputs, runErr := registered.Runner.Run("baseline-dev", sessionID, model.ChatContent{Texts: []model.TextPart{{Message: item.Prompt}}})
		status := "returned"
		if runErr != nil {
			status = "error"
		}
		result := map[string]any{
			"case_id": item.ID, "prompt": item.Prompt, "session_id": sessionID, "profile": "original-no-tools",
			"started_at": started.UTC().Format(time.RFC3339Nano), "elapsed_ms": time.Since(started).Milliseconds(),
			"status": status, "error": safeError(runErr, recorder.secret), "outputs": outputs,
			"model_calls": recorder.calls, "model_call_count": len(recorder.calls),
			"usage": nil, "quality_evaluated": false,
		}
		if err := writeJSON(filepath.Join(*output, item.ID+".json"), result); err != nil {
			return err
		}
		if runErr == nil {
			// .txt deliberately: the original model output has not been certified as valid XML.
			if err := os.WriteFile(filepath.Join(*output, item.ID+"-output.txt"), []byte(strings.Join(outputs, "\n")), 0600); err != nil {
				return err
			}
		}
		fmt.Printf("%s: %s, calls=%d, elapsed_ms=%d\n", item.ID, status, len(recorder.calls), time.Since(started).Milliseconds())
		if runErr != nil {
			return fmt.Errorf("collection stopped after %s: %s; unexecuted cases remain pending", item.ID, safeError(runErr, recorder.secret))
		}
	}
	return nil
}

func assembleOriginal(table model.AiAgentConfigTable, provider ports.ModelProvider) (model.RegisteredAgent, *recordingFactory, error) {
	// Explicit controlled profile: no discovery/tool calls. Do not alter the production YAML.
	table.Module.ChatModel.ToolMCPList = nil
	table.Module.ChatModel.ToolSkillsList = nil
	recorder := &recordingFactory{Factory: adk.NewFactory(), secret: table.Module.AiAPI.APIKey}
	registry := ports.NewInMemoryAgentRegistry()
	factory := armoryfactory.NewDefaultFactory(provider, nil, nil, recorder, recorder.Factory, registry)
	registered, err := factory.ArmoryStrategyHandler().Apply(context.Background(), model.ArmoryCommand{Table: table}, armory.NewDynamicContext())
	return registered, recorder, err
}

func listModels(api model.AiAPIConfig, timeout time.Duration, output string) error {
	endpoint := strings.TrimSuffix(strings.TrimRight(api.BaseURL, "/"), "/v1") + "/v1/models"
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("invalid model-list endpoint")
	}
	request.Header.Set("Authorization", "Bearer "+api.APIKey)
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("model-list request: %s", safeError(err, api.APIKey))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("model-list HTTP %d", response.StatusCode)
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode model list: %w", err)
	}
	ids := make([]string, 0, len(result.Data))
	for _, item := range result.Data {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	record := map[string]any{"kind": "advertised_models_not_verified", "model_ids": ids}
	if err := writeJSON(filepath.Join(output, "models.json"), record); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(record)
}

func readDataset(path string) (dataset, error) {
	var data dataset
	raw, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, err
	}
	if data.ID == "" || len(data.Cases) == 0 {
		return data, fmt.Errorf("dataset id and cases are required")
	}
	seen := map[string]bool{}
	for _, item := range data.Cases {
		if item.ID == "" || strings.ContainsAny(item.ID, `/\:`) || item.ID == "." || item.ID == ".." || strings.TrimSpace(item.Prompt) == "" || seen[item.ID] {
			return data, fmt.Errorf("case ids must be unique safe filenames and prompts must not be empty")
		}
		seen[item.ID] = true
	}
	return data, nil
}

func probeConnections(table model.AiAgentConfigTable, provider *ai.EinoProvider, timeout time.Duration, output string) error {
	checks := []map[string]any{}
	for _, tool := range table.Module.ChatModel.ToolMCPList {
		if tool.SSE == nil {
			continue
		}
		u, err := url.Parse(tool.SSE.BaseURI)
		entry := map[string]any{"name": tool.SSE.Name, "check": "tcp_only_not_mcp_handshake", "reachable": false}
		if err == nil && u.Hostname() != "" {
			port := u.Port()
			if port == "" {
				port = "80"
				if u.Scheme == "https" {
					port = "443"
				}
			}
			connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 3*time.Second)
			if dialErr == nil {
				entry["reachable"] = true
				connection.Close()
			}
		}
		checks = append(checks, entry)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	api, err := provider.NewAPI(ctx, table.Module.AiAPI)
	if err != nil {
		return err
	}
	chatModel, err := provider.NewChatModel(ctx, api, table.Module.ChatModel, nil)
	if err != nil {
		return err
	}
	started := time.Now()
	reply, callErr := chatModel.Generate(ctx, []ports.ChatMessage{{Role: ports.ChatRoleUser, Content: "连通性检查，请只回复OK。"}})
	record := map[string]any{"kind": "connectivity_probe_not_baseline", "model_request_ok": callErr == nil,
		"elapsed_ms": time.Since(started).Milliseconds(), "reply": reply.Content,
		"error": safeError(callErr, table.Module.AiAPI.APIKey), "mcp_checks": checks}
	if err := writeJSON(filepath.Join(output, "probe.json"), record); err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(record); err != nil {
		return err
	}
	if callErr != nil {
		return fmt.Errorf("model probe failed; see probe.json")
	}
	return nil
}

func safeError(err error, secret string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "[REDACTED]")
	}
	return message
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}
