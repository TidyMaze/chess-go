#!/bin/sh
# Keep the lichess bot playing, continuously, across all four time
# controls.
#
# The bot itself only answers challenges; it never starts one. Left alone
# it plays whatever arrives and then sits idle, which is how it ended up
# with 31 rapid games and 5 classical ones. This loop tops the queue up:
# every cycle it counts the games actually in flight, and if there is room
# it challenges opponents near its own rating in whichever mode has the
# fewest games, so all four ratings fill in rather than just the popular
# one.
#
# Usage:
#   LICHESS_BOT_TOKEN=xxx scripts/lichess_challenger.sh [max-in-flight]
#
# The token is read from the environment and never written anywhere.
# Lichess caps bot-versus-bot games at 100 per opponent per day and says
# so in the challenge response; an opponent that answers that way is
# skipped for the rest of the run rather than retried into a rate limit.
set -u

TOKEN=${LICHESS_BOT_TOKEN:?LICHESS_BOT_TOKEN is not set}
ME=${LICHESS_BOT_USER:-tidymazebot}
WANT=${1:-2}
SLEEP=${CHALLENGE_INTERVAL:-60}
CAPPED=/tmp/lichess_capped_$$.txt
: > "$CAPPED"

api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }

in_flight() {
  api "https://lichess.org/api/account/playing" | python3 -c \
    "import json,sys
try: print(len(json.load(sys.stdin).get('nowPlaying',[])))
except Exception: print(-1)"
}

# The mode with the fewest rated games, so the thin ones catch up. Prints
# "name limit increment".
thinnest_mode() {
  api "https://lichess.org/api/user/$ME" | python3 -c "
import json,sys
try: p=json.load(sys.stdin).get('perfs',{})
except Exception: p={}
clocks={'bullet':('120','1'),'blitz':('300','3'),'rapid':('600','5'),'classical':('1200','10')}
worst=min(clocks, key=lambda m: p.get(m,{}).get('games',0))
print(worst, clocks[worst][0], clocks[worst][1])"
}

# Online bots within 250 points of our rating in that mode, excluding
# ourselves and anyone already known to be capped for today.
candidates() {
  mode=$1
  api "https://lichess.org/api/user/$ME" > /tmp/me_$$.json
  curl -s "https://lichess.org/api/bot/online?nb=100" | python3 -c "
import json,sys
mode=sys.argv[1]
me=json.load(open(sys.argv[2])).get('perfs',{}).get(mode,{}).get('rating',2200)
capped=set(x.strip() for x in open(sys.argv[3]) if x.strip())
out=[]
for line in sys.stdin:
    line=line.strip()
    if not line: continue
    d=json.loads(line)
    if d['id']=='$ME' or d['id'] in capped: continue
    r=d.get('perfs',{}).get(mode,{}).get('rating')
    if r and abs(r-me)<=250: out.append((abs(r-me), d['id']))
out.sort()
print(' '.join(i for _,i in out[:6]))" "$mode" /tmp/me_$$.json "$CAPPED"
  rm -f /tmp/me_$$.json
}

challenge() {
  opp=$1; lim=$2; inc=$3
  resp=$(curl -s -X POST "https://lichess.org/api/challenge/$opp" \
    -H "Authorization: Bearer $TOKEN" \
    -d "rated=true" -d "clock.limit=$lim" -d "clock.increment=$inc" -d "color=random")
  case "$resp" in
    *"played 100 games"*)
      echo "$opp" >> "$CAPPED"
      echo "$(date +%H:%M:%S) $opp is at the daily cap, skipping it from now on"
      ;;
    *'"id"'*)
      echo "$(date +%H:%M:%S) challenged $opp at ${lim}+${inc}"
      ;;
    *)
      echo "$(date +%H:%M:%S) $opp declined or errored: $(echo "$resp" | head -c 120)"
      ;;
  esac
}

echo "$(date +%H:%M:%S) challenger up: keeping $WANT games in flight, checking every ${SLEEP}s"
while true; do
  n=$(in_flight)
  if [ "$n" -lt 0 ]; then
    echo "$(date +%H:%M:%S) could not read games in flight, retrying"
  elif [ "$n" -lt "$WANT" ]; then
    set -- $(thinnest_mode)
    mode=$1; lim=$2; inc=$3
    # Only send as many as there is room for. A challenge takes a while
    # to be accepted, so the in-flight count does not move between sends
    # and checking it in the loop would fire one per candidate.
    room=$((WANT - n))
    for opp in $(candidates "$mode"); do
      [ "$room" -le 0 ] && break
      challenge "$opp" "$lim" "$inc"
      room=$((room - 1))
      sleep 5
    done
  fi
  sleep "$SLEEP"
done
