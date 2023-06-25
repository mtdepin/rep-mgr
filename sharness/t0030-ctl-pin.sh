#!/bin/bash

test_description="Test cluster-ctl's pinning and unpinning functionality"

. lib/test-lib.sh

test_ipfs_init
test_cluster_init

test_expect_success IPFS,CLUSTER "pin data to cluster with ctl" '
    cid=`docker exec ipfs sh -c "echo test | ipfs add -q"`
    rep-mgr-ctl pin add "$cid" &&
    rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
    rep-mgr-ctl status "$cid" | grep -q -i "PINNED"
'

test_expect_success IPFS,CLUSTER "unpin data from cluster with ctl" '
    cid=`rep-mgr-ctl --enc=json pin ls | jq -r ".cid" | head -1`
    rep-mgr-ctl pin rm "$cid" &&
    !(rep-mgr-ctl pin ls "$cid" | grep -q "$cid") &&
    rep-mgr-ctl status "$cid" | grep -q -i "UNPINNED"
'

test_expect_success IPFS,CLUSTER "wait for data to pin to cluster with ctl" '
    cid=`docker exec ipfs sh -c "dd if=/dev/urandom bs=1024 count=2048 | ipfs add -q"`
    rep-mgr-ctl pin add --wait "$cid" | grep -q -i "PINNED" &&
    rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
    rep-mgr-ctl status "$cid" | grep -q -i "PINNED"
'

test_expect_success IPFS,CLUSTER "wait for data to unpin from cluster with ctl" '
    cid=`rep-mgr-ctl --enc=json pin ls | jq -r ".cid" | head -1`
    rep-mgr-ctl pin rm --wait "$cid" | grep -q -i "UNPINNED" &&
    !(rep-mgr-ctl pin ls "$cid" | grep -q "$cid") &&
    rep-mgr-ctl status "$cid" | grep -q -i "UNPINNED"
'

test_expect_success IPFS,CLUSTER "wait for data to pin to cluster with ctl with timeout" '
    cid=`docker exec ipfs sh -c "dd if=/dev/urandom bs=1024 count=2048 | ipfs add -q"`
    rep-mgr-ctl pin add --wait --wait-timeout 2s "$cid" | grep -q -i "PINNED" &&
    rep-mgr-ctl pin ls "$cid" | grep -q "$cid" &&
    rep-mgr-ctl status "$cid" | grep -q -i "PINNED"
'

test_expect_success IPFS,CLUSTER "wait for data to unpin from cluster with ctl with timeout" '
    cid=`rep-mgr-ctl --enc=json pin ls | jq -r ".cid" | head -1`
    rep-mgr-ctl pin rm --wait --wait-timeout 2s "$cid" | grep -q -i "UNPINNED" &&
    !(rep-mgr-ctl pin ls "$cid" | grep -q "$cid") &&
    rep-mgr-ctl status "$cid" | grep -q -i "UNPINNED"
'

cid=(`docker exec ipfs sh -c "mkdir -p /tmp/test1/test2 && touch /tmp/test1/test2/test3.txt && ipfs add -qr /tmp/test1"`)

test_expect_success IPFS,CLUSTER "pin data to cluster with ctl using ipfs paths" '
    rep-mgr-ctl pin add "/ipfs/${cid[2]}/test2/test3.txt" &&
    rep-mgr-ctl pin ls "${cid[0]}" | grep -q "${cid[0]}" &&
    rep-mgr-ctl status "${cid[0]}" | grep -q -i "PINNED"
'

test_expect_success IPFS,CLUSTER "unpin data to cluster with ctl using ipfs paths" '
    removed=(`rep-mgr-ctl pin rm "/ipfs/${cid[2]}/test2/test3.txt"`) &&
    echo "${removed[0]}" | grep -q "${cid[0]}" &&
    !(rep-mgr-ctl pin ls "${cid[0]}" | grep -q "${cid[0]}") &&
    rep-mgr-ctl status "${cid[0]}" | grep -q -i "UNPINNED"
'

test_expect_failure IPFS,CLUSTER "pin data to cluster with ctl using ipns paths" '
    name=`docker exec ipfs sh -c "ipfs name publish -Q ${cid[0]}"`
    rep-mgr-ctl pin add --wait --wait-timeout 2s "/ipns/$name" &&
    rep-mgr-ctl pin ls "${cid[0]}" | grep -q "${cid[0]}" &&
    rep-mgr-ctl status "${cid[0]}" | grep -q -i "PINNED"
'

test_expect_failure IPFS,CLUSTER "unpin data to cluster with ctl using ipns paths" '
    removed=(`rep-mgr-ctl pin rm --wait --wait-timeout 2s "/ipns/$name"`) &&
    echo "${removed[0]}" | grep -q "${cid[0]}" &&
    !(rep-mgr-ctl pin ls "${cid[0]}" | grep -q "${cid[0]}") &&
    rep-mgr-ctl status "${cid[0]}" | grep -q -i "UNPINNED"
'

test_expect_success IPFS,CLUSTER "pin data to cluster with user allocations" '
    pid=`rep-mgr-ctl --enc=json id | jq -r ".id"`
    rep-mgr-ctl pin add --allocations ${pid} -r 1 "${cid[0]}"
    rep-mgr-ctl pin ls "${cid[0]}" | grep -q "${cid[0]}" &&
    rep-mgr-ctl status "${cid[0]}" | grep -q -i "PINNED"
    allocations=`rep-mgr-ctl --enc=json pin ls | jq -r .allocations[]`
    echo $allocations | wc -w | grep -q 1 &&
    echo $allocations | grep -q ${pid}
'

test_expect_success IPFS,CLUSTER "pin update a pin" '
   cid1=`docker exec ipfs sh -c "echo test | ipfs add -q"`
   rep-mgr-ctl pin add "$cid1"
   cid2=`docker exec ipfs sh -c "echo test2 | ipfs add -q"`
   rep-mgr-ctl pin update $cid1 $cid2 &&
   rep-mgr-ctl pin ls $cid2
'

test_expect_success IPFS,CLUSTER "pin with metadata" '
   cid3=`docker exec ipfs sh -c "echo test3 | ipfs add -q"`
   rep-mgr-ctl pin add --metadata kind=text "$cid3"
   cid4=`docker exec ipfs sh -c "echo test4 | ipfs add -q"`
   rep-mgr-ctl pin add "$cid4"
   rep-mgr-ctl pin ls "$cid3" | grep -q "Metadata: yes" &&
   rep-mgr-ctl --enc=json pin ls "$cid3" | jq .metadata | grep -q "\"kind\": \"text\"" &&
   rep-mgr-ctl pin ls "$cid4" | grep -q "Metadata: no"
'

test_expect_success IPFS,CLUSTER "pin in direct mode" '
   echo "direct" > expected_mode &&
   cid=`docker exec ipfs sh -c "echo test-pin-direct | ipfs add -q -pin=false"` &&
   echo "$cid direct" > expected_pin_ls &&
   rep-mgr-ctl pin add --mode direct "$cid" &&
   rep-mgr-ctl pin ls "$cid" | grep -q "PIN-DIRECT" &&
   docker exec ipfs sh -c "ipfs pin ls --type direct $cid" > actual_pin_ls &&
   rep-mgr-ctl --enc=json pin ls "$cid" | jq -r .mode > actual_mode &&
   test_cmp expected_mode actual_mode &&
   test_cmp expected_pin_ls actual_pin_ls
'

test_clean_ipfs
test_clean_cluster

test_done
