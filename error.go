package main

import "fmt"

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e APIError) Error() string {
	return fmt.Sprintf("api error: code=%d, message=%s", e.Code, e.Message)
}
