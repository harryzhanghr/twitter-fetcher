#!/usr/bin/env python3
"""
One-time OAuth2 PKCE setup for twitter-fetcher.
Opens browser for authorization, captures the callback, exchanges for tokens,
and stores the refresh token in macOS Keychain.

Usage:
    python3 scripts/oauth_setup.py
"""
import base64
import hashlib
import http.server
import os
import re
import secrets
import subprocess
import sys
import urllib.parse
import webbrowser
from pathlib import Path

import requests

KEYCHAIN_SERVICE = "harry-twitter-bot.x.refresh_token"
CALLBACK_PORT = 8080
CALLBACK_URL = f"http://localhost:{CALLBACK_PORT}/callback"
TOKEN_URL = "https://api.x.com/2/oauth2/token"
AUTH_URL = "https://twitter.com/i/oauth2/authorize"
SCOPES = "tweet.read users.read offline.access"


def read_env(key: str) -> str:
    env_path = Path(__file__).parent.parent / ".env"
    for line in env_path.read_text().splitlines():
        if line.startswith(f"{key}="):
            return line.split("=", 1)[1].strip()
    raise SystemExit(f"{key} not found in .env")


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


def main() -> None:
    client_id = read_env("X_CLIENT_ID")
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

    write_keychain(KEYCHAIN_SERVICE, account, refresh_token)
    print(f"\nSuccess!")
    print(f"refresh_token stored in Keychain ({KEYCHAIN_SERVICE})")
    print(f"access_token (valid now): {access_token[:20]}...")


if __name__ == "__main__":
    main()
