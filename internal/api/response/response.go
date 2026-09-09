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

// FailureWithData 用于需要携带结构化诊断的失败响应；原 Failure 继续服务简单错误。
func FailureWithData(code, info string, data any) Envelope[any] {
	return Envelope[any]{
		Code: code,
		Info: info,
		Data: data,
	}
}
