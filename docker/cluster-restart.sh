#!/bin/sh

# Restart the cluster process
sleep 4
while true; do
  export CLUSTER_SECRET=""
  pgrep rep-mgr-service || echo "CLUSTER RESTARTING"; rep-mgr-service daemon --debug &
  sleep 10
done
