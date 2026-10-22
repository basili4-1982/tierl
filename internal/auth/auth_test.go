package auth

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

var fast = HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}

type memUsers struct {
	mu       sync.Mutex
	byID     map[string]*User
	failGet  error
	failUpd  error
	failMake error
	updates  int
}

func newMem() *memUsers { return &memUsers{byID: map[string]*User{}} }

func (m *memUsers) Create(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMake != nil {
		return m.failMake
	}
	for _, x := range m.byID {
		if x.Email == u.Email {
			return ErrEmailTaken
		}
	}
	c := *u
	m.byID[u.ID] = &c
	return nil
}

func (m *memUsers) GetByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGet != nil {
		return nil, m.failGet
	}
	for _, x := range m.byID {
		if x.Email == email {
			c := *x
			return &c, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *memUsers) GetByID(_ context.Context, id string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if x, ok := m.byID[id]; ok {
		c := *x
		return &c, nil
	}
	return nil, ErrUserNotFound
}

func (m *memUsers) UpdatePasswordHash(_ context.Context, id, hash string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpd != nil {
		return m.failUpd
	}
	x, ok := m.byID[id]
	if !ok {
		return ErrUserNotFound
	}
	x.PasswordHash, x.UpdatedAt = hash, now
	m.updates++
	return nil
}

func newSvc(t *testing.T, u Users) *Service {
	t.Helper()
	s, err := NewService(u, Options{Hash: fast, Now: func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const goodPw = "correct-horse-battery"

func TestHashVerify(t *testing.T) {
	h, err := NewHasher(fast)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := h.Hash(goodPw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("unexpected PHC: %s", enc)
	}
	if ok, err := h.Verify(goodPw, enc); err != nil || !ok {
		t.Fatalf("verify: %v %v", ok, err)
	}
	if ok, err := h.Verify("wrong-password", enc); err != nil || ok {
		t.Fatalf("wrong password: %v %v", ok, err)
	}
	if h.Params() != fast {
		t.Fatal("params mismatch")
	}
}

func TestHashSaltsDiffer(t *testing.T) {
	h, _ := NewHasher(fast)
	a, _ := h.Hash(goodPw)
	b, _ := h.Hash(goodPw)
	if a == b {
		t.Fatal("same password produced identical hashes: salt is not random")
	}
}

func TestHashPortable(t *testing.T) {
	// Produced by another hasher with other parameters: Verify must use the
	// parameters stored in the string, not its own.
	strong, _ := NewHasher(HashParams{MemoryKiB: 256, Iterations: 2, Parallelism: 2})
	enc, _ := strong.Hash(goodPw)
	weak, _ := NewHasher(fast)
	if ok, err := weak.Verify(goodPw, enc); err != nil || !ok {
		t.Fatalf("cross-parameter verify: %v %v", ok, err)
	}
	// Reference vector from the argon2 reference implementation (argon2id,
	// v=19, m=65536, t=2, p=1, password "password", salt "somesalt").
	const ref = "$argon2id$v=19$m=65536,t=2,p=1$c29tZXNhbHQ$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc"
	if ok, err := weak.Verify("password", ref); err != nil || !ok {
		t.Fatalf("reference vector: %v %v", ok, err)
	}
}

func TestNeedsRehash(t *testing.T) {
	old, _ := NewHasher(fast)
	enc, _ := old.Hash(goodPw)
	if need, err := old.NeedsRehash(enc); err != nil || need {
		t.Fatalf("same params: %v %v", need, err)
	}
	newer, _ := NewHasher(HashParams{MemoryKiB: 128, Iterations: 1, Parallelism: 1})
	if need, err := newer.NeedsRehash(enc); err != nil || !need {
		t.Fatalf("changed params: %v %v", need, err)
	}
	if _, err := newer.NeedsRehash("garbage"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("want ErrInvalidHash, got %v", err)
	}
}

func TestParseInvalid(t *testing.T) {
	h, _ := NewHasher(fast)
	good, _ := h.Hash(goodPw)
	parts := strings.Split(good, "$") // "", argon2id, v=19, params, salt, key
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}
	tests := map[string]string{
		"empty":           "",
		"too few parts":   "$argon2id$v=19$m=64,t=1,p=1$c2FsdA",
		"leading junk":    "x" + good,
		"wrong algo":      with(1, "argon2i"),
		"wrong version":   with(2, "v=16"),
		"missing param":   with(3, "m=64,t=1"),
		"wrong order":     with(3, "t=1,m=64,p=1"),
		"non numeric":     with(3, "m=x,t=1,p=1"),
		"p overflow":      with(3, "m=64,t=1,p=300"),
		"zero iters":      with(3, "m=64,t=0,p=1"),
		"huge memory":     with(3, "m=4294967295,t=1,p=1"),
		"bad salt b64":    with(4, "!!!"),
		"short salt":      with(4, "AAAA"),
		"bad key b64":     with(5, "***"),
		"short key":       with(5, "AAAA"),
		"padded salt":     with(4, parts[4]+"=="),
		"huge iterations": with(3, "m=64,t=101,p=1"),
	}
	for name, enc := range tests {
		t.Run(name, func(t *testing.T) {
			if ok, err := h.Verify(goodPw, enc); ok || !errors.Is(err, ErrInvalidHash) {
				t.Fatalf("want ErrInvalidHash, got ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestHashParamsValidate(t *testing.T) {
	bad := []HashParams{
		{MemoryKiB: 64, Iterations: 0, Parallelism: 1},
		{MemoryKiB: 64, Iterations: 1, Parallelism: 0},
		{MemoryKiB: 4, Iterations: 1, Parallelism: 1},
		{MemoryKiB: maxMemoryKiB + 1, Iterations: 1, Parallelism: 1},
	}
	for _, p := range bad {
		if _, err := NewHasher(p); err == nil {
			t.Errorf("%+v must be rejected", p)
		}
	}
	if err := DefaultHashParams().Validate(); err != nil {
		t.Fatal(err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestRandomFailure(t *testing.T) {
	h, _ := NewHasher(fast)
	h.rand = errReader{}
	if _, err := h.Hash(goodPw); !errors.Is(err, ErrRandom) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v", err)
	}
	if _, err := newID(errReader{}); !errors.Is(err, ErrRandom) {
		t.Fatalf("got %v", err)
	}
	if _, err := NewService(newMem(), Options{Hash: fast, Rand: errReader{}}); !errors.Is(err, ErrRandom) {
		t.Fatalf("NewService: %v", err)
	}
	s := newSvc(t, newMem())
	s.rand = errReader{}
	if _, err := s.Register(context.Background(), RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"}); !errors.Is(err, ErrRandom) {
		t.Fatalf("Register id: %v", err)
	}
	s.rand = strings.NewReader(strings.Repeat("x", 64))
	s.hasher.rand = errReader{}
	if _, err := s.Register(context.Background(), RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"}); !errors.Is(err, ErrRandom) {
		t.Fatalf("Register salt: %v", err)
	}
}

func TestNewServiceDefaultsAndErrors(t *testing.T) {
	s, err := NewService(newMem(), Options{Hash: HashParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1}})
	if err != nil || s.now == nil {
		t.Fatal(err)
	}
	if _, err := NewService(newMem(), Options{Hash: HashParams{MemoryKiB: 1, Iterations: 1, Parallelism: 1}}); err == nil {
		t.Fatal("bad params must fail")
	}
	d, err := NewService(newMem(), Options{})
	if err != nil || d.hasher.Params() != DefaultHashParams() {
		t.Fatalf("defaults: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	svc := newSvc(t, newMem())
	long := strings.Repeat("я", 129)
	tests := []struct {
		name  string
		in    RegisterInput
		field string
	}{
		{"empty email", RegisterInput{"", goodPw, "A"}, "email"},
		{"no at", RegisterInput{"anna.example.com", goodPw, "A"}, "email"},
		{"display name in email", RegisterInput{"Bob <bob@example.com>", goodPw, "A"}, "email"},
		{"no dot in domain", RegisterInput{"a@localhost", goodPw, "A"}, "email"},
		{"email too long", RegisterInput{strings.Repeat("a", 250) + "@b.co", goodPw, "A"}, "email"},
		{"short password", RegisterInput{"a@b.co", "123456789", "A"}, "password"},
		{"long password", RegisterInput{"a@b.co", long, "A"}, "password"},
		{"empty name", RegisterInput{"a@b.co", goodPw, "   "}, "display_name"},
		{"long name", RegisterInput{"a@b.co", goodPw, strings.Repeat("x", 51)}, "display_name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Register(context.Background(), tt.in)
			var ve *ValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &ve) || ve.Field != tt.field {
				t.Fatalf("want validation error on %s, got %v", tt.field, err)
			}
		})
	}
}

func TestPasswordLengthBoundaries(t *testing.T) {
	for _, n := range []int{10, 128} {
		if err := ValidatePassword(strings.Repeat("я", n)); err != nil {
			t.Errorf("%d runes must be accepted: %v", n, err)
		}
	}
	for _, n := range []int{0, 9, 129} {
		if err := ValidatePassword(strings.Repeat("я", n)); err == nil {
			t.Errorf("%d runes must be rejected", n)
		}
	}
}

func TestRegisterLogin(t *testing.T) {
	mem := newMem()
	svc := newSvc(t, mem)
	ctx := context.Background()
	u, err := svc.Register(ctx, RegisterInput{Email: "  Anna@Example.COM ", Password: goodPw, DisplayName: " Anna "})
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "anna@example.com" || u.DisplayName != "Anna" || u.EmailVerified || len(u.ID) != 36 || u.ID[14] != '4' {
		t.Fatalf("unexpected user: %+v", u)
	}
	if strings.Contains(u.PasswordHash, goodPw) {
		t.Fatal("hash contains the password")
	}

	got, err := svc.Login(ctx, "ANNA@example.com", goodPw)
	if err != nil || got.ID != u.ID {
		t.Fatalf("login: %v %+v", err, got)
	}
	back, err := svc.Get(ctx, u.ID)
	if err != nil || back.Email != u.Email {
		t.Fatalf("get: %v", err)
	}
	if _, err := svc.Get(ctx, "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("want ErrUserNotFound, got %v", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc := newSvc(t, newMem())
	ctx := context.Background()
	if _, err := svc.Register(ctx, RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Register(ctx, RegisterInput{Email: "A@B.CO", Password: goodPw, DisplayName: "B"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
}

func TestRegisterStorageError(t *testing.T) {
	mem := newMem()
	boom := errors.New("boom")
	mem.failMake = boom
	_, err := newSvc(t, mem).Register(context.Background(), RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestLoginIndistinguishable(t *testing.T) {
	svc := newSvc(t, newMem())
	ctx := context.Background()
	if _, err := svc.Register(ctx, RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"}); err != nil {
		t.Fatal(err)
	}
	cases := map[string][2]string{
		"wrong password": {"a@b.co", "wrong-password-1"},
		"unknown email":  {"nobody@b.co", goodPw},
		"malformed":      {"not-an-email", goodPw},
		"huge password":  {"a@b.co", strings.Repeat("x", 5000)},
		"empty":          {"", ""},
	}
	var first error
	for name, c := range cases {
		_, err := svc.Login(ctx, c[0], c[1])
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: want ErrInvalidCredentials, got %v", name, err)
		}
		if first == nil {
			first = err
		} else if err.Error() != first.Error() {
			t.Fatalf("%s: message differs: %q vs %q", name, err, first)
		}
	}
}

func TestLoginUnknownEmailStillHashes(t *testing.T) {
	// Not a timing assertion (flaky); checks that the dummy hash is a valid
	// PHC string with the configured parameters, i.e. the dummy path does real work.
	svc := newSvc(t, newMem())
	if need, err := svc.hasher.NeedsRehash(svc.dummyHash); err != nil || need {
		t.Fatalf("dummy hash: need=%v err=%v", need, err)
	}
}

func TestLoginStorageErrors(t *testing.T) {
	mem := newMem()
	svc := newSvc(t, mem)
	ctx := context.Background()
	boom := errors.New("db down")
	mem.failGet = boom
	_, err := svc.Login(ctx, "a@b.co", goodPw)
	if !errors.Is(err, boom) || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v", err)
	}

	mem.failGet = nil
	mem.byID["x"] = &User{ID: "x", Email: "a@b.co", PasswordHash: "corrupt"}
	if _, err := svc.Login(ctx, "a@b.co", goodPw); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("got %v", err)
	}
}

func TestLoginRehash(t *testing.T) {
	mem := newMem()
	ctx := context.Background()
	oldSvc := newSvc(t, mem)
	u, err := oldSvc.Register(ctx, RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := NewService(mem, Options{Hash: HashParams{MemoryKiB: 128, Iterations: 2, Parallelism: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newer.Login(ctx, "a@b.co", "wrong-password-1"); !errors.Is(err, ErrInvalidCredentials) || mem.updates != 0 {
		t.Fatalf("failed login must not rehash: %v updates=%d", err, mem.updates)
	}
	got, err := newer.Login(ctx, "a@b.co", goodPw)
	if err != nil {
		t.Fatal(err)
	}
	if mem.updates != 1 || got.PasswordHash == u.PasswordHash || !strings.Contains(got.PasswordHash, "m=128,t=2,p=1") {
		t.Fatalf("hash not upgraded: updates=%d %s", mem.updates, got.PasswordHash)
	}
	// Upgraded hash still verifies and is not rehashed again.
	if _, err := newer.Login(ctx, "a@b.co", goodPw); err != nil || mem.updates != 1 {
		t.Fatalf("second login: %v updates=%d", err, mem.updates)
	}
}

func TestLoginRehashFailureIsIgnored(t *testing.T) {
	mem := newMem()
	ctx := context.Background()
	if _, err := newSvc(t, mem).Register(ctx, RegisterInput{Email: "a@b.co", Password: goodPw, DisplayName: "A"}); err != nil {
		t.Fatal(err)
	}
	newer, _ := NewService(mem, Options{Hash: HashParams{MemoryKiB: 128, Iterations: 1, Parallelism: 1}})
	mem.failUpd = errors.New("read-only")
	if _, err := newer.Login(ctx, "a@b.co", goodPw); err != nil {
		t.Fatalf("login must succeed despite failed rehash: %v", err)
	}
	// Salt source failure during rehash is ignored too.
	newer.hasher.rand = errReader{}
	mem.failUpd = nil
	if _, err := newer.Login(ctx, "a@b.co", goodPw); err != nil {
		t.Fatalf("login must succeed despite rehash hashing failure: %v", err)
	}
}
