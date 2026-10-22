// Package similarity implements the answer similarity metric from ADR-001.
//
// All functions are pure: no I/O, no global state and no dependencies on other
// internal packages. Tiers are numbered from 0 to Tiers-1.
package similarity

// Default values from ADR-001 and SPEC section 5.
const (
	// DefaultMinOverlap is the minimal number of items placed in both answers
	// for the pair to be comparable.
	DefaultMinOverlap = 3
	// DefaultThreshold is the minimal similarity for "thinks like you".
	DefaultThreshold = 0.80
	// DefaultMinAnswers is the minimal number of answers in a survey below
	// which only "not enough data" is reported.
	DefaultMinAnswers = 20
)

// epsilon absorbs floating point error when comparing with the threshold.
const epsilon = 1e-9

// ItemID identifies a survey item.
type ItemID string

// Answer maps a placed item to its tier index (0..Tiers-1). Items the
// participant skipped are absent.
type Answer map[ItemID]int

// Options are the parameters of the metric.
type Options struct {
	// Tiers is the number of tiers T in the survey (must be >= 2).
	Tiers int
	// MinOverlap is the minimal overlap size for a defined similarity.
	MinOverlap int
	// Threshold is the minimal similarity counted as "thinks like you".
	Threshold float64
	// MinAnswers is the minimal number of answers to show a share.
	MinAnswers int
}

// DefaultOptions returns options with the ADR-001 defaults for the given
// number of tiers.
func DefaultOptions(tiers int) Options {
	return Options{
		Tiers:      tiers,
		MinOverlap: DefaultMinOverlap,
		Threshold:  DefaultThreshold,
		MinAnswers: DefaultMinAnswers,
	}
}

// Similarity returns 1 - sum|ta-tb| / (N*(T-1)) over the items placed in both
// answers, where N is the overlap size. The second result is false when the
// similarity is undefined: fewer than opts.MinOverlap common items (an empty
// overlap is always undefined) or opts.Tiers < 2. Items whose tier is outside
// [0, Tiers-1] in either answer are treated as not placed.
func Similarity(a, b Answer, opts Options) (float64, bool) {
	if opts.Tiers < 2 {
		return 0, false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	maxTier := opts.Tiers - 1
	n, sum := 0, 0
	for id, ta := range a {
		tb, ok := b[id]
		if !ok || !validTier(ta, maxTier) || !validTier(tb, maxTier) {
			continue
		}
		n++
		sum += abs(ta - tb)
	}
	if n == 0 || n < opts.MinOverlap {
		return 0, false
	}
	return 1 - float64(sum)/float64(n*maxTier), true
}

// Result is the "thinks like you" outcome for one participant.
type Result struct {
	// NotEnoughData is true when the survey has fewer than MinAnswers answers;
	// Share must not be shown then.
	NotEnoughData bool
	// Answers is the number of answers in the survey (participant included).
	Answers int
	// Compared is the number of other answers with a defined similarity.
	Compared int
	// Matched is the number of compared answers with similarity >= Threshold.
	Matched int
	// Share is Matched/Compared in [0, 1]. It is 0 when Compared is 0 or
	// NotEnoughData is true; check Compared to tell "nobody" from "no pairs".
	Share float64
}

// ThinkLikeYou computes the share of other participants whose similarity with
// p is at least opts.Threshold, counting only pairs with defined similarity.
// others must not contain p itself; the survey size is len(others)+1. When it
// is below opts.MinAnswers the result has NotEnoughData set and no share.
func ThinkLikeYou(p Answer, others []Answer, opts Options) Result {
	res := Result{Answers: len(others) + 1}
	if res.Answers < opts.MinAnswers {
		res.NotEnoughData = true
		return res
	}
	for _, q := range others {
		s, ok := Similarity(p, q, opts)
		if !ok {
			continue
		}
		res.Compared++
		if s >= opts.Threshold-epsilon {
			res.Matched++
		}
	}
	if res.Compared > 0 {
		res.Share = float64(res.Matched) / float64(res.Compared)
	}
	return res
}

func validTier(t, maxTier int) bool { return t >= 0 && t <= maxTier }

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
