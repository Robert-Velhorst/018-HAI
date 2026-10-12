package config

import (
	"automation-hub-idp/internal/app/utils"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	baseBlockDurationMinutes        string = "BLOCKING_TIME_EXPONENTIATION_BASIS"
	maxLoginAttemptsBeforeBlock     string = "MAX_LOGIN_ATTEMPTS_BEFORE_BLOCK"
	minTimeBetweenAttemptsInSeconds string = "MIN_TIME_BETWEEN_ATTEMPTS_IN_SECONDS"
	expirationTimeResetTokenInHours string = "EXPIRATION_TIME_RESET_TOKEN_IN_HOURS"
	accessTokenDurationMinutes      string = "ACCESS_TOKEN_DURATION_MINUTES"
	refreshTokenDurationDays        string = "REFRESH_TOKEN_DURATION_DAYS"
	passwordResetTopic              string = "PASSWORD_RESET_TOPIC"
	accountBlockedTopic             string = "ACCOUNT_BLOCKED_TOPIC"
	accountCreatedTopic             string = "ACCOUNT_CREATED_TOPIC"
	jwtSecret                              = "JWT_SECRET"
)

type authenticationConfig struct {
	BaseBlockDurationMinutes      int
	MaxLoginAttemptsBeforeBlock   int
	MinTimeBetweenAttemptsSeconds time.Duration
	ExpirationTimeResetTokenHours time.Duration
	AccessTokenDurationMinutes    time.Duration
	RefreshTokenDurationDays      time.Duration
	PasswordResetTopic            string
	AccountBlockedTopic           string
	AccountCreatedTopic           string
	JwtSecret                     string
	PasswordHasher                utils.PasswordHasher
}

func newAuthenticationConfig() (*authenticationConfig, error) {
	passwordResetTopicValue := strings.TrimSpace(getEnvString(passwordResetTopic, ""))
	accountBlockedTopicValue := strings.TrimSpace(getEnvString(accountBlockedTopic, ""))
	accountCreatedTopicValue := strings.TrimSpace(getEnvString(accountCreatedTopic, ""))
	for name, value := range map[string]string{
		passwordResetTopic:  passwordResetTopicValue,
		accountBlockedTopic: accountBlockedTopicValue,
		accountCreatedTopic: accountCreatedTopicValue,
	} {
		if value == "" {
			return nil, fmt.Errorf("authentication topic is required: set %s", name)
		}
	}

	baseBlockDurationMinutesValue, err := authenticationIntSetting(baseBlockDurationMinutes, 0, true)
	if err != nil {
		return nil, err
	}
	maxLoginAttemptsBeforeBlockValue, err := authenticationIntSetting(maxLoginAttemptsBeforeBlock, 0, true)
	if err != nil {
		return nil, err
	}
	minTimeBetweenAttemptsValue, err := authenticationIntSetting(minTimeBetweenAttemptsInSeconds, 0, false)
	if err != nil {
		return nil, err
	}
	resetTokenHoursValue, err := authenticationIntSetting(expirationTimeResetTokenInHours, 24, true)
	if err != nil {
		return nil, err
	}
	accessTokenMinutesValue, err := authenticationIntSetting(accessTokenDurationMinutes, 15, true)
	if err != nil {
		return nil, err
	}
	refreshTokenDaysValue, err := authenticationIntSetting(refreshTokenDurationDays, 4, true)
	if err != nil {
		return nil, err
	}
	for _, setting := range []struct {
		name  string
		value int
		unit  time.Duration
	}{
		{baseBlockDurationMinutes, baseBlockDurationMinutesValue, time.Minute},
		{minTimeBetweenAttemptsInSeconds, minTimeBetweenAttemptsValue, time.Second},
		{expirationTimeResetTokenInHours, resetTokenHoursValue, time.Hour},
		{accessTokenDurationMinutes, accessTokenMinutesValue, time.Minute},
		{refreshTokenDurationDays, refreshTokenDaysValue, 24 * time.Hour},
	} {
		if int64(setting.value) > math.MaxInt64/int64(setting.unit) {
			return nil, fmt.Errorf("%s exceeds the supported duration range", setting.name)
		}
	}

	jwtSecretValue := getEnvString(jwtSecret, "")
	if strings.TrimSpace(jwtSecretValue) == "" {
		return nil, fmt.Errorf("JWT signing secret is required: set %s", jwtSecret)
	}
	if len([]byte(jwtSecretValue)) < 32 {
		return nil, fmt.Errorf("%s must contain at least 32 bytes", jwtSecret)
	}

	return &authenticationConfig{
		BaseBlockDurationMinutes:      baseBlockDurationMinutesValue,
		MaxLoginAttemptsBeforeBlock:   maxLoginAttemptsBeforeBlockValue,
		MinTimeBetweenAttemptsSeconds: time.Duration(minTimeBetweenAttemptsValue),
		ExpirationTimeResetTokenHours: time.Duration(resetTokenHoursValue),
		AccessTokenDurationMinutes:    time.Duration(accessTokenMinutesValue),
		RefreshTokenDurationDays:      time.Duration(refreshTokenDaysValue) * (24 * time.Hour),
		PasswordResetTopic:            passwordResetTopicValue,
		AccountBlockedTopic:           accountBlockedTopicValue,
		AccountCreatedTopic:           accountCreatedTopicValue,
		JwtSecret:                     jwtSecretValue,
		PasswordHasher:                utils.DefaultBcryptHasher(),
	}, nil
}

func authenticationIntSetting(name string, defaultValue int, mustBePositive bool) (int, error) {
	value := defaultValue
	if raw, exists := os.LookupEnv(name); exists {
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", name)
		}
		value = parsed
	}
	if mustBePositive && value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	if !mustBePositive && value < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return value, nil
}
