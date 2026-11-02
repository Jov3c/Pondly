package mail

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

type Mailer interface {
	SendOTP(ctx context.Context, to, code string) error
}

type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (m *SMTPMailer) SendOTP(ctx context.Context, to, code string) error {
	if m == nil || m.Host == "" || m.From == "" {
		return fmt.Errorf("smtp not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	to = strings.TrimSpace(to)
	if strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("invalid recipient")
	}
	subject := "Pondly Owner 验证码"
	body := "你的 Pondly 主人登录验证码是：" + code + "\r\n\r\n" +
		"验证码短期有效，只能使用一次。若不是你本人操作，请忽略此邮件。\r\n"
	message := strings.Join([]string{
		"From: " + m.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")
	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	if err := smtp.SendMail(addr, auth, m.From, []string{to}, []byte(message)); err != nil {
		return fmt.Errorf("send otp: %w", err)
	}
	return nil
}
