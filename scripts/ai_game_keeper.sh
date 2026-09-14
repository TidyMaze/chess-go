#!/bin/sh
# Keep the bot playing when nobody will accept a challenge from it.
#
# Outgoing challenges to other bots are refused account wide, and have been
# for hours: not a short window, not a daily cap on UTC, not a cap per
# opponent, all three tested. See BUGS.md. Incoming challenges still arrive
# but only in bursts, so between them the bot sits idle.
#
# Challenging the lichess AI goes through a different endpoint that is not
# blocked, so this tops the queue up with AI games. They are unrated and
# move none of the four ratings, which is the point to be honest about:
# this keeps the bot playing and exercises the clock and the search, it
# does not make progress toward a rating target. Rated games come from
# incoming challenges, and this deliberately leaves room for them.
#
# Usage:
#   LICHESS_BOT_TOKEN=xxx scripts/ai_game_keeper.sh [games-in-flight]
set -u

TOKEN=${LICHESS_BOT_TOKEN:?LICHESS_BOT_TOKEN is not set}
WANT=${1:-2}
SLEEP=${KEEPER_INTERVAL:-60}
LEVEL=${KEEPER_LEVEL:-6}

api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }

in_flight() {
  api "https://lichess.org/api/account/playing" | python3 -c \
    "import json,sys
try: print(len(json.load(sys.stdin).get('nowPlaying',[])))
except Exception: print(-1)"
}

# Fast controls only. A long game ties a slot up for half an hour for a
# result that moves no rating, and the whole reason for this script is that
# the bot would otherwise be doing nothing at all.
next_control() {
  case $(( $(date +%s) % 3 )) in
    0) echo "60 1" ;;
    1) echo "120 1" ;;
    *) echo "180 2" ;;
  esac
}

echo "$(date +%H:%M:%S) ai keeper up: topping up to $WANT games, checking every ${SLEEP}s"
while true; do
  n=$(in_flight)
  if [ "$n" -lt 0 ]; then
    echo "$(date +%H:%M:%S) could not read games in flight, retrying"
  elif [ "$n" -lt "$WANT" ]; then
    ctl=$(next_control)
    lim=$(echo "$ctl" | cut -d' ' -f1)
    inc=$(echo "$ctl" | cut -d' ' -f2)
    resp=$(curl -s -X POST "https://lichess.org/api/challenge/ai" \
      -H "Authorization: Bearer $TOKEN" \
      -d "level=$LEVEL" -d "clock.limit=$lim" -d "clock.increment=$inc" -d "color=random")
    case "$resp" in
      *'"id"'*)
        echo "$(date +%H:%M:%S) started an AI game at ${lim}+${inc} ($n in flight before)"
        ;;
      *)
        echo "$(date +%H:%M:%S) AI challenge refused: $(echo "$resp" | head -c 120)"
        sleep 120
        ;;
    esac
  fi
  sleep "$SLEEP"
done
