package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePasswordLengthBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "minimum characters", password: strings.Repeat("a", MinimumPasswordLength)},
		{name: "unicode characters counted as characters", password: strings.Repeat("é", MinimumPasswordLength)},
		{name: "bcrypt byte limit", password: strings.Repeat("a", MaximumPasswordBytes)},
		{name: "below minimum", password: strings.Repeat("a", MinimumPasswordLength-1), wantErr: ErrPasswordTooShort},
		{name: "above bcrypt byte limit", password: strings.Repeat("a", MaximumPasswordBytes+1), wantErr: ErrPasswordTooLong},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePassword(test.password)
			if test.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}
