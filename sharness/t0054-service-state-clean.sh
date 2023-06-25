#!/bin/bash

test_description="Test service state import"

. lib/test-lib.sh

test_ipfs_init
test_cluster_init

test_expect_success IPFS,CLUSTER "state cleanup refreshes state on restart (crdt)" '
     cid=`docker exec ipfs sh -c "echo test_54 | ipfs add -q"` &&
     rep-mgr-ctl pin add "$cid" && sleep 5 &&
     rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
     rep-mgr-ctl status "$cid" | grep -q -i "PINNED" &&
     [ 1 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ] &&
     cluster_kill && sleep 5 &&
     rep-mgr-service --config "test-config" state cleanup -f &&
     cluster_start && sleep 5 &&
     [ 0 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ]
'

test_expect_success IPFS,CLUSTER "export + cleanup + import == noop (crdt)" '
    cid=`docker exec ipfs sh -c "echo test_54 | ipfs add -q"` &&
    rep-mgr-ctl pin add "$cid" && sleep 5 &&
    [ 1 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ] &&
    cluster_kill && sleep 5 &&
    rep-mgr-service --config "test-config" state export -f import.json &&
    rep-mgr-service --config "test-config" state cleanup -f &&
    rep-mgr-service --config "test-config" state import -f import.json &&
    cluster_start && sleep 5 &&
    rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
    rep-mgr-ctl status "$cid" | grep -q -i "PINNED" &&
    [ 1 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ]
'

cluster_kill
sleep 5
test_cluster_init "" raft

test_expect_success IPFS,CLUSTER "state cleanup refreshes state on restart (raft)" '
     cid=`docker exec ipfs sh -c "echo test_54 | ipfs add -q"` &&
     rep-mgr-ctl pin add "$cid" && sleep 5 &&
     rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
     rep-mgr-ctl status "$cid" | grep -q -i "PINNED" &&
     [ 1 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ] &&
     cluster_kill && sleep 5 &&
     rep-mgr-service --config "test-config" state cleanup -f &&
     cluster_start && sleep 5 &&
     [ 0 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ]
'

test_expect_success IPFS,CLUSTER "export + cleanup + import == noop (raft)" '
    cid=`docker exec ipfs sh -c "echo test_54 | ipfs add -q"` &&
    rep-mgr-ctl pin add "$cid" && sleep 5 &&
    [ 1 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ] &&
    cluster_kill && sleep 5 &&
    rep-mgr-service --config "test-config" state export -f import.json &&
    rep-mgr-service --config "test-config" state cleanup -f &&
    rep-mgr-service --config "test-config" state import -f import.json &&
    cluster_start && sleep 5 &&
    rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
    rep-mgr-ctl status "$cid" | grep -q -i "PINNED" &&
    [ 1 -eq "$(rep-mgr-ctl --enc=json status | jq -n "[inputs] | length")" ]
'


test_clean_ipfs
test_clean_cluster

test_done
