# deploy.sh — build, push to GCR, and deploy to Cloud Run
# Run this from the backend/ directory
# Prerequisites: gcloud CLI authenticated, Docker running

PROJECT_ID="REPLACE_WITH_YOUR_GCP_PROJECT_ID"  # e.g. stride-fitness-prod
REGION="us-central1"
SERVICE="stride-backend"
IMAGE="gcr.io/$PROJECT_ID/$SERVICE"

echo "Building image..."
docker build -t $IMAGE .

echo "Pushing to GCR..."
docker push $IMAGE

echo "Deploying to Cloud Run..."
gcloud run deploy $SERVICE \
  --image $IMAGE \
  --region $REGION \
  --platform managed \
  --allow-unauthenticated \
  --min-instances 1 \
  --max-instances 10 \
  --memory 512Mi \
  --cpu 1 \
  --timeout 60 \
  --add-cloudsql-instances "$PROJECT_ID:$REGION:stride-db" \
  --set-env-vars \
    DATABASE_URL="$(gcloud secrets versions access latest --secret=database-url)",\
    CLAUDE_API_KEY="$(gcloud secrets versions access latest --secret=claude-api-key)",\
    JWT_SECRET="$(gcloud secrets versions access latest --secret=jwt-secret)"

echo "Deployed. URL:"
gcloud run services describe $SERVICE --region $REGION --format 'value(status.url)'
