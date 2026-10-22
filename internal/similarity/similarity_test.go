package similarity

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func opts(t int) Options { return DefaultOptions(t) }

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions(5)
	want := Options{Tiers: 5, MinOverlap: DefaultMinOverlap, Threshold: DefaultThreshold, MinAnswers: DefaultMinAnswers}
	if o != want {
		t.Fatalf("got %+v, want %+v", o, want)
	}
}

func TestSimilarity(t *testing.T) {
	tests := []struct {
		name   string
		a, b   Answer
		opts   Options
		want   float64
		wantOK bool
	}{
		{"identical", Answer{"a": 0, "b": 1, "c": 2}, Answer{"a": 0, "b": 1, "c": 2}, opts(3), 1, true},
		{"opposite T=2", Answer{"a": 0, "b": 0, "c": 0}, Answer{"a": 1, "b": 1, "c": 1}, opts(2), 0, true},
		{"opposite T=5", Answer{"a": 0, "b": 4, "c": 0}, Answer{"a": 4, "b": 0, "c": 4}, opts(5), 0, true},
		{"one tier off of 3 items T=5", Answer{"a": 0, "b": 1, "c": 2}, Answer{"a": 1, "b": 1, "c": 2}, opts(5), 1 - 1.0/12, true},
		{"two tiers off weighs double", Answer{"a": 0, "b": 1, "c": 2}, Answer{"a": 2, "b": 1, "c": 2}, opts(5), 1 - 2.0/12, true},
		{"partial overlap uses common only", Answer{"a": 0, "b": 1, "c": 2, "x": 4}, Answer{"a": 0, "b": 1, "c": 2, "y": 0}, opts(5), 1, true},
		{"empty overlap", Answer{"a": 0}, Answer{"b": 0}, Options{Tiers: 3}, 0, false},
		{"both empty min 0", Answer{}, Answer{}, Options{Tiers: 3}, 0, false},
		{"nil answers", nil, nil, opts(3), 0, false},
		{"small overlap", Answer{"a": 0, "b": 1}, Answer{"a": 0, "b": 1}, opts(3), 0, false},
		{"overlap equals min", Answer{"a": 0, "b": 1, "c": 1}, Answer{"a": 0, "b": 1, "c": 1}, opts(3), 1, true},
		{"min overlap 1", Answer{"a": 0}, Answer{"a": 1}, Options{Tiers: 2, MinOverlap: 1}, 0, true},
		{"tiers < 2", Answer{"a": 0, "b": 0, "c": 0}, Answer{"a": 0, "b": 0, "c": 0}, opts(1), 0, false},
		{"tiers 0", Answer{"a": 0}, Answer{"a": 0}, Options{}, 0, false},
		{"out of range ignored", Answer{"a": 0, "b": 1, "c": 2, "d": 9}, Answer{"a": 0, "b": 1, "c": 2, "d": 0}, opts(3), 1, true},
		{"negative ignored", Answer{"a": -1, "b": 1}, Answer{"a": 0, "b": 1}, Options{Tiers: 3, MinOverlap: 1}, 1, true},
		{"out of range in b ignored", Answer{"a": 1, "b": 1}, Answer{"a": 7, "b": 1}, Options{Tiers: 3, MinOverlap: 1}, 1, true},
		{"all invalid", Answer{"a": 5}, Answer{"a": 5}, Options{Tiers: 3, MinOverlap: 0}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Similarity(tt.a, tt.b, tt.opts)
			if ok != tt.wantOK || math.Abs(got-tt.want) > 1e-12 {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, ok, tt.want, tt.wantOK)
			}
			// symmetry on the same case
			got2, ok2 := Similarity(tt.b, tt.a, tt.opts)
			if got2 != got || ok2 != ok {
				t.Fatalf("not symmetric: (%v,%v) vs (%v,%v)", got, ok, got2, ok2)
			}
		})
	}
}

func randAnswer(r *rand.Rand, items, tiers int) Answer {
	a := Answer{}
	for i := 0; i < items; i++ {
		if r.Intn(4) == 0 {
			continue // skipped
		}
		a[ItemID(fmt.Sprintf("i%d", i))] = r.Intn(tiers)
	}
	return a
}

func TestSimilarityProperties(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for n := 0; n < 5000; n++ {
		tiers := 2 + r.Intn(9)
		o := Options{Tiers: tiers, MinOverlap: r.Intn(5)}
		a, b := randAnswer(r, 3+r.Intn(20), tiers), randAnswer(r, 3+r.Intn(20), tiers)

		ab, okAB := Similarity(a, b, o)
		ba, okBA := Similarity(b, a, o)
		if ab != ba || okAB != okBA {
			t.Fatalf("asymmetric: %v/%v vs %v/%v", ab, okAB, ba, okBA)
		}
		if okAB && (ab < 0 || ab > 1) {
			t.Fatalf("out of range: %v", ab)
		}
		if !okAB && ab != 0 {
			t.Fatalf("undefined must return 0, got %v", ab)
		}
		if s, ok := Similarity(a, a, o); ok && s != 1 {
			t.Fatalf("self-similarity %v != 1", s)
		}

		// extreme opposite: tiers 0 vs T-1
		lo, hi := Answer{}, Answer{}
		for id := range a {
			lo[id], hi[id] = 0, tiers-1
		}
		if s, ok := Similarity(lo, hi, o); ok && s != 0 {
			t.Fatalf("opposite similarity %v != 0", s)
		}
	}
}

func answers(n int, a Answer) []Answer {
	out := make([]Answer, n)
	for i := range out {
		out[i] = a
	}
	return out
}

func TestThinkLikeYou(t *testing.T) {
	p := Answer{"a": 0, "b": 1, "c": 2}
	same := Answer{"a": 0, "b": 1, "c": 2}
	far := Answer{"a": 2, "b": 1, "c": 0}  // sum 4 / (3*2) -> 1/3
	near := Answer{"a": 0, "b": 1, "c": 1} // 1 - 1/6 = 0.833
	tiny := Answer{"a": 0}                 // undefined
	o := opts(3)

	tests := []struct {
		name   string
		others []Answer
		opts   Options
		want   Result
	}{
		{"not enough data", answers(18, same), o, Result{NotEnoughData: true, Answers: 19}},
		{"exactly min answers", answers(19, same), o, Result{Answers: 20, Compared: 19, Matched: 19, Share: 1}},
		{"mixed", append(append(answers(10, same), answers(5, far)...), answers(4, near)...), o,
			Result{Answers: 20, Compared: 19, Matched: 14, Share: 14.0 / 19}},
		{"undefined pairs excluded", append(answers(15, tiny), answers(4, far)...), o,
			Result{Answers: 20, Compared: 4, Matched: 0, Share: 0}},
		{"no comparable pairs", answers(19, tiny), o, Result{Answers: 20}},
		{"empty others, min 0", nil, Options{Tiers: 3}, Result{Answers: 1}},
		{"empty others, default min", nil, o, Result{NotEnoughData: true, Answers: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ThinkLikeYou(p, tt.others, tt.opts)
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestThinkLikeYouThresholdBoundary(t *testing.T) {
	// 5 items, T=6 (max distance 5 per item): one item off by 5 -> 1 - 5/25 = 0.8.
	p := Answer{"a": 0, "b": 0, "c": 0, "d": 0, "e": 0}
	q := Answer{"a": 5, "b": 0, "c": 0, "d": 0, "e": 0}
	o := Options{Tiers: 6, MinOverlap: 3, Threshold: 0.8, MinAnswers: 2}
	got := ThinkLikeYou(p, []Answer{q}, o)
	want := Result{Answers: 2, Compared: 1, Matched: 1, Share: 1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	o.Threshold = 0.81
	if got := ThinkLikeYou(p, []Answer{q}, o); got.Matched != 0 {
		t.Fatalf("0.8 must not pass 0.81: %+v", got)
	}
}

func TestThinkLikeYouProperties(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for n := 0; n < 500; n++ {
		tiers := 2 + r.Intn(6)
		o := Options{Tiers: tiers, MinOverlap: 1 + r.Intn(3), Threshold: r.Float64(), MinAnswers: r.Intn(10)}
		p := randAnswer(r, 10, tiers)
		others := make([]Answer, r.Intn(30))
		for i := range others {
			others[i] = randAnswer(r, 10, tiers)
		}
		res := ThinkLikeYou(p, others, o)
		if res.Share < 0 || res.Share > 1 || res.Matched > res.Compared || res.Compared > len(others) {
			t.Fatalf("invariant broken: %+v", res)
		}
		if res.NotEnoughData != (len(others)+1 < o.MinAnswers) {
			t.Fatalf("NotEnoughData mismatch: %+v", res)
		}
		// threshold 0 matches everyone comparable
		o.Threshold = 0
		o.MinAnswers = 0
		if all := ThinkLikeYou(p, others, o); all.Matched != all.Compared {
			t.Fatalf("threshold 0: %+v", all)
		}
	}
}
