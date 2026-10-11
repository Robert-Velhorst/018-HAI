package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	userDb     string = "DB_USER"
	passwordDb string = "DB_PASSWORD"
	dbName     string = "DB_NAME"
	dbHost     string = "DB_HOST"
	dbPort     string = "DB_PORT"
	runMode    string = "RUN_MODE"
)

type postgresConfig struct {
	User     string
	Password string
	DbName   string
	DbHost   string
	DbPort   int
}

func newPostgresConfig() (*postgresConfig, error) {

	port := getEnvInt(dbPort, -1)
	if port == -1 {
		errorMessage := fmt.Sprintf("error: Port is not a valid number port, please check the environment variable: %s", dbPort)
		return nil, errors.New(errorMessage)
	}
	if port <= 0 || port > 65535 {
		errorMessage := fmt.Sprintf("error: Port %d is not valid, please check the environment variable: %s", port, dbPort)
		return nil, errors.New(errorMessage)
	}
	host := strings.TrimSpace(getEnvString(dbHost, ""))
	if host == "" {
		errorMessage := fmt.Sprintf("error: Host is not set, please check the environment variable: %s", dbHost)
		return nil, errors.New(errorMessage)
	}
	name := strings.TrimSpace(getEnvString(dbName, ""))
	if name == "" {
		errorMessage := fmt.Sprintf("error: Name is not set, please check the environment variable: %s", dbName)
		return nil, errors.New(errorMessage)
	}
	user := strings.TrimSpace(getEnvString(userDb, ""))
	if user == "" {
		return nil, fmt.Errorf("error: User is not set, please check the environment variable: %s", userDb)
	}
	password := getEnvString(passwordDb, "")
	if password == "" {
		return nil, fmt.Errorf("error: Password is not set, please check the environment variable: %s", passwordDb)
	}
	if strings.EqualFold(strings.TrimSpace(getEnvString(runMode, "production")), "production") && weakDatabasePassword(password) {
		return nil, fmt.Errorf("error: %s must be a non-placeholder secret of at least 16 characters in production", passwordDb)
	}

	return &postgresConfig{
		User:     user,
		Password: password,
		DbName:   name,
		DbHost:   host,
		DbPort:   port,
	}, nil
}

func weakDatabasePassword(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len([]byte(value)) < 16 {
		return true
	}
	for _, marker := range []string{"change-this", "changeme", "example", "placeholder"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return normalized == "postgres" || normalized == "password"
}
