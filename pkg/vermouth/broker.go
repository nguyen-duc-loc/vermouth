package vermouth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// DLQSuffix names the dead letter topic that sits beside every topic. Redpanda
// has no native dead letter path, so this suffix plus the park code below is
// the whole of it (STK-13).
const DLQSuffix = ".dlq"

const (
	// producerRecordRetries bounds how often the client itself retries a
	// record before it hands the error back, which is what turns a broker
	// hiccup into a relay retry rather than a lost event.
	producerRecordRetries = 5
	// topicsPerName counts the topics every publish topic brings with it:
	// itself and its dead letter topic (STK-13).
	topicsPerName = 2
	// ensureTopicsTimeout bounds startup: a broker that cannot answer in
	// half a minute is a broker the service should fail loudly against.
	ensureTopicsTimeout = 30 * time.Second
)

// DLQTopic is the dead letter topic for a topic.
func DLQTopic(topic string) string { return topic + DLQSuffix }

// NewProducer opens a producer client. It is a plain client with no consumer
// group, used by the relay and by the dead letter park.
func NewProducer(seeds []string, service string) (*kgo.Client, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ClientID(service+".producer"),
		kgo.ProducerLinger(0),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordRetries(producerRecordRetries),
	)
	if err != nil {
		return nil, fmt.Errorf("open producer: %w", err)
	}
	return client, nil
}

// EnsureTopics creates a service's own publish topic and that topic's dead
// letter topic, and is called from main before the relay or any consumer
// starts (STK-16).
//
// Redpanda runs with automatic topic creation off, so a forgotten call fails
// loudly at startup rather than quietly producing a topic with default
// retention. Partition count is fixed here and must never change afterwards,
// because changing it remaps keys to partitions and breaks per key ordering
// (STK-17, INV-6). Retention is unlimited, so a projection can be rebuilt from
// the beginning (INV-11).
func EnsureTopics(ctx context.Context, seeds []string, partitions int32, topics ...string) error {
	if len(topics) == 0 {
		return nil
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(seeds...), kgo.ClientID("vermouth.admin"))
	if err != nil {
		return fmt.Errorf("ensure topics: open admin client: %w", err)
	}
	defer client.Close()

	admin := kadm.NewClient(client)
	retention := "-1"
	wanted := make([]string, 0, len(topics)*topicsPerName)
	for _, topic := range topics {
		wanted = append(wanted, topic, DLQTopic(topic))
	}

	ctx, cancel := context.WithTimeout(ctx, ensureTopicsTimeout)
	defer cancel()

	responses, err := admin.CreateTopics(ctx, partitions, 1, map[string]*string{
		"retention.ms": &retention,
	}, wanted...)
	if err != nil {
		return fmt.Errorf("ensure topics: %w", err)
	}
	for _, response := range responses {
		if response.Err != nil && !isTopicExists(response.Err) {
			return fmt.Errorf("ensure topic %s: %w", response.Topic, response.Err)
		}
	}
	return nil
}

func isTopicExists(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return message == "TOPIC_ALREADY_EXISTS" ||
		strings.Contains(strings.ToLower(message), "already exists")
}
