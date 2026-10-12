package utils

import "errors"

var ErrPasswordMismatch = errors.New("password mismatch")

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hashedPassword, password string) error
}
