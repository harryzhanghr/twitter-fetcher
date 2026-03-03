#!/usr/bin/env python3
"""
One-time OAuth2 PKCE setup for twitter-fetcher.
Opens browser for authorization, captures the callback, exchanges for tokens,
and stores the refresh token (in a file or macOS Keychain, based on TOKEN_STORE).

Usage:
    python3 scripts/oauth_setup.py
"""
import base64
import hashlib
import http.server
import os
import secrets
import subprocess
import sys
import urllib.parse
import webbrowser
from pathlib import Path

import requests

CALLBACK_PORT = 8080
CALLBACK_URL = f"http://localhost:{CALLBACK_PORT}/callback"
TOKEN_URL = "https://api.x.com/2/oauth2/token"
AUTH_URL = "https://twitter.com/i/oauth2/authorize"
SCOPES = "tweet.read users.read offline.access"

# Defaults — overridden by .env values if present.
TOKEN_STORE = "file"
KEYCHAIN_SERVICE = "twitter-fetcher.refresh_token"
REFRESH_TOKEN_FILE = ".refresh_token"


def read_env() -> dict:
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


def pkce_pair() -> tuple[str, str]:
    verifier = secrets.token_urlsafe(64)
    digest = hashlib.sha256(verifier.encode()).digest()
    challenge = base64.urlsafe_b64encode(digest).rstrip(b"=").decode()
    return verifier, challenge


def write_keychain(service: str, account: str, secret: str) -> None:
    r = subprocess.run(
        ["security", "add-generic-password", "-U", "-a", account, "-s", service, "-w", secret],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        raise SystemExit(f"Keychain write failed: {r.stderr.strip()}")


def write_token_file(path: str, token: str) -> None:
    """Write refresh token to file with restrictive permissions."""
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(token + "\n")


def main() -> None:
    env = read_env()
    client_id = env.get("X_CLIENT_ID")
    if not client_id:
        raise SystemExit("X_CLIENT_ID not found in .env")

    token_store = env.get("TOKEN_STORE", TOKEN_STORE)
    keychain_service = env.get("KEYCHAIN_SERVICE", KEYCHAIN_SERVICE)
    refresh_token_file = env.get("REFRESH_TOKEN_FILE", REFRESH_TOKEN_FILE)

    account = os.environ.get("USER", "")
    state = secrets.token_urlsafe(16)
    verifier, challenge = pkce_pair()

    auth_params = urllib.parse.urlencode({
        "response_type": "code",
        "client_id": client_id,
        "redirect_uri": CALLBACK_URL,
        "scope": SCOPES,
        "state": state,
        "code_challenge": challenge,
        "code_challenge_method": "S256",
    })
    auth_link = f"{AUTH_URL}?{auth_params}"

    print(f"Opening browser for authorization...")
    print(f"If it doesn't open, visit:\n  {auth_link}\n")
    webbrowser.open(auth_link)

    # Capture the callback.
    code_holder: dict = {}

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_): pass
        def do_GET(self):
            parsed = urllib.parse.urlparse(self.path)
            params = urllib.parse.parse_qs(parsed.query)
            code_holder["code"] = params.get("code", [None])[0]
            code_holder["state"] = params.get("state", [None])[0]
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"<h2>Authorized! You can close this tab.</h2>")

    server = http.server.HTTPServer(("localhost", CALLBACK_PORT), Handler)
    print(f"Waiting for callback on port {CALLBACK_PORT}...")
    server.handle_request()

    if code_holder.get("state") != state:
        raise SystemExit("State mismatch — possible CSRF. Aborting.")
    code = code_holder.get("code")
    if not code:
        raise SystemExit("No authorization code received.")

    # Exchange code for tokens.
    resp = requests.post(TOKEN_URL, data={
        "grant_type": "authorization_code",
        "code": code,
        "redirect_uri": CALLBACK_URL,
        "client_id": client_id,
        "code_verifier": verifier,
    }, headers={"Content-Type": "application/x-www-form-urlencoded"}, timeout=30)

    if resp.status_code != 200:
        raise SystemExit(f"Token exchange failed ({resp.status_code}): {resp.text}")

    payload = resp.json()
    refresh_token = payload.get("refresh_token")
    access_token = payload.get("access_token")
    if not refresh_token:
        raise SystemExit(f"No refresh_token in response: {payload}")

    # Store the refresh token.
    if token_store == "keychain":
        write_keychain(keychain_service, account, refresh_token)
        print(f"\nSuccess!")
        print(f"refresh_token stored in Keychain ({keychain_service})")
    else:
        project_root = Path(__file__).parent.parent
        token_path = project_root / refresh_token_file
        write_token_file(str(token_path), refresh_token)
        print(f"\nSuccess!")
        print(f"refresh_token stored in {token_path}")

    print(f"access_token (valid now): {access_token[:20]}...")


if __name__ == "__main__":
    main()
