package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSMTPPasswordResetSenderRequiresCompleteConfiguration(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "", "operator@example.com", true)
	require.False(t, sender.Configured())

	err := sender.SendPasswordReset("recipient@example.com", "reset-code", time.Now().Add(time.Hour))
	require.EqualError(t, err, "password reset email delivery is not configured")
}

func TestSMTPPasswordResetSenderRejectsInvalidPort(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "not-a-port", "operator@example.com", "app-password", "operator@example.com", true)
	require.False(t, sender.Configured())
}

func TestSMTPPasswordResetSenderNormalizesTransportPort(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		implicit  bool
	}{
		{" +465 ", "465", true}, {"0465", "465", true}, {" +0587 ", "587", false}, {"1", "1", false}, {"65535", "65535", false},
	} {
		sender := NewSMTPPasswordResetSender("smtp.example.com", tc.raw, "operator@example.com", "app-password", "operator@example.com", true)
		require.True(t, sender.Configured())
		require.Equal(t, tc.want, sender.port)
		require.Equal(t, tc.implicit, isImplicitTLSPort(sender.port))
		host, port, err := net.SplitHostPort(net.JoinHostPort(sender.host, sender.port))
		require.NoError(t, err)
		require.Equal(t, "smtp.example.com", host)
		require.Equal(t, tc.want, port)
	}
	for _, raw := range []string{"", "0", "-1", "65536", "not-a-port", "99999999999999999999999"} {
		sender := NewSMTPPasswordResetSender("smtp.example.com", raw, "operator@example.com", "app-password", "operator@example.com", true)
		require.False(t, sender.Configured())
	}
}

func TestSMTPPasswordResetSenderPreservesExactPassword(t *testing.T) {
	for _, password := range []string{" leading", "trailing ", " both ", "\tpassword\n", " "} {
		sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", password, "operator@example.com", true)
		require.True(t, sender.Configured())
		if sender.password != password {
			t.Fatal("SMTP password altered")
		}
	}
	missing := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "", "operator@example.com", true)
	require.False(t, missing.Configured(), "an absent password still disables authenticated delivery")
}

func TestSMTPPasswordResetSenderAllowsImplicitTLSOnlyWhenEncryptionIsRequired(t *testing.T) {
	port := strconv.Itoa(smtpImplicitTLSPort)
	secure := NewSMTPPasswordResetSender("smtp.example.com", port, "operator@example.com", "app-password", "operator@example.com", true)
	require.True(t, secure.Configured())

	plaintextAllowed := NewSMTPPasswordResetSender("smtp.example.com", port, "operator@example.com", "app-password", "operator@example.com", false)
	require.False(t, plaintextAllowed.Configured())
	require.True(t, isImplicitTLSPort("0465"))
}

func TestSMTPPasswordResetSenderUsesMailboxForEnvelopeAndDisplayNameForHeader(t *testing.T) {
	address, err := parseSMTPAddress(`HAI Password Reset <operator@example.com>`)
	require.NoError(t, err)
	require.Equal(t, "operator@example.com", address.Address, "SMTP MAIL FROM requires the mailbox, not the display-name form")
	require.Equal(t, "HAI Password Reset", address.Name, "the message header should preserve the display name")
	require.Equal(t, `"HAI Password Reset" <operator@example.com>`, address.String(), "the encoded message header must remain standards-compliant")

	sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "app-password", `HAI Password Reset <operator@example.com>`, true)
	require.True(t, sender.Configured(), "a valid display name is supported when its envelope address is separated")
}

func TestSMTPPasswordResetSenderRequiresSTARTTLS(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "app-password", "operator@example.com", false)
	require.False(t, sender.Configured())

	unauthenticated := NewSMTPPasswordResetSender("smtp.example.com", "587", "", "", "operator@example.com", false)
	require.False(t, unauthenticated.Configured())
}

func TestSMTPPasswordResetSenderValidatesRecipientBeforeConnecting(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "app-password", "operator@example.com", true)
	require.True(t, sender.Configured())

	err := sender.SendPasswordReset("not-an-email", "reset-code", time.Now().Add(time.Hour))
	require.EqualError(t, err, "password reset email delivery failed")
	stage, ok := PasswordResetDeliveryFailureStage(err)
	require.True(t, ok)
	require.Equal(t, "recipient", stage)
}

func TestPasswordResetDeliveryErrorExposesOnlyAllowlistedStage(t *testing.T) {
	secretCause := errors.New("provider rejected private@example.com with password=never-log-this")
	err := NewPasswordResetDeliveryError("authentication", secretCause)

	require.ErrorIs(t, err, secretCause)
	require.NotContains(t, err.Error(), secretCause.Error())
	stage, ok := PasswordResetDeliveryFailureStage(err)
	require.True(t, ok)
	require.Equal(t, "authentication", stage)

	unknownStage := NewPasswordResetDeliveryError("provider said private@example.com", secretCause)
	_, ok = PasswordResetDeliveryFailureStage(unknownStage)
	require.False(t, ok, "untrusted stage text must not be copied into logs")
}

func TestOpenSMTPConnectionUsesVerifiedImplicitTLS(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	t.Cleanup(server.Close)

	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	tlsConfig := smtpTLSConfig(host)
	tlsConfig.RootCAs = roots

	connection, err := openSMTPConnection(host, "465", localSMTPDialer(server.Listener.Addr().String()), tlsConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })

	tlsConnection, ok := connection.(*tls.Conn)
	require.True(t, ok, "port 465 must return a TLS-wrapped connection")
	require.NotEmpty(t, tlsConnection.ConnectionState().VerifiedChains, "server certificate must be verified")
}

func TestOpenSMTPConnectionRejectsUntrustedImplicitTLSWithoutPlaintextRetry(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	t.Cleanup(server.Close)

	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	dialCalls := 0
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCalls++
		return localSMTPDialer(server.Listener.Addr().String())(ctx, network, address)
	}

	connection, err := openSMTPConnection(host, "465", dial, smtpTLSConfig(host))
	require.Nil(t, connection)
	require.Error(t, err)
	require.Contains(t, err.Error(), "certificate")
	require.Equal(t, 1, dialCalls, "failed implicit TLS must not retry with plaintext SMTP")
}

func TestOpenSMTPConnectionVerifiesImplicitTLSHostname(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	t.Cleanup(server.Close)

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	tlsConfig := smtpTLSConfig("wrong.invalid")
	tlsConfig.RootCAs = roots
	connection, err := openSMTPConnection("wrong.invalid", "465", localSMTPDialer(server.Listener.Addr().String()), tlsConfig)
	require.Nil(t, connection)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong.invalid")
	require.False(t, tlsConfig.InsecureSkipVerify)
}

func TestOpenSMTPConnectionLeavesSubmissionPortForSTARTTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	connection, err := openSMTPConnection("smtp.example.com", "587", localSMTPDialer(listener.Addr().String()), smtpTLSConfig("smtp.example.com"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	_, isTLS := connection.(*tls.Conn)
	require.False(t, isTLS, "port 587 must retain the SMTP-then-STARTTLS sequence")

	select {
	case serverConnection := <-accepted:
		t.Cleanup(func() { _ = serverConnection.Close() })
	case <-time.After(time.Second):
		t.Fatal("test SMTP listener did not accept the local connection")
	}
}

func TestOpenSMTPConnectionBoundsDialWithContextDeadline(t *testing.T) {
	var observedDeadline time.Time
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var ok bool
		observedDeadline, ok = ctx.Deadline()
		require.True(t, ok, "dial context must carry a timeout")
		return nil, context.DeadlineExceeded
	}

	connection, err := openSMTPConnection("smtp.example.com", "465", dial, smtpTLSConfig("smtp.example.com"))
	require.Nil(t, connection)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.WithinDuration(t, time.Now().Add(smtpDialTimeout), observedDeadline, 100*time.Millisecond)
}

func localSMTPDialer(target string) smtpDialContextFunc {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}
}
