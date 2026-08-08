package auth

import "strings"

// PHI field positions in HL7 PID segment (0-indexed after split by |)
// PID|SET_ID|EXTERNAL_ID|PATIENT_ID|ALT_ID|PATIENT_NAME|MOTHER_MAIDEN|DOB|SEX|ALIAS|RACE|ADDRESS|...
var phiFields = map[string][]int{
	"PID": {3, 5, 7, 9, 11, 13, 14, 19}, // MRN, Name, DOB, Alias, Address, Phone, Business Phone, SSN
	"NK1": {2, 4, 5, 6},                  // Name, Address, Phone, Business Phone
	"GT1": {3, 5, 6, 12},                 // Name, Address, Phone, SSN
}

// MaskHL7Payload masks PHI fields in raw HL7 message for non-privileged viewers.
func MaskHL7Payload(raw string, canViewPHI bool) string {
	if canViewPHI || raw == "" {
		return raw
	}
	lines := strings.Split(raw, "\r")
	for i, line := range lines {
		fields := strings.Split(line, "|")
		if len(fields) == 0 {
			continue
		}
		segType := fields[0]
		positions, ok := phiFields[segType]
		if !ok {
			continue
		}
		for _, pos := range positions {
			if pos < len(fields) && fields[pos] != "" {
				fields[pos] = maskValue(fields[pos])
			}
		}
		lines[i] = strings.Join(fields, "|")
	}
	return strings.Join(lines, "\r")
}

// MaskJSONPayload masks common PHI field values in a JSON string.
func MaskJSONPayload(payload string, canViewPHI bool) string {
	if canViewPHI || payload == "" {
		return payload
	}
	// Simple string-level masking for known PHI keys in JSON
	phiKeys := []string{"patientId", "patient_id", "patient_name", "patientName"}
	result := payload
	for _, key := range phiKeys {
		// Find "key":"value" patterns and mask the value
		search := `"` + key + `":"`
		idx := strings.Index(result, search)
		for idx >= 0 {
			valueStart := idx + len(search)
			valueEnd := strings.Index(result[valueStart:], `"`)
			if valueEnd > 0 {
				original := result[valueStart : valueStart+valueEnd]
				masked := maskValue(original)
				result = result[:valueStart] + masked + result[valueStart+valueEnd:]
			}
			next := strings.Index(result[idx+1:], search)
			if next < 0 {
				break
			}
			idx = idx + 1 + next
		}
	}
	return result
}

func maskValue(v string) string {
	if len(v) <= 2 {
		return "***"
	}
	// Keep first char, mask middle, keep last char
	return string(v[0]) + strings.Repeat("*", len(v)-2) + string(v[len(v)-1])
}

// CanViewPHI returns true if the role has permission to see unmasked PHI data.
func CanViewPHI(role string) bool {
	return role == "admin" || role == "developer" || role == "viewer" || role == "super_admin"
}
