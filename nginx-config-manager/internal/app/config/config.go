package config

import (
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	configDir      string = "CONFIG_DIR"
	kafkaBrokers   string = "KAFKA_BROKERS"
	kafkaTopic     string = "KAFKA_TOPIC"
	reloadEnabled  string = "NGINX_RELOAD_ENABLED"
	consumerGroup  string = "NGINX_CONFIG_MANAGER_GROUP_ID"
	inboxDir       string = "NGINX_CONFIG_MANAGER_INBOX_DIR"
	maxAttempts    string = "NGINX_CONFIG_MANAGER_MAX_ATTEMPTS"
	inboxRetention string = "NGINX_CONFIG_MANAGER_INBOX_RETENTION"
	healthPort     string = "NGINX_CONFIG_MANAGER_HEALTH_PORT"
)

type Configuration struct {
	ConfigDir     string
	Brokers       []string
	Topic         string
	ReloadEnabled bool
	ConsumerGroup string
	InboxDir      string
	MaxAttempts   int
	Retention     time.Duration
	HealthPort    string
}

var AppConfig Configuration

func Init() {
	kafkaBrokersList := getStringListFromEnv(kafkaBrokers, "kafka1:9092,kafka2:9093,kafka3:9094")
	configPath := getEnvString(configDir, "/app/sites-enabled")
	inboxPath := strings.TrimSpace(os.Getenv(inboxDir))
	if inboxPath == "" {
		inboxPath = filepath.Join(configPath, ".hai-event-inbox")
	}
	AppConfig = Configuration{
		ConfigDir:     configPath,
		Brokers:       kafkaBrokersList,
		Topic:         getEnvString(kafkaTopic, "automation-events"),
		ReloadEnabled: getEnvBool(reloadEnabled, false),
		ConsumerGroup: getEnvString(consumerGroup, "hai-nginx-config-manager"),
		InboxDir:      inboxPath,
		MaxAttempts:   getEnvInt(maxAttempts, 5),
		Retention:     getEnvDuration(inboxRetention, 720*time.Hour),
		HealthPort:    getEnvString(healthPort, "8080"),
	}
}

func (c Configuration) Validate() error {
	if strings.TrimSpace(c.ConfigDir) == "" || len(c.Brokers) == 0 || strings.TrimSpace(c.Topic) == "" ||
		strings.TrimSpace(c.ConsumerGroup) == "" || strings.TrimSpace(c.InboxDir) == "" {
		return errors.New("nginx config manager requires config directory, Kafka brokers/topic/group, and persistent inbox directory")
	}
	for _, broker := range c.Brokers {
		host, port, err := net.SplitHostPort(strings.TrimSpace(broker))
		if err != nil || strings.TrimSpace(host) == "" {
			return errors.New("KAFKA_BROKERS must contain host:port entries")
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return errors.New("KAFKA_BROKERS contains an invalid port")
		}
	}
	if c.MaxAttempts < 1 || c.Retention <= 0 {
		return errors.New("nginx config manager inbox retry count and retention must be positive")
	}
	port, err := strconv.Atoi(strings.TrimSpace(c.HealthPort))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("NGINX_CONFIG_MANAGER_HEALTH_PORT must be between 1 and 65535")
	}
	if c.ReloadEnabled {
		return errors.New("NGINX_RELOAD_ENABLED=true is unsupported without an approved gateway reload capability")
	}
	return nil
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
		return strings.TrimSpace(value)
	}
	log.Printf("Using default value for %s: %s", key, defaultValue)
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
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
