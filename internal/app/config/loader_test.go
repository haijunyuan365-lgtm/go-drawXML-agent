package config

import (
	"path/filepath"
	"testing"
)

// 读取真实绘图 YAML，防止字段拼写或缩进错误导致 Guardrail 实际没有启用。
func TestDrawioConfigSelectsOutputValidator(t *testing.T) {
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
	if table.Module.Runner.OutputValidator != "drawio-xml" {
		t.Fatalf("unexpected output validator: %q", table.Module.Runner.OutputValidator)
	}
}
