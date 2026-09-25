package agents

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/logging"
)

// EmailSender defines the function signature for delivering emails.
type EmailSender func(ctx context.Context, cfg *config.Config, subject, reportText string) error

// SendReportEmail delivers formatted agent reports via pure Go SMTP.
// If subject is empty, it defaults to "X Account Insights Report - <ENV>".
func SendReportEmail(ctx context.Context, cfg *config.Config, subject, reportText string) error {
	logging.Infof("Preparing to send report email to %s...", cfg.ReportRecipient)

	if err := cfg.CheckEmailConfig(); err != nil {
		logging.Errorf("Email configuration error: %v", err)
		return fmt.Errorf("email configuration error: %w", err)
	}

	if subject == "" {
		subject = fmt.Sprintf("X Account Insights Report - %s", strings.ToUpper(cfg.Environment))
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	boundary := fmt.Sprintf("===boundary_x_agent_%d===", time.Now().UnixNano())

	htmlBody := fmt.Sprintf(`<html>
<body style="font-family: monospace; white-space: pre; background-color: #f4f4f4; padding: 20px;">
    <div style="background-color: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
        %s
    </div>
    <p style="font-family: sans-serif; color: #666; font-size: 12px; margin-top: 20px;">
        Sent by X-Agent Framework.
    </p>
</body>
</html>`, html.EscapeString(reportText))

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", cfg.ReportSender))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", cfg.ReportRecipient))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary))

	// Plain text part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	msg.WriteString(reportText)
	msg.WriteString("\r\n\r\n")

	// HTML part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n\r\n")

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	var auth smtp.Auth
	if cfg.SMTPUser != "" && cfg.SMTPPassword != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)
	}

	bodyBytes := []byte(msg.String())

	// 1. Direct TLS (port 465 or cfg.SMTPUseTLS)
	if cfg.SMTPUseTLS || cfg.SMTPPort == 465 {
		tlsConfig := &tls.Config{
			ServerName: cfg.SMTPHost,
		}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("TLS dial failed: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return fmt.Errorf("SMTP client failed: %w", err)
		}
		defer client.Close()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("SMTP auth failed: %w", err)
			}
		}
		return transmitSMTP(client, cfg.ReportSender, cfg.ReportRecipient, bodyBytes)
	}

	// 2. Plain or STARTTLS connection (port 587/25)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("TCP dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("SMTP client failed: %w", err)
	}
	defer client.Close()

	if cfg.SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: cfg.SMTPHost}
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS negotiation failed: %w", err)
			}
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	return transmitSMTP(client, cfg.ReportSender, cfg.ReportRecipient, bodyBytes)
}

func transmitSMTP(client *smtp.Client, sender, recipient string, body []byte) error {
	if err := client.Mail(sender); err != nil {
		return fmt.Errorf("MAIL FROM failed: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("RCPT TO failed: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA command failed: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return fmt.Errorf("writing body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("closing DATA writer failed: %w", err)
	}
	logging.Info("Report email sent successfully!")
	return client.Quit()
}
