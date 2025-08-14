#!/bin/bash

# GitHub organization users without location checker (GraphQL version)
# Uses GitHub CLI token for authentication
# Usage: ./script.sh <org_name>

# Check if organization name is provided
if [ $# -lt 1 ]; then
    echo "Usage: $0 <github_org>"
    echo "Example: $0 microsoft"
    exit 1
fi

ORG="$1"

# Get token from GitHub CLI
echo "Getting authentication token from GitHub CLI..."
TOKEN=$(gh auth token 2>/dev/null)

if [ -z "$TOKEN" ]; then
    echo "Error: Unable to get GitHub token"
    echo "Please ensure you're logged in with: gh auth login"
    exit 1
fi

echo "Fetching users from organization: $ORG"
echo "Using GraphQL API for efficient data retrieval..."
echo ""

# GraphQL query to get organization members with location data
# Fetches 100 members at a time with their profile information
read -r -d '' QUERY_TEMPLATE <<'EOF'
{
  organization(login: "ORG_NAME") {
    membersWithRole(first: 100, after: CURSOR) {
      pageInfo {
        hasNextPage
        endCursor
      }
      nodes {
        login
        name
        location
        company
        url
      }
    }
  }
}
EOF

# Initialize variables
has_next_page=true
cursor=null
all_users=()
page=0

# Fetch all members using GraphQL
while [ "$has_next_page" = "true" ]; do
    page=$((page + 1))
    echo "Fetching page $page..."

    # Prepare the query
    query="${QUERY_TEMPLATE//ORG_NAME/$ORG}"
    if [ "$cursor" = "null" ]; then
        query="${query//after: CURSOR/}"
    else
        query="${query//CURSOR/\"$cursor\"}"
    fi

    # Escape the query for JSON
    escaped_query=$(echo "$query" | jq -Rs .)

    # Make the GraphQL request
    response=$(curl -s -H "Authorization: bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -X POST \
        -d "{\"query\": $escaped_query}" \
        https://api.github.com/graphql)

    # Check for errors
    if echo "$response" | jq -e '.errors' > /dev/null 2>&1; then
        echo "Error in GraphQL query:"
        echo "$response" | jq -r '.errors[].message'
        exit 1
    fi

    # Check if organization exists and we have access
    if echo "$response" | jq -e '.data.organization == null' > /dev/null 2>&1; then
        echo "Error: Unable to access organization '$ORG'"
        echo "Please check the organization name and your token's permissions"
        exit 1
    fi

    # Extract pagination info
    has_next_page=$(echo "$response" | jq -r '.data.organization.membersWithRole.pageInfo.hasNextPage')
    cursor=$(echo "$response" | jq -r '.data.organization.membersWithRole.pageInfo.endCursor')

    # Process users from this page
    users=$(echo "$response" | jq -c '.data.organization.membersWithRole.nodes[]')

    while IFS= read -r user; do
        all_users+=("$user")
    done <<< "$users"

    # Show progress
    echo "  Fetched $(echo "$users" | wc -l) users (Total so far: ${#all_users[@]})"
done

echo ""
echo "Total users fetched: ${#all_users[@]}"
echo ""

# Now process all users and find those without location
users_without_location=()

for user_json in "${all_users[@]}"; do
    login=$(echo "$user_json" | jq -r '.login')
    location=$(echo "$user_json" | jq -r '.location // "null"')

    if [ "$location" = "null" ] || [ -z "$location" ]; then
        users_without_location+=("$user_json")
    fi
done

if [ ${#users_without_location[@]} -eq 0 ]; then
    echo "All users in the organization have their location set!"
else
    # Print header
    printf "%-20s %-30s %-30s %-25s\n" "Username" "Name" "Company" "Profile URL"
    printf "%-20s %-30s %-30s %-25s\n" "--------" "----" "-------" "-----------"

    # Print each user
    for user_json in "${users_without_location[@]}"; do
        login=$(echo "$user_json" | jq -r '.login')
        name=$(echo "$user_json" | jq -r '.name // "N/A"')
        company=$(echo "$user_json" | jq -r '.company // "N/A"')
        url=$(echo "$user_json" | jq -r '.url')

        printf "%-20s %-30s %-30s %-25s\n" "$login" "$name" "$company" "$url"
    done

fi

echo ""
echo "Done!"
