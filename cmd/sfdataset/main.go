package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
	"chess/pool"
)

type sampleRecord struct {
	game   int32
	target float32
	static float32
	own    []int32
	opp    []int32
}

func writeBinaryRecord(w io.Writer, r sampleRecord) error {
	var scratch [14]byte
	binary.LittleEndian.PutUint32(scratch[0:4], uint32(r.game))
	binary.LittleEndian.PutUint32(scratch[4:8], math.Float32bits(r.target))
	binary.LittleEndian.PutUint32(scratch[8:12], math.Float32bits(r.static))
	scratch[12] = byte(len(r.own))
	scratch[13] = byte(len(r.opp))

	if _, err := w.Write(scratch[:]); err != nil {
		return err
	}

	buf := make([]byte, 2*(len(r.own)+len(r.opp)))
	idx := 0
	for _, f := range r.own {
		binary.LittleEndian.PutUint16(buf[idx:idx+2], uint16(f))
		idx += 2
	}
	for _, f := range r.opp {
		binary.LittleEndian.PutUint16(buf[idx:idx+2], uint16(f))
		idx += 2
	}
	_, err := w.Write(buf)
	return err
}

func loadFENs(paths string, limit int) ([]string, error) {
	var fens []string
	seen := make(map[string]bool)

	for _, path := range strings.Split(paths, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, "\t")
			fen := parts[0]
			words := strings.Fields(fen)
			if len(words) < 2 {
				continue
			}
			key := words[0] + " " + words[1]
			if seen[key] {
				continue
			}
			seen[key] = true
			fens = append(fens, fen)
			if limit > 0 && len(fens) >= limit {
				f.Close()
				return fens, nil
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return fens, nil
}

func main() {
	inPath := flag.String("in", "openings_mid.txt", "path to file containing FENs (one per line)")
	outPath := flag.String("out", "sf_dataset.bin", "path to binary pool output")
	sfPath := flag.String("stockfish", "/opt/homebrew/bin/stockfish", "path to stockfish binary")
	depth := flag.Int("depth", 6, "stockfish search depth per position (0 for static eval)")
	limit := flag.Int("limit", 50000, "number of positions to label")
	workers := flag.Int("workers", runtime.NumCPU(), "parallel stockfish workers")
	flag.Parse()

	fens, err := loadFENs(*inPath, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading FENs: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("loaded %d unique positions from %s\n", len(fens), *inPath)

	out, err := os.OpenFile(*outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating %s: %v\n", *outPath, err)
		os.Exit(1)
	}
	defer out.Close()
	writer := bufio.NewWriterSize(out, 1<<22)

	jobs := make(chan string, *workers*2)
	results := make(chan sampleRecord, *workers*4)
	var wg sync.WaitGroup
	var savedCount int64

	for w := 0; w < *workers; w++ {

		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			sf, err := engine.NewStockfish(*sfPath, 20, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "worker %d: stockfish error: %v\n", workerID, err)
				return
			}
			defer sf.Close()

			for fen := range jobs {
				g, err := game.ParseFEN(fen)
				if err != nil {
					continue
				}

				var score float64
				var ok bool
				var mate bool

				if *depth > 0 {
					score, mate, ok = sf.Evaluate(g, *depth)
				} else {
					score, ok = sf.StaticEval(g)
				}

				if !ok || mate {
					continue
				}

				// Normalize score to White's perspective
				if *depth > 0 && g.Turn == board.Black {
					score = -score
				}

				// Clamp to [-12, 12] pawns
				if score > 12 {
					score = 12
				} else if score < -12 {
					score = -12
				}

				own := engine.AppendHalfKPFeatures(nil, &g.Board, board.White)
				opp := engine.AppendHalfKPFeatures(nil, &g.Board, board.Black)

				idx := atomic.AddInt64(&savedCount, 1)
				results <- sampleRecord{
					game:   int32((idx / 20) + 1),
					target: float32(score),
					static: float32(score),
					own:    own,
					opp:    opp,
				}
			}
		}(w)

	}

	go func() {
		for _, fen := range fens {
			jobs <- fen
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var written int64
	t0 := time.Now()
	lastPrint := t0

	for rec := range results {
		if err := writeBinaryRecord(writer, rec); err != nil {
			fmt.Fprintf(os.Stderr, "write error: %v\n", err)
			break
		}
		atomic.AddInt64(&written, 1)

		if time.Since(lastPrint) >= 2*time.Second {
			lastPrint = time.Now()
			n := atomic.LoadInt64(&written)
			elapsed := time.Since(t0).Seconds()
			rate := float64(n) / elapsed
			eta := float64(len(fens)-int(n)) / rate
			fmt.Printf("  %d / %d positions (%.0f pos/s, ETA %.1fs)\n", n, len(fens), rate, eta)
		}
	}

	writer.Flush()
	out.Sync()

	total := atomic.LoadInt64(&written)
	fmt.Printf("Done: wrote %d Stockfish-scored positions to %s in %.1fs\n",
		total, *outPath, time.Since(t0).Seconds())

	_ = pool.WriteProvenance(*outPath, pool.Provenance{
		Positions:  "openings",
		Source:     *inPath,
		Labeller:   fmt.Sprintf("Stockfish 19 (depth %d)", *depth),
		LabelDepth: *depth,
		Buckets:    8,
		Note:       fmt.Sprintf("Rescored %d positions with Stockfish 19", total),
		Lambda:     1.0,
	})
}
