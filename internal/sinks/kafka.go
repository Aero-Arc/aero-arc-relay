package sinks

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/pkg/telemetry"
	"gopkg.in/confluentinc/confluent-kafka-go.v1/kafka"
)

// KafkaSink implements Sink interface for Apache Kafka
type KafkaSink struct {
	producer *kafka.Producer
	topic    string
	*BaseAsyncSink
}

// NewKafkaSink creates a new Kafka sink
func NewKafkaSink(cfg *config.KafkaConfig) (*KafkaSink, error) {
	// Convert brokers slice to comma-separated string
	bootstrapServers := strings.Join(cfg.Brokers, ",")

	// Create producer configuration
	configMap := kafka.ConfigMap{
		"bootstrap.servers": bootstrapServers,
		"acks":              "all",    // Wait for all replicas to acknowledge
		"retries":           3,        // Retry up to 3 times
		"retry.backoff.ms":  100,      // Wait 100ms between retries
		"compression.type":  "snappy", // Use snappy compression
		"linger.ms":         10,       // Wait up to 10ms to batch messages
		"batch.size":        16384,    // 16KB batch size
	}

	producer, err := kafka.NewProducer(&configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	log.Printf("Kafka producer created successfully")
	sink := &KafkaSink{
		producer: producer,
		topic:    cfg.Topic,
	}
	sink.BaseAsyncSink = NewBaseAsyncSink(cfg.QueueSize, cfg.BackpressurePolicy, "kafka", sink.handleMessage)

	log.Printf("Kafka sink created successfully")

	return sink, nil
}

// WriteMessage sends telemetry message to Kafka
func (k *KafkaSink) WriteMessage(msg telemetry.TelemetryEnvelope) error {
	err := k.BaseAsyncSink.Enqueue(msg)
	if err != nil {
		return fmt.Errorf("failed to enqueue message: %w", err)
	}
	return nil
}

func (k *KafkaSink) handleMessage(msg telemetry.TelemetryEnvelope) error {
	// Serialize message to JSON
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Create Kafka message
	// Note: Timestamp is set by Kafka broker automatically
	// If you need to preserve the original timestamp, consider using headers
	message := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &k.topic,
			Partition: kafka.PartitionAny,
		},
		Key:   []byte(msg.Source),
		Value: jsonData,
		Headers: []kafka.Header{
			{
				Key:   "original_timestamp",
				Value: []byte(msg.TimestampRelay.Format(time.RFC3339Nano)),
			},
		},
	}

	// Send message asynchronously
	deliveryChan := make(chan kafka.Event, 1)
	err = k.producer.Produce(message, deliveryChan)
	if err != nil {
		return fmt.Errorf("failed to produce message to Kafka: %w", err)
	}

	// Wait for delivery report with timeout
	select {
	case e := <-deliveryChan:
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				return fmt.Errorf("delivery failed: %w", ev.TopicPartition.Error)
			}
			// Message delivered successfully
		case kafka.Error:
			return fmt.Errorf("producer error: %w", ev)
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for message delivery")
	}

	return nil
}

// Close closes the Kafka sink
func (k *KafkaSink) Close() error {
	k.BaseAsyncSink.Close()

	// Flush any remaining messages before closing
	k.producer.Flush(15 * 1000) // Wait up to 15 seconds
	k.producer.Close()
	return nil
}
