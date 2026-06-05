package executor

import "strings"

func taskFailure(message string, err error) TaskResult {
	if err == nil {
		return TaskResult{Success: false, Message: message}
	}
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return TaskResult{Success: false, Message: message}
	}
	return TaskResult{Success: false, Message: message + ": " + detail}
}
