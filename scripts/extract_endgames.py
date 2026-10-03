import sys
import chess
import chess.pgn

target = 100000
count = 0
game_idx = 0
out_fens = []

while count < target:
    game = chess.pgn.read_game(sys.stdin)
    if game is None:
        break
    game_idx += 1
    if game_idx < 25000:
        continue
    board = game.board()
    ply = 0
    for move in game.mainline_moves():
        board.push(move)
        ply += 1
        if 32 <= ply <= 65 and ply % 4 == 0:
            if not board.is_check() and len(board.piece_map()) <= 18:
                out_fens.append(board.fen())
                count += 1
                if count >= target:
                    break

with open("openings_endgames_chunk2.txt", "w") as f:
    for fen in out_fens:
        f.write(fen + "\n")

print(f"Extracted {len(out_fens)} endgame FENs")
