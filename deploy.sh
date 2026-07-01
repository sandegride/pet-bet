#!/bin/bash
set -e

SERVER="root@77.83.87.194"
SERVER_PATH="/opt/pet-bet"

echo "=== 1. Commit & push to GitHub ==="
cd "$(dirname "$0")"

git add -A
git commit -m "feat: new bet types, admin panel, UI overhaul, solo/MMR filters, HWID support"
git push origin main

echo ""
echo "=== 2. Deploy to server $SERVER ==="
ssh "$SERVER" bash -s << 'REMOTE'
set -e
cd /opt/pet-bet

echo "-- git pull --"
git pull origin main

echo "-- docker compose build & restart --"
docker compose build
docker compose up -d

echo "-- running migrations --"
docker compose exec -T bot migrate -path /app/migrations -database "$DATABASE_URL" up 2>/dev/null || \
  docker compose run --rm bot /app/migrate -path /migrations -database "$DATABASE_URL" up 2>/dev/null || \
  echo "(migrations skipped — run manually if needed)"

echo "-- done --"
docker compose ps
REMOTE

echo ""
echo "=== Deploy complete ==="
