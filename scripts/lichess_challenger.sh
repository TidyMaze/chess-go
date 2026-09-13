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
RECENT=/tmp/lichess_recent_$$.txt
COOLDOWN=${CHALLENGE_COOLDOWN:-900}
BACKOFF_BASE=${CHALLENGE_BACKOFF:-600}
BACKOFF=$BACKOFF_BASE
BACKOFF_MAX=${CHALLENGE_BACKOFF_MAX:-3600}
PERCYCLE=${CHALLENGE_PER_CYCLE:-1}
RATELIMITED=0
: > "$CAPPED"
: > "$RECENT"

api() { curl -s -H "Authorization: Bearer $TOKEN" "$@"; }

in_flight() {
  api "https://lichess.org/api/account/playing" | python3 -c \
    "import json,sys
try: print(len(json.load(sys.stdin).get('nowPlaying',[])))
except Exception: print(-1)"
}

# Which mode to challenge in next. MODES limits the pool: bullet and
# blitz finish in minutes, so they are what keeps games actually running,
# while a classical game ties a slot up for half an hour and a rating
# built only on the thinnest mode never arrives. Within the pool the mode
# with the fewest rated games goes first, so both fill evenly. Prints
# "name limit increment".
thinnest_mode() {
  api "https://lichess.org/api/user/$ME" | MODES="${MODES:-bullet,blitz}" python3 -c "
import json,os,sys
try: p=json.load(sys.stdin).get('perfs',{})
except Exception: p={}
clocks={'bullet':('120','1'),'blitz':('300','3'),'rapid':('600','5'),'classical':('1200','10')}
allowed=[m for m in os.environ['MODES'].split(',') if m in clocks]
if not allowed: allowed=list(clocks)
worst=min(allowed, key=lambda m: p.get(m,{}).get('games',0))
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
# Anyone challenged recently is skipped. Candidates are sorted by rating
# distance, so without this the nearest opponent wins every cycle and is
# challenged again and again: fourteen times in a row, observed twice.
import time as _t
_now=_t.time(); _cut=float(sys.argv[4])
try:
    for _ln in open(sys.argv[5]):
        _p=_ln.split()
        if len(_p)==2 and _now-float(_p[1]) < _cut:
            capped.add(_p[0])
except FileNotFoundError:
    pass
out=[]
for line in sys.stdin:
    line=line.strip()
    if not line: continue
    d=json.loads(line)
    if d['id']=='$ME' or d['id'] in capped: continue
    r=d.get('perfs',{}).get(mode,{}).get('rating')
    if r and abs(r-me)<=250: out.append((abs(r-me), d['id']))
out.sort()
print(' '.join(i for _,i in out[:6]))" "$mode" /tmp/me_$$.json "$CAPPED" "$COOLDOWN" "$RECENT"
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
      echo "$opp $(date +%s)" >> "$RECENT"
      echo "$(date +%H:%M:%S) challenged $opp at ${lim}+${inc}"
      BACKOFF=$BACKOFF_BASE
      ;;
    *"Too many requests"*)
      # Lichess is rate limiting us. Retrying on the same cadence just
      # keeps the limit alive, which is how the bot ended up with one
      # game in flight and eight minutes of refusals in the log. A fixed
      # ten minute wait was not enough either, so the wait doubles each
      # time it happens and only resets after a challenge gets through.
      echo "$(date +%H:%M:%S) rate limited, backing off for ${BACKOFF}s"
      RATELIMITED=1
      sleep "$BACKOFF"
      BACKOFF=$((BACKOFF * 2))
      [ "$BACKOFF" -gt "$BACKOFF_MAX" ] && BACKOFF=$BACKOFF_MAX
      ;;
    *)
      echo "$opp $(date +%s)" >> "$RECENT"
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
    # At most a couple per cycle. Sending five at once, every minute, is
    # what drew the rate limit in the first place.
    room=$((WANT - n))
    [ "$room" -gt "$PERCYCLE" ] && room=$PERCYCLE
    RATELIMITED=0
    for opp in $(candidates "$mode"); do
      [ "$room" -le 0 ] && break
      [ "$RATELIMITED" = "1" ] && break
      challenge "$opp" "$lim" "$inc"
      room=$((room - 1))
      sleep 10
    done
  fi
  sleep "$SLEEP"
done
