package kafka

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	return c.consumer.SubscribeTopics(topics, nil)
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
				log.Printf("Error: %v\n", e)
				if e.Code() == kafka.ErrAllBrokersDown {
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
