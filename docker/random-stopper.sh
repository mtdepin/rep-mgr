#! /bin/sh

# $1=sleep time, $2=first command, $3=second command
# Loop forever
STOPPED=1
sleep 2
while true; do
    if [ "$(($RANDOM % 10))" -gt "4" ]; then
        # Take down cluster
        if [ "$STOPPED" -eq "1" ]; then
            STOPPED=0
            killall -STOP rep-mgr-service
        else # Take down cluster
            STOPPED=1
            killall -CONT rep-mgr-service
        fi
    fi
    sleep 1
done
