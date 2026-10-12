package workbench

import "fmt"

// 请求本身已经耗尽重试预算，调度层不得再把整个步骤重跑四次。
type RetriesExhausted struct{ Cause error }

func (e *RetriesExhausted) Error() string {
	return fmt.Sprintf("已自动重试 3 次（初次加重试共4次），仍未完成：%v", e.Cause)
}
func (e *RetriesExhausted) Unwrap() error { return e.Cause }
