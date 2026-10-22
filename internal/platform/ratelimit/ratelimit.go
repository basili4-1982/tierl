// Package ratelimit ограничивает частоту запросов по ключу (хэш IP, email).
//
// Лимитер работает по алгоритму token bucket: у каждого ключа своё ведро
// ёмкостью Burst, которое пополняется со скоростью одного токена за Interval.
// Часы подаются зависимостью, поэтому тесты обходятся без sleep.
package ratelimit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Decision - результат проверки одного запроса.
type Decision struct {
	// Allowed - запрос укладывается в лимит.
	Allowed bool
	// RetryAfter - через сколько можно повторить; для разрешённого запроса 0.
	RetryAfter time.Duration
}

// Limiter решает, пропускать ли запрос с данным ключом.
type Limiter interface {
	Allow(key string) Decision
}

// Clock возвращает текущее время.
type Clock func() time.Time

// Config задаёт параметры in-memory лимитера.
type Config struct {
	// Burst - ёмкость ведра: сколько запросов подряд допустимо.
	Burst int
	// Interval - время пополнения одного токена.
	Interval time.Duration
	// IdleTTL - через сколько неактивности ключ считается устаревшим.
	// Ноль означает время полного пополнения ведра (Burst * Interval).
	IdleTTL time.Duration
	// Now - источник времени; по умолчанию time.Now.
	Now Clock
}

// ErrInvalidConfig возвращается при некорректных параметрах.
var ErrInvalidConfig = errors.New("ratelimit: invalid config")

type bucket struct {
	tokens float64
	last   time.Time
}

// Memory - потокобезопасный in-memory Limiter.
type Memory struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	burst    float64
	interval time.Duration
	ttl      time.Duration
	now      Clock
	nextGC   time.Time
}

var _ Limiter = (*Memory)(nil)

// NewMemory создаёт лимитер. Устаревшие ключи удаляются попутно с вызовами
// Allow (не чаще раза в IdleTTL), фоновых горутин нет.
func NewMemory(cfg Config) (*Memory, error) {
	if cfg.Burst <= 0 || cfg.Interval <= 0 || cfg.IdleTTL < 0 {
		return nil, ErrInvalidConfig
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	ttl := cfg.IdleTTL
	if ttl == 0 {
		ttl = time.Duration(cfg.Burst) * cfg.Interval
	}
	return &Memory{
		buckets:  make(map[string]*bucket),
		burst:    float64(cfg.Burst),
		interval: cfg.Interval,
		ttl:      ttl,
		now:      now,
		nextGC:   now().Add(ttl),
	}, nil
}

// Allow списывает токен ключа или сообщает, когда повторить.
func (m *Memory) Allow(key string) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.gc(now)

	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: m.burst, last: now}
		m.buckets[key] = b
	} else if el := now.Sub(b.last); el > 0 {
		b.tokens = min(m.burst, b.tokens+float64(el)/float64(m.interval))
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return Decision{Allowed: true}
	}
	wait := time.Duration((1 - b.tokens) * float64(m.interval))
	return Decision{RetryAfter: wait}
}

// Len возвращает число отслеживаемых ключей.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}

// gc удаляет ключи, не тронутые дольше ttl. Вызывать под мьютексом.
func (m *Memory) gc(now time.Time) {
	if now.Before(m.nextGC) {
		return
	}
	for k, b := range m.buckets {
		if now.Sub(b.last) >= m.ttl {
			delete(m.buckets, k)
		}
	}
	m.nextGC = now.Add(m.ttl)
}

// HashIP возвращает hex(HMAC-SHA256(salt, ip)). Адрес нормализуется: порт
// отбрасывается, IPv6 приводится к каноническому виду. Пустая соль недопустима
// для вызывающего кода - проверяйте её при загрузке конфигурации.
func HashIP(salt []byte, ip string) string {
	ip = strings.TrimSpace(ip)
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		ip = parsed.String()
	}
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
