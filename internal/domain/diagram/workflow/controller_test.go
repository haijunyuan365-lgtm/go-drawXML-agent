package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"ai-agent-scaffold/internal/domain/diagram/drawio"
	"ai-agent-scaffold/internal/domain/diagram/repair"
	"ai-agent-scaffold/internal/domain/diagram/reviewer"
	"ai-agent-scaffold/internal/domain/validation"
)

const badCandidate = `<mxfile><diagram/></mxfile>`

func validCandidate(id string) string {
	return fmt.Sprintf(
		`<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="%s" vertex="1" parent="1"><mxGeometry width="80" height="40"/></mxCell></root></mxGraphModel></diagram></mxfile>`,
		id,
	)
}

type analystFunc func(context.Context, string) (string, error)

func (f analystFunc) Analyze(ctx context.Context, requirement string) (string, error) {
	return f(ctx, requirement)
}

type drawerFunc func(context.Context, DrawInput) (string, error)

func (f drawerFunc) Draw(ctx context.Context, input DrawInput) (string, error) {
	return f(ctx, input)
}

type reviewerFunc func(context.Context, reviewer.Prompt) (string, error)

func (f reviewerFunc) Review(ctx context.Context, prompt reviewer.Prompt) (string, error) {
	return f(ctx, prompt)
}

type repairerFunc func(context.Context, repair.Prompt) (string, error)

func (f repairerFunc) Repair(ctx context.Context, prompt repair.Prompt) (string, error) {
	return f(ctx, prompt)
}

type validatorSpy struct {
	mu     sync.Mutex
	inner  validation.Validator
	inputs []string
}

func newValidatorSpy() *validatorSpy {
	return &validatorSpy{inner: drawio.NewValidator()}
}

func (v *validatorSpy) Name() string {
	return v.inner.Name()
}

func (v *validatorSpy) Validate(output string) validation.Result {
	v.mu.Lock()
	v.inputs = append(v.inputs, output)
	v.mu.Unlock()
	return v.inner.Validate(output)
}

func (v *validatorSpy) Inputs() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.inputs...)
}

func defaultDependencies() Dependencies {
	return Dependencies{
		Analyst: analystFunc(func(context.Context, string) (string, error) {
			return "分析结果", nil
		}),
		Drawer: drawerFunc(func(context.Context, DrawInput) (string, error) {
			return validCandidate("initial"), nil
		}),
		Reviewer: reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
			return `{"passed":true,"issues":[]}`, nil
		}),
		Repairer: repairerFunc(func(context.Context, repair.Prompt) (string, error) {
			return validCandidate("repaired"), nil
		}),
		Validator: drawio.NewValidator(),
	}
}

func mustController(t *testing.T, maxRepairs int, dependencies Dependencies) *Controller {
	t.Helper()
	controller, err := NewController(Config{MaxRepairs: maxRepairs}, dependencies)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	return controller
}

func runInput(runID string) RunInput {
	return RunInput{RunID: runID, OriginalRequirement: "画出下单到扣减库存的流程"}
}

func decodeReviewInput(t *testing.T, prompt reviewer.Prompt) reviewer.Input {
	t.Helper()
	var input reviewer.Input
	if err := json.Unmarshal([]byte(prompt.Payload), &input); err != nil {
		t.Fatalf("decode reviewer prompt: %v", err)
	}
	return input
}

func decodeRepairInput(t *testing.T, prompt repair.Prompt) repair.Input {
	t.Helper()
	var input repair.Input
	if err := json.Unmarshal([]byte(prompt.Payload), &input); err != nil {
		t.Fatalf("decode repair prompt: %v", err)
	}
	return input
}

func requireFailure(t *testing.T, state State, err error, code FailureCode, stage Stage) {
	t.Helper()
	if err == nil {
		t.Fatal("expected workflow error")
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("expected Failure, got %T: %v", err, err)
	}
	if failure.Code != code || failure.Stage != stage {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if state.Failure != failure {
		t.Fatalf("state must expose the returned failure: state=%+v err=%+v", state.Failure, failure)
	}
	if state.FinalXML != "" {
		t.Fatalf("failed run must not expose final XML: %q", state.FinalXML)
	}
}

// 首次候选通过时，Analyzer/Drawer/Reviewer 各调用一次，Repair 必须保持零调用。
func TestRunSucceedsWithoutRepair(t *testing.T) {
	dependencies := defaultDependencies()
	var analystCalls, drawerCalls, reviewerCalls, repairCalls int
	dependencies.Analyst = analystFunc(func(_ context.Context, requirement string) (string, error) {
		analystCalls++
		if requirement != runInput("first-pass").OriginalRequirement {
			t.Fatalf("unexpected requirement: %q", requirement)
		}
		return "订单服务调用库存服务", nil
	})
	dependencies.Drawer = drawerFunc(func(_ context.Context, input DrawInput) (string, error) {
		drawerCalls++
		if input.AnalysisResult != "订单服务调用库存服务" {
			t.Fatalf("unexpected draw input: %+v", input)
		}
		return validCandidate("initial"), nil
	})
	dependencies.Reviewer = reviewerFunc(func(_ context.Context, prompt reviewer.Prompt) (string, error) {
		reviewerCalls++
		input := decodeReviewInput(t, prompt)
		if input.CurrentXML != validCandidate("initial") {
			t.Fatalf("reviewer received wrong candidate: %s", input.CurrentXML)
		}
		return `{"passed":true,"issues":[]}`, nil
	})
	dependencies.Repairer = repairerFunc(func(context.Context, repair.Prompt) (string, error) {
		repairCalls++
		return "", errors.New("repair must not be called")
	})

	state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(context.Background(), runInput("first-pass"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if state.Status != StatusSucceeded || state.Stage != StageCompleted {
		t.Fatalf("unexpected terminal state: %+v", state)
	}
	if state.Attempt != 0 || state.RepairsUsed != 0 {
		t.Fatalf("first candidate should be attempt zero: %+v", state)
	}
	if state.FinalXML != validCandidate("initial") || state.FinalXML != state.CurrentXML {
		t.Fatalf("final XML must be the reviewed candidate: %+v", state)
	}
	if analystCalls != 1 || drawerCalls != 1 || reviewerCalls != 1 || repairCalls != 0 {
		t.Fatalf("unexpected calls: analyst=%d drawer=%d reviewer=%d repair=%d", analystCalls, drawerCalls, reviewerCalls, repairCalls)
	}
}

// 语义拒绝只把当前候选和本轮 Reviewer 问题交给 Repair；修复后从 Validator 重新进入闭环。
func TestRunRepairsRejectedCandidateOnce(t *testing.T) {
	initial := validCandidate("initial")
	repaired := validCandidate("repaired")
	validator := newValidatorSpy()
	dependencies := defaultDependencies()
	dependencies.Validator = validator
	reviewCalls := 0
	dependencies.Reviewer = reviewerFunc(func(_ context.Context, prompt reviewer.Prompt) (string, error) {
		reviewCalls++
		input := decodeReviewInput(t, prompt)
		if reviewCalls == 1 {
			if input.CurrentXML != initial {
				t.Fatalf("first review received wrong candidate: %s", input.CurrentXML)
			}
			return `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少订单到库存的调用关系","element_ids":["order","inventory"]}]}`, nil
		}
		if input.CurrentXML != repaired {
			t.Fatalf("second review received wrong candidate: %s", input.CurrentXML)
		}
		return `{"passed":true,"issues":[]}`, nil
	})
	repairCalls := 0
	dependencies.Repairer = repairerFunc(func(_ context.Context, prompt repair.Prompt) (string, error) {
		repairCalls++
		input := decodeRepairInput(t, prompt)
		if input.CurrentXML != initial || len(input.Issues) != 1 {
			t.Fatalf("repair received stale or incomplete input: %+v", input)
		}
		if input.Issues[0].Source != repair.IssueSourceReviewer || input.Issues[0].Code != "missing_edge" {
			t.Fatalf("repair lost reviewer issue meaning: %+v", input.Issues[0])
		}
		return repaired, nil
	})

	state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(context.Background(), runInput("repair-once"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if state.Status != StatusSucceeded || state.Attempt != 1 || state.RepairsUsed != 1 {
		t.Fatalf("unexpected repaired state: %+v", state)
	}
	if state.FinalXML != repaired || reviewCalls != 2 || repairCalls != 1 {
		t.Fatalf("wrong final candidate or calls: state=%+v reviews=%d repairs=%d", state, reviewCalls, repairCalls)
	}
	if got := validator.Inputs(); len(got) != 2 || got[0] != initial || got[1] != repaired {
		t.Fatalf("each candidate must be validated once: %#v", got)
	}
}

// 坏 Repair 是一个已经消耗预算的新候选：跳过 Reviewer，把 Validator 问题交给下一轮 Repair。
func TestRunRevalidatesBadRepairBeforeReviewingAgain(t *testing.T) {
	initial := validCandidate("initial")
	repaired := validCandidate("repaired")
	validator := newValidatorSpy()
	dependencies := defaultDependencies()
	dependencies.Validator = validator
	reviewCalls := 0
	dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
		reviewCalls++
		if reviewCalls == 1 {
			return `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少调用关系","element_ids":[]}]}`, nil
		}
		return `{"passed":true,"issues":[]}`, nil
	})
	repairCalls := 0
	dependencies.Repairer = repairerFunc(func(_ context.Context, prompt repair.Prompt) (string, error) {
		repairCalls++
		input := decodeRepairInput(t, prompt)
		switch repairCalls {
		case 1:
			if input.CurrentXML != initial || input.Issues[0].Source != repair.IssueSourceReviewer {
				t.Fatalf("first repair input is wrong: %+v", input)
			}
			return badCandidate, nil
		case 2:
			if input.CurrentXML != badCandidate {
				t.Fatalf("second repair must receive the latest bad candidate: %+v", input)
			}
			if len(input.Issues) != 1 ||
				input.Issues[0].Source != repair.IssueSourceValidator ||
				input.Issues[0].Code != "missing_graph_model" {
				t.Fatalf("second repair must receive validator issues: %+v", input.Issues)
			}
			return repaired, nil
		default:
			return "", fmt.Errorf("unexpected repair call %d", repairCalls)
		}
	})

	state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(context.Background(), runInput("bad-repair"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if state.FinalXML != repaired || state.Attempt != 2 || state.RepairsUsed != 2 {
		t.Fatalf("unexpected state after recovering from bad repair: %+v", state)
	}
	if reviewCalls != 2 {
		t.Fatalf("bad XML must skip reviewer; got %d reviewer calls", reviewCalls)
	}
	if got := validator.Inputs(); len(got) != 3 || got[0] != initial || got[1] != badCandidate || got[2] != repaired {
		t.Fatalf("unexpected validation sequence: %#v", got)
	}
}

// 持续语义失败时最多检查三个候选（初稿 + 两次修复），且最后一份失败候选不能进入 FinalXML。
func TestRunStopsAfterRepairBudgetIsExhausted(t *testing.T) {
	dependencies := defaultDependencies()
	var analystCalls, drawerCalls, reviewCalls, repairCalls int
	dependencies.Analyst = analystFunc(func(context.Context, string) (string, error) {
		analystCalls++
		return "分析结果", nil
	})
	dependencies.Drawer = drawerFunc(func(context.Context, DrawInput) (string, error) {
		drawerCalls++
		return validCandidate("initial"), nil
	})
	dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
		reviewCalls++
		return `{"passed":false,"issues":[{"type":"missing_node","description":"仍缺少库存节点","element_ids":[]}]}`, nil
	})
	dependencies.Repairer = repairerFunc(func(context.Context, repair.Prompt) (string, error) {
		repairCalls++
		return validCandidate(fmt.Sprintf("repair-%d", repairCalls)), nil
	})

	state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(context.Background(), runInput("exhausted"))
	requireFailure(t, state, err, FailureRepairBudgetExhausted, StageReview)
	if state.Status != StatusFailed || state.Attempt != 2 || state.RepairsUsed != 2 {
		t.Fatalf("unexpected exhausted state: %+v", state)
	}
	if state.CurrentXML != validCandidate("repair-2") {
		t.Fatalf("last candidate should remain available only for diagnosis: %s", state.CurrentXML)
	}
	if analystCalls != 1 || drawerCalls != 1 || reviewCalls != 3 || repairCalls != 2 {
		t.Fatalf("unexpected calls: analyst=%d drawer=%d reviewer=%d repair=%d", analystCalls, drawerCalls, reviewCalls, repairCalls)
	}
}

// max_repairs=0 是合法对照配置；初稿结构失败时不得调用 Reviewer 或 Repair。
func TestRunHonorsZeroRepairBudgetAndSkipsReviewerForInvalidXML(t *testing.T) {
	dependencies := defaultDependencies()
	dependencies.Drawer = drawerFunc(func(context.Context, DrawInput) (string, error) {
		return badCandidate, nil
	})
	reviewCalls, repairCalls := 0, 0
	dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
		reviewCalls++
		return "", errors.New("reviewer must not be called")
	})
	dependencies.Repairer = repairerFunc(func(context.Context, repair.Prompt) (string, error) {
		repairCalls++
		return "", errors.New("repairer must not be called")
	})

	state, err := mustController(t, 0, dependencies).Run(context.Background(), runInput("zero-budget"))
	requireFailure(t, state, err, FailureRepairBudgetExhausted, StageValidation)
	if reviewCalls != 0 || repairCalls != 0 || state.Attempt != 0 || state.RepairsUsed != 0 {
		t.Fatalf("zero budget made unexpected calls: state=%+v reviews=%d repairs=%d", state, reviewCalls, repairCalls)
	}
	if state.LastValidation == nil || state.LastValidation.Passed {
		t.Fatalf("validation diagnosis must be retained: %+v", state.LastValidation)
	}
}

// Reviewer 协议损坏不是质量拒绝，必须原地失败而不是用 Repair 掩盖协议错误。
func TestRunStopsOnReviewerProtocolError(t *testing.T) {
	dependencies := defaultDependencies()
	dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
		return "not-json", nil
	})
	repairCalls := 0
	dependencies.Repairer = repairerFunc(func(context.Context, repair.Prompt) (string, error) {
		repairCalls++
		return validCandidate("unexpected"), nil
	})

	state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(context.Background(), runInput("protocol-error"))
	requireFailure(t, state, err, FailureReviewProtocol, StageReview)
	var protocolErr *reviewer.ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("failure must retain the protocol cause: %v", err)
	}
	if repairCalls != 0 || state.RepairsUsed != 0 {
		t.Fatalf("protocol errors must not consume repair budget: state=%+v repairs=%d", state, repairCalls)
	}
}

// 模型调用错误保留统一 code，并用 stage 指出故障角色；Repair 调用失败还应记录已消耗的调用数。
func TestRunClassifiesModelErrorsByStage(t *testing.T) {
	modelErr := errors.New("upstream unavailable")
	tests := []struct {
		name            string
		stage           Stage
		change          func(*Dependencies)
		wantRepairsUsed int
		wantAttempt     int
	}{
		{
			name:  "analyst",
			stage: StageAnalysis,
			change: func(dependencies *Dependencies) {
				dependencies.Analyst = analystFunc(func(context.Context, string) (string, error) {
					return "", modelErr
				})
			},
		},
		{
			name:  "drawer",
			stage: StageDraw,
			change: func(dependencies *Dependencies) {
				dependencies.Drawer = drawerFunc(func(context.Context, DrawInput) (string, error) {
					return "", modelErr
				})
			},
		},
		{
			name:  "reviewer",
			stage: StageReview,
			change: func(dependencies *Dependencies) {
				dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
					return "", modelErr
				})
			},
		},
		{
			name:  "repairer",
			stage: StageRepair,
			change: func(dependencies *Dependencies) {
				dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
					return `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少边","element_ids":[]}]}`, nil
				})
				dependencies.Repairer = repairerFunc(func(context.Context, repair.Prompt) (string, error) {
					return "", modelErr
				})
			},
			wantRepairsUsed: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := defaultDependencies()
			test.change(&dependencies)
			state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(
				context.Background(),
				runInput("model-error-"+test.name),
			)
			requireFailure(t, state, err, FailureModelError, test.stage)
			if !errors.Is(err, modelErr) {
				t.Fatalf("failure must retain model cause: %v", err)
			}
			if state.RepairsUsed != test.wantRepairsUsed || state.Attempt != test.wantAttempt {
				t.Fatalf("unexpected attempt accounting: %+v", state)
			}
		})
	}
}

// 模型没有报错但返回空内容时，应在产出阶段直接失败，不能拖到后续 Prompt 构建才变成内部错误。
func TestRunRejectsEmptyStageOutputs(t *testing.T) {
	tests := []struct {
		name            string
		stage           Stage
		change          func(*Dependencies)
		wantRepairsUsed int
	}{
		{
			name:  "analysis",
			stage: StageAnalysis,
			change: func(dependencies *Dependencies) {
				dependencies.Analyst = analystFunc(func(context.Context, string) (string, error) {
					return " ", nil
				})
			},
		},
		{
			name:  "drawer",
			stage: StageDraw,
			change: func(dependencies *Dependencies) {
				dependencies.Drawer = drawerFunc(func(context.Context, DrawInput) (string, error) {
					return "", nil
				})
			},
		},
		{
			name:  "repairer",
			stage: StageRepair,
			change: func(dependencies *Dependencies) {
				dependencies.Reviewer = reviewerFunc(func(context.Context, reviewer.Prompt) (string, error) {
					return `{"passed":false,"issues":[{"type":"missing_edge","description":"缺少边","element_ids":[]}]}`, nil
				})
				dependencies.Repairer = repairerFunc(func(context.Context, repair.Prompt) (string, error) {
					return "", nil
				})
			},
			wantRepairsUsed: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := defaultDependencies()
			test.change(&dependencies)
			state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(
				context.Background(),
				runInput("empty-output-"+test.name),
			)
			requireFailure(t, state, err, FailureInvalidStageOutput, test.stage)
			if state.RepairsUsed != test.wantRepairsUsed || state.Attempt != 0 {
				t.Fatalf("empty output produced an unexpected candidate: %+v", state)
			}
		})
	}
}

// 取消发生后不能再启动下一阶段；返回错误保留 context.Canceled 以便上层可靠判断。
func TestRunStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dependencies := defaultDependencies()
	dependencies.Analyst = analystFunc(func(context.Context, string) (string, error) {
		cancel()
		return "分析结果", nil
	})
	drawerCalls := 0
	dependencies.Drawer = drawerFunc(func(context.Context, DrawInput) (string, error) {
		drawerCalls++
		return validCandidate("unexpected"), nil
	})

	state, err := mustController(t, DefaultMaxRepairs, dependencies).Run(ctx, runInput("cancelled"))
	requireFailure(t, state, err, FailureCancelled, StageAnalysis)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("failure must unwrap to context.Canceled: %v", err)
	}
	if state.Status != StatusCancelled || drawerCalls != 0 {
		t.Fatalf("cancelled run started a later stage: state=%+v drawerCalls=%d", state, drawerCalls)
	}
}

// 同一个 Controller 可以被并发复用；每次 Run 的 analysis、candidate、run_id 必须保持请求内一致。
func TestRunKeepsConcurrentRequestStateIsolated(t *testing.T) {
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	var analystCalls, drawerCalls, reviewerCalls atomic.Int32
	dependencies := defaultDependencies()
	dependencies.Analyst = analystFunc(func(_ context.Context, requirement string) (string, error) {
		analystCalls.Add(1)
		ready <- struct{}{}
		<-release
		return "analysis:" + requirement, nil
	})
	dependencies.Drawer = drawerFunc(func(_ context.Context, input DrawInput) (string, error) {
		drawerCalls.Add(1)
		if input.AnalysisResult != "analysis:"+input.OriginalRequirement {
			return "", fmt.Errorf("cross-request analysis: %+v", input)
		}
		return validCandidate(input.OriginalRequirement), nil
	})
	dependencies.Reviewer = reviewerFunc(func(_ context.Context, prompt reviewer.Prompt) (string, error) {
		reviewerCalls.Add(1)
		var input reviewer.Input
		if err := json.Unmarshal([]byte(prompt.Payload), &input); err != nil {
			return "", err
		}
		if input.AnalysisResult != "analysis:"+input.OriginalRequirement {
			return "", fmt.Errorf("cross-request review input: %+v", input)
		}
		if input.CurrentXML != validCandidate(input.OriginalRequirement) {
			return "", fmt.Errorf("cross-request candidate: %s", input.CurrentXML)
		}
		return `{"passed":true,"issues":[]}`, nil
	})

	controller := mustController(t, DefaultMaxRepairs, dependencies)
	type runResult struct {
		state State
		err   error
	}
	results := make(chan runResult, 2)
	for _, input := range []RunInput{
		{RunID: "run-a", OriginalRequirement: "request-a"},
		{RunID: "run-b", OriginalRequirement: "request-b"},
	} {
		input := input
		go func() {
			state, err := controller.Run(context.Background(), input)
			results <- runResult{state: state, err: err}
		}()
	}

	<-ready
	<-ready
	close(release)

	states := make(map[string]State, 2)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent run failed: %v", result.err)
		}
		states[result.state.RunID] = result.state
	}
	for runID, requirement := range map[string]string{
		"run-a": "request-a",
		"run-b": "request-b",
	} {
		state, ok := states[runID]
		if !ok {
			t.Fatalf("missing state for %s: %+v", runID, states)
		}
		if state.OriginalRequirement != requirement ||
			state.AnalysisResult != "analysis:"+requirement ||
			state.FinalXML != validCandidate(requirement) {
			t.Fatalf("state leaked across requests: %+v", state)
		}
	}
	if analystCalls.Load() != 2 || drawerCalls.Load() != 2 || reviewerCalls.Load() != 2 {
		t.Fatalf(
			"unexpected concurrent calls: analyst=%d drawer=%d reviewer=%d",
			analystCalls.Load(),
			drawerCalls.Load(),
			reviewerCalls.Load(),
		)
	}
}

func TestNewControllerRejectsInvalidConfiguration(t *testing.T) {
	for _, maxRepairs := range []int{-1, MaxSupportedRepairs + 1} {
		if _, err := NewController(Config{MaxRepairs: maxRepairs}, defaultDependencies()); err == nil {
			t.Fatalf("expected max repairs %d to fail", maxRepairs)
		}
	}

	tests := []struct {
		name   string
		change func(*Dependencies)
	}{
		{name: "analyst", change: func(dependencies *Dependencies) { dependencies.Analyst = nil }},
		{name: "drawer", change: func(dependencies *Dependencies) { dependencies.Drawer = nil }},
		{name: "reviewer", change: func(dependencies *Dependencies) { dependencies.Reviewer = nil }},
		{name: "repairer", change: func(dependencies *Dependencies) { dependencies.Repairer = nil }},
		{name: "validator", change: func(dependencies *Dependencies) { dependencies.Validator = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := defaultDependencies()
			test.change(&dependencies)
			if _, err := NewController(Config{MaxRepairs: DefaultMaxRepairs}, dependencies); err == nil {
				t.Fatal("expected missing dependency to fail")
			}
		})
	}
}
