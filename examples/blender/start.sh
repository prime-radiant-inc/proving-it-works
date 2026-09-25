#!/usr/bin/env bash
# Start Blender on a virtual display in the blender-desk container, ready
# for movie desk to film and drive. Stop it with: docker stop blender-desk
#
# Usage: examples/blender/start.sh   (after: docker build -t movie-blender examples/blender)
set -euo pipefail
docker run -d --init --rm --name blender-desk movie-blender > /dev/null
docker exec -d blender-desk Xvfb :99 -screen 0 1600x900x24 +extension GLX
sleep 1
docker exec -d -e DISPLAY=:99 blender-desk blender --window-geometry 0 0 1600 900
echo "Blender is starting on :99 in blender-desk. Film it with:"
echo "  movie desk start SESSION --display :99 --title 'A snowman, built in Blender' -- docker exec blender-desk"
