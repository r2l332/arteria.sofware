package scyllautil

import (
	"fmt"
	"log"
	"time"

	"github.com/gocql/gocql"
)

type Config struct {
	Hosts       []string
	Keyspace    string
	Consistency gocql.Consistency
	Timeout     time.Duration
	MaxRetries  int
}

func DefaultConfig() Config {
	return Config{
		Hosts:       []string{"127.0.0.1"},
		Keyspace:    "arteria",
		Consistency: gocql.LocalOne,
		Timeout:     5 * time.Second,
		MaxRetries:  3,
	}
}

func Connect(cfg Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = cfg.Consistency
	cluster.Timeout = cfg.Timeout
	cluster.RetryPolicy = &gocql.SimpleRetryPolicy{NumRetries: cfg.MaxRetries}
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())

	var session *gocql.Session
	var err error

	// Retry connection with backoff
	for i := 0; i < 10; i++ {
		session, err = cluster.CreateSession()
		if err == nil {
			log.Printf("[SCYLLA] connected to keyspace %q", cfg.Keyspace)
			return session, nil
		}
		log.Printf("[SCYLLA] connection attempt %d failed: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	return nil, fmt.Errorf("scylla connect after retries: %w", err)
}
