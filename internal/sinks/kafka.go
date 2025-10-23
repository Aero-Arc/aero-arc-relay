package sinks

import (
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
)

// KafkaSink implements Sink interface for Apache Kafka
type KafkaSink struct {
	producer sarama.SyncProducer
	topic    string
}

// NewKafkaSink creates a new Kafka sink
func NewKafkaSink(cfg *config.KafkaConfig) (*KafkaSink, error) {
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 3
	config.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(cfg.Brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	return &KafkaSink{
		producer: producer,
		topic:    cfg.Topic,
	}, nil
}

// Write sends telemetry data to Kafka
func (k *KafkaSink) Write(data *telemetry.Data) error {
	// Serialize data to JSON
	jsonData, err := data.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize data: %w", err)
	}

	// Create Kafka message
	message := &sarama.ProducerMessage{
		Topic:     k.topic,
		Key:       sarama.StringEncoder(data.Source),
		Value:     sarama.ByteEncoder(jsonData),
		Timestamp: time.Now(),
	}

	// Send message
	partition, offset, err := k.producer.SendMessage(message)
	if err != nil {
		return fmt.Errorf("failed to send message to Kafka: %w", err)
	}

	// Log successful send (optional)
	_ = partition
	_ = offset

	return nil
}

// Close closes the Kafka sink
func (k *KafkaSink) Close() error {
	return k.producer.Close()
}
