package repair

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ai-agent-scaffold/internal/domain/diagram/reviewer"
	"ai-agent-scaffold/internal/domain/validation"
)

const validCandidate = `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="a" vertex="1" parent="1"><mxGeometry width="80" height="40"/></mxCell></root></mxGraphModel></diagram></mxfile>`

// Repair 输入必须包含“本轮问题 + 当前版本”，这是后续循环不会修错旧稿的基础。
func TestBuildPromptCarriesCurrentCandidateAndIssues(t *testing.T) {
	input := Input{
		OriginalRequirement: "画登录流程",
		AnalysisResult:      "开始到登录成功",
		CurrentXML:          validCandidate,
		Issues: []Issue{{
			Source:     IssueSourceReviewer,
			Code:       "missing_edge",
			Message:    "缺少登录到成功的连线",
			ElementIDs: []string{"login", "success"},
		}},
	}
	prompt, err := BuildPrompt(input)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if prompt.Instruction == "" {
		t.Fatal("repair instruction must not be empty")
	}
	if !strings.Contains(prompt.Payload, "<mxfile>") {
		t.Fatalf("XML tags should remain readable in prompt payload: %s", prompt.Payload)
	}

	var got Input
	if err := json.Unmarshal([]byte(prompt.Payload), &got); err != nil {
		t.Fatalf("decode prompt payload: %v", err)
	}
	if got.CurrentXML != validCandidate || len(got.Issues) != 1 {
		t.Fatalf("unexpected repair payload: %+v", got)
	}
	if got.Issues[0].Code != "missing_edge" || got.Issues[0].ElementIDs[1] != "success" {
		t.Fatalf("unexpected current issue: %+v", got.Issues[0])
	}
}

func TestIssueAdaptersKeepValidatorAndReviewerMeaningSeparate(t *testing.T) {
	validatorIssues := FromValidationIssues([]validation.Issue{{
		Code:      "dangling_reference",
		Message:   "target references an unknown id",
		Path:      "/mxfile/diagram[1]",
		ElementID: "edge-1",
		Attribute: "target",
	}})
	reviewerIssues := FromReviewerIssues([]reviewer.Issue{{
		Type:        "missing_edge",
		Description: "需求中的调用关系没有画出",
		ElementIDs:  []string{"order", "inventory"},
	}})

	if validatorIssues[0].Source != IssueSourceValidator || validatorIssues[0].Code != "dangling_reference" {
		t.Fatalf("unexpected validator issue: %+v", validatorIssues[0])
	}
	if reviewerIssues[0].Source != IssueSourceReviewer || reviewerIssues[0].Code != "missing_edge" {
		t.Fatalf("unexpected reviewer issue: %+v", reviewerIssues[0])
	}
}

func TestParseResponseReturnsExactValidatedXML(t *testing.T) {
	got, err := ParseResponse(validCandidate)
	if err != nil {
		t.Fatalf("parse repair response: %v", err)
	}
	if got != validCandidate {
		t.Fatalf("repair parser changed XML:\ngot:  %s\nwant: %s", got, validCandidate)
	}
}

// Repair 坏输出仍归入确定性校验问题；S04 可以据此消耗预算并决定是否再修一次。
func TestParseResponseRejectsNonBareOrInvalidXML(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		code string
	}{
		{name: "markdown", xml: "```xml\n" + validCandidate + "\n```", code: "repair_output_not_bare_xml"},
		{name: "leading whitespace", xml: " " + validCandidate, code: "repair_output_not_bare_xml"},
		{name: "trailing explanation", xml: validCandidate + "已修复", code: "repair_output_not_bare_xml"},
		{name: "invalid structure", xml: `<mxfile><diagram/></mxfile>`, code: "missing_graph_model"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseResponse(test.xml)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if got != "" {
				t.Fatalf("invalid repair must not return XML, got %q", got)
			}
			var validationErr *validation.Error
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected validation.Error, got %T: %v", err, err)
			}
			if !hasIssueCode(validationErr.Result.Issues, test.code) {
				t.Fatalf("expected issue %q, got %+v", test.code, validationErr.Result.Issues)
			}
		})
	}
}

func TestBuildPromptRejectsMissingOrUnusableIssueData(t *testing.T) {
	base := Input{
		OriginalRequirement: "画登录流程",
		AnalysisResult:      "开始到结束",
		CurrentXML:          validCandidate,
	}
	if _, err := BuildPrompt(base); err == nil {
		t.Fatal("expected empty issues to fail")
	}

	base.Issues = []Issue{{Source: "unknown", Code: "missing_edge", Message: "缺少边", ElementIDs: []string{}}}
	if _, err := BuildPrompt(base); err == nil {
		t.Fatal("expected unknown issue source to fail")
	}
}

func hasIssueCode(issues []validation.Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
