package api

import "fmt"

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("API Error: %d %s", e.Code, e.Message)
}

var _ error = (*Error)(nil)
