package mllp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

const (
	StartBlock = 0x0B // VT (vertical tab)
	EndBlock   = 0x1C // FS (file separator)
	CarriageRt = 0x0D // CR
)

// Handler is called with the raw HL7 message bytes (without MLLP framing).
type Handler func(msg []byte) error

// Server listens for MLLP connections and dispatches HL7 messages to the handler.
type Server struct {
	Addr    string
	Handler Handler

	listener net.Listener
	wg       sync.WaitGroup
	quit     chan struct{}
}

// Start begins listening for MLLP connections.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("mllp listen: %w", err)
	}
	s.listener = ln
	s.quit = make(chan struct{})

	log.Printf("[MLLP] listening on %s", s.Addr)

	go s.acceptLoop()
	return nil
}

// Stop gracefully shuts down the MLLP server.
func (s *Server) Stop() {
	close(s.quit)
	s.listener.Close()
	s.wg.Wait()
	log.Println("[MLLP] server stopped")
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				log.Printf("[MLLP] accept error: %v", err)
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		select {
		case <-s.quit:
			return
		default:
		}

		msg, err := readMLLPFrame(reader)
		if err != nil {
			if err != io.EOF {
				log.Printf("[MLLP] read error from %s: %v", conn.RemoteAddr(), err)
			}
			return
		}

		if len(msg) == 0 {
			continue
		}

		if err := s.Handler(msg); err != nil {
			log.Printf("[MLLP] handler error: %v", err)
		}
	}
}

// readMLLPFrame reads a single MLLP-framed message: <VT>...data...<FS><CR>
func readMLLPFrame(r *bufio.Reader) ([]byte, error) {
	// Wait for start block
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == StartBlock {
			break
		}
	}

	// Read until end block
	var buf bytes.Buffer
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == EndBlock {
			// Consume trailing CR if present
			next, err := r.Peek(1)
			if err == nil && len(next) > 0 && next[0] == CarriageRt {
				r.ReadByte()
			}
			break
		}
		buf.WriteByte(b)
	}

	return buf.Bytes(), nil
}
