package telegram

import "fmt"

type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("telegram %s: [%d] %s (retry after %ds)", e.Method, e.Code, e.Description, e.RetryAfter)
	}
	return fmt.Sprintf("telegram %s: [%d] %s", e.Method, e.Code, e.Description)
}

func (e *APIError) IsRetryable() bool {
	return e.Code == 429 || e.Code >= 500
}
