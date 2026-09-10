package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ai-agent-scaffold/internal/domain/agent/model"
)

// 读取真实绘图 YAML，防止字段拼写或缩进错误导致 Guardrail 实际没有启用。
func TestDrawioConfigSelectsOutputValidator(t *testing.T) {
	table := loadDrawioTable(t)
	if table.Module.Runner.OutputValidator != "drawio-xml" {
		t.Fatalf("unexpected output validator: %q", table.Module.Runner.OutputValidator)
	}
}

// Day 5 将生产 Runner 切到显式四角色闭环；成功结果仍由原同步入口返回裸 XML。
func TestDrawioConfigSelectsRepairWorkflowAtPublicBoundary(t *testing.T) {
	table := loadDrawioTable(t)
	if table.Module.Runner.AgentName != "drawio_repair_process" {
		t.Fatalf("unexpected runner agent: %q", table.Module.Runner.AgentName)
	}

	workflow, ok := findWorkflow(table.Module.AgentWorkflows, "drawio_repair_process")
	if !ok {
		t.Fatal("drawio_repair_process not found")
	}
	if workflow.Type != model.WorkflowTypeDrawIORepair {
		t.Fatalf("unexpected workflow type: %q", workflow.Type)
	}
	wantRoles := model.DrawIORepairRoles{
		Analyst:  "agent_analyst",
		Drawer:   "agent_drawer",
		Reviewer: "agent_semantic_reviewer",
		Repairer: "agent_repairer",
	}
	if !reflect.DeepEqual(workflow.Roles, wantRoles) {
		t.Fatalf("unexpected workflow roles: got %#v, want %#v", workflow.Roles, wantRoles)
	}
	if workflow.MaxRepairs == nil || *workflow.MaxRepairs != 2 {
		t.Fatalf("unexpected repair budget: %v", workflow.MaxRepairs)
	}
	if len(workflow.SubAgents) != 0 {
		t.Fatalf("drawio repair workflow must use named roles, got %#v", workflow.SubAgents)
	}
	if table.Module.Runner.OutputValidator != "drawio-xml" {
		t.Fatalf("unexpected final output validator: %q", table.Module.Runner.OutputValidator)
	}
}

// 旧串行工作流和 XML Reviewer 必须继续存在，供 Day 1 baseline 与后续 A/B 对照显式选择。
func TestDrawioConfigKeepsLegacySequentialWorkflowForBaseline(t *testing.T) {
	table := loadDrawioTable(t)
	workflow, ok := findWorkflow(table.Module.AgentWorkflows, "sequential_draw_process")
	if !ok {
		t.Fatal("sequential_draw_process not found")
	}
	wantOrder := []string{"agent_analyst", "agent_drawer", "agent_reviewer"}
	if !reflect.DeepEqual(workflow.SubAgents, wantOrder) {
		t.Fatalf("legacy workflow changed: got %#v, want %#v", workflow.SubAgents, wantOrder)
	}

	reviewerConfig, ok := findAgent(table.Module.Agents, "agent_reviewer")
	if !ok {
		t.Fatal("legacy agent_reviewer not found")
	}
	if reviewerConfig.OutputKey != "final_result" {
		t.Fatalf("unexpected reviewer output key: %q", reviewerConfig.OutputKey)
	}
	if !strings.Contains(reviewerConfig.Instruction, "严禁输出任何 JSON") ||
		!strings.Contains(reviewerConfig.Instruction, "</mxfile>") {
		t.Fatal("legacy reviewer must still promise bare XML")
	}
}

// nil 表示 YAML 未配置，补默认值 2；显式 0 必须保留，不能被当成“未配置”。
func TestDrawIORepairMaxRepairsDefaultAndExplicitZero(t *testing.T) {
	table := loadDrawioTable(t)
	workflowIndex := findWorkflowIndex(t, table.Module.AgentWorkflows, "drawio_repair_process")

	table.Module.AgentWorkflows[workflowIndex].MaxRepairs = nil
	normalizeDefaults(&table)
	if got := table.Module.AgentWorkflows[workflowIndex].MaxRepairs; got == nil || *got != 2 {
		t.Fatalf("missing max-repairs should default to 2, got %v", got)
	}

	zero := 0
	table.Module.AgentWorkflows[workflowIndex].MaxRepairs = &zero
	normalizeDefaults(&table)
	if got := table.Module.AgentWorkflows[workflowIndex].MaxRepairs; got == nil || *got != 0 {
		t.Fatalf("explicit zero repair budget was overwritten: %v", got)
	}
}

// 四角色、预算和互斥字段都在启动装配前失败，避免真实请求运行到一半才暴露配置错误。
func TestValidateDrawIORepairWorkflowRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.AgentWorkflowConfig)
		want   string
	}{
		{name: "missing role", mutate: func(w *model.AgentWorkflowConfig) { w.Roles.Reviewer = "" }, want: "roles.reviewer is required"},
		{name: "unknown role", mutate: func(w *model.AgentWorkflowConfig) { w.Roles.Repairer = "missing" }, want: "references unknown agent"},
		{name: "duplicate role", mutate: func(w *model.AgentWorkflowConfig) { w.Roles.Repairer = w.Roles.Reviewer }, want: "duplicates roles.reviewer"},
		{name: "negative budget", mutate: func(w *model.AgentWorkflowConfig) { value := -1; w.MaxRepairs = &value }, want: "max-repairs must be between"},
		{name: "excessive budget", mutate: func(w *model.AgentWorkflowConfig) { value := 4; w.MaxRepairs = &value }, want: "max-repairs must be between"},
		{name: "positional agents conflict", mutate: func(w *model.AgentWorkflowConfig) { w.SubAgents = []string{"agent_analyst"} }, want: "cannot define sub-agents"},
		{name: "iteration conflict", mutate: func(w *model.AgentWorkflowConfig) { w.MaxIterations = 3 }, want: "cannot define max-iterations"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table := loadDrawioTable(t)
			workflowIndex := findWorkflowIndex(t, table.Module.AgentWorkflows, "drawio_repair_process")
			test.mutate(&table.Module.AgentWorkflows[workflowIndex])
			err := validateTable("drawIoAgent", table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func loadDrawioTable(t *testing.T) model.AiAgentConfigTable {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "http://example.test")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("BAIDU_SEARCH_MCP_BASE_URI", "http://example.test/mcp")

	path := filepath.Join("..", "..", "..", "configs", "agent", "agent-draw-io.yaml")
	tables, err := LoadAgentTablesFile(path)
	if err != nil {
		t.Fatalf("load drawio config: %v", err)
	}
	table, ok := tables["drawIoAgent"]
	if !ok {
		t.Fatal("drawIoAgent table not found")
	}
	return table
}

func findWorkflow(workflows []model.AgentWorkflowConfig, name string) (model.AgentWorkflowConfig, bool) {
	for _, workflow := range workflows {
		if workflow.Name == name {
			return workflow, true
		}
	}
	return model.AgentWorkflowConfig{}, false
}

func findWorkflowIndex(t *testing.T, workflows []model.AgentWorkflowConfig, name string) int {
	t.Helper()
	for index, workflow := range workflows {
		if workflow.Name == name {
			return index
		}
	}
	t.Fatalf("workflow %q not found", name)
	return -1
}

func findAgent(agents []model.AgentConfig, name string) (model.AgentConfig, bool) {
	for _, agent := range agents {
		if agent.Name == name {
			return agent, true
		}
	}
	return model.AgentConfig{}, false
}
