package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"testing/iotest"
)

func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 5), B: 100, A: 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, testImage(w, h)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, testImage(w, h), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func gifBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.Encode(&b, testImage(10, 10), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// 1x1 lossless WebP.
const tinyWebP = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

// pngHeader forges a PNG that declares w x h but carries no pixel data.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestProcess_Valid(t *testing.T) {
	webp, _ := base64.StdEncoding.DecodeString(tinyWebP)
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"png", pngBytes(t, 20, 10), TypePNG},
		{"jpeg", jpegBytes(t, 20, 10), TypeJPEG},
		{"gif", gifBytes(t), TypePNG},
		{"webp", webp, TypePNG},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ct, err := Process(bytes.NewReader(tt.in), DefaultLimits())
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if ct != tt.want {
				t.Errorf("contentType = %q, want %q", ct, tt.want)
			}
			if _, _, err := image.DecodeConfig(bytes.NewReader(out)); err != nil {
				t.Errorf("output is not decodable: %v", err)
			}
		})
	}
}

func TestProcess_Downscale(t *testing.T) {
	lim := DefaultLimits()
	lim.OutSide = 50
	for name, size := range map[string][2]int{"landscape": {200, 100}, "portrait": {100, 200}, "square": {120, 120}} {
		t.Run(name, func(t *testing.T) {
			out, _, err := Process(bytes.NewReader(pngBytes(t, size[0], size[1])), lim)
			if err != nil {
				t.Fatal(err)
			}
			cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
			if max(cfg.Width, cfg.Height) != 50 {
				t.Errorf("got %dx%d, want longest side 50", cfg.Width, cfg.Height)
			}
		})
	}
	// Small images are not enlarged.
	out, _, _ := Process(bytes.NewReader(pngBytes(t, 20, 10)), lim)
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Width != 20 || cfg.Height != 10 {
		t.Errorf("small image resized to %dx%d", cfg.Width, cfg.Height)
	}
	// Extreme aspect ratio keeps at least one pixel.
	out, _, err := Process(bytes.NewReader(pngBytes(t, 400, 1)), lim)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(out)); cfg.Height != 1 {
		t.Errorf("height = %d, want 1", cfg.Height)
	}
}

func TestProcess_StripsEXIF(t *testing.T) {
	src := jpegBytes(t, 16, 16)
	payload := append([]byte("Exif\x00\x00"), []byte("SECRET-GPS-DATA")...)
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	withExif := append(append(append([]byte{}, src[:2]...), seg...), append(payload, src[2:]...)...)
	if !bytes.Contains(withExif, []byte("SECRET-GPS-DATA")) {
		t.Fatal("fixture broken")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(withExif)); err != nil {
		t.Fatalf("fixture not decodable: %v", err)
	}
	out, ct, err := Process(bytes.NewReader(withExif), DefaultLimits())
	if err != nil || ct != TypeJPEG {
		t.Fatalf("Process: %v %q", err, ct)
	}
	if bytes.Contains(out, []byte("SECRET-GPS-DATA")) || bytes.Contains(out, []byte("Exif")) {
		t.Error("EXIF survived re-encoding")
	}
}

func TestProcess_Rejects(t *testing.T) {
	good := pngBytes(t, 20, 20)
	small := Limits{MaxBytes: 1 << 20, MaxSide: 100, MaxPixels: 5000, OutSide: 64}
	tests := []struct {
		name string
		in   []byte
		lim  Limits
		want error
	}{
		{"empty", nil, DefaultLimits(), ErrEmpty},
		{"too large", good, Limits{MaxBytes: int64(len(good)) - 1, MaxSide: 100, MaxPixels: 1e6, OutSide: 64}, ErrTooLarge},
		{"exactly at limit passes header checks", good, Limits{MaxBytes: int64(len(good)), MaxSide: 100, MaxPixels: 1e6, OutSide: 64}, nil},
		{"text", []byte("hello, definitely not an image"), DefaultLimits(), ErrUnsupportedType},
		{"html named png", []byte("<html><script>alert(1)</script></html>"), DefaultLimits(), ErrUnsupportedType},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), DefaultLimits(), ErrUnsupportedType},
		{"bmp", append([]byte("BM"), make([]byte, 64)...), DefaultLimits(), ErrUnsupportedType},
		{"pdf", []byte("%PDF-1.7 ..."), DefaultLimits(), ErrUnsupportedType},
		{"riff but not webp", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), DefaultLimits(), ErrUnsupportedType},
		{"side too large", pngBytes(t, 101, 1), small, ErrDimensionsTooLarge},
		{"pixels too large", pngBytes(t, 90, 90), small, ErrDimensionsTooLarge},
		{"decompression bomb header", pngHeader(60000, 60000), DefaultLimits(), ErrDimensionsTooLarge},
		{"zero size header", pngHeader(0, 5), DefaultLimits(), ErrCorrupt},
		{"png signature only", []byte("\x89PNG\r\n\x1a\n"), DefaultLimits(), ErrCorrupt},
		{"truncated png", good[:len(good)-40], DefaultLimits(), ErrCorrupt},
		{"truncated jpeg", jpegBytes(t, 40, 40)[:200], DefaultLimits(), ErrCorrupt},
		{"truncated webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 "), DefaultLimits(), ErrCorrupt},
		{"truncated gif", gifBytes(t)[:20], DefaultLimits(), ErrCorrupt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ct, err := Process(bytes.NewReader(tt.in), tt.lim)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if out != nil || ct != "" {
				t.Error("data returned together with an error")
			}
		})
	}
}

// A large body is cut at MaxBytes+1 and never fully buffered.
func TestProcess_OversizeStreamIsBounded(t *testing.T) {
	r := &countingReader{r: strings.NewReader(strings.Repeat("A", 1<<20))}
	_, _, err := Process(r, Limits{MaxBytes: 1000, MaxSide: 10, MaxPixels: 100, OutSide: 10})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if r.n > 1001+4096 {
		t.Errorf("read %d bytes for a 1000-byte limit", r.n)
	}
}

type countingReader struct {
	r *strings.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestProcess_ReadError(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := Process(iotest.ErrReader(boom), DefaultLimits())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestKey(t *testing.T) {
	a := Key([]byte("x"), TypePNG)
	if a != Key([]byte("x"), TypePNG) || a == Key([]byte("y"), TypePNG) {
		t.Error("key is not content-addressed")
	}
	if !strings.HasSuffix(a, ".png") || !strings.HasSuffix(Key([]byte("x"), TypeJPEG), ".jpg") {
		t.Errorf("unexpected extensions: %s", a)
	}
	if err := ValidateKey(a); err != nil {
		t.Errorf("generated key invalid: %v", err)
	}
}
