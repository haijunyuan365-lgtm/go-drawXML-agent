package reviewer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Prompt 必须同时携带原始需求、分析结果和当前候选，防止 Reviewer 只看二手摘要。
func TestBuildPromptCarriesCompleteReviewContext(t *testing.T) {
	input := Input{
		OriginalRequirement: "画出下单成功后扣减库存的流程",
		AnalysisResult:      "订单服务调用库存服务",
		CurrentXML:          `<mxfile><diagram/></mxfile>`,
	}
	prompt, err := BuildPrompt(input)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if prompt.Instruction == "" {
		t.Fatal("reviewer instruction must not be empty")
	}
	if !strings.Contains(prompt.Payload, "<mxfile>") {
		t.Fatalf("XML tags should remain readable in prompt payload: %s", prompt.Payload)
	}

	var got Input
	if err := json.Unmarshal([]byte(prompt.Payload), &got); err != nil {
		t.Fatalf("decode prompt payload: %v", err)
	}
	if got != input {
		t.Fatalf("unexpected prompt payload: %+v", got)
	}
}

func TestParseResponseDistinguishesPassAndQualityRejection(t *testing.T) {
	passed, err := ParseResponse(`{"passed":true,"issues":[]}`)
	if err != nil || !passed.Passed || len(passed.Issues) != 0 {
		t.Fatalf("unexpected pass result: %+v, err=%v", passed, err)
	}

	rejected, err := ParseResponse(`{"passed":false,"issues":[{"type":"missing_edge","description":"缺少订单到库存的调用关系","element_ids":["order","inventory"]}]}`)
	if err != nil {
		t.Fatalf("quality rejection is not a protocol error: %v", err)
	}
	if rejected.Passed || len(rejected.Issues) != 1 {
		t.Fatalf("unexpected rejection result: %+v", rejected)
	}
	if rejected.Issues[0].Type != "missing_edge" {
		t.Fatalf("unexpected issue: %+v", rejected.Issues[0])
	}
}

// 每一种坏回复都必须变成 review_protocol_error，不能被误判为普通质量不通过。
func TestParseResponseRejectsMalformedOrContradictoryReplies(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		violation string
	}{
		{name: "empty", response: "  ", violation: violationEmptyResponse},
		{name: "truncated", response: `{"passed":false,"issues":[`, violation: violationInvalidJSON},
		{name: "markdown fence", response: "```json\n{\"passed\":true,\"issues\":[]}\n```", violation: violationInvalidJSON},
		{name: "missing passed", response: `{"issues":[]}`, violation: violationMissingPassed},
		{name: "wrong passed type", response: `{"passed":"true","issues":[]}`, violation: violationInvalidJSON},
		{name: "missing issues", response: `{"passed":true}`, violation: violationMissingIssues},
		{name: "null issues", response: `{"passed":true,"issues":null}`, violation: violationMissingIssues},
		{name: "unknown result field", response: `{"passed":true,"issues":[],"note":"ok"}`, violation: violationInvalidJSON},
		{name: "missing issue field", response: `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少边"}]}`, violation: violationInvalidIssue},
		{name: "empty issue description", response: `{"passed":false,"issues":[{"type":"missing_edge","description":" ","element_ids":[]}]}`, violation: violationInvalidIssue},
		{name: "empty element id", response: `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少边","element_ids":[""]}]}`, violation: violationInvalidIssue},
		{name: "passed with issue", response: `{"passed":true,"issues":[{"type":"missing_edge","description":"缺少边","element_ids":[]}]}`, violation: violationInconsistentResult},
		{name: "rejected without issue", response: `{"passed":false,"issues":[]}`, violation: violationInconsistentResult},
		{name: "trailing object", response: `{"passed":true,"issues":[]} {"passed":true,"issues":[]}`, violation: violationTrailingContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseResponse(test.response)
			if err == nil {
				t.Fatal("expected protocol error")
			}
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) {
				t.Fatalf("expected ProtocolError, got %T: %v", err, err)
			}
			if protocolErr.Code != ProtocolErrorCode || protocolErr.Violation != test.violation {
				t.Fatalf("unexpected classification: %+v", protocolErr)
			}
		})
	}
}

func TestParseResponseNormalizesIssueWhitespace(t *testing.T) {
	result, err := ParseResponse(`{"passed":false,"issues":[{"type":" missing_edge ","description":" 缺少调用关系 ","element_ids":[" order "," inventory "]}]}`)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	issue := result.Issues[0]
	if issue.Type != "missing_edge" || issue.Description != "缺少调用关系" {
		t.Fatalf("unexpected normalized issue: %+v", issue)
	}
	if issue.ElementIDs[0] != "order" || issue.ElementIDs[1] != "inventory" {
		t.Fatalf("unexpected normalized element ids: %#v", issue.ElementIDs)
	}
}
