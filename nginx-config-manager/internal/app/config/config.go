package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	configDir     string = "CONFIG_DIR"
	kafkaBrokers  string = "KAFKA_BROKERS"
	kafkaTopic    string = "KAFKA_TOPIC"
	reloadEnabled string = "NGINX_RELOAD_ENABLED"
	healthPort    string = "NGINX_CONFIG_MANAGER_HEALTH_PORT"
	consumerGroup string = "NGINX_CONFIG_MANAGER_GROUP_ID"
	inboxDir      string = "NGINX_CONFIG_MANAGER_INBOX_DIR"
	maxAttempts   string = "NGINX_CONFIG_MANAGER_MAX_EVENT_ATTEMPTS"
	retention     string = "NGINX_CONFIG_MANAGER_INBOX_RETENTION_HOURS"
)

type Configuration struct {
	ConfigDir     string
	Brokers       []string
	Topic         string
	ReloadEnabled bool
	HealthPort    string
	ConsumerGroup string
	InboxDir      string
	MaxAttempts   int
	Retention     time.Duration
}

var AppConfig Configuration

func Init() {
	configuredDir := getEnvString(configDir, "/app/sites-enabled")
	kafkaBrokersList := getStringListFromEnv(kafkaBrokers, "")
	AppConfig = Configuration{
		ConfigDir:     configuredDir,
		Brokers:       kafkaBrokersList,
		Topic:         getEnvString(kafkaTopic, "automation-events"),
		ReloadEnabled: getEnvBool(reloadEnabled, false),
		HealthPort:    getEnvString(healthPort, "8081"),
		ConsumerGroup: getEnvString(consumerGroup, "hai-nginx-config-manager-v1"),
		InboxDir:      getEnvString(inboxDir, filepath.Join(configuredDir, ".hai-event-inbox")),
		MaxAttempts:   getEnvInt(maxAttempts, 5, 1, 100),
		Retention:     time.Duration(getEnvInt(retention, 720, 1, 24*365)) * time.Hour,
	}
}

func getStringListFromEnv(envVarName, defaultValue string) []string {
	value := getEnvString(envVarName, defaultValue)
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
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

func getEnvInt(key string, defaultValue, minimum, maximum int) int {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		log.Printf("Using default value for %s: %d", key, defaultValue)
		return defaultValue
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		log.Printf("Invalid integer for %s: %s. Using default: %d", key, value, defaultValue)
		return defaultValue
	}
	return parsed
}

func (c Configuration) Validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS must contain at least one broker")
	}
	if strings.TrimSpace(c.Topic) == "" {
		return fmt.Errorf("KAFKA_TOPIC is required")
	}
	if strings.TrimSpace(c.ConsumerGroup) == "" {
		return fmt.Errorf("NGINX_CONFIG_MANAGER_GROUP_ID is required")
	}
	if strings.TrimSpace(c.InboxDir) == "" {
		return fmt.Errorf("NGINX_CONFIG_MANAGER_INBOX_DIR is required")
	}
	if c.MaxAttempts < 1 || c.Retention <= 0 {
		return fmt.Errorf("inbox retry and retention settings must be positive")
	}
	return nil
}
