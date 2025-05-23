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
	log.Printf("🔧 Creating Kafka producer with servers: %s", bootstrapServers)
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
					log.Printf("❌ Failed to deliver message to %s: %v", *ev.TopicPartition.Topic, ev.TopicPartition.Error)
				} else {
					log.Printf("✅ Message delivered to %s [partition %d] at offset %v",
						*ev.TopicPartition.Topic, ev.TopicPartition.Partition, ev.TopicPartition.Offset)
				}
			}
		}
	}()

	return &Producer{producer: p}, nil
}

// SendMessage sends a message to the specified topic
func (p *Producer) SendMessage(topic string, key string, value interface{}) error {
	log.Printf("📤 Attempting to send message to topic: %s, key: %s", topic, key)

	jsonValue, err := json.Marshal(value)
	if err != nil {
		log.Printf("❌ Failed to marshal message: %v", err)
		return err
	}

	log.Printf("📤 Message content: %s", string(jsonValue))

	err = p.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(key),
		Value:          jsonValue,
	}, nil)

	if err != nil {
		log.Printf("❌ Failed to produce message to %s: %v", topic, err)
		return err
	}

	log.Printf("✅ Message queued for topic: %s", topic)
	return nil
}

// Close closes the producer
func (p *Producer) Close() {
	log.Println("🔒 Closing Kafka producer")
	p.producer.Close()
}
