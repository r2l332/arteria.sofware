package hl7

import (
	"strings"
)

// Message represents a parsed HL7v2 message with extracted metadata.
type Message struct {
	MessageType     string // MSH-9.1 (e.g., "ADT")
	TriggerEvent    string // MSH-9.2 (e.g., "A01")
	SendingFacility string // MSH-4
	PatientID       string // PID-3
	RawPayload      string
}

// Parse extracts key metadata from an HL7v2 message.
// It uses a lightweight custom scanner for speed rather than a full library.
func Parse(raw []byte) Message {
	payload := string(raw)
	segments := strings.Split(payload, "\r")

	msg := Message{
		RawPayload: payload,
	}

	for _, seg := range segments {
		fields := strings.Split(seg, "|")
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "MSH":
			msg.parseMSH(fields)
		case "PID":
			msg.parsePID(fields)
		}
	}

	return msg
}

func (m *Message) parseMSH(fields []string) {
	// MSH fields are offset by 1 because MSH-1 is the field separator itself.
	// fields[0] = "MSH"
	// fields[1] = encoding chars (^~\&)
	// fields[2] = MSH-3 Sending Application
	// fields[3] = MSH-4 Sending Facility
	// fields[8] = MSH-9 Message Type
	if len(fields) > 3 {
		m.SendingFacility = fields[3]
	}
	if len(fields) > 8 {
		msgType := fields[8]
		components := strings.Split(msgType, "^")
		if len(components) > 0 {
			m.MessageType = components[0]
		}
		if len(components) > 1 {
			m.TriggerEvent = components[1]
		}
	}
}

func (m *Message) parsePID(fields []string) {
	// PID-3 is Patient Identifier List (field index 3)
	// fields[0] = "PID"
	// fields[3] = PID-3 Patient Identifier
	if len(fields) > 3 {
		// PID-3 may have components separated by ^; take the first (ID number)
		components := strings.Split(fields[3], "^")
		m.PatientID = components[0]
	}
}
