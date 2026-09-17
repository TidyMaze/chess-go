package main

import (
	"encoding/binary"
	"math"
	"testing"
)

func encode(rs []record) []byte {
	var out []byte
	put32 := func(v uint32) { out = binary.LittleEndian.AppendUint32(out, v) }
	for _, r := range rs {
		put32(uint32(r.game))
		put32(math.Float32bits(r.target))
		put32(math.Float32bits(r.static))
		out = append(out, byte(len(r.own)), byte(len(r.opp)))
		for _, f := range r.own {
			out = binary.LittleEndian.AppendUint16(out, f)
		}
		for _, f := range r.opp {
			out = binary.LittleEndian.AppendUint16(out, f)
		}
	}
	return out
}

func TestDecodeRoundTripsAndStopsOnATruncatedTail(t *testing.T) {
	in := []record{
		{game: 1, target: 0.5, static: 0.4, own: []uint16{1, 2, 3}, opp: []uint16{4, 5, 6}},
		{game: 2, target: -2.5, static: -2, own: []uint16{7}, opp: []uint16{8}},
	}
	var got []record
	if n := decode(encode(in), func(r record) bool { got = append(got, r); return true }); n != 2 {
		t.Fatalf("decoded %d records, want 2", n)
	}
	if got[0].game != 1 || got[0].target != 0.5 || len(got[0].own) != 3 || got[1].own[0] != 7 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	// A crash mid-write leaves a partial record; that is a stop, not an error.
	full := encode(in)
	if n := decode(full[:len(full)-3], func(record) bool { return true }); n != 1 {
		t.Errorf("a truncated tail should yield %d complete records, got %d", 1, n)
	}
}

// Kings carry no feature, so the piece count is the feature count plus two.
func TestMenCountsBothKings(t *testing.T) {
	if got := men(record{own: make([]uint16, 30), opp: make([]uint16, 30)}); got != 32 {
		t.Errorf("30 features is a full board of %d men, got %d", 32, got)
	}
	if got := men(record{own: nil, opp: nil}); got != 2 {
		t.Errorf("a bare kings endgame is 2 men, got %d", got)
	}
}

func TestPhaseBoundaries(t *testing.T) {
	for _, c := range []struct {
		men  int
		want string
	}{{32, "opening"}, {26, "opening"}, {25, "middlegame"}, {16, "middlegame"}, {15, "endgame"}, {3, "endgame"}} {
		if got := phase(c.men); got != c.want {
			t.Errorf("%d men is %q, want %q", c.men, got, c.want)
		}
	}
}

func TestLabelBandsSplitDecisiveFromLevel(t *testing.T) {
	for _, c := range []struct {
		t    float64
		want string
	}{{0, "level (<0.3)"}, {-0.29, "level (<0.3)"}, {0.5, "slight (0.3-1)"}, {-2, "clear (1-3)"},
		{4, "winning (3-6)"}, {-9, "decisive (6+)"}} {
		if got := labelBand(c.t); got != c.want {
			t.Errorf("%.2f is %q, want %q", c.t, got, c.want)
		}
	}
}

// Two positions with the same men on the same squares are the same
// position; swapping which perspective they came from is not.
func TestPositionKeyDistinguishesPerspectives(t *testing.T) {
	a := record{own: []uint16{1, 2}, opp: []uint16{3, 4}}
	b := record{own: []uint16{1, 2}, opp: []uint16{3, 4}}
	c := record{own: []uint16{3, 4}, opp: []uint16{1, 2}}
	if positionKey(a) != positionKey(b) {
		t.Error("identical positions hashed differently")
	}
	if positionKey(a) == positionKey(c) {
		t.Error("swapped perspectives hashed the same")
	}
}
