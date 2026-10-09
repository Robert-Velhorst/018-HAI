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

const (
	smtpDialTimeout     = 10 * time.Second
	smtpSessionTimeout  = 30 * time.Second
	smtpImplicitTLSPort = 465
)

// SMTPPasswordResetSender delivers short-lived reset codes over a standard
// encrypted SMTP connection. Port 465 uses implicit TLS; other ports use
// STARTTLS. Neither mode falls back to plaintext.
type SMTPPasswordResetSender struct {
	host            string
	port            string
	username        string
	password        string
	from            string
	requireStartTLS bool
}

// PasswordResetDeliveryError carries a redacted SMTP stage for operator logs.
// Its rendered message never includes provider text, addresses, or credentials.
type PasswordResetDeliveryError struct {
	Stage string
	cause error
}

func (e *PasswordResetDeliveryError) Error() string {
	return "password reset email delivery failed"
}

func (e *PasswordResetDeliveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewPasswordResetDeliveryError(stage string, cause error) *PasswordResetDeliveryError {
	return &PasswordResetDeliveryError{Stage: stage, cause: cause}
}

// PasswordResetDeliveryFailureStage returns only a known, non-sensitive stage
// label suitable for logs. Unknown errors and caller-supplied labels stay hidden.
func PasswordResetDeliveryFailureStage(err error) (string, bool) {
	var deliveryError *PasswordResetDeliveryError
	if !errors.As(err, &deliveryError) || deliveryError == nil {
		return "", false
	}
	switch deliveryError.Stage {
	case "connect", "deadline", "protocol", "tls", "authentication", "sender", "recipient", "submission":
		return deliveryError.Stage, true
	default:
		return "", false
	}
}

func NewSMTPPasswordResetSender(host, port, username, password, from string, requireStartTLS bool) *SMTPPasswordResetSender {
	port = strings.TrimSpace(port)
	if number, err := strconv.Atoi(port); err == nil && number >= 1 && number <= 65535 {
		port = strconv.Itoa(number)
	}
	return &SMTPPasswordResetSender{
		host:            strings.TrimSpace(host),
		port:            port,
		username:        strings.TrimSpace(username),
		password:        password,
		from:            strings.TrimSpace(from),
		requireStartTLS: requireStartTLS,
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
	if _, err := parseSMTPAddress(s.from); err != nil {
		return false
	}
	if (s.username == "") != (s.password == "") {
		return false
	}
	// Reset tokens are bearer credentials, so encrypted transport is mandatory.
	// The port selects implicit TLS (465) or explicit STARTTLS (other ports).
	return s.requireStartTLS
}

func (s *SMTPPasswordResetSender) SendPasswordReset(email, resetToken string, expiresAt time.Time) error {
	if !s.Configured() {
		return errors.New("password reset email delivery is not configured")
	}
	parsedFrom, err := parseSMTPAddress(s.from)
	if err != nil {
		return NewPasswordResetDeliveryError("sender", err)
	}
	parsedRecipient, err := mail.ParseAddress(email)
	if err != nil || parsedRecipient.Address != email {
		return NewPasswordResetDeliveryError("recipient", errors.New("invalid recipient mailbox"))
	}

	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	connection, err := openSMTPConnection(s.host, s.port, dialer.DialContext, smtpTLSConfig(s.host))
	if err != nil {
		return NewPasswordResetDeliveryError("connect", err)
	}
	if err := connection.SetDeadline(time.Now().Add(smtpSessionTimeout)); err != nil {
		_ = connection.Close()
		return NewPasswordResetDeliveryError("deadline", err)
	}
	client, err := smtp.NewClient(connection, s.host)
	if err != nil {
		_ = connection.Close()
		return NewPasswordResetDeliveryError("protocol", err)
	}
	defer client.Quit() //nolint:errcheck // The message result is decided before QUIT.

	if !isImplicitTLSPort(s.port) {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return NewPasswordResetDeliveryError("tls", errors.New("required STARTTLS extension is unavailable"))
		}
		if err := client.StartTLS(smtpTLSConfig(s.host)); err != nil {
			return NewPasswordResetDeliveryError("tls", err)
		}
	}
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return NewPasswordResetDeliveryError("authentication", err)
		}
	}
	if err := client.Mail(parsedFrom.Address); err != nil {
		return NewPasswordResetDeliveryError("sender", err)
	}
	if err := client.Rcpt(parsedRecipient.Address); err != nil {
		return NewPasswordResetDeliveryError("recipient", err)
	}

	writer, err := client.Data()
	if err != nil {
		return NewPasswordResetDeliveryError("submission", err)
	}
	message := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: HAI password reset code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour one-time HAI password reset code is:\r\n\r\n%s\r\n\r\nIt expires at %s. If you did not request this reset, you can ignore this email.\r\n", parsedRecipient.Address, parsedFrom.String(), resetToken, expiresAt.UTC().Format(time.RFC1123))
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return NewPasswordResetDeliveryError("submission", err)
	}
	if err := writer.Close(); err != nil {
		return NewPasswordResetDeliveryError("submission", err)
	}
	return nil
}

func parseSMTPAddress(value string) (*mail.Address, error) {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address == "" {
		return nil, errors.New("invalid SMTP mailbox")
	}
	return address, nil
}

type smtpDialContextFunc func(context.Context, string, string) (net.Conn, error)

func openSMTPConnection(host, port string, dialContext smtpDialContextFunc, tlsConfig *tls.Config) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), smtpDialTimeout)
	defer cancel()

	connection, err := dialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	if !isImplicitTLSPort(port) {
		return connection, nil
	}

	tlsConnection := tls.Client(connection, tlsConfig.Clone())
	if err := tlsConnection.HandshakeContext(ctx); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("establish implicit smtp TLS: %w", err)
	}
	return tlsConnection, nil
}

func smtpTLSConfig(serverName string) *tls.Config {
	return &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}
}

func isImplicitTLSPort(port string) bool {
	parsedPort, err := strconv.Atoi(port)
	return err == nil && parsedPort == smtpImplicitTLSPort
}
