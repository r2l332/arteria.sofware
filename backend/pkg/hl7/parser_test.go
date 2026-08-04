package hl7

import (
	"testing"
)

func TestParse_ADT_A01(t *testing.T) {
	raw := []byte("MSH|^~\\&|SRC|HOSP_A|DST|FAC|202608040800||ADT^A01|12345|P|2.3\rPID|||PAT001||Doe^John\r")

	msg := Parse(raw)

	if msg.MessageType != "ADT" {
		t.Errorf("expected MessageType=ADT, got %q", msg.MessageType)
	}
	if msg.TriggerEvent != "A01" {
		t.Errorf("expected TriggerEvent=A01, got %q", msg.TriggerEvent)
	}
	if msg.SendingFacility != "HOSP_A" {
		t.Errorf("expected SendingFacility=HOSP_A, got %q", msg.SendingFacility)
	}
	if msg.PatientID != "PAT001" {
		t.Errorf("expected PatientID=PAT001, got %q", msg.PatientID)
	}
}

func TestParse_ORM_O01(t *testing.T) {
	raw := []byte("MSH|^~\\&|LAB|LAB_FAC|HIS|HIS_FAC|202608040900||ORM^O01|99999|P|2.3\rPID|||MRN123^^^^^MR||Smith^Jane\r")

	msg := Parse(raw)

	if msg.MessageType != "ORM" {
		t.Errorf("expected MessageType=ORM, got %q", msg.MessageType)
	}
	if msg.TriggerEvent != "O01" {
		t.Errorf("expected TriggerEvent=O01, got %q", msg.TriggerEvent)
	}
	if msg.SendingFacility != "LAB_FAC" {
		t.Errorf("expected SendingFacility=LAB_FAC, got %q", msg.SendingFacility)
	}
	if msg.PatientID != "MRN123" {
		t.Errorf("expected PatientID=MRN123, got %q", msg.PatientID)
	}
}
