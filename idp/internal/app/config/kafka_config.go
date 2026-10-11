package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
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

func newKafkaConfig() (*kafkaConfig, error) {
	if !getEnvBool(eventBusEnabled, false) {
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
