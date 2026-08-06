"""
Arteria Python Filter: Extract Patient Name and ID

Reads the JSON-transformed MessageEnvelope from stdin, extracts
the patient name and ID from the parsed HL7 data, and outputs a
compact JSON summary suitable for sending back to the VM.
"""
import sys
import json


def main():
    envelope_raw = sys.stdin.read()
    envelope = json.loads(envelope_raw)

    raw_payload = envelope.get("rawPayload", "")
    properties = envelope.get("properties") or {}

    # Parse the JSON payload (output of previous filter)
    try:
        parsed = json.loads(raw_payload)
    except (json.JSONDecodeError, TypeError):
        parsed = {"segments": []}

    # Extract patient info from PID segment
    patient_id = ""
    patient_name = ""
    date_of_birth = ""
    sex = ""

    for seg in parsed.get("segments", []):
        if seg.get("type") == "PID":
            patient_id = seg.get("patient_id", "")
            name_obj = seg.get("patient_name", {})
            if isinstance(name_obj, dict):
                parts = [name_obj.get("given", ""), name_obj.get("family", "")]
                patient_name = " ".join(p for p in parts if p)
            date_of_birth = seg.get("date_of_birth", "")
            sex = seg.get("sex", "")
            break

    # Extract message metadata from MSH
    message_id = envelope.get("messageId", "")
    message_type = envelope.get("messageType", "")
    trigger_event = envelope.get("triggerEvent", "")
    sending_facility = envelope.get("sendingFacility", "")

    # Build compact output for the VM
    output = {
        "messageId": message_id,
        "messageType": f"{message_type}^{trigger_event}",
        "sendingFacility": sending_facility,
        "patientId": patient_id,
        "rawPayload": json.dumps({
            "patient_id": patient_id,
            "patient_name": patient_name,
            "date_of_birth": date_of_birth,
            "sex": sex,
            "message_id": message_id,
            "message_type": f"{message_type}^{trigger_event}",
            "facility": sending_facility,
            "processed_at": properties.get("transform", "extracted"),
        }),
        "triggerEvent": trigger_event,
        "properties": {
            "patient_id": patient_id,
            "patient_name": patient_name,
            "content_type": "application/json",
            "transform": "patient_extract",
        },
    }

    json.dump(output, sys.stdout)


if __name__ == "__main__":
    main()
