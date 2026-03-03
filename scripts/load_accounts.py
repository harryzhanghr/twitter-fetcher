#!/usr/bin/env python3
"""
Reads usernames from a CSV, looks up their Twitter user IDs in batches,
and inserts them into the twitter_accounts table.
Safely rotates the refresh token after each use.

Usage:
    python3 scripts/load_accounts.py <csv_path> [--replace]
"""
import csv
import os
import subprocess
import sys
import time
from pathlib import Path

import requests
import psycopg2

TOKEN_URL   = "https://api.x.com/2/oauth2/token"
USERS_URL   = "https://api.x.com/2/users/by"
BATCH_SIZE  = 100

# Defaults — overridden by .env values if present.
TOKEN_STORE = "file"
KEYCHAIN_SVC = "twitter-fetcher.refresh_token"
REFRESH_TOKEN_FILE = ".refresh_token"


def read_env():
    """Read key=value pairs from .env file at project root."""
    env_path = Path(__file__).parent.parent / ".env"
    values = {}
    for line in env_path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" in line:
            key, val = line.split("=", 1)
            values[key.strip()] = val.strip()
    return values


def keychain_read(service):
    r = subprocess.run(
        ["security", "find-generic-password", "-w", "-a", os.environ["USER"], "-s", service],
        capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"Keychain read failed for {service}: {r.stderr.strip()}")
    return r.stdout.strip()


def keychain_write(service, value):
    r = subprocess.run(
        ["security", "add-generic-password", "-U", "-a", os.environ["USER"], "-s", service, "-w", value],
        capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"Keychain write failed for {service}: {r.stderr.strip()}")


def read_refresh_token(env):
    token_store = env.get("TOKEN_STORE", TOKEN_STORE)
    if token_store == "keychain":
        return keychain_read(env.get("KEYCHAIN_SERVICE", KEYCHAIN_SVC))
    # File mode.
    token_file = Path(__file__).parent.parent / env.get("REFRESH_TOKEN_FILE", REFRESH_TOKEN_FILE)
    if token_file.exists():
        token = token_file.read_text().strip()
        if token:
            return token
    # Fall back to env var.
    token = os.environ.get("X_REFRESH_TOKEN", "")
    if token:
        return token
    sys.exit("No refresh token found. Run scripts/oauth_setup.py first.")


def write_refresh_token(env, token):
    token_store = env.get("TOKEN_STORE", TOKEN_STORE)
    if token_store == "keychain":
        keychain_write(env.get("KEYCHAIN_SERVICE", KEYCHAIN_SVC), token)
    else:
        token_file = Path(__file__).parent.parent / env.get("REFRESH_TOKEN_FILE", REFRESH_TOKEN_FILE)
        fd = os.open(str(token_file), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as f:
            f.write(token + "\n")


def get_access_token(client_id, env):
    refresh_token = read_refresh_token(env)
    resp = requests.post(TOKEN_URL,
        data={"grant_type": "refresh_token", "refresh_token": refresh_token, "client_id": client_id},
        headers={"Content-Type": "application/x-www-form-urlencoded"}, timeout=30)
    if resp.status_code != 200:
        sys.exit(f"Token refresh failed ({resp.status_code}): {resp.text}")
    payload = resp.json()
    new_refresh = payload.get("refresh_token")
    if new_refresh:
        write_refresh_token(env, new_refresh)
        print(f"  Refresh token rotated")
    return payload["access_token"]


def read_usernames(csv_path):
    usernames = []
    with open(csv_path) as f:
        reader = csv.reader(f)
        next(reader)  # skip header
        for row in reader:
            if len(row) < 2:
                continue
            handle = row[1].strip().lstrip("@")
            if handle:
                usernames.append(handle)
    return usernames


def lookup_batch(usernames, access_token):
    resp = requests.get(USERS_URL,
        params={"usernames": ",".join(usernames), "user.fields": "id,name,username"},
        headers={"Authorization": f"Bearer {access_token}"}, timeout=30)
    if resp.status_code == 429:
        print("  Rate limited — waiting 60s...")
        time.sleep(60)
        return lookup_batch(usernames, access_token)
    if resp.status_code != 200:
        print(f"  Warning: batch lookup returned {resp.status_code}: {resp.text[:200]}")
        return []
    return resp.json().get("data", [])


def main():
    # Require CSV path as argument.
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    if not args:
        print("Usage: python3 scripts/load_accounts.py <csv_path> [--replace]")
        print("  csv_path: CSV file with usernames in column index 1")
        sys.exit(1)
    csv_path = args[0]

    env = read_env()
    client_id = env.get("X_CLIENT_ID")
    db_url = env.get("DATABASE_URL")
    if not client_id:
        sys.exit("X_CLIENT_ID not found in .env")
    if not db_url:
        sys.exit("DATABASE_URL not found in .env")

    print(f"Reading usernames from {csv_path}...")
    usernames = read_usernames(csv_path)
    print(f"Found {len(usernames)} usernames")

    print("Getting access token...")
    access_token = get_access_token(client_id, env)
    print(f"  Access token obtained")

    print(f"Looking up user IDs in batches of {BATCH_SIZE}...")
    users = []
    for i in range(0, len(usernames), BATCH_SIZE):
        batch = usernames[i:i+BATCH_SIZE]
        results = lookup_batch(batch, access_token)
        users.extend(results)
        print(f"  Batch {i//BATCH_SIZE + 1}/{(len(usernames)+BATCH_SIZE-1)//BATCH_SIZE}: {len(results)} resolved")

    print(f"\nResolved {len(users)}/{len(usernames)} usernames to user IDs")

    replace = "--replace" in sys.argv
    conn = psycopg2.connect(db_url)
    cur = conn.cursor()

    if replace:
        print("Replace mode: disabling all existing accounts...")
        cur.execute("UPDATE twitter_accounts SET enabled = FALSE, updated_at = NOW()")
        print(f"  Disabled {cur.rowcount} existing accounts")

    print("Upserting into twitter_accounts...")
    inserted = 0
    updated = 0
    for u in users:
        cur.execute(
            """INSERT INTO twitter_accounts (user_id, username)
               VALUES (%s, %s)
               ON CONFLICT (user_id) DO UPDATE
                 SET enabled = TRUE, username = EXCLUDED.username, updated_at = NOW()""",
            (u["id"], u["username"]))
        if cur.statusmessage == "INSERT 0 1":
            inserted += 1
        else:
            updated += 1
    conn.commit()
    cur.close()
    conn.close()
    print(f"  New: {inserted}, Re-enabled/updated: {updated}")
    print("Done.")


if __name__ == "__main__":
    main()
