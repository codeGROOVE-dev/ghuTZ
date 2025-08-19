#!/bin/sh
GCP_PROJECT="ghutz-468911"
GCP_REGION="us-central1"
APP_ID="ghutz"

# exit if any step fails
set -eux -o pipefail

# The Google Artifact Registry repository to create
export KO_DOCKER_REPO="gcr.io/${GCP_PROJECT}/${APP_ID}"

# Publish the code at . to $KO_DOCKER_REPO
IMAGE="$(ko publish ./cmd/gutz-server/...)"

# Deploy the newly built binary to Google Cloud Run with environment variables for ADC
gcloud run deploy "${APP_ID}" \
  --image="${IMAGE}" \
  --region "${GCP_REGION}" \
  --project "${GCP_PROJECT}"
