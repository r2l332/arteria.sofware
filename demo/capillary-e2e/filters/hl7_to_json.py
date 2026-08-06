"""
Arteria Python Filter: HL7 to JSON Transform

Reads a MessageEnvelope JSON from stdin, parses the HL7 rawPayload,
converts it to a structured JSON representation, and outputs the
transformed envelope on stdout.
"""
import sys
import json


def parse_hl7_to_json(raw_hl7):
    """Parse raw HL7 message into structured JSON."""
    segments = raw_hl7.replace("\n", "\r").split("\r")
    segments = [s for s in segments if s.strip()]

    result = {"segments": []}

    for seg_str in segments:
        fields = seg_str.split("|")
        seg_type = fields[0] if fields else ""

        segment = {"type": seg_type, "fields": fields[1:]}

        # Extract well-known fields
        if seg_type == "MSH":
            segment["sending_application"] = fields[2] if len(fields) > 2 else ""
            segment["sending_facility"] = fields[3] if len(fields) > 3 else ""
            segment["receiving_application"] = fields[4] if len(fields) > 4 else ""
            segment["receiving_facility"] = fields[5] if len(fields) > 5 else ""
            segment["timestamp"] = fields[6] if len(fields) > 6 else ""
            segment["message_type"] = fields[8] if len(fields) > 8 else ""
            segment["message_id"] = fields[9] if len(fields) > 9 else ""
            segment["version"] = fields[11] if len(fields) > 11 else ""
        elif seg_type == "PID":
            segment["patient_id"] = fields[3] if len(fields) > 3 else ""
            # Name is in format Last^First^Middle
            name_field = fields[5] if len(fields) > 5 else ""
            name_parts = name_field.split("^")
            segment["patient_name"] = {
                "family": name_parts[0] if len(name_parts) > 0 else "",
                "given": name_parts[1] if len(name_parts) > 1 else "",
                "middle": name_parts[2] if len(name_parts) > 2 else "",
            }
            segment["date_of_birth"] = fields[7] if len(fields) > 7 else ""
            segment["sex"] = fields[8] if len(fields) > 8 else ""
        elif seg_type == "PV1":
            segment["patient_class"] = fields[2] if len(fields) > 2 else ""
            segment["assigned_location"] = fields[3] if len(fields) > 3 else ""

        result["segments"].append(segment)

    return result


def main():
    envelope_raw = sys.stdin.read()
    envelope = json.loads(envelope_raw)

    raw_hl7 = envelope.get("rawPayload", "")
    parsed = parse_hl7_to_json(raw_hl7)

    # Replace rawPayload with JSON representation
    envelope["rawPayload"] = json.dumps(parsed)
    envelope["properties"] = envelope.get("properties") or {}
    envelope["properties"]["content_type"] = "application/json"
    envelope["properties"]["transform"] = "hl7_to_json"

    json.dump(envelope, sys.stdout)


if __name__ == "__main__":
    main()
