package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/r2l332/arteria.app/backend/pkg/tunnel"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	brokerAddr := envOr("BROKER_ADDR", "localhost:9443")
	configDir := envOr("AGENT_CONFIG_DIR", "/etc/arteria-agent")

	agent := tunnel.NewAgent(tunnel.AgentConfig{
		BrokerAddr: brokerAddr,
		ConfigDir:  configDir,
	})

	switch os.Args[1] {
	case "enroll":
		token := ""
		if len(os.Args) > 2 {
			token = os.Args[2]
		}
		if token == "" {
			token = os.Getenv("ENROLL_TOKEN")
		}
		if token == "" {
			fmt.Println("Error: enrollment token required")
			fmt.Println("  arteria-agent enroll <token>")
			fmt.Println("  or set ENROLL_TOKEN environment variable")
			os.Exit(1)
		}

		fmt.Printf("Enrolling with broker at %s...\n", brokerAddr)
		if err := agent.Enroll(token); err != nil {
			log.Fatalf("Enrollment failed: %v", err)
		}
		fmt.Println("Enrollment successful. Run 'arteria-agent connect' to start the tunnel.")

	case "connect":
		fmt.Printf("Connecting to broker at %s...\n", brokerAddr)
		go func() {
			if err := agent.Connect(); err != nil {
				log.Fatalf("Connection failed: %v", err)
			}
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\nShutting down agent...")
		agent.Stop()

	case "status":
		configDir := envOr("AGENT_CONFIG_DIR", "/etc/arteria-agent")
		nodeID, err := os.ReadFile(configDir + "/node-id")
		if err != nil {
			fmt.Println("Not enrolled. Run 'arteria-agent enroll <token>' first.")
			os.Exit(1)
		}
		fmt.Printf("Node ID:     %s\n", string(nodeID))
		fmt.Printf("Broker:      %s\n", brokerAddr)
		fmt.Printf("Config dir:  %s\n", configDir)

		if _, err := os.Stat(configDir + "/node.pem"); err == nil {
			fmt.Println("Certificate: present")
		} else {
			fmt.Println("Certificate: missing")
		}

	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Arteria Tunnel Agent")
	fmt.Println()
	fmt.Println("Usage: arteria-agent <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  enroll <token>    Enroll this agent with the Arteria broker")
	fmt.Println("  connect           Connect to the broker and start tunneling")
	fmt.Println("  status            Show agent enrollment status")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  BROKER_ADDR       Broker address (default: localhost:9443)")
	fmt.Println("  AGENT_CONFIG_DIR  Config/cert directory (default: /etc/arteria-agent)")
	fmt.Println("  ENROLL_TOKEN      Enrollment token (alternative to CLI arg)")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
