package order

import (
	"context"
	"time"
)

// postCommitTimeout 限制提交后即时动作的最长执行时间，避免请求线程被后台投影/广播长期占住。
const postCommitTimeout = 3 * time.Second

// postCommitContext 创建提交后即时动作使用的有界后台上下文。
func postCommitContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), postCommitTimeout)
}
