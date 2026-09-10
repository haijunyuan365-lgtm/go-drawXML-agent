// Package workflow 编排一次 Draw.io 生成请求中的分析、绘制、校验、审查和有限修复。
// 本包只依赖窄领域端口；生产 Agent/YAML 由 infrastructure/adk 适配并经原 Runner 调用。
package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ai-agent-scaffold/internal/domain/diagram/repair"
	"ai-agent-scaffold/internal/domain/diagram/reviewer"
	"ai-agent-scaffold/internal/domain/validation"
)

const (
	// DefaultMaxRepairs 是首版质量闭环的默认修复预算。
	DefaultMaxRepairs = 2
	// MaxSupportedRepairs 限制配置上界，避免错误配置制造高成本的近似无限循环。
	MaxSupportedRepairs = 3
)

// Config 只描述质量修复循环自己的预算，不复用 Agent 内部的工具循环次数。
type Config struct {
	MaxRepairs int
}

type Stage string

const (
	StagePending    Stage = "pending"
	StageAnalysis   Stage = "analysis"
	StageDraw       Stage = "draw"
	StageValidation Stage = "validation"
	StageReview     Stage = "review"
	StageRepair     Stage = "repair"
	StageCompleted  Stage = "completed"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type FailureCode string

const (
	FailureInvalidRequest        FailureCode = "invalid_request"
	FailureInvalidStageOutput    FailureCode = "invalid_stage_output"
	FailureModelError            FailureCode = "model_error"
	FailureReviewProtocol        FailureCode = reviewer.ProtocolErrorCode
	FailureRepairBudgetExhausted FailureCode = "repair_budget_exhausted"
	FailureWorkflowError         FailureCode = "workflow_error"
	FailureCancelled             FailureCode = "cancelled"
	FailureDeadlineExceeded      FailureCode = "deadline_exceeded"
)

// Failure 将“失败类别”和“发生阶段”正交记录。
// 同一个 model_error 可以发生在不同角色，调用方无需通过错误字符串猜位置。
type Failure struct {
	Code    FailureCode `json:"code"`
	Stage   Stage       `json:"stage"`
	Message string      `json:"message"`
	cause   error
}

func (f *Failure) Error() string {
	if f == nil {
		return "diagram workflow failed"
	}
	return fmt.Sprintf("%s at %s: %s", f.Code, f.Stage, f.Message)
}

func (f *Failure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.cause
}

// State 属于单次 Run 调用，Controller 自身不保存任何请求级可变字段。
// Attempt 是当前候选版本号；RepairsUsed 是已发起的 Repair 调用数。
// Repair 调用失败时不会产生新候选，因此两者允许不同。
type State struct {
	RunID               string             `json:"run_id"`
	OriginalRequirement string             `json:"original_requirement"`
	AnalysisResult      string             `json:"analysis_result,omitempty"`
	CurrentXML          string             `json:"current_xml,omitempty"`
	Attempt             int                `json:"attempt"`
	RepairsUsed         int                `json:"repairs_used"`
	LastValidation      *validation.Result `json:"last_validation,omitempty"`
	LastReview          *reviewer.Result   `json:"last_review,omitempty"`
	Stage               Stage              `json:"stage"`
	Status              Status             `json:"status"`
	Failure             *Failure           `json:"failure,omitempty"`
	FinalXML            string             `json:"final_xml,omitempty"`
}

type RunInput struct {
	RunID               string
	OriginalRequirement string
}

type DrawInput struct {
	OriginalRequirement string
	AnalysisResult      string
}

// 以下四个窄接口隔离领域控制流和具体 Agent 运行时。
// S04 使用测试替身，S05 由 infrastructure/adk 中的生产适配器接到真实 Runner。
type Analyst interface {
	Analyze(ctx context.Context, requirement string) (string, error)
}

type Drawer interface {
	Draw(ctx context.Context, input DrawInput) (string, error)
}

type SemanticReviewer interface {
	Review(ctx context.Context, prompt reviewer.Prompt) (string, error)
}

type Repairer interface {
	Repair(ctx context.Context, prompt repair.Prompt) (string, error)
}

type Dependencies struct {
	Analyst   Analyst
	Drawer    Drawer
	Reviewer  SemanticReviewer
	Repairer  Repairer
	Validator validation.Validator
}

// Controller 只保存不可变配置和并发安全应由实现方保证的端口。
// 所有候选、轮次和诊断都在 Run 的局部 State 中，防止不同请求串状态。
type Controller struct {
	config    Config
	analyst   Analyst
	drawer    Drawer
	reviewer  SemanticReviewer
	repairer  Repairer
	validator validation.Validator
}

func NewController(config Config, dependencies Dependencies) (*Controller, error) {
	if config.MaxRepairs < 0 || config.MaxRepairs > MaxSupportedRepairs {
		return nil, fmt.Errorf("max repairs must be between 0 and %d", MaxSupportedRepairs)
	}
	if dependencies.Analyst == nil {
		return nil, errors.New("analyst is required")
	}
	if dependencies.Drawer == nil {
		return nil, errors.New("drawer is required")
	}
	if dependencies.Reviewer == nil {
		return nil, errors.New("reviewer is required")
	}
	if dependencies.Repairer == nil {
		return nil, errors.New("repairer is required")
	}
	if dependencies.Validator == nil {
		return nil, errors.New("validator is required")
	}

	return &Controller{
		config:    config,
		analyst:   dependencies.Analyst,
		drawer:    dependencies.Drawer,
		reviewer:  dependencies.Reviewer,
		repairer:  dependencies.Repairer,
		validator: dependencies.Validator,
	}, nil
}

// Run 执行一个有界状态机：Analyst/Drawer 各一次，候选按
// Validator -> Reviewer -> (必要时) Repair 的顺序推进。
// FinalXML 只在同一个候选先后通过确定性校验和语义审查后赋值。
func (c *Controller) Run(ctx context.Context, input RunInput) (State, error) {
	state := State{
		RunID:               strings.TrimSpace(input.RunID),
		OriginalRequirement: input.OriginalRequirement,
		Stage:               StagePending,
		Status:              StatusRunning,
	}
	if ctx == nil {
		return failRun(&state, FailureInvalidRequest, StagePending, "context is required", nil)
	}
	if state.RunID == "" {
		return failRun(&state, FailureInvalidRequest, StagePending, "run ID is required", nil)
	}
	if strings.TrimSpace(input.OriginalRequirement) == "" {
		return failRun(&state, FailureInvalidRequest, StagePending, "original requirement is required", nil)
	}
	if failure := contextFailure(ctx, StagePending); failure != nil {
		return finishFailure(&state, failure)
	}

	state.Stage = StageAnalysis
	analysis, err := c.analyst.Analyze(ctx, input.OriginalRequirement)
	if err != nil {
		runErr := c.failCall(&state, ctx, StageAnalysis, "analyst call failed", err)
		return state, runErr
	}
	state.AnalysisResult = analysis
	if failure := contextFailure(ctx, StageAnalysis); failure != nil {
		return finishFailure(&state, failure)
	}
	if strings.TrimSpace(analysis) == "" {
		return failRun(&state, FailureInvalidStageOutput, StageAnalysis, "analyst returned an empty result", nil)
	}

	state.Stage = StageDraw
	candidate, err := c.drawer.Draw(ctx, DrawInput{
		OriginalRequirement: input.OriginalRequirement,
		AnalysisResult:      analysis,
	})
	if err != nil {
		runErr := c.failCall(&state, ctx, StageDraw, "drawer call failed", err)
		return state, runErr
	}
	state.CurrentXML = candidate
	if failure := contextFailure(ctx, StageDraw); failure != nil {
		return finishFailure(&state, failure)
	}
	if strings.TrimSpace(candidate) == "" {
		return failRun(&state, FailureInvalidStageOutput, StageDraw, "drawer returned an empty result", nil)
	}

	issues, rejectedAt, passed, err := c.evaluateInitialCandidate(ctx, &state)
	if err != nil {
		return state, err
	}

	for !passed {
		if failure := contextFailure(ctx, rejectedAt); failure != nil {
			return finishFailure(&state, failure)
		}
		// 在调用 Repair 之前检查预算，保证 max_repairs=N 时最多只发起 N 次修复。
		if state.RepairsUsed >= c.config.MaxRepairs {
			message := fmt.Sprintf("repair budget exhausted after %d repair attempt(s)", state.RepairsUsed)
			return failRun(&state, FailureRepairBudgetExhausted, rejectedAt, message, nil)
		}

		prompt, promptErr := repair.BuildPrompt(repair.Input{
			OriginalRequirement: state.OriginalRequirement,
			AnalysisResult:      state.AnalysisResult,
			CurrentXML:          state.CurrentXML,
			Issues:              issues,
		})
		if promptErr != nil {
			return failRun(&state, FailureWorkflowError, StageRepair, "build repair input: "+promptErr.Error(), promptErr)
		}

		state.Stage = StageRepair
		// RepairsUsed 统计实际发起的模型调用；即使调用失败，也已经消耗了预算和成本。
		state.RepairsUsed++
		repairedOutput, repairErr := c.repairer.Repair(ctx, prompt)
		if repairErr != nil {
			runErr := c.failCall(&state, ctx, StageRepair, "repair call failed", repairErr)
			return state, runErr
		}
		if failure := contextFailure(ctx, StageRepair); failure != nil {
			return finishFailure(&state, failure)
		}
		if strings.TrimSpace(repairedOutput) == "" {
			return failRun(&state, FailureInvalidStageOutput, StageRepair, "repairer returned an empty result", nil)
		}

		// 收到 Repair 响应才生成新候选版本。即使 XML 不合法，也保留它用于诊断和下一轮定向修复。
		state.Attempt++
		state.CurrentXML = repairedOutput
		state.Stage = StageValidation
		validatedXML, parseErr := repair.ParseResponseWithValidator(repairedOutput, c.validator)
		if failure := contextFailure(ctx, StageValidation); failure != nil {
			return finishFailure(&state, failure)
		}
		if parseErr != nil {
			var validationErr *validation.Error
			if !errors.As(parseErr, &validationErr) {
				return failRun(&state, FailureWorkflowError, StageValidation, "parse repair output: "+parseErr.Error(), parseErr)
			}
			state.LastValidation = validationResultPtr(validationErr.Result)
			issues = repair.FromValidationIssues(validationErr.Result.Issues)
			rejectedAt = StageValidation
			continue
		}

		state.CurrentXML = validatedXML
		state.LastValidation = validationResultPtr(validation.Result{
			Passed: true,
			Issues: []validation.Issue{},
		})

		issues, passed, err = c.reviewCurrentCandidate(ctx, &state)
		rejectedAt = StageReview
		if err != nil {
			return state, err
		}
	}

	state.Stage = StageCompleted
	state.Status = StatusSucceeded
	state.FinalXML = state.CurrentXML
	state.Failure = nil
	return state, nil
}

func (c *Controller) evaluateInitialCandidate(ctx context.Context, state *State) ([]repair.Issue, Stage, bool, error) {
	state.Stage = StageValidation
	result := c.validator.Validate(state.CurrentXML)
	state.LastValidation = validationResultPtr(result)
	if failure := contextFailure(ctx, StageValidation); failure != nil {
		return nil, StageValidation, false, recordFailure(state, failure)
	}
	if !result.Passed {
		return repair.FromValidationIssues(result.Issues), StageValidation, false, nil
	}

	issues, passed, err := c.reviewCurrentCandidate(ctx, state)
	return issues, StageReview, passed, err
}

func (c *Controller) reviewCurrentCandidate(ctx context.Context, state *State) ([]repair.Issue, bool, error) {
	state.Stage = StageReview
	if failure := contextFailure(ctx, StageReview); failure != nil {
		return nil, false, recordFailure(state, failure)
	}

	prompt, err := reviewer.BuildPrompt(reviewer.Input{
		OriginalRequirement: state.OriginalRequirement,
		AnalysisResult:      state.AnalysisResult,
		CurrentXML:          state.CurrentXML,
	})
	if err != nil {
		return nil, false, recordFailure(state, newFailure(
			FailureWorkflowError,
			StageReview,
			"build reviewer input: "+err.Error(),
			err,
		))
	}

	response, err := c.reviewer.Review(ctx, prompt)
	if err != nil {
		return nil, false, c.failCall(state, ctx, StageReview, "reviewer call failed", err)
	}
	if failure := contextFailure(ctx, StageReview); failure != nil {
		return nil, false, recordFailure(state, failure)
	}

	result, err := reviewer.ParseResponse(response)
	if err != nil {
		return nil, false, recordFailure(state, newFailure(
			FailureReviewProtocol,
			StageReview,
			err.Error(),
			err,
		))
	}
	state.LastReview = reviewResultPtr(result)
	if result.Passed {
		return nil, true, nil
	}
	return repair.FromReviewerIssues(result.Issues), false, nil
}

func (c *Controller) failCall(state *State, ctx context.Context, stage Stage, message string, cause error) error {
	if failure := contextFailure(ctx, stage); failure != nil {
		return recordFailure(state, failure)
	}
	return recordFailure(state, newFailure(FailureModelError, stage, message+": "+cause.Error(), cause))
}

func contextFailure(ctx context.Context, stage Stage) *Failure {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	cause := context.Cause(ctx)
	if cause == nil {
		cause = ctx.Err()
	}
	code := FailureCancelled
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = FailureDeadlineExceeded
	}
	return newFailure(code, stage, cause.Error(), cause)
}

func newFailure(code FailureCode, stage Stage, message string, cause error) *Failure {
	return &Failure{Code: code, Stage: stage, Message: message, cause: cause}
}

func failRun(state *State, code FailureCode, stage Stage, message string, cause error) (State, error) {
	return finishFailure(state, newFailure(code, stage, message, cause))
}

func finishFailure(state *State, failure *Failure) (State, error) {
	err := recordFailure(state, failure)
	return *state, err
}

func recordFailure(state *State, failure *Failure) error {
	state.Stage = failure.Stage
	state.Status = StatusFailed
	if failure.Code == FailureCancelled || failure.Code == FailureDeadlineExceeded {
		state.Status = StatusCancelled
	}
	state.FinalXML = ""
	state.Failure = failure
	return failure
}

func validationResultPtr(result validation.Result) *validation.Result {
	copyResult := result
	copyResult.Issues = append([]validation.Issue(nil), result.Issues...)
	return &copyResult
}

func reviewResultPtr(result reviewer.Result) *reviewer.Result {
	copyResult := result
	copyResult.Issues = append([]reviewer.Issue(nil), result.Issues...)
	return &copyResult
}
