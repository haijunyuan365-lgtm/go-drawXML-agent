package tree

import "context"

type Handler[C any, D any, R any] interface {
	Apply(ctx context.Context, command C, dynamic D) (R, error)
}

func Route[C any, D any, R any](ctx context.Context, next Handler[C, D, R], command C, dynamic D) (R, error) {
	if next == nil {
		//泛型 R 不一定是指针、切片、map 或接口，也可能是结构体,结构体不能返回 nil
		return zero[R](), nil
	}
	return next.Apply(ctx, command, dynamic)
}

func zero[T any]() T {
	//value默认为 类型为T的0值
	var value T
	return value
}
