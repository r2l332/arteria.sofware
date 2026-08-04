package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/r2l332/arteria.app/backend/pkg/logging"
	"github.com/r2l332/arteria.app/backend/pkg/metrics"
	"github.com/r2l332/arteria.app/backend/pkg/mllp"
	"github.com/r2l332/arteria.app/backend/pkg/natsutil"
)

func main() {
	log, err := logging.FromEnv("ingestion")
	if err != nil {
		panic("failed to init logger: " + err.Error())
	}
	defer log.Close()

	cpLog := log.WithComponent("comm-point")
	natsLog := log.WithComponent("nats")

	// Configuration from environment
	natsURL := envOrDefault("NATS_URL", nats.DefaultURL)
	ingestionPort := envOrDefault("INGESTION_PORT", "2575")

	log.Info("starting ingestion service", logging.Fields{
		"nats_url": natsURL,
		"port":     ingestionPort,
		"log_level": os.Getenv("LOG_LEVEL"),
	})

	// Connect to NATS JetStream
	natsCfg := natsutil.DefaultConfig()
	natsCfg.URL = natsURL

	nc, js, err := natsutil.Connect(natsCfg)
	if err != nil {
		log.Fatal("failed to connect to NATS", logging.Fields{"error": err.Error()})
	}
	defer nc.Close()
	natsLog.Info("connected", logging.Fields{"url": nc.ConnectedUrl()})

	// Initialize metrics
	m := metrics.New()

	// Register the MLLP input communication point
	cpID := envOrDefault("COMM_POINT_ID", "a1000000-0000-0000-0000-000000000001")
	cpName := envOrDefault("COMM_POINT_NAME", "Default MLLP Input")
	cp := m.ForCP(cpID, cpName, "INPUT")

	// Serve metrics via NATS request-reply
	nc.Subscribe("arteria.metrics.ingestion", func(msg *nats.Msg) {
		snap := m.Snap()
		data, _ := json.Marshal(snap)
		msg.Respond(data)
	})

	// Create the MLLP server
	server := &mllp.Server{
		Addr: ":" + ingestionPort,
		Handler: func(msg []byte) error {
			msgID := uuid.New().String()
			subject := "arteria.ingest.raw"

			m.Received.Add(1)
			m.BytesIn.Add(int64(len(msg)))
			cp.RecordReceive(len(msg))
			cp.Log("INFO", "message received", msgID, "", len(msg))

			cpLog.Trace("received MLLP frame", logging.Fields{
				"message_id": msgID,
				"size_bytes": len(msg),
			})

			cpLog.Debug("publishing to NATS", logging.Fields{
				"message_id": msgID,
				"subject":    subject,
			})

			_, err := js.Publish(subject, msg, nats.MsgId(msgID))
			if err != nil {
				cpLog.Error("publish failed", logging.Fields{
					"message_id": msgID,
					"subject":    subject,
					"error":      err.Error(),
				})
				cp.RecordError()
				cp.Log("ERROR", "publish to NATS failed", msgID, err.Error(), 0)
				return err
			}

			cpLog.Info("message ingested", logging.Fields{
				"message_id": msgID,
				"subject":    subject,
				"size_bytes": len(msg),
			})
			cp.Log("INFO", "published to NATS", msgID, "", len(msg))
			m.Processed.Add(1)
			return nil
		},
	}

	if err := server.Start(); err != nil {
		log.Fatal("failed to start MLLP server", logging.Fields{
			"error": err.Error(),
			"port":  ingestionPort,
		})
	}
	cpLog.Info("MLLP communication point started", logging.Fields{
		"protocol": "MLLP",
		"address":  ":" + ingestionPort,
		"direction": "INPUT",
	})

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("received shutdown signal", logging.Fields{"signal": sig.String()})

	server.Stop()
	nc.Drain()
	log.Info("shutdown complete")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
