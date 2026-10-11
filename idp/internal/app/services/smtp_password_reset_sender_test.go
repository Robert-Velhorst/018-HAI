package services

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSMTPPasswordResetSenderRequiresCompleteConfiguration(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "", "operator@example.com", true)
	require.False(t, sender.Configured())

	err := sender.SendPasswordReset(context.Background(), "recipient@example.com", "reset-code", time.Now().Add(time.Hour))
	require.EqualError(t, err, "password reset email delivery is not configured")
}

func TestSMTPPasswordResetSenderRejectsInvalidPort(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "not-a-port", "operator@example.com", "app-password", "operator@example.com", true)
	require.False(t, sender.Configured())
}

func TestSMTPPasswordResetSenderValidatesRecipientBeforeConnecting(t *testing.T) {
	sender := NewSMTPPasswordResetSender("smtp.example.com", "587", "operator@example.com", "app-password", "operator@example.com", true)
	require.True(t, sender.Configured())

	err := sender.SendPasswordReset(context.Background(), "not-an-email", "reset-code", time.Now().Add(time.Hour))
	require.EqualError(t, err, "invalid recipient email")
}

func TestSMTPPasswordResetSenderHonorsContextDeadlineWhileWaitingForGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	sender := NewSMTPPasswordResetSender(host, port, "", "", "operator@example.com", false, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	err = sender.SendPasswordReset(ctx, "recipient@example.com", "reset-code", time.Now().Add(time.Hour))
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 500*time.Millisecond)

	select {
	case conn := <-accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("smtp test server did not accept the connection")
	}
}
