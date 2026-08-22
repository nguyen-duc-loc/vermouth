package vermouth

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// envTokenPublicKeys holds the verifying half of the signing key pair, and is
// read by every service including the gateway (STK-14).
const envTokenPublicKeys = "TOKEN_PUBLIC_KEYS"

// The defaults every service starts from when the environment says nothing.
// They are named rather than inlined so one place answers "how often does the
// relay wake, and how hard does a consumer try".
const (
	defaultTopicPartitions = 3
	defaultRelayInterval   = 750 * time.Millisecond
	defaultRetryMax        = 5
	defaultRetryBaseDelay  = 200 * time.Millisecond
)

// configNeeds says which parts of the environment a service actually has. It
// is a struct rather than two boolean arguments so a call site reads as what it
// needs rather than as true, true.
type configNeeds struct {
	database bool
	broker   bool
}

// Config is the typed shape of a service's environment (STK-8). It is parsed
// once at startup; a missing or unparseable required variable stops startup
// with a named error rather than defaulting silently.
type Config struct {
	// Service is the name that appears on every log line (STK-9).
	Service string
	// HTTPAddr is the address this service listens on.
	HTTPAddr string
	// DatabaseURL is this service's own database, and nobody else's (STK-5).
	DatabaseURL string
	// BrokerSeeds are the Redpanda bootstrap addresses.
	BrokerSeeds []string
	// PublishTopic is the one topic this service publishes to (STK-11).
	// Empty means the service publishes nothing, so it creates no topic.
	PublishTopic string
	// TopicPartitions is fixed at topic creation and must never change (STK-17).
	TopicPartitions int32
	// RelayInterval is how often the relay wakes to drain the outbox (STK-18).
	RelayInterval time.Duration
	// RetryMax and RetryBaseDelay bound consumer retries before a message is
	// parked in the dead letter topic (STK-13). Configuration, not literals.
	RetryMax       int
	RetryBaseDelay time.Duration
	// PublicKeys verifies tokens locally, keyed by kid (STK-14).
	PublicKeys map[string]ed25519.PublicKey
	// LogLevel is debug, info, warn or error.
	LogLevel string
}

// MissingEnvError names the variable that stopped startup.
type MissingEnvError struct {
	Name   string
	Reason string
}

func (e *MissingEnvError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("configuration: %s is required and not set", e.Name)
	}
	return fmt.Sprintf("configuration: %s is invalid: %s", e.Name, e.Reason)
}

// LoadConfig reads the environment into a Config for a service that owns a
// database and talks to the broker. publishTopic is passed rather than read
// from the environment because it is a property of the service's code, not of
// the deployment (STK-11); pass an empty string for a service that publishes
// nothing.
func LoadConfig(service, publishTopic string) (Config, error) {
	return loadConfig(service, publishTopic, configNeeds{database: true, broker: true})
}

// LoadGatewayConfig reads the environment for the gateway, which owns no
// database and neither publishes nor consumes (spec 0001: the gateway holds no
// data). It still holds the public keys, because it verifies every token.
func LoadGatewayConfig(service string) (Config, error) {
	return loadConfig(service, "", configNeeds{})
}

func loadConfig(service, publishTopic string, needs configNeeds) (Config, error) {
	cfg := Config{
		Service:      service,
		PublishTopic: publishTopic,
		LogLevel:     envOr("LOG_LEVEL", "info"),
	}

	upper := strings.ToUpper(strings.ReplaceAll(service, "-", "_"))

	addr, err := requireEnv(upper + "_HTTP_ADDR")
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPAddr = addr

	if needs.database {
		dbURL, err := requireEnv(upper + "_DATABASE_URL")
		if err != nil {
			return Config{}, err
		}
		cfg.DatabaseURL = dbURL
	}

	if needs.broker {
		seeds, err := requireEnv("BROKER_SEEDS")
		if err != nil {
			return Config{}, err
		}
		cfg.BrokerSeeds = splitAndTrim(seeds)
	}

	partitions, err := envInt("TOPIC_PARTITIONS", defaultTopicPartitions)
	if err != nil {
		return Config{}, err
	}
	if partitions < 1 || partitions > math.MaxInt32 {
		return Config{}, &MissingEnvError{Name: "TOPIC_PARTITIONS", Reason: "must be a positive partition count"}
	}
	cfg.TopicPartitions = int32(partitions)

	cfg.RelayInterval, err = envDuration("RELAY_INTERVAL", defaultRelayInterval)
	if err != nil {
		return Config{}, err
	}
	cfg.RetryMax, err = envInt("CONSUMER_RETRY_MAX", defaultRetryMax)
	if err != nil {
		return Config{}, err
	}
	cfg.RetryBaseDelay, err = envDuration("CONSUMER_RETRY_BASE_DELAY", defaultRetryBaseDelay)
	if err != nil {
		return Config{}, err
	}

	keys, err := ParsePublicKeys(os.Getenv(envTokenPublicKeys))
	if err != nil {
		return Config{}, err
	}
	cfg.PublicKeys = keys

	return cfg, nil
}

// ParsePublicKeys reads TOKEN_PUBLIC_KEYS, a comma separated list of
// kid:base64 raw Ed25519 public keys. Several entries are allowed on purpose,
// so a new key can be added before anything switches to it (spec 0001,
// identity propagation).
func ParsePublicKeys(raw string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)
	if strings.TrimSpace(raw) == "" {
		return keys, nil
	}
	for _, entry := range splitAndTrim(raw) {
		kid, encoded, found := strings.Cut(entry, ":")
		if !found || kid == "" || encoded == "" {
			return nil, &MissingEnvError{Name: envTokenPublicKeys, Reason: "each entry must be kid:base64"}
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, &MissingEnvError{Name: envTokenPublicKeys, Reason: "entry " + kid + " is not valid base64"}
		}
		if len(decoded) != ed25519.PublicKeySize {
			return nil, &MissingEnvError{Name: envTokenPublicKeys, Reason: "entry " + kid + " is not an Ed25519 public key"}
		}
		keys[kid] = ed25519.PublicKey(decoded)
	}
	return keys, nil
}

func requireEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", &MissingEnvError{Name: name}
	}
	return value, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &MissingEnvError{Name: name, Reason: "not a whole number"}
	}
	return value, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, &MissingEnvError{Name: name, Reason: "not a duration such as 750ms or 2s"}
	}
	return value, nil
}

func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
