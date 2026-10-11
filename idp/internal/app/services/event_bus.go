package services

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/services/iservice"
	"log"
)

// DefaultLogger keeps authentication available when the optional event bus is
// disabled. Local logs remain visible through the container runtime.
func DefaultLogger() (iservice.Logger, error) {
	if config.KafkaConfig == nil || !config.KafkaConfig.Enabled {
		return localLogger{}, nil
	}
	return NewKafkaLogger(config.KafkaConfig.BrokersAddr, config.KafkaConfig.LoggerTopic)
}

// DefaultMessageSender makes account-event delivery optional. Account creation
// and login must not depend on an idle desktop broker.
func DefaultMessageSender() (iservice.MessageSender, error) {
	if config.KafkaConfig == nil || !config.KafkaConfig.Enabled {
		return discardedMessageSender{}, nil
	}
	return NewKafkaMessageSender()
}

type localLogger struct{}

func (localLogger) Info(message string, args ...interface{})  { log.Printf("INFO: "+message, args...) }
func (localLogger) Error(message string, args ...interface{}) { log.Printf("ERROR: "+message, args...) }
func (localLogger) Warn(message string, args ...interface{})  { log.Printf("WARN: "+message, args...) }
func (localLogger) Debug(message string, args ...interface{}) { log.Printf("DEBUG: "+message, args...) }

type discardedMessageSender struct{}

func (discardedMessageSender) Send(topic string, message interface{}) error {
	log.Printf("Event bus disabled; not publishing account event for topic %q", topic)
	return nil
}
