package kafka

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type MessageHandler func(key []byte, value []byte) error

type Consumer struct {
	consumer *kafka.Consumer
}

func NewConsumer(bootstrapServers, groupID string) (*Consumer, error) {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":  bootstrapServers,
		"group.id":           groupID,
		"auto.offset.reset":  "earliest",
		"enable.auto.commit": "true",
	})
	if err != nil {
		return nil, err
	}

	return &Consumer{consumer: c}, nil
}

func (c *Consumer) Subscribe(topics []string) error {
	maxRetries := 15
	retryDelay := time.Second * 2

	var err error
	for i := 0; i < maxRetries; i++ {
		err = c.consumer.SubscribeTopics(topics, nil)
		if err == nil {
			log.Printf("Successfully subscribed to topics: %v", topics)
			return nil
		}

		// Check if it's the "unknown topic" error and retry
		if i < maxRetries-1 {
			log.Printf("Failed to subscribe to topics: %v, retrying in %v... (attempt %d/%d)",
				err, retryDelay, i+1, maxRetries)
			time.Sleep(retryDelay)
			// Increase retry delay for next attempt (exponential backoff)
			retryDelay = time.Duration(float64(retryDelay) * 1.5)
		}
	}

	return err
}

func (c *Consumer) ConsumeMessages(handler MessageHandler) {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	run := true
	for run {
		select {
		case sig := <-sigchan:
			log.Printf("Caught signal %v: terminating\n", sig)
			run = false
		default:
			ev := c.consumer.Poll(100)
			if ev == nil {
				continue
			}

			switch e := ev.(type) {
			case *kafka.Message:
				err := handler(e.Key, e.Value)
				if err != nil {
					log.Printf("Error processing message: %v\n", err)
				}
			case kafka.Error:
				// Don't stop on topic subscription errors, as they may be resolved later
				isTopicError := e.Code() == kafka.ErrUnknownTopicOrPart ||
					e.Code() == kafka.ErrBadMsg ||
					e.Code() == kafka.ErrTimedOut
				if isTopicError {
					log.Printf("Kafka error: %v\n", e)
				} else if e.Code() == kafka.ErrAllBrokersDown {
					log.Printf("Fatal Kafka error: %v\n", e)
					run = false
				}
			}
		}
	}
}

// UnmarshalMessage unmarshals a Kafka message value into the provided struct
func UnmarshalMessage(value []byte, v interface{}) error {
	return json.Unmarshal(value, v)
}

func (c *Consumer) Close() {
	c.consumer.Close()
}
