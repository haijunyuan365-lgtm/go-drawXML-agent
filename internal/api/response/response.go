package response

import "ai-agent-scaffold/pkg/types"

type Envelope[T any] struct {
	Code string `json:"code"`
	Info string `json:"info"`
	Data T      `json:"data,omitempty"`
}

func Success[T any](data T) Envelope[T] {
	return Envelope[T]{
		Code: types.CodeSuccess,
		Info: types.InfoSuccess,
		Data: data,
	}
}

func Failure(code, info string) Envelope[any] {
	return Envelope[any]{
		Code: code,
		Info: info,
	}
}
