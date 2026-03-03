# CLAUDE.md — twitter-fetcher

## Critical: OAuth2 Refresh Token Handling

**NEVER** consume the refresh token (via curl or any manual API call) without capturing and storing the new refresh token that Twitter returns.

Twitter rotates the refresh token on every use — the old one is immediately invalidated. If you call the token endpoint and only extract `access_token` from the response, the new `refresh_token` is lost and the user must go through the full browser OAuth flow again to recover.

### Token storage

The service supports two storage backends, configured via `TOKEN_STORE` in `.env`:

- **`file`** (default): Stores the refresh token in `.refresh_token` (configurable via `REFRESH_TOKEN_FILE`). Can bootstrap from `X_REFRESH_TOKEN` env var.
- **`keychain`**: Uses macOS Keychain via the `security` CLI. Service name configurable via `KEYCHAIN_SERVICE` (default: `twitter-fetcher.refresh_token`).

### Safe way to manually refresh (keychain mode):
Always capture the full response and update the Keychain in one command:

```bash
KEYCHAIN_SERVICE=${KEYCHAIN_SERVICE:-twitter-fetcher.refresh_token}
REFRESH_TOKEN=$(security find-generic-password -w -a $USER -s $KEYCHAIN_SERVICE)
CLIENT_ID=$(grep X_CLIENT_ID .env | cut -d= -f2-)
RESPONSE=$(curl -s -X POST "https://api.x.com/2/oauth2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=refresh_token&refresh_token=${REFRESH_TOKEN}&client_id=${CLIENT_ID}")
echo $RESPONSE | python3 -c "import sys,json; d=json.load(sys.stdin); print('access_token:', d.get('access_token','MISSING')[:30])"
NEW_REFRESH=$(echo $RESPONSE | python3 -c "import sys,json; print(json.load(sys.stdin).get('refresh_token',''))")
security add-generic-password -U -a $USER -s $KEYCHAIN_SERVICE -w "$NEW_REFRESH"
echo "Keychain updated with new refresh token"
```

### To re-authorize from scratch (if token is lost):
```bash
pip3 install -r requirements.txt
python3 scripts/oauth_setup.py
```
