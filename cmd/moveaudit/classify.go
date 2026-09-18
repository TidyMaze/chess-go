package main

import (
	"chess/board"
	"chess/game"
)

// moveKinds are the labels a move can carry. A move is often several of
// them, so they are counted independently rather than as one category:
// what matters is which kinds we under-play compared with the judge, and
// a capture that also gives check belongs in both counts.
var moveKinds = []string{"capture", "check", "pawn push", "pawn to 6th or 7th", "develops", "king move", "retreat"}

// classify labels a move in a position. It takes the position by FEN
// because the caller has the FEN and applying a move needs a board that
// nothing else holds a pointer into.
func classify(fen string, m game.Move) map[string]bool {
	out := map[string]bool{}
	g, err := game.ParseFEN(fen)
	if err != nil {
		return out
	}
	piece, ok := g.Board.PieceAt(m.From)
	if !ok {
		return out
	}
	mover := g.Turn
	if _, captured := g.Board.PieceAt(m.To); captured {
		out["capture"] = true
	}
	if piece.Type == board.Pawn {
		out["pawn push"] = true
		rank := m.To.Rank
		if mover == board.Black {
			rank = 7 - rank
		}
		if rank >= 5 {
			out["pawn to 6th or 7th"] = true
		}
	}
	if piece.Type == board.King {
		out["king move"] = true
	}
	// Developing means leaving the back rank for the first time with a
	// piece that is not a pawn or the king.
	homeRank := 0
	if mover == board.Black {
		homeRank = 7
	}
	if piece.Type != board.Pawn && piece.Type != board.King &&
		m.From.Rank == homeRank && m.To.Rank != homeRank {
		out["develops"] = true
	}
	// Retreating means moving back toward the home rank.
	fromAdvance, toAdvance := m.From.Rank, m.To.Rank
	if mover == board.Black {
		fromAdvance, toAdvance = 7-fromAdvance, 7-toAdvance
	}
	if toAdvance < fromAdvance {
		out["retreat"] = true
	}
	g.ApplyMove(m.From, m.To)
	if g.Board.IsInCheck(mover.Other()) {
		out["check"] = true
	}
	return out
}

// kindGap counts, for each kind of move, how often the judge played one
// and we did not, against how often we played one and the judge did not.
// A kind we systematically under-play is an evaluation term we are
// missing; one we over-play is a term we weigh too heavily.
type kindGap struct {
	judgeOnly map[string]int
	ourOnly   map[string]int
}

func newKindGap() *kindGap {
	return &kindGap{judgeOnly: map[string]int{}, ourOnly: map[string]int{}}
}

func (k *kindGap) add(fen string, ours, theirs game.Move) {
	o, t := classify(fen, ours), classify(fen, theirs)
	for _, kind := range moveKinds {
		switch {
		case t[kind] && !o[kind]:
			k.judgeOnly[kind]++
		case o[kind] && !t[kind]:
			k.ourOnly[kind]++
		}
	}
}
