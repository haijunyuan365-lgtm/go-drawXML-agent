package drawio

import (
	"testing"

	"ai-agent-scaffold/internal/domain/validation"
)

const validXML = `<mxfile><diagram id="page-1"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="e1" edge="1" parent="1" source="a" target="b"><mxGeometry relative="1" as="geometry"/></mxCell><mxCell id="a" value="开始 &amp; 准备" vertex="1" parent="1"><mxGeometry x="0" y="0" width="120" height="60" as="geometry"/></mxCell><mxCell id="b" vertex="1" parent="1"><mxGeometry width="120.5" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>`

// 合法样例同时包含基础 cell、节点、边、中文转义和前向引用，防止规则过严。
// 这组测试覆盖合法结果、稳定错误分类、前向引用和确定性顺序，
// 防止后续扩展规则时把可用图纸误拒绝，或让 Repair 收到不稳定诊断。
func TestValidatorAcceptsSupportedDrawioXML(t *testing.T) {
	result := NewValidator().Validate(validXML)
	if !result.Passed || len(result.Issues) != 0 {
		t.Fatalf("expected valid XML to pass, got %+v", result)
	}
}

// 表格测试固定每类失败的稳定问题码，后续 Repair 和 Eval 会依赖这些分类。
func TestValidatorRejectsInvalidAndUnsupportedDocuments(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		code string
	}{
		{name: "empty", xml: "  ", code: "empty_document"},
		{name: "unclosed", xml: `<mxfile><diagram>`, code: "invalid_xml"},
		{name: "multiple roots", xml: `<mxfile></mxfile><mxfile></mxfile>`, code: "multiple_documents"},
		{name: "wrong root", xml: `<diagram/>`, code: "invalid_root"},
		{name: "multiple pages", xml: `<mxfile><diagram/><diagram/></mxfile>`, code: "unsupported_multi_page"},
		{name: "compressed", xml: `<mxfile><diagram>7V1bc5s4FP41</diagram></mxfile>`, code: "unsupported_compressed_diagram"},
		{name: "object wrapper", xml: `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><object id="a"><mxCell id="a"/></object></root></mxGraphModel></diagram></mxfile>`, code: "unsupported_cell_wrapper"},
		{name: "duplicate id", xml: `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="0"/></root></mxGraphModel></diagram></mxfile>`, code: "duplicate_id"},
		{name: "missing id", xml: `<mxfile><diagram><mxGraphModel><root><mxCell/></root></mxGraphModel></diagram></mxfile>`, code: "missing_id"},
		{name: "dangling parent", xml: `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="a" parent="missing"/></root></mxGraphModel></diagram></mxfile>`, code: "dangling_reference"},
		{name: "missing edge target", xml: `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="e" edge="1" source="0"/></root></mxGraphModel></diagram></mxfile>`, code: "missing_edge_endpoint"},
		{name: "invalid geometry", xml: `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="a" vertex="1" parent="0"><mxGeometry width="0" height="NaN"/></mxCell></root></mxGraphModel></diagram></mxfile>`, code: "invalid_geometry_dimension"},
	}

	validator := NewValidator()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := validator.Validate(test.xml)
			if result.Passed {
				t.Fatal("expected validation failure")
			}
			if !containsCode(result.Issues, test.code) {
				t.Fatalf("expected issue %q, got %+v", test.code, result.Issues)
			}
		})
	}
}

// 连线可以先于节点出现，此测试防止实现退化成只检查“此前已见 ID”。
func TestValidatorResolvesReferencesAfterCollectingAllCells(t *testing.T) {
	xml := `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="edge" edge="1" parent="1" source="later-a" target="later-b"/><mxCell id="later-a" vertex="1" parent="1"><mxGeometry width="80" height="40"/></mxCell><mxCell id="later-b" vertex="1" parent="1"><mxGeometry width="80" height="40"/></mxCell></root></mxGraphModel></diagram></mxfile>`

	result := NewValidator().Validate(xml)
	if !result.Passed {
		t.Fatalf("forward references should pass, got %+v", result.Issues)
	}
}

// 相同输入必须返回相同问题顺序，避免评测和修复提示随 map 顺序波动。
func TestValidatorReturnsIssuesInStableOrder(t *testing.T) {
	xml := `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="a" vertex="1" parent="missing"><mxGeometry width="0" height="bad"/></mxCell></root></mxGraphModel></diagram></mxfile>`
	validator := NewValidator()
	first := validator.Validate(xml)
	second := validator.Validate(xml)

	if len(first.Issues) != len(second.Issues) {
		t.Fatalf("issue count changed: %d != %d", len(first.Issues), len(second.Issues))
	}
	for index := range first.Issues {
		if first.Issues[index] != second.Issues[index] {
			t.Fatalf("issue order changed at %d: %+v != %+v", index, first.Issues[index], second.Issues[index])
		}
	}
}

func containsCode(issues []validation.Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
