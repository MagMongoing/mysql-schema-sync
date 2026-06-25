package internal

import (
	"context"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/xanygo/anygo/xnet/xsmtp"
)

// EmailStruct email conf info
type EmailStruct struct {
	SMTPHost     string `json:"smtp_host"`
	From         string `json:"from"`
	Password     string `json:"password"`
	To           string `json:"to"`
	SendMailAble bool   `json:"send_mail"`
}

const tableStyle = `
<sTyle type='text/css'>
      table {border-collapse: collapse;border-spacing: 0;}
     .tb_1{border:1px solid #cccccc;table-layout:fixed;word-break:break-all;width: 100%;background:#ffffff;margin-bottom:5px}
     .tb_1 caption{text-align: center;background: #F0F4F6;font-weight: bold;padding-top: 5px;height: 25px;border:1px solid #cccccc;border-bottom:none}
     .tb_1 a{margin:0 3px 0 3px}
     .tb_1 tr th,.tb_1 tr td{padding: 3px;border:1px solid #cccccc;line-height:20px}
     .tb_1 thead tr th{font-weight:bold;text-align: center;background:#e3eaee}
     .tb_1 tbody tr th{text-align: right;background:#f0f4f6;padding-right:5px}
     .tb_1 tfoot{color:#cccccc}
     .td_c td{text-align: center}
     .td_r td{text-align: right}
     .t_c{text-align: center !important;}
     .t_r{text-align: right !important;}
     .t_l{text-align: left !important;}
</stYle>
`

// SendMail send mail
func (m *EmailStruct) SendMail(title string, body string) {
	if !m.SendMailAble {
		log.Println("send email : no")
		return
	}
	if len(m.SMTPHost) == 0 || len(m.From) == 0 || len(m.To) == 0 {
		log.Println("smtp_host, from,to is empty")
		return
	}
	host, port, err := net.SplitHostPort(m.SMTPHost)
	if err != nil {
		log.Println("invalid SMTPHost:", m.SMTPHost)
		return
	}
	var sendTo []string
	for _, _to := range strings.Split(m.To, ";") {
		_to = strings.TrimSpace(_to)
		if len(_to) != 0 && strings.Contains(_to, "@") {
			sendTo = append(sendTo, _to)
		}
	}

	if len(sendTo) < 1 {
		log.Println("mail receiver is empty")
		return
	}

	body = mailBody(body)
	portInt, err := strconv.Atoi(port)
	if err != nil {
		log.Printf("invalid SMTP port %q: %s", port, err)
		return
	}

	cfg := &xsmtp.Config{
		Host:     host,
		Port:     portInt,
		Username: m.From,
		Password: m.Password,
	}
	mail := &xsmtp.Mail{
		To:      sendTo,
		Subject: title,
		Content: body,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	err = cfg.Send(ctx, mail)
	if err == nil {
		log.Println("send mail success")
	} else {
		// Sanitize error to avoid leaking SMTP credentials in logs.
		// For very short passwords, literal substring replacement risks corrupting
		// the diagnostic message — fall back to a generic error in that case.
		errMsg := err.Error()
		if len(m.Password) >= 10 {
			errMsg = strings.ReplaceAll(errMsg, m.Password, "***")
		} else if len(m.Password) > 0 {
			errMsg = "send mail failed (error redacted to prevent short-password leakage)"
		}
		// Also redact the SMTP username (From) — SMTP error replies routinely
		// echo the AUTH username back, and combined with host context this leaks
		// the account identity. M6: for short usernames where literal replacement
		// may corrupt diagnostic words, redact the entire error.
		if len(m.From) >= 4 {
			errMsg = strings.ReplaceAll(errMsg, m.From, "***")
		} else if len(m.From) > 0 {
			errMsg = "send mail failed (error redacted to prevent short-username leakage)"
		}
		log.Println("send mail failed, err:", errMsg)
	}
}

func mailBody(body string) string {
	body = tableStyle + "\n" + body
	body += "<br/><hr style='border:none;border-top:1px solid #ccc'/>" +
		"<center>Powered by <a href='" + AppURL + "'>mysql-schema-sync</a>&nbsp;" + Version + "</center>"
	return body
}
