#!/bin/bash

set -e

# Check requirements
command -v gh &> /dev/null || { echo "Error: gh CLI not installed"; exit 1; }
command -v jq &> /dev/null || { echo "Error: jq not installed"; exit 1; }

TOKEN=$(gh auth token)
[ -z "$TOKEN" ] && { echo "Error: Not authenticated with GitHub CLI"; exit 1; }

# Temp file for users
TEMP_USERS=$(mktemp)
trap "rm -f $TEMP_USERS" EXIT

echo "Collecting GitHub users..."

# Get your followers
CURRENT_USER=$(gh api user --jq '.login')
gh api "users/$CURRENT_USER/followers" --paginate --jq '.[].login' >> "$TEMP_USERS" 2>/dev/null || true

# Get org members
gh api "orgs/chainguard-dev/members" --paginate --jq '.[].login' >> "$TEMP_USERS" 2>/dev/null || true

# Get contributors for each repo
for REPO in malcontent terraform-infra-common darkfiles vex images tekton-demo rumble ssc-reading-list; do
    echo "Fetching contributors for chainguard-dev/$REPO..."
    gh api "repos/chainguard-dev/$REPO/contributors" --paginate --jq '.[].login' >> "$TEMP_USERS" 2>/dev/null || true
    gh api "repos/chainguard-dev/$REPO/commits" --paginate --jq '.[].author.login // empty' | head -100 >> "$TEMP_USERS" 2>/dev/null || true
done


# Get contributors for each repo
for REPO in triage-party; do
    echo "Fetching contributors for google/$REPO..."
    gh api "repos/google/$REPO/contributors" --paginate --jq '.[].login' >> "$TEMP_USERS" 2>/dev/null || true
    gh api "repos/google/$REPO/commits" --paginate --jq '.[].author.login // empty' | head -100 >> "$TEMP_USERS" 2>/dev/null || true
done


# Get contributors for each repo
for REPO in krata; do
    echo "Fetching contributors for edera-dev/$REPO..."
    gh api "repos/edera-dev/$REPO/contributors" --paginate --jq '.[].login' >> "$TEMP_USERS" 2>/dev/null || true
    gh api "repos/edera-dev/$REPO/commits" --paginate --jq '.[].author.login // empty' | head -100 >> "$TEMP_USERS" 2>/dev/null || true
done

# De-duplicate
UNIQUE_USERS=$(sort -u "$TEMP_USERS" | grep -v '^$' | grep -v 'null' | sort -r)
USER_COUNT=$(echo "$UNIQUE_USERS" | wc -l | tr -d ' ')

echo "Found $USER_COUNT unique users"

# Run detection API for each user
for USER in $UNIQUE_USERS; do
    echo "$USER: "
    curl -s 'https://tz.github.robot-army.dev/api/v1/detect' \
        -X POST \
        -H 'User-Agent: Precache' \
        -H 'Accept: */*' \
        -H 'Accept-Language: en-US,en;q=0.5' \
        -H 'Accept-Encoding: gzip, deflate, br, zstd' \
        -H 'Referer: https://tz.github.robot-army.dev/' \
        -H 'Content-Type: application/json' \
        -H 'Origin: https://tz.github.robot-army.dev' \
        -H 'Connection: keep-alive' \
        -H 'Sec-Fetch-Dest: empty' \
        -H 'Sec-Fetch-Mode: cors' \
        -H 'Sec-Fetch-Site: same-origin' \
        -H 'Priority: u=0' \
        -H 'TE: trailers' \
        --data-raw "{\"username\":\"$USER\"}"
    echo
    sleep 10
done
