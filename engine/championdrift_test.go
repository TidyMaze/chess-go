package engine

import (
	"sort"
	"strings"
	"testing"
)

// The three champion files describe the same engine playing under three
// clocks: the raced champion, the lichess bot, and the UI. They carry
// their search feature lists independently, and nothing kept them in
// step.
//
// That is not hypothetical. champion_ui.json spent days without the
// `improving` flag after it was adopted at +12 +/- 11 over 4,000 games,
// so the champion offered to a human in the UI was measurably weaker
// than the one winning the races, and nobody noticed until an unrelated
// edit printed all three lists side by side.
//
// Evaluation terms are allowed to differ: champion.json carries
// kingsafety, which hand_blend 0 makes inert anyway. Search features are
// not, because a search feature is strength.
func TestChampionFilesAgreeOnSearchFeatures(t *testing.T) {
	evaluationTerms := map[string]bool{"kingsafety": true, "structure": true, "mobility": true}
	searchFeatures := func(path string) []string {
		c := ReadChampion("../" + path)
		if c.Features == "" {
			t.Fatalf("%s names no features, so it is not the champion it claims to be", path)
		}
		var out []string
		for _, f := range strings.Split(c.Features, ",") {
			f = strings.TrimSpace(f)
			if f != "" && !evaluationTerms[f] {
				out = append(out, f)
			}
		}
		sort.Strings(out)
		return out
	}
	want := searchFeatures("champion.json")
	for _, other := range []string{"champion_bot.json", "champion_ui.json"} {
		got := searchFeatures(other)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s searches differently from champion.json\n  %s has %v\n  champion.json has %v",
				other, other, got, want)
		}
	}
}
