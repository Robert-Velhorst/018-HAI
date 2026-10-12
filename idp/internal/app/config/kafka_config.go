package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	kafkaEnabled    string = "IDP_KAFKA_ENABLED"
	eventBusEnabled string = "HAI_EVENT_BUS_ENABLED"
	loggerTopic     string = "LOGGER_TOPIC"
	mailTopic       string = "MAIL_TOPIC"
	clientID        string = "KAFKA_CLIENT_ID"
	brokersAddr     string = "BROKERS_ADDR"
)

type kafkaConfig struct {
	Enabled     bool
	LoggerTopic string
	MailTopic   string
	ClientID    string
	BrokersAddr []string
}

// newOptionalKafkaConfig keeps the local IDP operational without a broker.
// Kafka is an opt-in event stream: authentication and persistence remain local
// and durable when it is disabled.
func newOptionalKafkaConfig() (*kafkaConfig, error) {
	enabled, err := getEnvBool(kafkaEnabled, true)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	return newKafkaConfig()
}

func newKafkaConfig() (*kafkaConfig, error) {
	enabled, err := getEnvBool(eventBusEnabled, false)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &kafkaConfig{}, nil
	}

	logTopic := strings.TrimSpace(getEnvString(loggerTopic, ""))
	if logTopic == "" {
		return nil, errors.New("Kafka logger topic is required: set " + loggerTopic)
	}
	emailTopic := strings.TrimSpace(getEnvString(mailTopic, ""))
	if emailTopic == "" {
		return nil, errors.New("Kafka mail topic is required: set " + mailTopic)
	}
	brokersValue := strings.TrimSpace(getEnvString(brokersAddr, ""))
	if brokersValue == "" {
		return nil, errors.New("Kafka broker list is required: set " + brokersAddr)
	}
	brokersList := make([]string, 0)
	for _, broker := range strings.Split(brokersValue, ",") {
		if broker = strings.TrimSpace(broker); broker != "" {
			brokersList = append(brokersList, broker)
		}
	}
	if len(brokersList) == 0 {
		return nil, errors.New("Kafka broker list contains no usable addresses: set " + brokersAddr)
	}
	client := strings.TrimSpace(getEnvString(clientID, "IDP-AUTOMATIONS-HUB"))
	if client == "" {
		return nil, fmt.Errorf("Kafka client ID is required: set %s", clientID)
	}

	return &kafkaConfig{
		Enabled:     true,
		LoggerTopic: logTopic,
		MailTopic:   emailTopic,
		ClientID:    client,
		BrokersAddr: brokersList,
	}, nil
}
