// Command poolmerge writes one training pool from several, keeping each
// distinct position once.
//
// The audit that motivated it: five pools holding 15,755,343 positions
// carry 10,957,828 distinct ones, so 30.5% are repeats, while no single
// pool exceeds 8.3%. The duplication is between pools, not inside them.
// A repeated position is counted twice by the loss for no new information,
// which quietly weights the corpus toward whatever the pools have in
// common, and it costs memory that the packing step does not have to spare.
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
)

func main() {
	out := flag.String("out", "", "pool file to write")
	minMen := flag.Int("min-men", 0, "keep only positions with at least this many men")
	maxMen := flag.Int("max-men", 64, "keep only positions with at most this many men")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: poolmerge -out merged.bin [-min-men N] [-max-men N] pool.bin ...")
		os.Exit(1)
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<22)

	seen := make(map[uint64]struct{}, 1<<24)
	var read, written, dupes, filtered int
	// Game ids restart in every pool, so they are renumbered as the files
	// are walked. Without that the by-game train/test split would put two
	// unrelated games on the same side believing they are one.
	gameBase := int32(0)
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		before, maxGame := written, int32(0)
		n := decode(data, func(r record) bool {
			read++
			if r.game > maxGame {
				maxGame = r.game
			}
			m := men(r)
			if m < *minMen || m > *maxMen {
				filtered++
				return true
			}
			k := positionKey(r)
			if _, ok := seen[k]; ok {
				dupes++
				return true
			}
			seen[k] = struct{}{}
			r.game += gameBase
			writeRecord(w, r)
			written++
			return true
		})
		gameBase += maxGame + 1
		fmt.Printf("%-24s %9d read, %9d kept\n", path, n, written-before)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\n%d read, %d written, %d duplicates dropped (%.1f%%), %d outside the material range\n",
		read, written, dupes, 100*float64(dupes)/math.Max(1, float64(read)), filtered)
}

func writeRecord(w *bufio.Writer, r record) {
	var b [4]byte
	put32 := func(v uint32) {
		binary.LittleEndian.PutUint32(b[:], v)
		w.Write(b[:])
	}
	put32(uint32(r.game))
	put32(math.Float32bits(r.target))
	put32(math.Float32bits(r.static))
	w.WriteByte(byte(len(r.own)))
	w.WriteByte(byte(len(r.opp)))
	for _, feats := range [2][]uint16{r.own, r.opp} {
		for _, v := range feats {
			binary.LittleEndian.PutUint16(b[:2], v)
			w.Write(b[:2])
		}
	}
}
