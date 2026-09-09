package validation

import "fmt"

// Issue describes one deterministic contract violation found in an output.
type Issue struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Path      string `json:"path,omitempty"`
	ElementID string `json:"elementId,omitempty"`
	Attribute string `json:"attribute,omitempty"`
}

// Result is the structured result returned by an output validator.
type Result struct {
	Passed bool    `json:"passed"`
	Issues []Issue `json:"issues"`
}

// Validator checks one final agent output without making another model call.
type Validator interface {
	Name() string
	Validate(output string) Result
}

// Error carries the structured validation result across application layers.
type Error struct {
	Validator string `json:"validator"`
	Result    Result `json:"result"`
}

func (e *Error) Error() string {
	if e == nil {
		return "output validation failed"
	}
	if len(e.Result.Issues) == 0 {
		return fmt.Sprintf("output validation %q failed", e.Validator)
	}

	first := e.Result.Issues[0]
	return fmt.Sprintf(
		"output validation %q failed with %d issue(s): %s: %s",
		e.Validator,
		len(e.Result.Issues),
		first.Code,
		first.Message,
	)
}
