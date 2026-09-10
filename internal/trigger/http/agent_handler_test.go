package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	diagramworkflow "ai-agent-scaffold/internal/domain/diagram/workflow"
	"ai-agent-scaffold/internal/domain/validation"
	"ai-agent-scaffold/pkg/types"

	"github.com/gin-gonic/gin"
)

// 验证 HTTP 边界不会把 Validator 名称和 issues 降级成普通错误文本。
func TestWriteErrorPreservesStructuredValidationIssues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	writeError(ctx, &validation.Error{
		Validator: "drawio-xml",
		Result: validation.Result{
			Passed: false,
			Issues: []validation.Issue{{
				Code:      "dangling_reference",
				Message:   "source references unknown mxCell id",
				ElementID: "edge-1",
				Attribute: "source",
			}},
		},
	})

	var body struct {
		Code string `json:"code"`
		Data struct {
			Validator string            `json:"validator"`
			Result    validation.Result `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != types.CodeOutputValidationFailed {
		t.Fatalf("unexpected code: %q", body.Code)
	}
	if body.Data.Validator != "drawio-xml" || len(body.Data.Result.Issues) != 1 {
		t.Fatalf("structured validation data was lost: %+v", body.Data)
	}
}

// 验证质量闭环失败会保留领域错误分类，而不是降级成 unknown error 或成功 XML。
func TestWriteErrorPreservesDiagramWorkflowFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	writeError(ctx, &diagramworkflow.Failure{
		Code:    diagramworkflow.FailureRepairBudgetExhausted,
		Stage:   diagramworkflow.StageReview,
		Message: "repair budget exhausted after 2 repair attempt(s)",
	})

	var body struct {
		Code string `json:"code"`
		Data struct {
			Code  diagramworkflow.FailureCode `json:"code"`
			Stage diagramworkflow.Stage       `json:"stage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != types.CodeDiagramWorkflowFailed {
		t.Fatalf("unexpected code: %q", body.Code)
	}
	if body.Data.Code != diagramworkflow.FailureRepairBudgetExhausted || body.Data.Stage != diagramworkflow.StageReview {
		t.Fatalf("structured workflow failure was lost: %+v", body.Data)
	}
}
