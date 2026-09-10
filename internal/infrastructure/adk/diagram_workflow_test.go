package adk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "ai-agent-scaffold/internal/app/config"
	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/agent/service/armory"
	armoryfactory "ai-agent-scaffold/internal/domain/agent/service/armory/factory"
	"ai-agent-scaffold/internal/domain/agent/service/chat"
	"ai-agent-scaffold/internal/domain/diagram/repair"
	"ai-agent-scaffold/internal/domain/diagram/reviewer"
	diagramworkflow "ai-agent-scaffold/internal/domain/diagram/workflow"
	httptrigger "ai-agent-scaffold/internal/trigger/http"
	"ai-agent-scaffold/pkg/types"

	"github.com/gin-gonic/gin"
)

const integrationBadCandidate = `<mxfile><diagram/></mxfile>`

// fixedModelProvider 让完整 YAML/Armory/Runner 链路使用同一个可控模型替身，
// 测试不访问网络，但不会绕过生产 Factory 或生产 Agent 实现。
type fixedModelProvider struct {
	chatModel ports.ChatModel
}

func (p fixedModelProvider) NewAPI(context.Context, model.AiAPIConfig) (ports.ModelAPI, error) {
	return struct{}{}, nil
}

func (p fixedModelProvider) NewChatModel(
	context.Context,
	ports.ModelAPI,
	model.ChatModelConfig,
	[]ports.Tool,
) (ports.ChatModel, error) {
	return p.chatModel, nil
}

// 该测试从真实 agent-draw-io.yaml 开始，经 Armory、Factory、Runner、ChatService
// 和原 /api/v1/chat 入口，证明一次语义拒绝会进入 Repair 并只交付通过后的候选。
func TestDrawIORepairWorkflowRunsThroughConfiguredHTTPEntry(t *testing.T) {
	initial := integrationCandidate("initial")
	repaired := integrationCandidate("repaired")
	chatModel := &scriptedChatModel{replies: []ports.ChatReply{
		{Content: "订单服务调用库存服务"},
		{Content: initial},
		{Content: `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少订单到库存的调用关系","element_ids":["order","inventory"]}]}`},
		{Content: repaired},
		{Content: `{"passed":true,"issues":[]}`},
	}}
	router := newConfiguredDrawIOTestRouter(t, chatModel)

	started := time.Now()
	recorder := performChatRequest(t, router, "画出下单到扣减库存的流程")
	elapsed := time.Since(started)

	var body struct {
		Code string `json:"code"`
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	decodeHTTPBody(t, recorder, &body)
	if body.Code != types.CodeSuccess || body.Data.Content != repaired {
		t.Fatalf("unexpected success response: code=%q content=%q", body.Code, body.Data.Content)
	}
	if len(chatModel.messages) != 5 {
		t.Fatalf("expected analyst + drawer + two reviews + one repair, got %d calls", len(chatModel.messages))
	}
	if !strings.Contains(chatModel.messages[1][0].Content, "订单服务调用库存服务") {
		t.Fatal("drawer did not receive analysis_result through the existing variable scope")
	}

	firstReview := decodeReviewerPayload(t, chatModel.messages[2])
	if firstReview.OriginalRequirement != "画出下单到扣减库存的流程" || firstReview.CurrentXML != initial {
		t.Fatalf("reviewer received stale or incomplete context: %+v", firstReview)
	}
	repairInput := decodeRepairPayload(t, chatModel.messages[3])
	if repairInput.CurrentXML != initial || len(repairInput.Issues) != 1 ||
		repairInput.Issues[0].Source != repair.IssueSourceReviewer {
		t.Fatalf("repairer did not receive current candidate and reviewer issue: %+v", repairInput)
	}
	secondReview := decodeReviewerPayload(t, chatModel.messages[4])
	if secondReview.CurrentXML != repaired {
		t.Fatalf("repaired candidate was not reviewed again: %+v", secondReview)
	}

	// S09 才建设正式指标存储；Day 5 先把最小调用次数和入口耗时留在集成测试证据中。
	t.Logf("configured sync workflow: model_calls=%d elapsed=%s", len(chatModel.messages), elapsed)
}

// 坏 XML 会跳过 Reviewer；两次 Repair 仍失败后，HTTP 返回结构化工作流失败而不是 XML。
func TestDrawIORepairWorkflowFailureDoesNotMasqueradeAsDiagram(t *testing.T) {
	chatModel := &scriptedChatModel{replies: []ports.ChatReply{
		{Content: "绘制登录流程"},
		{Content: integrationBadCandidate},
		{Content: integrationBadCandidate},
		{Content: integrationBadCandidate},
	}}
	router := newConfiguredDrawIOTestRouter(t, chatModel)
	recorder := performChatRequest(t, router, "画一个登录流程图")

	var body struct {
		Code string `json:"code"`
		Data struct {
			Code  diagramworkflow.FailureCode `json:"code"`
			Stage diagramworkflow.Stage       `json:"stage"`
		} `json:"data"`
	}
	decodeHTTPBody(t, recorder, &body)
	if body.Code != types.CodeDiagramWorkflowFailed {
		t.Fatalf("unexpected response code: %q", body.Code)
	}
	if body.Data.Code != diagramworkflow.FailureRepairBudgetExhausted ||
		body.Data.Stage != diagramworkflow.StageValidation {
		t.Fatalf("unexpected workflow failure: %+v", body.Data)
	}
	if len(chatModel.messages) != 4 {
		t.Fatalf("expected analyst + drawer + two repairs and no reviewer, got %d calls", len(chatModel.messages))
	}
}

// Reviewer/Repairer 的提示词是版本化领域协议；YAML 漂移必须在 Factory 装配时失败。
func TestNewDrawIORepairAgentRejectsProtocolInstructionDrift(t *testing.T) {
	chatModel := &scriptedChatModel{}
	newRole := func(name, instruction string) *Agent {
		return &Agent{name: name, kind: "llm", instruction: instruction, chatModel: chatModel}
	}
	roles := model.DrawIORepairRoles{
		Analyst: "analyst", Drawer: "drawer", Reviewer: "reviewer", Repairer: "repairer",
	}
	maxRepairs := 2
	_, err := NewFactory().NewDrawIORepairAgent(
		context.Background(),
		model.AgentWorkflowConfig{
			Type: model.WorkflowTypeDrawIORepair, Name: "quality", Roles: roles, MaxRepairs: &maxRepairs,
		},
		[]ports.Agent{
			newRole("analyst", "analyze"),
			newRole("drawer", "draw"),
			newRole("reviewer", "old reviewer edits XML directly"),
			newRole("repairer", repair.Instruction),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "instruction does not match") {
		t.Fatalf("expected protocol drift to fail during assembly, got %v", err)
	}
}

func newConfiguredDrawIOTestRouter(t *testing.T, chatModel ports.ChatModel) *gin.Engine {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "http://example.test")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("BAIDU_SEARCH_MCP_BASE_URI", "http://example.test/mcp")

	path := filepath.Join("..", "..", "..", "configs", "agent", "agent-draw-io.yaml")
	tables, err := appconfig.LoadAgentTablesFile(path)
	if err != nil {
		t.Fatalf("load drawio config: %v", err)
	}
	table := tables["drawIoAgent"]
	// 外部工具不是本测试目标；清空发现配置后，其他 YAML、Factory 和 Runner 路径保持生产形态。
	table.Module.ChatModel.ToolMCPList = nil
	table.Module.ChatModel.ToolSkillsList = nil
	table.Module.Runner.PluginNameList = nil

	registry := ports.NewInMemoryAgentRegistry()
	runtimeFactory := NewFactory()
	assembly := armoryfactory.NewDefaultFactory(
		fixedModelProvider{chatModel: chatModel},
		nil,
		nil,
		runtimeFactory,
		runtimeFactory,
		registry,
	)
	_, err = assembly.ArmoryStrategyHandler().Apply(
		context.Background(),
		model.ArmoryCommand{Table: table},
		armory.NewDynamicContext(),
	)
	if err != nil {
		t.Fatalf("assemble drawio workflow: %v", err)
	}

	service := chat.NewService(registry, ports.NewInMemorySessionStore())
	gin.SetMode(gin.TestMode)
	router := gin.New()
	httptrigger.RegisterAgentRoutes(router, service)
	return router
}

func performChatRequest(t *testing.T, router http.Handler, message string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"agentId": "300000",
		"userId":  "day5-test-user",
		"message": message,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected HTTP status %d: %s", recorder.Code, recorder.Body.String())
	}
	return recorder
}

func decodeHTTPBody(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode HTTP response: %v\nbody=%s", err, recorder.Body.String())
	}
}

func decodeReviewerPayload(t *testing.T, messages []ports.ChatMessage) reviewer.Input {
	t.Helper()
	requireProtocolMessages(t, messages, reviewer.Instruction)
	var input reviewer.Input
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		t.Fatalf("decode reviewer payload: %v", err)
	}
	return input
}

func decodeRepairPayload(t *testing.T, messages []ports.ChatMessage) repair.Input {
	t.Helper()
	requireProtocolMessages(t, messages, repair.Instruction)
	var input repair.Input
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		t.Fatalf("decode repair payload: %v", err)
	}
	return input
}

func requireProtocolMessages(t *testing.T, messages []ports.ChatMessage, expectedInstruction string) {
	t.Helper()
	if len(messages) != 2 {
		t.Fatalf("protocol role should receive system + payload, got %#v", messages)
	}
	if strings.TrimSpace(messages[0].Content) != strings.TrimSpace(expectedInstruction) {
		t.Fatalf("unexpected protocol instruction: %q", messages[0].Content)
	}
}

func integrationCandidate(id string) string {
	return fmt.Sprintf(
		`<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="%s" vertex="1" parent="1"><mxGeometry width="80" height="40"/></mxCell></root></mxGraphModel></diagram></mxfile>`,
		id,
	)
}
