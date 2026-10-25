package mailer

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"strings"
	"text/template"
	"time"
)

// Kind selects a letter template.
type Kind string

// Letter kinds.
const (
	VerifyEmail   Kind = "verify_email"
	ResetPassword Kind = "reset_password"
)

// Lang is a letter language.
type Lang string

// Supported languages.
const (
	LangRU Lang = "ru"
	LangEN Lang = "en"
)

// ParseLang normalizes a language tag, falling back to Russian.
func ParseLang(raw string) Lang {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(LangEN):
		return LangEN
	default:
		return LangRU
	}
}

// Data fills a letter template. TTL is already localized (see FormatTTL).
type Data struct {
	Name string
	URL  string
	TTL  string
}

type letter struct {
	subject *template.Template
	text    *template.Template
	html    *htmltemplate.Template
}

// letters holds the ru/en variants of every letter (SPEC section 9: email in
// ru and en, text and HTML).
var letters = map[Kind]map[Lang]letter{
	VerifyEmail: {
		LangRU: mustLetter(
			"Подтвердите email в Tierl",
			`Привет, {{.Name}}!

Подтвердите адрес почты, чтобы создавать опросы в Tierl:
{{.URL}}

Ссылка действует {{.TTL}} и срабатывает один раз.
Если вы не регистрировались, просто проигнорируйте это письмо.

— Tierl`,
			`<p>Привет, {{.Name}}!</p>
<p>Подтвердите адрес почты, чтобы создавать опросы в Tierl:</p>
<p><a href="{{.URL}}">Подтвердить email</a></p>
<p>Ссылка действует {{.TTL}} и срабатывает один раз.</p>
<p>Если вы не регистрировались, просто проигнорируйте это письмо.</p>
<p>— Tierl</p>`),
		LangEN: mustLetter(
			"Confirm your Tierl email",
			`Hi {{.Name}},

Confirm your email address to create tier lists in Tierl:
{{.URL}}

The link is valid for {{.TTL}} and can be used once.
If you did not sign up, just ignore this email.

— Tierl`,
			`<p>Hi {{.Name}},</p>
<p>Confirm your email address to create tier lists in Tierl:</p>
<p><a href="{{.URL}}">Confirm email</a></p>
<p>The link is valid for {{.TTL}} and can be used once.</p>
<p>If you did not sign up, just ignore this email.</p>
<p>— Tierl</p>`),
	},
	ResetPassword: {
		LangRU: mustLetter(
			"Сброс пароля в Tierl",
			`Привет, {{.Name}}!

Мы получили запрос на сброс пароля. Задайте новый пароль по ссылке:
{{.URL}}

Ссылка действует {{.TTL}} и срабатывает один раз. После смены пароля все
активные сессии завершаются.
Если вы не запрашивали сброс, просто проигнорируйте это письмо.

— Tierl`,
			`<p>Привет, {{.Name}}!</p>
<p>Мы получили запрос на сброс пароля. Задайте новый пароль по ссылке:</p>
<p><a href="{{.URL}}">Задать новый пароль</a></p>
<p>Ссылка действует {{.TTL}} и срабатывает один раз. После смены пароля все активные сессии завершаются.</p>
<p>Если вы не запрашивали сброс, просто проигнорируйте это письмо.</p>
<p>— Tierl</p>`),
		LangEN: mustLetter(
			"Reset your Tierl password",
			`Hi {{.Name}},

We received a password reset request. Choose a new password here:
{{.URL}}

The link is valid for {{.TTL}} and can be used once. After the change all
active sessions are signed out.
If you did not request a reset, just ignore this email.

— Tierl`,
			`<p>Hi {{.Name}},</p>
<p>We received a password reset request. Choose a new password here:</p>
<p><a href="{{.URL}}">Choose a new password</a></p>
<p>The link is valid for {{.TTL}} and can be used once. After the change all active sessions are signed out.</p>
<p>If you did not request a reset, just ignore this email.</p>
<p>— Tierl</p>`),
	},
}

func mustLetter(subject, text, html string) letter {
	return letter{
		subject: template.Must(template.New("subject").Parse(subject)),
		text:    template.Must(template.New("text").Parse(text)),
		html:    htmltemplate.Must(htmltemplate.New("html").Parse(html)),
	}
}

// Render builds a message for the kind and language. An unsupported language
// falls back to Russian; To is filled in by the caller.
func Render(lang Lang, kind Kind, d Data) (Message, error) {
	byLang, ok := letters[kind]
	if !ok {
		return Message{}, fmt.Errorf("mailer: unknown letter kind %q", kind)
	}
	l, ok := byLang[lang]
	if !ok {
		l = byLang[LangRU]
	}
	msg := Message{}
	var buf bytes.Buffer
	if err := l.subject.Execute(&buf, d); err != nil {
		return Message{}, fmt.Errorf("mailer: subject: %w", err)
	}
	msg.Subject = buf.String()
	buf.Reset()
	if err := l.text.Execute(&buf, d); err != nil {
		return Message{}, fmt.Errorf("mailer: text body: %w", err)
	}
	msg.Text = buf.String()
	buf.Reset()
	if err := l.html.Execute(&buf, d); err != nil {
		return Message{}, fmt.Errorf("mailer: html body: %w", err)
	}
	msg.HTML = buf.String()
	return msg, nil
}

// FormatTTL renders a token lifetime in the letter language, with Russian
// plural forms (1 час, 2 часа, 5 часов).
func FormatTTL(lang Lang, d time.Duration) string {
	hours := int(d / time.Hour)
	if hours <= 0 {
		hours = 1
	}
	if lang == LangEN {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	switch {
	case hours%100 >= 11 && hours%100 <= 14:
		return fmt.Sprintf("%d часов", hours)
	case hours%10 == 1:
		return fmt.Sprintf("%d час", hours)
	case hours%10 >= 2 && hours%10 <= 4:
		return fmt.Sprintf("%d часа", hours)
	default:
		return fmt.Sprintf("%d часов", hours)
	}
}
