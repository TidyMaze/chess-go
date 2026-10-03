import sys
import chess
import chess.pgn

target_mid = 500000
target_end = 500000
count_mid = 0
count_end = 0
game_idx = 0

f_mid = open("openings_large_mid_500k.txt", "w")
f_end = open("openings_large_end_500k.txt", "w")

while count_mid < target_mid or count_end < target_end:
    game = chess.pgn.read_game(sys.stdin)
    if game is None:
        break
    game_idx += 1
    # Skip games used in earlier pools (first 50k games)
    if game_idx < 50000:
        continue
        
    board = game.board()
    ply = 0
    for move in game.mainline_moves():
        board.push(move)
        ply += 1
        
        # Middlegames: plies 14-28, quiet, >= 24 pieces
        if 14 <= ply <= 28 and ply % 4 == 0 and count_mid < target_mid:
            if not board.is_check() and len(board.piece_map()) >= 24:
                f_mid.write(board.fen() + "\n")
                count_mid += 1
                
        # Endgames: plies 34-70, quiet, <= 16 pieces
        elif 34 <= ply <= 70 and ply % 4 == 0 and count_end < target_end:
            if not board.is_check() and len(board.piece_map()) <= 16:
                f_end.write(board.fen() + "\n")
                count_end += 1

f_mid.close()
f_end.close()
print(f"Extracted {count_mid} midgame and {count_end} endgame FENs")
