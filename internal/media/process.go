package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Typed errors returned by Process; test with errors.Is.
var (
	// ErrEmpty means the upload has no bytes.
	ErrEmpty = errors.New("media: empty file")
	// ErrTooLarge means the upload exceeds Limits.MaxBytes.
	ErrTooLarge = errors.New("media: file too large")
	// ErrUnsupportedType means the content is not PNG, JPEG, WebP or GIF.
	ErrUnsupportedType = errors.New("media: unsupported image type")
	// ErrDimensionsTooLarge means the pixel dimensions exceed the limits.
	ErrDimensionsTooLarge = errors.New("media: image dimensions too large")
	// ErrCorrupt means the header or pixel data cannot be decoded.
	ErrCorrupt = errors.New("media: corrupt image")
)

// Content types produced and accepted.
const (
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
	TypeWebP = "image/webp"
	TypeGIF  = "image/gif"
)

// Limits bound the upload and the stored result.
type Limits struct {
	// MaxBytes caps the uploaded file size.
	MaxBytes int64
	// MaxSide caps width and height of the input header (decompression-bomb
	// guard, checked before decoding).
	MaxSide int
	// MaxPixels caps width*height of the input (checked before decoding).
	MaxPixels int
	// OutSide is the longest side of the stored image; larger images are
	// downscaled keeping the aspect ratio, smaller ones are not enlarged.
	OutSide int
}

// DefaultLimits suit element pictures: 5 MiB upload, up to 8000 px per side
// and 40 MP, stored at most 1024 px on the long side.
func DefaultLimits() Limits {
	return Limits{MaxBytes: 5 << 20, MaxSide: 8000, MaxPixels: 40_000_000, OutSide: 1024}
}

// Process validates an uploaded image and returns a sanitised re-encoding.
//
// The type is detected from content only (never from name or headers). Sizes
// are read from the header and checked before any pixel is decoded. The image
// is decoded and re-encoded, which drops EXIF and all other metadata: JPEG
// stays JPEG, PNG/WebP/GIF become PNG (animated GIFs keep only the first
// frame). The EXIF orientation tag is not applied.
func Process(r io.Reader, lim Limits) (data []byte, contentType string, err error) {
	raw, err := io.ReadAll(io.LimitReader(r, lim.MaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("media: read upload: %w", err)
	}
	if len(raw) == 0 {
		return nil, "", ErrEmpty
	}
	if int64(len(raw)) > lim.MaxBytes {
		return nil, "", ErrTooLarge
	}

	switch http.DetectContentType(raw) {
	case TypePNG, TypeJPEG, TypeGIF:
	default:
		if !isWebP(raw) {
			return nil, "", ErrUnsupportedType
		}
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrCorrupt, err) //nolint:errorlint // sentinel is the wrapped cause
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", fmt.Errorf("%w: zero size", ErrCorrupt)
	}
	if cfg.Width > lim.MaxSide || cfg.Height > lim.MaxSide || cfg.Width > lim.MaxPixels/cfg.Height {
		return nil, "", fmt.Errorf("%w: %dx%d", ErrDimensionsTooLarge, cfg.Width, cfg.Height)
	}

	img, format, err := decode(raw)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrCorrupt, err) //nolint:errorlint // sentinel is the wrapped cause
	}
	img = downscale(img, lim.OutSide)

	var buf bytes.Buffer
	if format == "jpeg" {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", fmt.Errorf("media: encode jpeg: %w", err)
		}
		return buf.Bytes(), TypeJPEG, nil
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("media: encode png: %w", err)
	}
	return buf.Bytes(), TypePNG, nil
}

// Key returns a content-addressed object name: sha256 of data plus extension.
func Key(data []byte, contentType string) string {
	sum := sha256.Sum256(data)
	ext := ".png"
	if contentType == TypeJPEG {
		ext = ".jpg"
	}
	return hex.EncodeToString(sum[:]) + ext
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

func decode(raw []byte) (image.Image, string, error) {
	if isWebP(raw) {
		img, err := webp.Decode(bytes.NewReader(raw))
		return img, "webp", err
	}
	if http.DetectContentType(raw) == TypeGIF {
		g, err := gif.Decode(bytes.NewReader(raw)) // first frame only
		return g, "gif", err
	}
	return image.Decode(bytes.NewReader(raw))
}

func downscale(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxSide <= 0 || (w <= maxSide && h <= maxSide) {
		return src
	}
	nw, nh := maxSide, maxSide
	if w >= h {
		nh = max(1, h*maxSide/w)
	} else {
		nw = max(1, w*maxSide/h)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}
