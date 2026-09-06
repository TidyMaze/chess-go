// Command evalgap finds what kind of position this engine misjudges.
//
// Everything measured so far says "the evaluation is the ceiling", which
// is true and has stopped being useful: twelve changes have been tried
// against that diagnosis and all came back at or below zero. This asks
// the narrower question that actually determines what to build next.
//
// The trick is to look at the *signed* error, bucketed by position type,
// rather than the squared error overall. A large squared error says the
// evaluation is noisy. A large signed error inside a bucket says it is
// systematically wrong about that kind of position, which is what a
// missing term looks like: if the engine is consistently 0.6 pawns too
// optimistic whenever its king is exposed, then it is missing king
// safety, and no amount of tuning the terms it has will fix that.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"

	"chess/board"
	"chess/engine"
	"chess/game"
	"chess/moves"
)

const stockfishPath = "/opt/homebrew/bin/stockfish"

type bucket struct {
	n          int
	sumSigned  float64
	sumSquared float64
}

func (b *bucket) add(err float64) {
	b.n++
	b.sumSigned += err
	b.sumSquared += err * err
}

func (b bucket) mean() float64 {
	if b.n == 0 {
		return 0
	}
	return b.sumSigned / float64(b.n)
}

func (b bucket) rms() float64 {
	if b.n == 0 {
		return 0
	}
	return math.Sqrt(b.sumSquared / float64(b.n))
}

// features describes a position in the terms an evaluation might be
// missing knowledge about.
type features struct {
	phase        string
	material     string
	ourKingOpen  bool
	theirKingOpen bool
	passers      string
	bishopPair   string
	rooksActive  string
}

// kingOpenness counts missing pawns in front of the king, which is the
// crude version of "is this king exposed".
func kingOpenness(b *board.Board, c board.Color) int {
	k := b.KingSquare(c)
	var buf [32]board.ColoredPiece
	shield := 0
	for _, p := range b.AppendAllPieces(buf[:0]) {
		if p.Color != c || p.Type != board.Pawn {
			continue
		}
		if p.Sq.File >= k.File-1 && p.Sq.File <= k.File+1 {
			ahead := p.Sq.Rank > k.Rank
			if c == board.Black {
				ahead = p.Sq.Rank < k.Rank
			}
			if ahead {
				shield++
			}
		}
	}
	return 3 - shield // 0 = fully sheltered, 3 = bare
}

func describe(b *board.Board) features {
	var buf [32]board.ColoredPiece
	pieces := b.AppendAllPieces(buf[:0])

	weight := 0
	var mat [2]float64
	var bishops [2]int
	var pawnAdv [2]int
	values := map[board.PieceType]float64{
		board.Pawn: 1, board.Knight: 3, board.Bishop: 3, board.Rook: 5, board.Queen: 9,
	}
	for _, p := range pieces {
		switch p.Type {
		case board.Knight, board.Bishop:
			weight++
		case board.Rook:
			weight += 2
		case board.Queen:
			weight += 4
		}
		mat[p.Color] += values[p.Type]
		if p.Type == board.Bishop {
			bishops[p.Color]++
		}
		if p.Type == board.Pawn {
			r := p.Sq.Rank
			if p.Color == board.Black {
				r = 7 - r
			}
			if r >= 4 {
				pawnAdv[p.Color]++
			}
		}
	}

	f := features{}
	switch {
	case weight >= 18:
		f.phase = "opening"
	case weight >= 8:
		f.phase = "middlegame"
	default:
		f.phase = "endgame"
	}

	d := mat[board.White] - mat[board.Black]
	switch {
	case d > 1.5:
		f.material = "White ahead"
	case d < -1.5:
		f.material = "Black ahead"
	default:
		f.material = "level"
	}

	f.ourKingOpen = kingOpenness(b, board.White) >= 2
	f.theirKingOpen = kingOpenness(b, board.Black) >= 2

	switch {
	case pawnAdv[board.White] > pawnAdv[board.Black]+1:
		f.passers = "White pawns advanced"
	case pawnAdv[board.Black] > pawnAdv[board.White]+1:
		f.passers = "Black pawns advanced"
	default:
		f.passers = "pawns level"
	}

	switch {
	case bishops[board.White] >= 2 && bishops[board.Black] < 2:
		f.bishopPair = "White has the pair"
	case bishops[board.Black] >= 2 && bishops[board.White] < 2:
		f.bishopPair = "Black has the pair"
	default:
		f.bishopPair = "no pair edge"
	}
	return f
}

func main() {
	positions := flag.Int("positions", 400, "positions to analyse")
	depth := flag.Int("sf-depth", 12, "Stockfish depth for the reference score")
	playDepth := flag.Int("play-depth", 3, "depth used to generate positions")
	out := flag.String("out", "evalgap.json", "where to write the findings")
	flag.Parse()

	sf, err := engine.NewStockfish(stockfishPath, 20, 0)
	if err != nil {
		fmt.Println("stockfish:", err)
		return
	}
	defer sf.Close()

	me := engine.Strong(*playDepth)
	handEval := &engine.Eval{Weights: engine.DefaultWeights(), UsePST: true,
		Tapered: true, Structure: true, Mobility: true, KingSafety: 0.01}

	byBucket := map[string]*bucket{}
	add := func(key string, err float64) {
		if byBucket[key] == nil {
			byBucket[key] = &bucket{}
		}
		byBucket[key].add(err)
	}

	overall := &bucket{}
	collected := 0
	for collected < *positions {
		g := game.New()
		g.EnableRepetitionTracking()
		tt := engine.NewTranspositionTable(16)
		for ply := 0; ply < 140 && collected < *positions && !g.IsOver(); ply++ {
			m, ok := engine.PlayerPickWith(me, g, tt)
			if !ok {
				break
			}
			g.ApplyMove(m.From, m.To)
			if ply < 10 || moves.IsInCheck(&g.Board, g.Turn) {
				continue
			}
			// Quiet positions only: where a capture is pending, the gap
			// between a static score and a searched one is the tactic, and
			// no evaluation term can be blamed for it.
			static := engine.PlayerStaticEval(me, &g.Board)
			stm := static
			if g.Turn == board.Black {
				stm = -static
			}
			if math.Abs(engine.QuiescenceScore(me, g)-stm) > 0.20 {
				continue
			}
			ref, mate, ok := sf.Evaluate(g, *depth)
			if !ok || mate {
				continue
			}
			if g.Turn == board.Black {
				ref = -ref
			}
			if ref > 8 {
				ref = 8
			} else if ref < -8 {
				ref = -8
			}
			mine := engine.PositionScoreEval(&g.Board, board.White, handEval)

			// Positive error means this engine is too optimistic for White.
			err := mine - ref
			overall.add(err)
			collected++

			f := describe(&g.Board)
			add("phase: "+f.phase, err)
			add("material: "+f.material, err)
			add("pawns: "+f.passers, err)
			add("bishops: "+f.bishopPair, err)
			if f.ourKingOpen && !f.theirKingOpen {
				add("kings: White's king exposed", err)
			} else if f.theirKingOpen && !f.ourKingOpen {
				add("kings: Black's king exposed", err)
			} else if f.ourKingOpen && f.theirKingOpen {
				add("kings: both exposed", err)
			} else {
				add("kings: both sheltered", err)
			}
		}
	}

	fmt.Printf("%d quiet positions, this engine's static score against Stockfish at depth %d\n\n",
		overall.n, *depth)
	fmt.Printf("overall: mean error %+.3f pawns, RMS %.3f\n\n", overall.mean(), overall.rms())
	fmt.Println("A large mean means a systematic misjudgement of that kind of")
	fmt.Println("position, which is what a missing term looks like. A large RMS")
	fmt.Println("with a small mean is noise, which tuning cannot fix.")
	fmt.Println()

	keys := make([]string, 0, len(byBucket))
	for k := range byBucket {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return math.Abs(byBucket[keys[i]].mean()) > math.Abs(byBucket[keys[j]].mean())
	})
	fmt.Printf("%-32s %6s %8s %8s\n", "position type", "n", "mean", "rms")
	for _, k := range keys {
		b := byBucket[k]
		flag := ""
		if math.Abs(b.mean()) > 0.35 && b.n >= 20 {
			flag = "  <- systematic"
		}
		fmt.Printf("%-32s %6d %+8.3f %8.3f%s\n", k, b.n, b.mean(), b.rms(), flag)
	}

	record := map[string]any{"positions": overall.n,
		"overall_mean": overall.mean(), "overall_rms": overall.rms()}
	rows := map[string]any{}
	for k, b := range byBucket {
		rows[k] = map[string]any{"n": b.n, "mean": b.mean(), "rms": b.rms()}
	}
	record["buckets"] = rows
	if data, err := json.MarshalIndent(record, "", "  "); err == nil {
		_ = os.WriteFile(*out, data, 0644)
	}
}
