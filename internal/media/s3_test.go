package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
)

// S3 tests run against a real MinIO (testcontainers). Without Docker they
// are skipped; TIERL_REQUIRE_DOCKER=1 (CI) turns the skip into a failure.
// TIERL_TEST_S3_ENDPOINT may point at an already running MinIO instead.

const (
	minioUser = "tierltest"
	minioPass = "tierltest-secret"
)

var (
	minioOnce sync.Once
	minioEP   string
	minioErr  error
)

func minioEndpoint(t *testing.T) string {
	t.Helper()
	minioOnce.Do(func() {
		if ep := os.Getenv("TIERL_TEST_S3_ENDPOINT"); ep != "" {
			minioEP = ep
			return
		}
		defer func() {
			// testcontainers panics when no Docker provider is available.
			if r := recover(); r != nil {
				minioErr = fmt.Errorf("%v", r)
			}
		}()
		ctx := context.Background()
		ctr, err := tcminio.Run(ctx, "cgr.dev/chainguard/minio@sha256:d88920d6a50bd0b088223b688be3b76c63e6205dbe494437f67ba1240ea12880",
			tcminio.WithUsername(minioUser), tcminio.WithPassword(minioPass))
		if err != nil {
			minioErr = err
			return
		}
		minioEP, minioErr = ctr.ConnectionString(ctx)
		// The container is reaped by testcontainers' Ryuk when the test binary exits.
		_ = testcontainers.Container(ctr)
	})
	if minioErr != nil {
		if os.Getenv("TIERL_REQUIRE_DOCKER") != "" {
			t.Fatalf("MinIO is required but unavailable: %v", minioErr)
		}
		t.Skipf("MinIO unavailable (set TIERL_REQUIRE_DOCKER=1 to fail): %v", minioErr)
	}
	return minioEP
}

func newTestS3(t *testing.T, bucket, publicURL string) *S3 {
	t.Helper()
	s, err := NewS3(S3Options{
		Endpoint: minioEndpoint(t), Bucket: bucket, AccessKey: minioUser, SecretKey: minioPass,
		Region: "us-east-1", PublicURL: publicURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3_Contract(t *testing.T) {
	s := newTestS3(t, "contract", "https://cdn.example.com/m")
	storageContract(t, s, "https://cdn.example.com/m/")
}

func TestS3_EnsureBucketIdempotentAndDefaultURL(t *testing.T) {
	s := newTestS3(t, "idem", "")
	if err := s.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("second EnsureBucket: %v", err)
	}
	want := "http://" + minioEndpoint(t) + "/idem/k.png"
	if got := s.URL("k.png"); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestS3_MissingBucketErrors(t *testing.T) {
	s, err := NewS3(S3Options{Endpoint: minioEndpoint(t), Bucket: "nope", AccessKey: minioUser, SecretKey: minioPass})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Put(ctx, "a.png", []byte("x"), TypePNG); err == nil {
		t.Error("Put into a missing bucket must fail")
	}
	if _, err := s.Get(ctx, "a.png"); err == nil {
		t.Error("Get from a missing bucket must fail")
	}
}

func TestS3_BadCredentials(t *testing.T) {
	s, err := NewS3(S3Options{Endpoint: minioEndpoint(t), Bucket: "contract", AccessKey: "x", SecretKey: "wrong-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(context.Background()); err == nil {
		t.Error("EnsureBucket with bad credentials must fail")
	}
}

func TestNewS3_Options(t *testing.T) {
	s, err := NewS3(S3Options{Endpoint: "s3.example.com", Bucket: "b", AccessKey: "a", SecretKey: "s", UseSSL: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.URL("k.png"); got != "https://s3.example.com/b/k.png" {
		t.Errorf("URL = %q", got)
	}
	if _, err := NewS3(S3Options{Endpoint: "http://bad/endpoint", Bucket: "b"}); err == nil {
		t.Error("endpoint with scheme/path must be rejected")
	}
}

func TestS3_UnreachableServer(t *testing.T) {
	s, err := NewS3(S3Options{Endpoint: "127.0.0.1:1", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.EnsureBucket(ctx); err == nil {
		t.Error("EnsureBucket must fail")
	}
	if _, err := s.Get(ctx, "a.png"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Get: %v", err)
	}
	if err := s.Delete(ctx, "a.png"); err == nil {
		t.Error("Delete must fail")
	}
}
