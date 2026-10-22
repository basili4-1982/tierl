package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Options configures an S3-compatible Storage (AWS S3, MinIO, ...).
type S3Options struct {
	Endpoint  string // host:port, no scheme
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	// PublicURL is the base URL objects are served from. Empty means
	// scheme://endpoint/bucket (path-style).
	PublicURL string
}

// S3 is a Storage backed by an S3-compatible bucket.
type S3 struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

// NewS3 builds the client. It does not contact the server; call EnsureBucket
// to create the bucket if it is absent.
func NewS3(o S3Options) (*S3, error) {
	cl, err := minio.New(o.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(o.AccessKey, o.SecretKey, ""),
		Secure: o.UseSSL,
		Region: o.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("media: s3 client: %w", err)
	}
	pub := o.PublicURL
	if pub == "" {
		scheme := "http"
		if o.UseSSL {
			scheme = "https"
		}
		pub = fmt.Sprintf("%s://%s/%s", scheme, o.Endpoint, o.Bucket)
	}
	return &S3{client: cl, bucket: o.Bucket, publicURL: pub}, nil
}

// EnsureBucket creates the bucket when it does not exist.
func (s *S3) EnsureBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("media: check bucket: %w", err)
	}
	if ok {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("media: make bucket: %w", err)
	}
	return nil
}

// Put uploads the object.
func (s *S3) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("media: s3 put: %w", err)
	}
	return nil
}

// Get downloads the object. The existence check is done eagerly (Stat), so a
// missing key returns ErrNotFound here rather than on first Read.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("media: s3 get: %w", err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("media: s3 stat: %w", err)
	}
	return obj, nil
}

// Delete removes the object; S3 treats a missing key as success.
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("media: s3 delete: %w", err)
	}
	return nil
}

// URL returns publicURL/key.
func (s *S3) URL(key string) string { return joinURL(s.publicURL, key) }
