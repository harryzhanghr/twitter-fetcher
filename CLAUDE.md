# CLAUDE.md — twitter-fetcher

## Critical: OAuth2 Refresh Token Handling

**NEVER** consume the refresh token (via curl or any manual API call) without capturing and storing the new refresh token that Twitter returns.

Twitter rotates the refresh token on every use — the old one is immediately invalidated. If you call the token endpoint and only extract `access_token` from the response, the new `refresh_token` is lost and the user must go through the full browser OAuth flow again to recover.

### Safe way to manually refresh:
Always capture the full response and update the Keychain in one command:

```bash
REFRESH_TOKEN=$(security find-generic-password -w -a $USER -s harry-twitter-bot.x.refresh_token)
CLIENT_ID=$(grep X_CLIENT_ID .env | cut -d= -f2-)
RESPONSE=$(curl -s -X POST "https://api.x.com/2/oauth2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=refresh_token&refresh_token=${REFRESH_TOKEN}&client_id=${CLIENT_ID}")
echo $RESPONSE | python3 -c "import sys,json; d=json.load(sys.stdin); print('access_token:', d.get('access_token','MISSING')[:30])"
NEW_REFRESH=$(echo $RESPONSE | python3 -c "import sys,json; print(json.load(sys.stdin).get('refresh_token',''))")
security add-generic-password -U -a $USER -s harry-twitter-bot.x.refresh_token -w "$NEW_REFRESH"
echo "Keychain updated with new refresh token"
```

### To re-authorize from scratch (if token is lost):
```bash
pip3 install requests
python3 scripts/oauth_setup.py
```
