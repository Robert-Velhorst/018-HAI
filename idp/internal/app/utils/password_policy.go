package utils

import (
	"errors"
	"unicode/utf8"
)

const (
	MinimumPasswordLength = 12
	MaximumPasswordBytes  = 72
)

var (
	ErrPasswordTooShort = errors.New("password must contain at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must not exceed 72 bytes")
)

func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinimumPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaximumPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}
