package gateway

import "testing"

// The operator-input rules are the gate in front of the only account that can
// read provider credentials, so they are tested as behaviour rather than left to
// the form in the console.

func TestValidateOperatorInputAcceptsReasonableValues(t *testing.T) {
	// Eight characters is the documented floor (NIST SP 800-63B baseline).
	for _, tc := range []struct{ user, pass string }{
		{"admin", "akuganteng"},
		{"operator", "correct-horse-battery"},
		{"a-b_c.d", "12345678"},
	} {
		if msg := validateOperatorInput(tc.user, tc.pass); msg != "" {
			t.Errorf("%q/%q should be accepted, got %q", tc.user, tc.pass, msg)
		}
	}
}

func TestValidateOperatorInputRejectsShortPassword(t *testing.T) {
	for _, pass := range []string{"", "a", "1234567"} {
		if msg := validateOperatorInput("admin", pass); msg == "" {
			t.Errorf("password %q (%d chars) must be refused", pass, len(pass))
		}
	}
}

func TestValidateOperatorInputUsernameRules(t *testing.T) {
	cases := []string{"", "ab", "has space", "has/slash", "has:colon", "has@at"}
	for _, user := range cases {
		if msg := validateOperatorInput(user, "long-enough-password"); msg == "" {
			t.Errorf("username %q must be refused", user)
		}
	}
	// Surrounding whitespace is trimmed rather than rejected.
	if msg := validateOperatorInput("  admin  ", "long-enough-password"); msg != "" {
		t.Errorf("a padded username should be accepted, got %q", msg)
	}
}

func TestValidateOperatorInputRejectsOverlongPassword(t *testing.T) {
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'x'
	}
	if msg := validateOperatorInput("admin", string(long)); msg == "" {
		t.Error("a 201-character password must be refused")
	}
}
