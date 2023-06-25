#!/bin/bash

test_description="Test cluster-ctl's information monitoring functionality"

. lib/test-lib.sh

test_ipfs_init
test_cluster_init

test_expect_success IPFS,CLUSTER "health graph succeeds and prints as expected" '
    rep-mgr-ctl health graph | grep -q "C0 -> I0"
'

test_expect_success IPFS,CLUSTER "health metrics with metric name must succeed" '
    rep-mgr-ctl health metrics ping &&
    rep-mgr-ctl health metrics freespace
'

test_expect_success IPFS,CLUSTER "health metrics without metric name doesn't fail" '
    rep-mgr-ctl health metrics
'

test_expect_success IPFS,CLUSTER "list latest metrics logged by this peer" '
    pid=`rep-mgr-ctl --enc=json id | jq -r ".id"`
    rep-mgr-ctl health metrics freespace | grep -q -E "(^$pid \| freespace: [0-9]+ (G|M|K)B \| Expires in: [0-9]+ seconds from now)"
'

test_expect_success IPFS,CLUSTER "alerts must succeed" '
    rep-mgr-ctl health alerts
'

test_clean_ipfs
test_clean_cluster

test_done
