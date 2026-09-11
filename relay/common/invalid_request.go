package common

import "errors"

// InvalidRequestError 表示请求本身不合法——参数取值越界、素材组合互斥之类，
// 换任何渠道、重试多少次都不会成功。
//
// 为什么需要一个类型而不是直接返回 error：任务中转把 BuildRequestBody 的失败
// 一律当成 500 渠道错误，于是同一条非法请求会被重试满 RetryTimes 次、逐个分组
// 顺延，最后回给调用方一句「可用渠道不存在」——真正的原因（哪个参数不合法）只
// 留在服务端日志里。调用方拿着这句话无从下手，而且每次都白跑几次渠道选择。
type InvalidRequestError struct {
	Err error
}

func (e *InvalidRequestError) Error() string {
	if e == nil || e.Err == nil {
		return "invalid request"
	}
	return e.Err.Error()
}

func (e *InvalidRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewInvalidRequest 包装一个请求合法性错误；err 为 nil 时返回 nil，便于直接透传。
func NewInvalidRequest(err error) error {
	if err == nil {
		return nil
	}
	return &InvalidRequestError{Err: err}
}

// IsInvalidRequest 报告该错误链上是否有请求合法性错误。
func IsInvalidRequest(err error) bool {
	if err == nil {
		return false
	}
	var target *InvalidRequestError
	return errors.As(err, &target)
}
