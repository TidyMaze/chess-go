package main

import (
	"bufio"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"math"
	"os"

	"chess/engine"
)

func mathFloat32bits(f float32) uint32     { return math.Float32bits(f) }
func mathFloat32frombits(b uint32) float32 { return math.Float32frombits(b) }
func inputsFor() int                       { return engine.HalfKPInputsFor(engine.FeatureKingBuckets) }

// Persistence for the two things that cost real time: the positions
// collected from self-play, and the network's weights together with
// Adam's accumulated state.
//
// Every restart before this threw both away. A generation is a minute of
// ten cores, so a pool of two million positions is over half an hour of
// compute, and the optimiser state is worth more than that because it
// encodes how far each weight has already been tuned.
//
// The pool is append-only. New positions are appended as each generation
// finishes, so a crash costs at most the generation in flight, and a
// restart reads the tail of the file up to the window size. Rewriting
// the whole file every generation would cost more than generating it.

const poolMagic uint32 = 0x4E4E5031 // "NNP1"

// appendPool writes samples to the end of the pool file.
//
// Features are int16 because the feature space is 5,120 wide, which fits
// with room to spare, and they dominate the record: storing them as
// int32 would nearly double the file for nothing.
func appendPool(path string, samples []sample) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	var scratch [8]byte
	put32 := func(v uint32) {
		binary.LittleEndian.PutUint32(scratch[:4], v)
		w.Write(scratch[:4])
	}
	for i := range samples {
		s := &samples[i]
		put32(uint32(s.game))
		put32(mathFloat32bits(float32(s.target)))
		put32(mathFloat32bits(float32(s.static)))
		w.WriteByte(byte(len(s.own)))
		w.WriteByte(byte(len(s.opp)))
		for _, feats := range [2][]int32{s.own, s.opp} {
			for _, v := range feats {
				binary.LittleEndian.PutUint16(scratch[:2], uint16(v))
				w.Write(scratch[:2])
			}
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

// loadPool reads the file and returns at most limit samples, keeping the
// most recent, which is the same sliding window the in-memory pool uses.
func loadPool(path string, limit int) ([]sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)

	var out []sample
	var head [14]byte
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			break // a truncated tail is a crash mid-write, not an error
		}
		s := sample{
			game:   int32(binary.LittleEndian.Uint32(head[0:4])),
			target: float64(mathFloat32frombits(binary.LittleEndian.Uint32(head[4:8]))),
			static: float64(mathFloat32frombits(binary.LittleEndian.Uint32(head[8:12]))),
		}
		nOwn, nOpp := int(head[12]), int(head[13])
		buf := make([]byte, 2*(nOwn+nOpp))
		if _, err := io.ReadFull(r, buf); err != nil {
			break
		}
		s.own = make([]int32, nOwn)
		s.opp = make([]int32, nOpp)
		for i := 0; i < nOwn; i++ {
			s.own[i] = int32(binary.LittleEndian.Uint16(buf[2*i:]))
		}
		for i := 0; i < nOpp; i++ {
			s.opp[i] = int32(binary.LittleEndian.Uint16(buf[2*(nOwn+i):]))
		}
		out = append(out, s)
		// Trim as we go rather than loading gigabytes and slicing after.
		// The slack is 25% and not 100% because the pool is now tens of
		// millions of positions: doubling it before trimming needs several
		// gigabytes of headroom this machine does not have. The cost is
		// more copying, which is pointer-sized and cheap next to the
		// allocation it avoids.
		if limit > 0 && len(out) > limit+limit/4 {
			out = append([]sample(nil), out[len(out)-limit:]...)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// netState is everything needed to resume training exactly, including
// Adam's second moment. Resuming without it restarts the adaptive step
// sizes from scratch, which undoes much of what the run had learned
// about each weight.
type netState struct {
	H           int
	W1, B1, W2  []float32
	B2          float32
	V1, VB1, V2 []float32
	VB2         float32
	Generation  int
	CumElo      int
}

func saveNet(path string, n *net, generation, cumElo int) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	st := netState{H: n.h, W1: n.w1, B1: n.b1, W2: n.w2, B2: n.b2,
		V1: n.v1, VB1: n.vb1, V2: n.v2, VB2: n.vb2,
		Generation: generation, CumElo: cumElo}
	if err := gob.NewEncoder(f).Encode(&st); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	// Rename over the old file so an interrupted write cannot leave a
	// half-written checkpoint where a good one used to be.
	return os.Rename(tmp, path)
}

func loadNet(path string, expectHidden int) (*net, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	var st netState
	if err := gob.NewDecoder(f).Decode(&st); err != nil {
		return nil, 0, 0, err
	}
	if st.H != expectHidden || len(st.W1) != inputsFor()*st.H {
		return nil, 0, 0, fmt.Errorf("checkpoint has hidden %d and %d weights, this build wants hidden %d and %d",
			st.H, len(st.W1), expectHidden, inputsFor()*expectHidden)
	}
	n := &net{h: st.H, w1: st.W1, b1: st.B1, w2: st.W2, b2: st.B2,
		v1: st.V1, vb1: st.VB1, v2: st.V2, vb2: st.VB2}
	return n, st.Generation, st.CumElo, nil
}

// newNetForTest builds a network with the real shapes but a fixed seed,
// so persistence can be tested without a training run.
func newNetForTest(h int) *net {
	return &net{
		h:   h,
		w1:  make([]float32, inputsFor()*h),
		b1:  make([]float32, h),
		w2:  make([]float32, 2*h),
		v1:  make([]float32, inputsFor()*h),
		vb1: make([]float32, h),
		v2:  make([]float32, 2*h),
	}
}
