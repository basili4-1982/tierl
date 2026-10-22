package ratelimit

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"
)

// KeyFunc строит ключ лимита из запроса. Пустой ключ означает "не лимитировать".
type KeyFunc func(r *http.Request) string

// IPKey возвращает KeyFunc по хэшу IP из r.RemoteAddr с префиксом scope
// (например "login"), чтобы разные маршруты не делили ведро. Заголовки
// X-Forwarded-For не читаются: их доверие решает слой, расставляющий
// RemoteAddr за прокси.
func IPKey(salt []byte, scope string) KeyFunc {
	return func(r *http.Request) string {
		return scope + ":ip:" + HashIP(salt, r.RemoteAddr)
	}
}

// EmailKey строит ключ для email: нормализация (нижний регистр, без пробелов)
// вызывающей стороне не требуется. Для хранения email не хэшируется, ключ живёт
// только в памяти лимитера.
func EmailKey(scope, email string) string {
	return scope + ":email:" + normalizeEmail(email)
}

// Middleware отвечает 429 с Retry-After и телом Error по api/openapi.yaml,
// если лимит для ключа исчерпан.
func Middleware(l Limiter, key KeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			d := l.Allow(k)
			if d.Allowed {
				next.ServeHTTP(w, r)
				return
			}
			WriteLimited(w, d.RetryAfter)
		})
	}
}

// WriteLimited пишет ответ 429. Retry-After округляется вверх до секунды, минимум 1.
func WriteLimited(w http.ResponseWriter, retryAfter time.Duration) {
	secs := max(1, int(math.Ceil(retryAfter.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(errorBody{
		Code:    "rate_limited",
		Message: "Слишком много запросов",
		Details: map[string]any{"retry_after": secs},
	})
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}
