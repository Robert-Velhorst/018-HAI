package config

import (
	"reflect"
	"testing"
)

func TestBrokerListTrimsAndDropsBlankEntries(t *testing.T) {
	t.Setenv(kafkaBrokers, " kafka:9092, ,backup:9092 ")

	brokers := getStringListFromEnv(kafkaBrokers, "")
	if want := []string{"kafka:9092", "backup:9092"}; !reflect.DeepEqual(brokers, want) {
		t.Fatalf("getStringListFromEnv() = %v, want %v", brokers, want)
	}
}
