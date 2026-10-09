package notifier

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"serverMonitoring/models"
)

type EmailNotifier struct {
	mu         sync.Mutex
	lastAlerts map[string]time.Time
}

var (
	notifierInstance *EmailNotifier
	notifierOnce     sync.Once
)

// GetNotifier returns the email notifier instance
func GetNotifier() *EmailNotifier {
	notifierOnce.Do(func() {
		notifierInstance = &EmailNotifier{
			lastAlerts: make(map[string]time.Time),
		}
	})
	return notifierInstance
}

// SendFailureAlert sends an alert email for a failing service check to target recipients (or fallback to global recipients)
func (n *EmailNotifier) SendFailureAlert(cfg models.EmailConfig, recipientEmails []string, result models.HealthCheckResult) error {
	if !cfg.Enabled {
		log.Printf("[NOTIFIER] Email notification disabled in config.")
		return nil
	}

	recipients := recipientEmails
	if len(recipients) == 0 {
		recipients = cfg.ToEmails
	}

	if len(recipients) == 0 || cfg.FromEmail == "" || cfg.AppPassword == "" {
		log.Printf("[NOTIFIER] Email notification skipped for %s: missing recipient emails or credentials", result.ServiceName)
		return fmt.Errorf("email credentials or recipient emails not fully configured")
	}

	effectiveCfg := cfg
	effectiveCfg.ToEmails = recipients

	subject := fmt.Sprintf("🚨 [ALERT] Service Failure: %s is DOWN", result.ServiceName)
	htmlBody := n.buildAlertHTML(result)
	plainText := n.buildAlertPlainText(result)

	return n.sendMail(effectiveCfg, subject, plainText, htmlBody)
}

// SendTestEmail sends a test alert email to verify SMTP configuration
func (n *EmailNotifier) SendTestEmail(cfg models.EmailConfig) error {
	if len(cfg.ToEmails) == 0 || cfg.FromEmail == "" || cfg.AppPassword == "" {
		return fmt.Errorf("from_email, app_password, or to_emails is missing")
	}

	testResult := models.HealthCheckResult{
		ServiceName:    "Zadroit Monitoring Test Service",
		Type:           "backend",
		URL:            "https://status.zadroit.com/test",
		Status:         false,
		HitTime:        models.NowFormatted(),
		Message:        "This is a verified test email from the Server Monitoring System.",
		HTTPStatus:     503,
		ResponseTimeMs: 42,
		ErrorDetail:    "Simulated alert test triggered from dashboard.",
		Data: map[string]interface{}{
			"service": false,
			"db":      true,
			"test":    true,
		},
	}

	subject := "🧪 [TEST] Zadroit Server Monitoring Alert System Test"
	htmlBody := n.buildAlertHTML(testResult)
	plainText := n.buildAlertPlainText(testResult)

	return n.sendMail(cfg, subject, plainText, htmlBody)
}

func (n *EmailNotifier) sendMail(cfg models.EmailConfig, subject, plainText, htmlBody string) error {
	// Clean up app password (remove spaces if user copied "bfit dhmb hcxt jrvg")
	cleanPassword := strings.ReplaceAll(cfg.AppPassword, " ", "")
	from := strings.TrimSpace(cfg.FromEmail)
	host := cfg.SMTPHost
	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}

	// Filter and deduplicate valid recipient email addresses
	var validRecipients []string
	seen := make(map[string]bool)
	for _, to := range cfg.ToEmails {
		trimmed := strings.ToLower(strings.TrimSpace(to))
		if trimmed != "" && strings.Contains(trimmed, "@") && !seen[trimmed] {
			seen[trimmed] = true
			validRecipients = append(validRecipients, trimmed)
		}
	}

	if len(validRecipients) == 0 {
		return fmt.Errorf("no valid recipient email addresses specified")
	}

	boundary := "ZADROIT_MONITORING_BOUNDARY_xyz123"
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("Zadroit Server Monitor <%s>", from)
	headers["To"] = strings.Join(validRecipients, ", ")
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = fmt.Sprintf("multipart/alternative; boundary=\"%s\"", boundary)

	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")

	// Plain text part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	msg.WriteString(plainText)
	msg.WriteString("\r\n\r\n")

	// HTML part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n\r\n")

	msg.WriteString(fmt.Sprintf("--%s--", boundary))

	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", from, cleanPassword, host)

	// Send using STARTTLS
	tlsconfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         host,
	}

	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to dial SMTP server %s: %w", addr, err)
	}
	defer client.Close()

	if err = client.StartTLS(tlsconfig); err != nil {
		return fmt.Errorf("failed to start TLS: %w", err)
	}

	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication failed (check Gmail App Password): %w", err)
	}

	if err = client.Mail(from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	for _, to := range validRecipients {
		if err = client.Rcpt(to); err != nil {
			log.Printf("[NOTIFIER WARNING] Failed to add recipient %s: %v", to, err)
			return fmt.Errorf("failed to add recipient %s: %w", to, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to open data writer: %w", err)
	}

	_, err = w.Write([]byte(msg.String()))
	if err != nil {
		return fmt.Errorf("failed to write email body: %w", err)
	}

	if err = w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	log.Printf("[NOTIFIER] Alert email successfully sent to %d recipient(s): %s", len(validRecipients), strings.Join(validRecipients, ", "))
	return nil
}

func (n *EmailNotifier) buildAlertHTML(result models.HealthCheckResult) string {
	dataJSON, _ := json.MarshalIndent(result.Data, "", "  ")

	var dataDisplay string
	if len(result.Data) > 0 {
		dataDisplay = fmt.Sprintf("<pre style=\"background:#1e1e2f; color:#00ffcc; padding:12px; border-radius:6px; font-family:monospace; font-size:13px; overflow-x:auto;\">%s</pre>", string(dataJSON))
	} else {
		dataDisplay = "<span style=\"color:#888;\">None</span>"
	}

	errorDetail := result.ErrorDetail
	if errorDetail == "" {
		errorDetail = result.Message
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Server Alert</title>
</head>
<body style="margin:0; padding:20px; font-family:'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color:#0f111a; color:#e2e8f0;">
  <table width="100%%" border="0" cellspacing="0" cellpadding="0" style="max-width:650px; margin:0 auto; background-color:#171923; border:1px solid #dc2626; border-radius:12px; overflow:hidden; box-shadow:0 10px 25px rgba(220, 38, 38, 0.2);">
    <tr>
      <td style="background: linear-gradient(135deg, #ef4444 0%%, #b91c1c 100%%); padding: 25px; text-align: center;">
        <h1 style="color:#ffffff; margin:0; font-size:24px; font-weight:700; letter-spacing:0.5px;">⚠️ SERVER MONITORING ALERT</h1>
        <p style="color:#fecaca; margin:6px 0 0 0; font-size:14px;">Zadroit Infrastructure Watchdog detected an anomaly</p>
      </td>
    </tr>
    <tr>
      <td style="padding: 30px;">
        <div style="background-color:#202434; border-left:4px solid #ef4444; padding:16px 20px; border-radius:4px; margin-bottom:24px;">
          <h2 style="color:#ffffff; margin:0 0 4px 0; font-size:18px;">%s</h2>
          <p style="color:#ef4444; margin:0; font-weight:600; font-size:14px;">Status: DOWN / ANOMALY DETECTED</p>
        </div>

        <table width="100%%" border="0" cellspacing="0" cellpadding="8" style="font-size:14px; margin-bottom:20px;">
          <tr style="border-bottom:1px solid #2d3748;">
            <td width="30%%" style="color:#94a3b8; font-weight:600;">Service Name:</td>
            <td style="color:#ffffff; font-weight:600;">%s</td>
          </tr>
          <tr style="border-bottom:1px solid #2d3748;">
            <td style="color:#94a3b8; font-weight:600;">Type:</td>
            <td><span style="background:#374151; color:#93c5fd; padding:3px 8px; border-radius:4px; font-size:12px; text-transform:uppercase;">%s</span></td>
          </tr>
          <tr style="border-bottom:1px solid #2d3748;">
            <td style="color:#94a3b8; font-weight:600;">Target URL:</td>
            <td style="word-break:break-all;"><a href="%s" style="color:#38bdf8; text-decoration:none;">%s</a></td>
          </tr>
          <tr style="border-bottom:1px solid #2d3748;">
            <td style="color:#94a3b8; font-weight:600;">Hit Time:</td>
            <td style="color:#f1f5f9;">%s</td>
          </tr>
          <tr style="border-bottom:1px solid #2d3748;">
            <td style="color:#94a3b8; font-weight:600;">HTTP Code:</td>
            <td style="color:#f87171; font-weight:bold;">%d</td>
          </tr>
          <tr style="border-bottom:1px solid #2d3748;">
            <td style="color:#94a3b8; font-weight:600;">Message:</td>
            <td style="color:#fca5a5;">%s</td>
          </tr>
          <tr>
            <td style="color:#94a3b8; font-weight:600;">Failure Details:</td>
            <td style="color:#fca5a5;">%s</td>
          </tr>
        </table>

        <div style="margin-top:20px;">
          <h3 style="color:#94a3b8; font-size:14px; margin:0 0 8px 0; text-transform:uppercase; letter-spacing:0.5px;">Received API / Response Data</h3>
          %s
        </div>

        <div style="margin-top:30px; text-align:center;">
          <p style="color:#64748b; font-size:12px; margin:0;">This is an automated alert generated by Zadroit Server Monitoring System. Checks are executed every 5 minutes.</p>
        </div>
      </td>
    </tr>
  </table>
</body>
</html>`,
		result.ServiceName,
		result.ServiceName,
		result.Type,
		result.URL,
		result.URL,
		result.HitTime,
		result.HTTPStatus,
		result.Message,
		errorDetail,
		dataDisplay,
	)
}

func (n *EmailNotifier) buildAlertPlainText(result models.HealthCheckResult) string {
	dataJSON, _ := json.MarshalIndent(result.Data, "", "  ")
	return fmt.Sprintf(`=======================================================
⚠️ SERVER MONITORING ALERT - %s IS DOWN
=======================================================

Service Name : %s
Type         : %s
URL          : %s
Hit Time     : %s
Status       : FAILED
HTTP Status  : %d
Message      : %s
Error Detail : %s

Response Data:
%s

-------------------------------------------------------
Automated notification from Zadroit Server Monitoring System.
`,
		result.ServiceName,
		result.ServiceName,
		result.Type,
		result.URL,
		result.HitTime,
		result.HTTPStatus,
		result.Message,
		result.ErrorDetail,
		string(dataJSON),
	)
}
