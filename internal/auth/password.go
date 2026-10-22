package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Default argon2id parameters: 64 MiB, 3 passes, 2 lanes. They are above the
// OWASP minimum (19 MiB, 2 passes, 1 lane); tune them to the deployment
// hardware through the configuration.
const (
	DefaultMemoryKiB   uint32 = 64 * 1024
	DefaultIterations  uint32 = 3
	DefaultParallelism uint8  = 2

	saltLen = 16
	keyLen  = 32

	// Upper bounds applied when parsing a stored hash, so a corrupted row
	// cannot make the server allocate gigabytes.
	maxMemoryKiB   = 1024 * 1024
	maxIterations  = 100
	minSaltLen     = 8
	maxSaltLen     = 64
	minKeyLen      = 16
	maxKeyLen      = 128
	phcPartsCount  = 6
	phcAlgorithm   = "argon2id"
	phcVersionText = "v=" // followed by argon2.Version
)

// HashParams are the argon2id cost parameters.
type HashParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultHashParams returns the safe default cost parameters.
func DefaultHashParams() HashParams {
	return HashParams{MemoryKiB: DefaultMemoryKiB, Iterations: DefaultIterations, Parallelism: DefaultParallelism}
}

// Validate reports whether the parameters are usable.
func (p HashParams) Validate() error {
	switch {
	case p.Iterations == 0 || p.Iterations > maxIterations:
		return fmt.Errorf("auth: argon2 iterations must be 1..%d", maxIterations)
	case p.Parallelism == 0:
		return errors.New("auth: argon2 parallelism must be at least 1")
	case p.MemoryKiB < 8*uint32(p.Parallelism) || p.MemoryKiB > maxMemoryKiB:
		return fmt.Errorf("auth: argon2 memory must be %d..%d KiB", 8*uint32(p.Parallelism), maxMemoryKiB)
	}
	return nil
}

// Hasher hashes and verifies passwords with argon2id. The zero value is not
// usable; build one with NewHasher.
type Hasher struct {
	params HashParams
	rand   io.Reader
}

// NewHasher returns a Hasher with the given parameters.
func NewHasher(p HashParams) (*Hasher, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &Hasher{params: p, rand: rand.Reader}, nil
}

// Params returns the cost parameters used for new hashes.
func (h *Hasher) Params() HashParams { return h.params }

// Hash returns the PHC string for password with a fresh random salt:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>
//
// salt and key are unpadded standard base64.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(h.rand, salt); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRandom, err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$%s$%s%d$m=%d,t=%d,p=%d$%s$%s", phcAlgorithm, phcVersionText, argon2.Version,
		h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether password matches the PHC hash. It uses the parameters
// stored in the hash and compares in constant time. A malformed hash yields
// ErrInvalidHash.
func (h *Hasher) Verify(password, encoded string) (bool, error) {
	ph, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), ph.salt, ph.params.Iterations, ph.params.MemoryKiB, ph.params.Parallelism, uint32(len(ph.key))) //nolint:gosec // key length is bounded by parsePHC
	return subtle.ConstantTimeCompare(got, ph.key) == 1, nil
}

// NeedsRehash reports whether the hash was made with parameters weaker or
// different from the current ones and should be recomputed after a successful
// login. A malformed hash yields ErrInvalidHash.
func (h *Hasher) NeedsRehash(encoded string) (bool, error) {
	ph, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	return ph.params != h.params || len(ph.key) != keyLen || len(ph.salt) != saltLen, nil
}

type phc struct {
	params HashParams
	salt   []byte
	key    []byte
}

func parsePHC(encoded string) (phc, error) {
	bad := func(reason string) (phc, error) {
		return phc{}, fmt.Errorf("%w: %s", ErrInvalidHash, reason)
	}
	// "$argon2id$v=19$m=..,t=..,p=..$salt$key" splits into an empty first part and five more.
	parts := strings.Split(encoded, "$")
	if len(parts) != phcPartsCount || parts[0] != "" {
		return bad("unexpected format")
	}
	if parts[1] != phcAlgorithm {
		return bad("unsupported algorithm")
	}
	if parts[2] != phcVersionText+strconv.Itoa(argon2.Version) {
		return bad("unsupported version")
	}

	var p HashParams
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return bad("bad parameters")
	}
	for i, prefix := range []string{"m=", "t=", "p="} {
		v, ok := strings.CutPrefix(fields[i], prefix)
		if !ok {
			return bad("bad parameters")
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return bad("bad parameters")
		}
		switch i {
		case 0:
			p.MemoryKiB = uint32(n)
		case 1:
			p.Iterations = uint32(n)
		default:
			if n > 255 {
				return bad("bad parameters")
			}
			p.Parallelism = uint8(n)
		}
	}
	if err := p.Validate(); err != nil {
		return bad("parameters out of range")
	}

	enc := base64.RawStdEncoding.Strict()
	salt, err := enc.DecodeString(parts[4])
	if err != nil || len(salt) < minSaltLen || len(salt) > maxSaltLen {
		return bad("bad salt")
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil || len(key) < minKeyLen || len(key) > maxKeyLen {
		return bad("bad key")
	}
	return phc{params: p, salt: salt, key: key}, nil
}
