package domain

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var fixed = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func deps() Deps {
	return Deps{Now: func() time.Time { return fixed }, Rand: rand.Reader}
}

func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: fmt.Sprintf("i%d", i), Label: fmt.Sprintf("Item %d", i), Position: i}
	}
	return out
}

func tiers(n int) []Tier {
	out := make([]Tier, n)
	for i := range out {
		out[i] = Tier{ID: fmt.Sprintf("t%d", i), Name: fmt.Sprintf("T%d", i), Color: "#ff7f7f", Position: i}
	}
	return out
}

func params() NewTierlistParams {
	return NewTierlistParams{
		ID: "id1", AuthorID: "u1", Title: "Title", Description: "d",
		Visibility: VisibilityUnlisted, Items: items(3), Tiers: tiers(2),
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestEnumsValid(t *testing.T) {
	for _, v := range []Visibility{VisibilityPublic, VisibilityUnlisted} {
		if !v.Valid() {
			t.Errorf("%q should be valid", v)
		}
	}
	if Visibility("x").Valid() {
		t.Error("unknown visibility valid")
	}
	for _, s := range []Status{StatusDraft, StatusOpen, StatusClosed, StatusHidden} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if Status("x").Valid() {
		t.Error("unknown status valid")
	}
}

func TestGenerateSlug(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		s, err := GenerateSlug(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(s) != SlugLength || len(s) < MinUnlistedSlugLength {
			t.Fatalf("bad length %d", len(s))
		}
		if err := ValidateSlug(s, VisibilityUnlisted); err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatal("duplicate slug")
		}
		seen[s] = true
	}
	// Deterministic source: all zero bytes map to the first alphabet symbol.
	s, _ := GenerateSlug(bytes.NewReader(make([]byte, SlugLength)))
	if s != strings.Repeat("A", SlugLength) {
		t.Errorf("got %q", s)
	}
	if _, err := GenerateSlug(failReader{}); !errors.Is(err, ErrRandom) {
		t.Errorf("want ErrRandom, got %v", err)
	}
	if _, err := GenerateSlug(bytes.NewReader(nil)); !errors.Is(err, ErrRandom) {
		t.Errorf("want ErrRandom on short read, got %v", err)
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
		v    Visibility
		ok   bool
	}{
		{"public short ok", "go", VisibilityPublic, false},
		{"public min", "abc", VisibilityPublic, true},
		{"public readable", "best-languages_1", VisibilityPublic, true},
		{"unlisted 11", strings.Repeat("a", 11), VisibilityUnlisted, false},
		{"unlisted 12", strings.Repeat("a", 12), VisibilityUnlisted, true},
		{"max", strings.Repeat("a", 64), VisibilityPublic, true},
		{"too long", strings.Repeat("a", 65), VisibilityPublic, false},
		{"bad chars", "abc def", VisibilityPublic, false},
		{"slash", "abc/def", VisibilityPublic, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlug(tc.slug, tc.v)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v ok=%v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalid) {
				t.Errorf("not ErrInvalid: %v", err)
			}
		})
	}
}

func TestNewTierlist(t *testing.T) {
	long := func(n int) string { return strings.Repeat("я", n) }
	tests := []struct {
		name string
		mod  func(*NewTierlistParams)
		ok   bool
	}{
		{"ok unlisted", func(*NewTierlistParams) {}, true},
		{"ok public", func(p *NewTierlistParams) { p.Visibility = VisibilityPublic; p.Slug = "best" }, true},
		{"empty id", func(p *NewTierlistParams) { p.ID = " " }, false},
		{"empty author", func(p *NewTierlistParams) { p.AuthorID = "" }, false},
		{"empty title", func(p *NewTierlistParams) { p.Title = "  " }, false},
		{"title 120 runes", func(p *NewTierlistParams) { p.Title = long(120) }, true},
		{"title 121 runes", func(p *NewTierlistParams) { p.Title = long(121) }, false},
		{"desc 1000", func(p *NewTierlistParams) { p.Description = long(1000) }, true},
		{"desc 1001", func(p *NewTierlistParams) { p.Description = long(1001) }, false},
		{"bad visibility", func(p *NewTierlistParams) { p.Visibility = "x" }, false},
		{"public no slug", func(p *NewTierlistParams) { p.Visibility = VisibilityPublic }, false},
		{"2 items", func(p *NewTierlistParams) { p.Items = items(2) }, false},
		{"50 items", func(p *NewTierlistParams) { p.Items = items(50) }, true},
		{"51 items", func(p *NewTierlistParams) { p.Items = items(51) }, false},
		{"1 tier", func(p *NewTierlistParams) { p.Tiers = tiers(1) }, false},
		{"10 tiers", func(p *NewTierlistParams) { p.Tiers = tiers(10) }, true},
		{"11 tiers", func(p *NewTierlistParams) { p.Tiers = tiers(11) }, false},
		{"dup item position", func(p *NewTierlistParams) { p.Items[1].Position = 0 }, false},
		{"item position out of range", func(p *NewTierlistParams) { p.Items[1].Position = 3 }, false},
		{"negative item position", func(p *NewTierlistParams) { p.Items[1].Position = -1 }, false},
		{"dup tier position", func(p *NewTierlistParams) { p.Tiers[1].Position = 0 }, false},
		{"item empty id", func(p *NewTierlistParams) { p.Items[0].ID = "" }, false},
		{"item empty label", func(p *NewTierlistParams) { p.Items[0].Label = " " }, false},
		{"item label 80", func(p *NewTierlistParams) { p.Items[0].Label = long(80) }, true},
		{"item label 81", func(p *NewTierlistParams) { p.Items[0].Label = long(81) }, false},
		{"image url too long", func(p *NewTierlistParams) { p.Items[0].ImageURL = strings.Repeat("a", MaxImageURLLen+1) }, false},
		{"image url ok", func(p *NewTierlistParams) { p.Items[0].ImageURL = "https://x/y.png" }, true},
		{"tier empty id", func(p *NewTierlistParams) { p.Tiers[0].ID = "" }, false},
		{"tier empty name", func(p *NewTierlistParams) { p.Tiers[0].Name = "" }, false},
		{"tier name 41", func(p *NewTierlistParams) { p.Tiers[0].Name = long(41) }, false},
		{"tier bad color", func(p *NewTierlistParams) { p.Tiers[0].Color = "red" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := params()
			tc.mod(&p)
			tl, err := NewTierlist(p, deps())
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v ok=%v", err, tc.ok)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalid) {
					t.Errorf("not ErrInvalid: %v", err)
				}
				return
			}
			if tl.Status != StatusDraft || !tl.CreatedAt.Equal(fixed) || !tl.UpdatedAt.Equal(fixed) {
				t.Errorf("bad initial state: %+v", tl)
			}
			if tl.Visibility == VisibilityUnlisted && len(tl.Slug) < MinUnlistedSlugLength {
				t.Errorf("short unlisted slug %q", tl.Slug)
			}
		})
	}
}

func TestNewTierlistUnlistedIgnoresSlugAndCopies(t *testing.T) {
	p := params()
	p.Slug = "mine"
	tl, err := NewTierlist(p, deps())
	if err != nil {
		t.Fatal(err)
	}
	if tl.Slug == "mine" {
		t.Error("caller slug must be ignored for unlisted")
	}
	p.Items[0].Label = "changed"
	if tl.Items[0].Label == "changed" {
		t.Error("items slice must be copied")
	}
}

func TestNewTierlistRandomFailure(t *testing.T) {
	d := deps()
	d.Rand = failReader{}
	_, err := NewTierlist(params(), d)
	if !errors.Is(err, ErrRandom) {
		t.Fatalf("want ErrRandom, got %v", err)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := invalid("title", "bad %d", 1)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "title" {
		t.Fatalf("errors.As failed: %v", err)
	}
	if err.Error() != "domain: invalid title: bad 1" {
		t.Errorf("got %q", err.Error())
	}
}

func TestLifecycle(t *testing.T) {
	later := fixed.Add(time.Hour)
	mk := func(s Status) *Tierlist { return &Tierlist{Status: s, UpdatedAt: fixed} }
	tests := []struct {
		name string
		from Status
		op   func(*Tierlist) error
		want Status
		ok   bool
	}{
		{"publish draft", StatusDraft, func(t *Tierlist) error { return t.Publish(later) }, StatusOpen, true},
		{"publish open", StatusOpen, func(t *Tierlist) error { return t.Publish(later) }, StatusOpen, false},
		{"close open", StatusOpen, func(t *Tierlist) error { return t.Close(later) }, StatusClosed, true},
		{"close draft", StatusDraft, func(t *Tierlist) error { return t.Close(later) }, StatusDraft, false},
		{"hide open", StatusOpen, func(t *Tierlist) error { return t.Hide(later) }, StatusHidden, true},
		{"hide hidden", StatusHidden, func(t *Tierlist) error { return t.Hide(later) }, StatusHidden, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tl := mk(tc.from)
			err := tc.op(tl)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("want ErrInvalidTransition, got %v", err)
			}
			if tl.Status != tc.want {
				t.Errorf("status %s want %s", tl.Status, tc.want)
			}
			wantTime := fixed
			if tc.ok {
				wantTime = later
			}
			if !tl.UpdatedAt.Equal(wantTime) {
				t.Errorf("UpdatedAt %v want %v", tl.UpdatedAt, wantTime)
			}
		})
	}
}

func TestAcceptsSubmissions(t *testing.T) {
	for s, want := range map[Status]bool{StatusDraft: false, StatusOpen: true, StatusClosed: false, StatusHidden: false} {
		if got := (&Tierlist{Status: s}).AcceptsSubmissions(); got != want {
			t.Errorf("%s: got %v", s, got)
		}
	}
}

func TestUpdateText(t *testing.T) {
	tl := &Tierlist{Title: "a", Description: "b"}
	if err := tl.UpdateText("", "x", fixed); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty title: %v", err)
	}
	if err := tl.UpdateText("t", strings.Repeat("a", 1001), fixed); !errors.Is(err, ErrInvalid) {
		t.Errorf("long desc: %v", err)
	}
	if tl.Title != "a" || tl.Description != "b" {
		t.Error("failed update must not change state")
	}
	if err := tl.UpdateText("t", "d", fixed); err != nil || tl.Title != "t" || tl.Description != "d" || !tl.UpdatedAt.Equal(fixed) {
		t.Errorf("update failed: %v %+v", err, tl)
	}
}

func TestReplaceStructure(t *testing.T) {
	tl := &Tierlist{Items: items(3), Tiers: tiers(2)}
	if err := tl.ReplaceStructure(items(4), tiers(3), 1, fixed); !errors.Is(err, ErrStructureLocked) {
		t.Errorf("want locked, got %v", err)
	}
	if err := tl.ReplaceStructure(items(1), tiers(3), 0, fixed); !errors.Is(err, ErrInvalid) {
		t.Errorf("want invalid, got %v", err)
	}
	if len(tl.Items) != 3 {
		t.Error("state changed on failure")
	}
	if err := tl.ReplaceStructure(items(4), tiers(3), 0, fixed); err != nil {
		t.Fatal(err)
	}
	if len(tl.Items) != 4 || len(tl.Tiers) != 3 || !tl.UpdatedAt.Equal(fixed) {
		t.Errorf("not replaced: %+v", tl)
	}
}
