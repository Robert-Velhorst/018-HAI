package config

import "testing"

func TestMailConfigPreservesExactPassword(t *testing.T) {
	t.Setenv(smtpRequireStartTLS, "true")
	for _, password := range []string{" leading", "trailing ", " both ", "\tpassword\n", " ", ""} {
		t.Setenv(smtpPassword, password)
		cfg, err := newMailConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Password != password {
			t.Fatal("SMTP password altered")
		}
	}
}
