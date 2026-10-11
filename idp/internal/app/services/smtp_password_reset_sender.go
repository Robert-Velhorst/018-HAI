package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const defaultSMTPDeliveryTimeout = 10 * time.Second

// SMTPPasswordResetSender delivers short-lived reset codes over a standard
// STARTTLS SMTP connection. It deliberately supports port 587-style STARTTLS;
// implicit TLS (usually port 465) is not silently downgraded to plaintext.
type SMTPPasswordResetSender struct {
	host            string
	port            string
	username        string
	password        string
	from            string
	requireStartTLS bool
	deliveryTimeout time.Duration
}

func NewSMTPPasswordResetSender(host, port, username, password, from string, requireStartTLS bool, timeout ...time.Duration) *SMTPPasswordResetSender {
	deliveryTimeout := defaultSMTPDeliveryTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		deliveryTimeout = timeout[0]
	}
	return &SMTPPasswordResetSender{
		host:            strings.TrimSpace(host),
		port:            strings.TrimSpace(port),
		username:        strings.TrimSpace(username),
		password:        strings.TrimSpace(password),
		from:            strings.TrimSpace(from),
		requireStartTLS: requireStartTLS,
		deliveryTimeout: deliveryTimeout,
	}
}

func (s *SMTPPasswordResetSender) Configured() bool {
	if s == nil || s.host == "" || s.port == "" || s.from == "" {
		return false
	}
	port, err := strconv.Atoi(s.port)
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	if _, err := mail.ParseAddress(s.from); err != nil {
		return false
	}
	return (s.username == "") == (s.password == "")
}

func (s *SMTPPasswordResetSender) SendPasswordReset(ctx context.Context, email, resetToken string, expiresAt time.Time) error {
	if !s.Configured() {
		return errors.New("password reset email delivery is not configured")
	}
	parsedRecipient, err := mail.ParseAddress(email)
	if err != nil || parsedRecipient.Address != email {
		return errors.New("invalid recipient email")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	dialer := &net.Dialer{Timeout: s.deliveryTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.host, s.port))
	if err != nil {
		return smtpContextError(ctx, "connect to smtp server", err)
	}
	defer conn.Close() //nolint:errcheck // The operation result is decided before socket cleanup.

	deadline := time.Now().Add(s.deliveryTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set smtp deadline: %w", err)
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stopCancellation()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return smtpContextError(ctx, "read smtp greeting", err)
	}
	defer client.Close() //nolint:errcheck // The message result is decided before connection cleanup.

	if s.requireStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not support required STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return smtpContextError(ctx, "start smtp TLS", err)
		}
	}
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return smtpContextError(ctx, "authenticate with smtp server", err)
		}
	}
	if err := client.Mail(s.from); err != nil {
		return smtpContextError(ctx, "set smtp sender", err)
	}
	if err := client.Rcpt(parsedRecipient.Address); err != nil {
		return smtpContextError(ctx, "set smtp recipient", err)
	}

	writer, err := client.Data()
	if err != nil {
		return smtpContextError(ctx, "open smtp message", err)
	}
	message := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: HAI password reset code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour one-time HAI password reset code is:\r\n\r\n%s\r\n\r\nIt expires at %s. If you did not request this reset, you can ignore this email.\r\n", parsedRecipient.Address, s.from, resetToken, expiresAt.UTC().Format(time.RFC1123))
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return smtpContextError(ctx, "write smtp message", err)
	}
	if err := writer.Close(); err != nil {
		return smtpContextError(ctx, "send smtp message", err)
	}
	return nil
}

func smtpContextError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return fmt.Errorf("%s: %w", operation, contextErr)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
