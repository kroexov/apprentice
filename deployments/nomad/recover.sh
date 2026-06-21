#!/bin/bash
set -euo pipefail

RCLONE_CONFIG="/etc/rclone/rclone.conf"
RCLONE="/usr/bin/rclone"
RCLONE_OPTS="--config $RCLONE_CONFIG"
TMP_DIR="/tmp"

usage() {
  echo "Usage: $0 <db-name> [--latest] [--yes]"
  echo ""
  echo "  <db-name>   Name of the database to restore"
  echo "  --latest    Automatically pick the most recent backup"
  echo "  --yes       Skip confirmation prompt before restoring"
  exit 1
}

if [[ $# -lt 1 ]]; then
  usage
fi

DB=""
AUTO_LATEST=false
AUTO_YES=false

for arg in "$@"; do
  case "$arg" in
    --latest) AUTO_LATEST=true ;;
    --yes)    AUTO_YES=true ;;
    --*)      echo "Unknown option: $arg"; usage ;;
    *)        DB="$arg" ;;
  esac
done

if [[ -z "$DB" ]]; then
  usage
fi

echo "[recover] Listing backups for '$DB' on Google Drive..."
BACKUPS=$($RCLONE $RCLONE_OPTS ls "gdrive:$DB/" 2>/dev/null | sort | awk '{print $2}')

if [[ -z "$BACKUPS" ]]; then
  echo "[recover] No backups found for '$DB' in gdrive:$DB/"
  exit 1
fi

echo ""
echo "Available backups:"
i=1
declare -a BACKUP_LIST
while IFS= read -r line; do
  echo "  $i) $line"
  BACKUP_LIST+=("$line")
  ((i++))
done <<< "$BACKUPS"
echo ""

if $AUTO_LATEST; then
  CHOSEN="${BACKUP_LIST[${#BACKUP_LIST[@]}-1]}"
  echo "[recover] Auto-selecting latest: $CHOSEN"
else
  read -rp "Select backup number [default: ${#BACKUP_LIST[@]} (latest)]: " SELECTION
  if [[ -z "$SELECTION" ]]; then
    SELECTION="${#BACKUP_LIST[@]}"
  fi
  if ! [[ "$SELECTION" =~ ^[0-9]+$ ]] || (( SELECTION < 1 || SELECTION > ${#BACKUP_LIST[@]} )); then
    echo "[recover] Invalid selection."
    exit 1
  fi
  CHOSEN="${BACKUP_LIST[$((SELECTION-1))]}"
fi

BACKUP_PATH="$TMP_DIR/$CHOSEN"

if ! $AUTO_YES; then
  echo ""
  echo "WARNING: This will DROP and recreate the database '$DB', then restore from '$CHOSEN'."
  read -rp "Are you sure? [y/N]: " CONFIRM
  if [[ "${CONFIRM,,}" != "y" ]]; then
    echo "[recover] Aborted."
    exit 0
  fi
fi

echo ""
echo "[recover] Downloading '$CHOSEN' from Google Drive..."
$RCLONE $RCLONE_OPTS copy "gdrive:$DB/$CHOSEN" "$TMP_DIR/"
echo "[recover] Download complete."

echo "[recover] Dropping database '$DB'..."
sudo -u postgres dropdb --if-exists "$DB"

echo "[recover] Creating database '$DB'..."
sudo -u postgres createdb "$DB"

echo "[recover] Restoring '$DB' from backup..."
gunzip -c "$BACKUP_PATH" | sudo -u postgres psql "$DB"

rm -f "$BACKUP_PATH"
echo ""
echo "[recover] Done. Database '$DB' successfully restored from '$CHOSEN'."
