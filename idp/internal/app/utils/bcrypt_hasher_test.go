package utils

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"testing"
)

func TestBcryptHasher_Hash(t *testing.T) {
	hasher := DefaultBcryptHasher()

	password := "my-secret-password"
	hashedPassword, err := hasher.Hash(password)

	assert.NoError(t, err)
	assert.NotEmpty(t, hashedPassword)

	// Ensure the hashed password is not the same as the plain password
	assert.NotEqual(t, password, hashedPassword)

	// Check the hashed password is valid bcrypt hash
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	assert.NoError(t, err)
}

func TestBcryptHasher_Compare(t *testing.T) {
	hasher := DefaultBcryptHasher()

	password := "my-secret-password"
	hashedPassword, err := hasher.Hash(password)
	assert.NoError(t, err)

	// Comparing the hashed password with the correct original password
	err = hasher.Compare(hashedPassword, password)
	assert.NoError(t, err)

	// Comparing the hashed password with an incorrect password
	err = hasher.Compare(hashedPassword, "wrong-password")
	require.ErrorIs(t, err, ErrPasswordMismatch)

	// A malformed stored hash is an internal verification failure, not a mismatch.
	err = hasher.Compare("invalid-hash", password)
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrPasswordMismatch))
}

func TestDefaultBcryptHasher(t *testing.T) {
	hasher := DefaultBcryptHasher().(*BcryptHasher)

	assert.Equal(t, bcrypt.DefaultCost, hasher.cost)
}
