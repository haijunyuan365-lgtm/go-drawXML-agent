package types

type AppError struct {
	Code string
	Info string
}

func NewAppError(code, info string) *AppError {
	return &AppError{
		Code: code,
		Info: info,
	}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}

	return e.Code + ": " + e.Info
}
