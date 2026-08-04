package natsutil

import (
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

type Config struct {
	URL            string
	MaxReconnects  int
	ReconnectWait  time.Duration
	ConnectTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		URL:            nats.DefaultURL,
		MaxReconnects:  10,
		ReconnectWait:  2 * time.Second,
		ConnectTimeout: 10 * time.Second,
	}
}

func Connect(cfg Config) (*nats.Conn, nats.JetStreamContext, error) {
	opts := []nats.Option{
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.Timeout(cfg.ConnectTimeout),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("[NATS] disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("[NATS] reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Println("[NATS] connection closed")
		}),
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("nats jetstream: %w", err)
	}

	// Ensure the arteria stream exists
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "ARTERIA",
		Subjects: []string{"arteria.ingest.>", "arteria.route.>", "arteria.dlq.>", "arteria.status.>"},
		Storage:  nats.MemoryStorage,
	})
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("nats add stream: %w", err)
	}

	log.Printf("[NATS] connected to %s", nc.ConnectedUrl())
	return nc, js, nil
}
