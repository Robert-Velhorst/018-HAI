package utils

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

type BcryptHasher struct {
	cost int
}

func NewBcryptHasher(cost int) PasswordHasher {
	return &BcryptHasher{cost: cost}
}

func DefaultBcryptHasher() PasswordHasher {
	return &BcryptHasher{cost: bcrypt.DefaultCost}
}

func (b *BcryptHasher) Hash(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), b.cost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

func (b *BcryptHasher) Compare(hashedPassword, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrPasswordMismatch
	}
	return err
}
