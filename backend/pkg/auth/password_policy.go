package auth

import (
	"fmt"
	"strings"
	"unicode"
)

// PasswordPolicy defines password complexity requirements.
type PasswordPolicy struct {
	MinLength        int  `json:"min_length"`
	MaxLength        int  `json:"max_length"`
	RequireUppercase bool `json:"require_uppercase"`
	RequireLowercase bool `json:"require_lowercase"`
	RequireDigit     bool `json:"require_digit"`
	RequireSpecial   bool `json:"require_special"`
	MinSpecialChars  int  `json:"min_special_chars"`
}

// DefaultPasswordPolicy returns the default policy for healthcare environments.
func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:        12,
		MaxLength:        128,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireDigit:     true,
		RequireSpecial:   true,
		MinSpecialChars:  1,
	}
}

// Validate checks a password against the policy. Returns nil if valid, or a descriptive error.
func (p PasswordPolicy) Validate(password string) error {
	var errors []string

	if len(password) < p.MinLength {
		errors = append(errors, fmt.Sprintf("minimum %d characters required", p.MinLength))
	}
	if p.MaxLength > 0 && len(password) > p.MaxLength {
		errors = append(errors, fmt.Sprintf("maximum %d characters allowed", p.MaxLength))
	}

	var hasUpper, hasLower, hasDigit bool
	specialCount := 0

	for _, ch := range password {
		switch {
		case unicode.IsUpper(ch):
			hasUpper = true
		case unicode.IsLower(ch):
			hasLower = true
		case unicode.IsDigit(ch):
			hasDigit = true
		case unicode.IsPunct(ch) || unicode.IsSymbol(ch):
			specialCount++
		}
	}

	if p.RequireUppercase && !hasUpper {
		errors = append(errors, "at least one uppercase letter required")
	}
	if p.RequireLowercase && !hasLower {
		errors = append(errors, "at least one lowercase letter required")
	}
	if p.RequireDigit && !hasDigit {
		errors = append(errors, "at least one digit required")
	}
	if p.RequireSpecial && specialCount < p.MinSpecialChars {
		errors = append(errors, fmt.Sprintf("at least %d special character(s) required", p.MinSpecialChars))
	}

	if len(errors) > 0 {
		return fmt.Errorf("password policy: %s", strings.Join(errors, "; "))
	}
	return nil
}
