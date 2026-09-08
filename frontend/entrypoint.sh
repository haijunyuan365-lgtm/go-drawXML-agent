#!/bin/sh

# Default to the build-time variable or localhost if not set
API_URL=${NEXT_PUBLIC_API_BASE_URL:-http://localhost:8091/api/v1}

echo "Generating env-config.js with API_URL: $API_URL"

# Write the environment configuration to a public file
cat <<EOF > /app/public/env-config.js
window.__ENV = {
  NEXT_PUBLIC_API_BASE_URL: "$API_URL",
};
EOF

# Also update the standalone server public folder if it exists (for production)
if [ -d "/app/.next/standalone/public" ]; then
  cp /app/public/env-config.js /app/.next/standalone/public/env-config.js
fi

# Execute the passed command
exec "$@"
