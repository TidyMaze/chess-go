package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// A Champion is the strongest configuration measured so far, written to
// disk so that the thing a human plays in the browser and the thing the
// experiments are compared against are the same object.
//
// Before this existed the UI hardcoded engine.Strong(depth), so every
// adopted improvement had to be remembered and re-applied by hand, and a
// campaign that adopts several changes would silently leave the UI on the
// first one.
type Champion struct {
	// Label names the change that won its match, for the UI to display.
	Label string `json:"label"`
	// Depth is the search depth the UI plays at, or the floor a timed
	// search must reach when TimeMS is set.
	Depth int `json:"depth"`
	// TimeMS, when positive, is a per-move budget in milliseconds. Under a
	// clock the engine deepens past Depth as time allows.
	TimeMS int `json:"time_ms,omitempty"`
	// Threads is how many search threads the engine runs (Lazy SMP). 0 or
	// 1 means one.
	Threads int `json:"threads,omitempty"`
	// Elo is the calibrated rating, and Margin its 95% interval. Both are
	// display-only: nothing branches on them.
	Elo    float64 `json:"elo"`
	Margin float64 `json:"margin"`
	// Adopted records when this became champion.
	Adopted string `json:"adopted"`

	// NetFile, when set, is a HalfKP checkpoint to evaluate with.
	NetFile string `json:"net_file,omitempty"`
	// HandBlend is the weight on the hand-written evaluation when a net is
	// loaded. 0 means the net decides alone.
	HandBlend float64 `json:"hand_blend,omitempty"`

	// Book, when set, is an opening book the champion plays from.
	Book string `json:"book,omitempty"`
	// Syzygy, when set, is a generated endgame tablebase file. The name is
	// historical: these are built by retrograde analysis here rather than
	// downloaded, because reading the real Syzygy format correctly is a
	// large piece of work and its seven-piece set is terabytes.
	Syzygy string `json:"tablebases,omitempty"`
	// Features, when set, is a comma-separated list of search features to enable.
	Features string `json:"features,omitempty"`
	// TTBits sizes the transposition table (2^TTBits entries).
	TTBits int `json:"tt_bits,omitempty"`
	// QuiescePly caps the capture search; 0 keeps the engine default.
	QuiescePly int `json:"qply,omitempty"`
	// Tune overrides search margins, "Name=value,..." as ParseSearchTune reads it.
	Tune string `json:"tune,omitempty"`
}

// DefaultChampion is what the engine was before this campaign started:
// Strong() with no network, which is the only configuration that has ever
// won its match.
func DefaultChampion() Champion {
	return Champion{
		Label: "Strong (mobility + attacker-counting king safety)",
		Depth: 5, Elo: 1955, Margin: 130,
	}
}

// ReadChampion reads the descriptor, falling back to the default rather
// than failing: a missing or corrupt file must not stop a human playing.
func ReadChampion(path string) Champion {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultChampion()
	}
	c := DefaultChampion()
	if err := json.Unmarshal(data, &c); err != nil {
		return DefaultChampion()
	}
	if c.Depth <= 0 {
		c.Depth = 5
	}
	return c
}

func WriteChampion(path string, c Champion) error {
	c.Adopted = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Player builds the playable engine this champion describes. A network
// that fails to load is skipped rather than fatal, for the same reason
// LoadChampion falls back: the hand-written evaluation always works.
func (c Champion) Player() Player {
	p, err := c.PlayerOrError()
	if err != nil {
		// The browser must keep working, so this degrades rather than
		// fails. It says so loudly: a champion that names a network and
		// silently plays without it is indistinguishable from one that has
		// none, and in the bootstrap ladder that turns every rung into a
		// copy of the last one with nothing to show for it.
		fmt.Fprintf(os.Stderr, "champion: %v (falling back to the hand-written evaluation)\n", err)
	}
	return p
}

// PlayerOrError is Player with the failure visible. Anything that depends
// on the champion actually being the champion, above all the ladder's
// labeller, must use this: labelling with the hand evaluation when the
// network was meant to be loaded produces a rung identical to the previous
// one, and nothing in the numbers would reveal it.
func (c Champion) PlayerOrError() (Player, error) {
	p := Strong(c.Depth)
	p.Name = "champion"
	if c.TimeMS > 0 {
		p.TimeBudget = time.Duration(c.TimeMS) * time.Millisecond
	}
	p.Threads = c.Threads
	if c.TTBits > 0 {
		p.TTBits = uint(c.TTBits)
	}
	if c.QuiescePly > 0 {
		p.QuiescePly = c.QuiescePly
	}
	tune, err := ParseSearchTune(c.Tune)
	if err != nil {
		return p, fmt.Errorf("champion tune: %w", err)
	}
	p.Tune = tune
	if c.NetFile != "" {
		n, err := LoadHalfKPNet(c.NetFile)
		if err != nil {
			return p, fmt.Errorf("network %s named by the champion did not load: %w", c.NetFile, err)
		}
		p.HalfKP = n
		p.HalfKPBlend = c.HandBlend
	}
	if c.Book != "" {
		b, err := LoadBook(c.Book)
		if err != nil {
			return p, fmt.Errorf("book %s named by the champion did not load: %w", c.Book, err)
		}
		p.Book = b
	}
	if c.Syzygy != "" {
		tb, err := LoadTablebases(c.Syzygy)
		if err != nil {
			return p, fmt.Errorf("tablebases %s named by the champion did not load: %w", c.Syzygy, err)
		}
		p.Tablebases = tb
	}
	if c.Features != "" {
		p.ApplyFeatures(c.Features)
	}
	return p, nil
}

// ChampionWatcher hands out the current champion, rebuilding it when the
// file on disk changes. The UI holds one of these so an adoption during a
// campaign reaches the browser without a restart.
type ChampionWatcher struct {
	path string

	mu      sync.RWMutex
	champ   Champion
	player  Player
	modTime time.Time
	checked time.Time
}

func NewChampionWatcher(path string) *ChampionWatcher {
	w := &ChampionWatcher{path: path}
	w.reload()
	return w
}

func (w *ChampionWatcher) reload() {
	c := ReadChampion(w.path)
	p := c.Player()
	w.mu.Lock()
	w.champ, w.player = c, p
	if st, err := os.Stat(w.path); err == nil {
		w.modTime = st.ModTime()
	}
	w.checked = time.Now()
	w.mu.Unlock()
}

// Current returns the champion and its player, checking the file for
// changes at most once a second. Polling a stat is cheaper than a file
// watcher and this is called once per human move.
func (w *ChampionWatcher) Current() (Champion, Player) {
	w.mu.RLock()
	stale := time.Since(w.checked) > time.Second
	c, p := w.champ, w.player
	w.mu.RUnlock()
	if !stale {
		return c, p
	}
	st, err := os.Stat(w.path)
	w.mu.Lock()
	w.checked = time.Now()
	changed := err == nil && st.ModTime().After(w.modTime)
	w.mu.Unlock()
	if changed {
		w.reload()
		w.mu.RLock()
		c, p = w.champ, w.player
		w.mu.RUnlock()
	}
	return c, p
}
