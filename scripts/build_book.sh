#!/bin/sh
# Rebuild the opening book the deployed bot plays from.
#
# The book is not versioned: it is 1.5 MB of derived data and the source
# database is 286 MB, so the repository keeps the recipe instead. It had
# kept neither for a while, which meant champion_bot.json depended on a
# file that existed on one machine and could not be reproduced from the
# repository at all.
#
# The source is a database of games people played. That is match history
# and is allowed. It must never be the Lichess *evaluation* dump, which is
# engine scores: see -extract-book, which reads that dump and is kept only
# so the difference stays documented.
#
# Usage:
#   scripts/build_book.sh [output] [min-elo] [min-games] [plies]
set -eu

OUT=${1:-games_book_v3.txt}
MIN_ELO=${2:-1800}
MIN_GAMES=${3:-10}
PLIES=${4:-16}
SRC=${BOOK_PGN:-lichess_games_2015-01.pgn.zst}

[ -f "$SRC" ] || { echo "no games database at $SRC" >&2; exit 1; }
go build -o nnue-bin ./nnue

# Why these defaults, measured with cmd/bookcheck at depth 7 over 200
# sampled positions, the engine judging its own book:
#
#   floor  games  positions  mean loss  90th pct  losing >0.5 pawns
#   none      20      37323     +0.116    +0.473               8.5%
#   2000      20       1847     +0.112    +0.357               4.0%
#   1800      10      16438     +0.103    +0.355               5.5%
#
# The floor is what matters: without one, the most played move is whatever
# was tempting to weak players. 2000 buys little more than 1800 and costs
# nine tenths of the coverage, and coverage is the point of a book.
zstd -dcq "$SRC" | nice -n 10 ./nnue-bin \
  -book-from-pgn "$OUT" -book-pgn - \
  -book-plies "$PLIES" -book-min "$MIN_GAMES" -book-min-elo "$MIN_ELO"

echo "wrote $OUT: $(wc -l < "$OUT") positions"
echo "check it with: go run ./cmd/bookcheck -book $OUT -depth 7"
