package config

import (
	"os"
	"strconv"
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
	KafkaConfig, err = newOptionalKafkaConfig()
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
	MailConfig = newMailConfig()
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

func getEnvBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}
