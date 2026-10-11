package config

import (
	"log"
	"os"
	"strings"
)

const (
	configDir     string = "CONFIG_DIR"
	kafkaBrokers  string = "KAFKA_BROKERS"
	kafkaTopic    string = "KAFKA_TOPIC"
	reloadEnabled string = "NGINX_RELOAD_ENABLED"
)

type Configuration struct {
	ConfigDir     string
	Brokers       []string
	Topic         string
	ReloadEnabled bool
}

var AppConfig Configuration

func Init() {
	kafkaBrokersList := getStringListFromEnv(kafkaBrokers, "kafka1:9092,kafka2:9093,kafka3:9094")
	AppConfig = Configuration{
		ConfigDir:     getEnvString(configDir, "/app/sites-enabled"),
		Brokers:       kafkaBrokersList,
		Topic:         getEnvString(kafkaTopic, "automation-events"),
		ReloadEnabled: getEnvBool(reloadEnabled, false),
	}
}

func getStringListFromEnv(envVarName, defaultValue string) []string {
	value := getEnvString(envVarName, defaultValue)
	values := make([]string, 0)
	for _, entry := range strings.Split(value, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			values = append(values, entry)
		}
	}
	return values
}

func getEnvString(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	log.Printf("Using default value for %s: %s", key, defaultValue)
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Printf("Using default value for %s: %v", key, defaultValue)
		return defaultValue
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		log.Printf("Invalid boolean for %s: %s. Using default: %v", key, value, defaultValue)
		return defaultValue
	}
}
