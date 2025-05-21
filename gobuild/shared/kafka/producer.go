package kafka

import (
	"encoding/json"
	"log"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Producer wraps the Kafka producer
type Producer struct {
	producer *kafka.Producer
}

// NewProducer creates a new Kafka producer
func NewProducer(bootstrapServers string) (*Producer, error) {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": bootstrapServers,
	})
	if err != nil {
		return nil, err
	}

	// Start a goroutine to handle delivery reports
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					log.Printf("Failed to deliver message: %v\n", ev.TopicPartition.Error)
				}
			}
		}
	}()

	return &Producer{producer: p}, nil
}

// SendMessage sends a message to the specified topic
func (p *Producer) SendMessage(topic string, key string, value interface{}) error {
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return p.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(key),
		Value:          jsonValue,
	}, nil)
}

// Close closes the producer
func (p *Producer) Close() {
	p.producer.Close()
}
