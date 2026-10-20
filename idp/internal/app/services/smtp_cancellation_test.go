package services

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSMTPGreetingInterruptionPreservesContextCause(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "canceled"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			require.NoError(t, listener.SetDeadline(time.Now().Add(3*time.Second)))
			host, port, err := net.SplitHostPort(listener.Addr().String())
			require.NoError(t, err)
			sender := NewSMTPPasswordResetSender(host, port, "", "", "sender@example.com", true)
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- sender.SendPasswordReset(ctx, "recipient@example.com", "synthetic-code", time.Now().Add(time.Hour))
			}()
			connection, err := listener.Accept()
			require.NoError(t, err)
			defer connection.Close()
			// Withhold the greeting: cancellation must interrupt a real socket read.
			if !deadline {
				cancel()
			}
			select {
			case err := <-result:
				require.ErrorIs(t, err, want)
				require.EqualError(t, err, "password reset email delivery failed")
				stage, ok := PasswordResetDeliveryFailureStage(err)
				require.True(t, ok)
				require.Contains(t, []string{"connect", "deadline", "protocol"}, stage)
			case <-time.After(3 * time.Second):
				t.Fatal("SMTP greeting did not stop after context interruption")
			}
		})
	}
}
