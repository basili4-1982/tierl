package api

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/basili4-1982/tierl/internal/domain"
	"github.com/basili4-1982/tierl/internal/tierlist"
)

const (
	ogW = 1200
	ogH = 630
)

// OGSpec is the content of a social preview image.
type OGSpec struct {
	Kind, Title, Subtitle, Author, Emoji string
	// The fields below are optional and let the result preview show the board.
	Tiers      []domain.Tier
	Items      []domain.Item
	Placements []tierlist.Placement
}

var (
	ogFontOnce sync.Once
	ogFont     *opentype.Font
	ogFontErr  error
)

func ogTypeface() (*opentype.Font, error) {
	ogFontOnce.Do(func() {
		ogFont, ogFontErr = opentype.Parse(goregular.TTF)
	})
	return ogFont, ogFontErr
}

func ogFace(size float64) (font.Face, error) {
	f, err := ogTypeface()
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// renderOG draws a deterministic 1200x630 PNG preview.
func renderOG(s OGSpec) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, ogW, ogH))

	// Deep blue gradient background.
	for y := 0; y < ogH; y++ {
		t := float64(y) / float64(ogH-1)
		c := lerpColor(color.RGBA{R: 15, G: 23, B: 42, A: 255}, color.RGBA{R: 30, G: 58, B: 138, A: 255}, t)
		draw.Draw(img, image.Rect(0, y, ogW, y+1), image.NewUniform(c), image.Point{}, draw.Src)
	}

	// Accent stripe and a subtle top-right glow.
	draw.Draw(img, image.Rect(0, 0, 12, ogH), image.NewUniform(color.RGBA{R: 56, G: 189, B: 248, A: 255}), image.Point{}, draw.Src)
	drawCircle(img, 1090, 80, 210, color.RGBA{R: 56, G: 189, B: 248, A: 24})

	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = "TIERL"
	}
	subtitle := strings.TrimSpace(s.Subtitle)

	drawString(img, "TIERL", 72, 92, 28, color.RGBA{R: 125, G: 211, B: 252, A: 255})
	if s.Emoji != "" {
		drawString(img, s.Emoji, 1020, 92, 42, color.White)
	}
	drawString(img, "СЕРВИС ТИР-ЛИСТОВ", 72, 130, 20, color.RGBA{R: 148, G: 163, B: 184, A: 255})

	titleSize := 62.0
	titleLines := wrapText(title, titleSize, float64(ogW-144), 3)
	if len(titleLines) == 1 {
		titleSize = 78
		titleLines = wrapText(title, titleSize, float64(ogW-144), 3)
	}
	y := 230.0
	for _, line := range titleLines {
		drawString(img, line, 72, y, titleSize, color.White)
		y += titleSize * 1.18
	}

	if subtitle != "" {
		y += 8
		subtitleLines := wrapText(subtitle, 32, float64(ogW-144), 2)
		for _, line := range subtitleLines {
			drawString(img, line, 72, y, 32, color.RGBA{R: 199, G: 218, B: 255, A: 255})
			y += 42
		}
	}

	if len(s.Tiers) > 0 && len(s.Placements) > 0 && y < 480 {
		drawBoard(img, s, y+10)
	}

	if s.Author != "" {
		drawString(img, "@"+s.Author, 72, float64(ogH-48), 26, color.RGBA{R: 148, G: 163, B: 184, A: 255})
	}
	drawString(img, "tierl", float64(ogW-160), float64(ogH-48), 26, color.RGBA{R: 125, G: 211, B: 252, A: 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode og png: %w", err)
	}
	return buf.Bytes(), nil
}

func drawBoard(img draw.Image, s OGSpec, startY float64) {
	tiers := append([]domain.Tier(nil), s.Tiers...)
	sortTiers(tiers)
	items := make(map[string]string, len(s.Items))
	for _, it := range s.Items {
		items[it.ID] = it.Label
	}
	byTier := make(map[string][]string, len(tiers))
	for _, p := range s.Placements {
		byTier[p.TierID] = append(byTier[p.TierID], items[p.ItemID])
	}
	if len(tiers) > 6 {
		tiers = tiers[:6]
	}
	rowH := math.Min(42, math.Max(22, (float64(ogH-70)-startY)/float64(len(tiers))))
	for i, tr := range tiers {
		top := startY + float64(i)*rowH
		if top+rowH > float64(ogH-70) {
			break
		}
		col, err := parseHexColor(tr.Color)
		if err != nil {
			col = color.RGBA{R: 71, G: 85, B: 105, A: 255}
		}
		draw.Draw(img, image.Rect(72, int(top), 270, int(top+rowH-4)), image.NewUniform(col), image.Point{}, draw.Src)
		var labelColor color.Color = color.White
		if isLight(col) {
			labelColor = color.RGBA{R: 15, G: 23, B: 42, A: 255}
		}
		drawString(img, truncate(tr.Name, 18), 84, top+rowH*0.66, math.Min(24, rowH*0.55), labelColor)
		line := strings.Join(byTier[tr.ID], ", ")
		drawString(img, truncate(line, 52), 288, top+rowH*0.66, math.Min(24, rowH*0.55), color.White)
	}
}

func sortTiers(tiers []domain.Tier) {
	for i := 1; i < len(tiers); i++ {
		for j := i; j > 0 && tiers[j].Position < tiers[j-1].Position; j-- {
			tiers[j], tiers[j-1] = tiers[j-1], tiers[j]
		}
	}
}

func drawCircle(dst draw.Image, cx, cy, r int, c color.Color) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				dst.Set(cx+x, cy+y, c)
			}
		}
	}
}

func drawString(dst draw.Image, text string, x, y, size float64, c color.Color) {
	if text == "" {
		return
	}
	face, err := ogFace(size)
	if err != nil {
		return
	}
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(int(math.Round(x)), int(math.Round(y))),
	}
	d.DrawString(text)
}

func wrapText(text string, size, maxWidth float64, maxLines int) []string {
	face, err := ogFace(size)
	if err != nil {
		return []string{truncate(text, 40)}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if font.MeasureString(face, candidate).Ceil() <= int(maxWidth) {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
		if len(lines) == maxLines {
			lines[len(lines)-1] = truncate(lines[len(lines)-1], 60) + "…"
			return lines
		}
	}
	lines = append(lines, current)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = truncate(lines[maxLines-1], 60) + "…"
	}
	return lines
}

func truncate(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

func parseHexColor(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("bad color %q", s)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}, nil
}

func isLight(c color.RGBA) bool {
	return 0.299*float64(c.R)+0.587*float64(c.G)+0.114*float64(c.B) > 160
}
