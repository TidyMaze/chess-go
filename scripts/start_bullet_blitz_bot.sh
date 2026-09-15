#!/usr/bin/env bash
# start_bullet_blitz_bot.sh: runs the chess-go bot daemon and the bullet/blitz auto-challenger.

set -euo pipefail
cd "$(dirname "$0")/.."

# Load token from environment or .env if not already set
if [ -z "${LICHESS_BOT_TOKEN:-}" ] && [ -f .env ]; then
    export $(grep -v '^#' .env | xargs) || true
fi

TOKEN="${LICHESS_BOT_TOKEN:-${1:-}}"
if [ -z "$TOKEN" ]; then
    echo "ERROR: LICHESS_BOT_TOKEN is not set."
    echo "Usage: LICHESS_BOT_TOKEN=lip_xxx ./scripts/start_bullet_blitz_bot.sh"
    echo "   or: ./scripts/start_bullet_blitz_bot.sh lip_xxx"
    exit 1
fi
export LICHESS_BOT_TOKEN="$TOKEN"
export LICHESS_BOT_USER="${LICHESS_BOT_USER:-tidymazebot}"
export MODES="bullet,blitz"

mkdir -p /tmp/chesslogs

echo "=== Cleaning up existing bot processes ==="
pkill -f lichessbot-bin || true
pkill -f lichess_challenger.sh || true
sleep 1

echo "=== Building latest engine binaries ==="
go build -o lichessbot-bin ./cmd/lichessbot
go build -o play-bin ./play

echo "=== Starting Lichess Bot Engine Server ($LICHESS_BOT_USER) ==="
nohup ./lichessbot-bin -username "$LICHESS_BOT_USER" -champion champion.json > /tmp/chesslogs/bot_engine.log 2>&1 &
BOT_PID=$!
echo "Bot server started (PID: $BOT_PID, log: /tmp/chesslogs/bot_engine.log)"

echo "=== Starting Bullet/Blitz Challenger Daemon ==="
nohup ./scripts/lichess_challenger.sh 2 > /tmp/chesslogs/challenger.log 2>&1 &
CHALLENGER_PID=$!
echo "Challenger daemon started (PID: $CHALLENGER_PID, log: /tmp/chesslogs/challenger.log)"

echo "=== Bot is now live and actively seeking bullet/blitz games! ==="
echo "PIDs: Bot Server=$BOT_PID, Challenger=$CHALLENGER_PID"
echo "Logs: tail -f /tmp/chesslogs/bot_engine.log /tmp/chesslogs/challenger.log"
