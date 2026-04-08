package model

import "time"

type ImportError struct {
	Filename  string
	StackTrace string
	Timestamp time.Time
}
