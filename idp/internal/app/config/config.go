package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

var (
	AuthenticationConfig *authenticationConfig
	ServerConfig         *serverConfig
	KafkaConfig          *kafkaConfig
	PostgresConfig       *postgresConfig
	RedisConfig          *redisConfig
	MailConfig           *mailConfig
	LocalPreviewConfig   *localPreviewConfig
)

func Setup() error {
	var err error
	KafkaConfig, err = newKafkaConfig()
	if err != nil {
		return err
	}
	PostgresConfig, err = newPostgresConfig()
	if err != nil {
		return err
	}
	RedisConfig, err = newRedisConfig()
	if err != nil {
		return err
	}
	ServerConfig, err = newServerConfig()
	if err != nil {
		return err
	}
	AuthenticationConfig, err = newAuthenticationConfig()
	if err != nil {
		return err
	}
	MailConfig, err = newMailConfig()
	if err != nil {
		return err
	}
	LocalPreviewConfig, err = newLocalPreviewConfig()
	if err != nil {
		return err
	}

	return nil
}

func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		intVal, err := strconv.Atoi(value)
		if err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvString(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) (bool, error) {
	if value, exists := os.LookupEnv(key); exists {
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, fmt.Errorf("%s must be a valid boolean", key)
		}
		return parsed, nil
	}
	return defaultValue, nil
}
