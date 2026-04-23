# deploy.sh — build, push to GCR, and deploy to Cloud Run
# Run this from the backend/ directory
# Prerequisites: gcloud CLI authenticated, Docker running

PROJECT_ID="stride-fitness-prod"
REGION="us-central1"
SERVICE="stride-backend"
IMAGE="gcr.io/$PROJECT_ID/$SERVICE"

echo "Building image..."
docker build --platform linux/amd64 -t $IMAGE .

echo "Pushing to GCR..."
docker push $IMAGE

echo "Granting Cloud Run service account access to secrets..."
PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')
SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:$SA" \
  --role="roles/secretmanager.secretAccessor" \
  --condition=None 2>/dev/null || true

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
  --set-secrets="DATABASE_URL=database-url:latest,CLAUDE_API_KEY=claude-api-key:latest,JWT_SECRET=jwt-secret:latest"

echo "Deployed. URL:"
gcloud run services describe $SERVICE --region $REGION --format 'value(status.url)'
