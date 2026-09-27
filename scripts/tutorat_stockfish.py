#!/usr/bin/env python3
"""Stockfish tutorat for TidyMazeBot games.

Analyzes game PGNs, identifies blunders and inaccuracies, and classifies weaknesses.
"""

import argparse
import io
import json
import re
import subprocess
import sys
import chess
import chess.engine
import chess.pgn

STOCKFISH_PATH = "/opt/homebrew/bin/stockfish"
BOT_NAME = "tidymazebot"

def analyze_game(pgn_text: str, engine: chess.engine.SimpleEngine, move_time: float = 0.15) -> dict:
    game = chess.pgn.read_game(io.StringIO(pgn_text))
    if not game:
        return {"error": "failed to parse PGN"}

    headers = dict(game.headers)
    white_name = headers.get("White", "").lower()
    black_name = headers.get("Black", "").lower()

    our_color = chess.WHITE if BOT_NAME in white_name else chess.BLACK
    bot_side = "White" if our_color == chess.WHITE else "Black"

    board = game.board()
    mistakes = []
    
    prev_eval = 0.0

    for ply, move in enumerate(game.mainline_moves()):
        is_our_turn = (board.turn == our_color)
        
        # Analyze before move
        info = engine.analyse(board, chess.engine.Limit(time=move_time))
        score = info["score"].pov(our_color)
        sf_best = info.get("pv", [None])[0]
        
        eval_before = score.score(mate_score=10000)
        
        # Make the move
        board.push(move)
        
        if is_our_turn:
            # Analyze after our move
            info_after = engine.analyse(board, chess.engine.Limit(time=move_time))
            score_after = info_after["score"].pov(our_color)
            eval_after = score_after.score(mate_score=10000)
            
            # Loss in centipawns
            cp_loss = eval_before - eval_after
            
            # If blunder (> 100 cp) or mistake (> 60 cp)
            if cp_loss >= 60 and sf_best and move != sf_best:
                fen_before = board.copy()
                fen_before.pop()
                mistakes.append({
                    "move_num": (ply // 2) + 1,
                    "color": bot_side,
                    "played": move.uci(),
                    "best": sf_best.uci(),
                    "eval_before": eval_before,
                    "eval_after": eval_after,
                    "cp_loss": cp_loss,
                    "fen": fen_before.fen(),
                })

    return {
        "event": headers.get("Event", ""),
        "site": headers.get("Site", ""),
        "white": headers.get("White", ""),
        "black": headers.get("Black", ""),
        "result": headers.get("Result", ""),
        "bot_side": bot_side,
        "mistakes": mistakes,
    }

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--game-id", help="Lichess game ID to fetch and analyze")
    parser.add_argument("--pgn", help="PGN file to analyze")
    parser.add_argument("--time", type=float, default=0.15, help="Time per move for Stockfish in seconds")
    args = parser.parse_args()

    engine = chess.engine.SimpleEngine.popen_uci(STOCKFISH_PATH)
    engine.configure({"Threads": 2, "Hash": 128})

    try:
        pgn_text = ""
        if args.game_id:
            cmd = ["curl", "-s", f"https://lichess.org/game/export/{args.game_id}"]
            pgn_text = subprocess.check_output(cmd).decode("utf-8")
        elif args.pgn:
            with open(args.pgn) as f:
                pgn_text = f.read()
        else:
            print("Specify --game-id or --pgn")
            sys.exit(1)

        result = analyze_game(pgn_text, engine, move_time=args.time)
        print(json.dumps(result, indent=2))
    finally:
        engine.quit()

if __name__ == "__main__":
    main()
