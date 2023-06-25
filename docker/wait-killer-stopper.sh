#! /bin/sh

## Wait for cluster service process to exist and the stop the
## killer
DONE=1
while [ $DONE = 1 ]; do
    sleep 0.1
    if [ $(pgrep -f rep-mgr-service) ]; then
        kill -STOP $(cat /data/rep-mgr/random-killer-pid)
        DONE=0
    fi
done
