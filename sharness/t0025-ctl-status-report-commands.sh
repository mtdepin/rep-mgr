#!/bin/bash

test_description="Test ctl's status reporting functionality.  Test errors on incomplete commands"

. lib/test-lib.sh

test_ipfs_init
test_cluster_init

test_expect_success IPFS,CLUSTER,JQ "cluster-ctl can read id" '
    id=`cluster_id`
    [ -n "$id" ] && ( rep-mgr-ctl id | egrep -q "$id" )
'

test_expect_success IPFS,CLUSTER "cluster-ctl list 1 peer" '
    peer_length=`rep-mgr-ctl --enc=json peers ls | jq -n "[inputs] | length"`
    [ $peer_length -eq 1 ]
'

test_expect_success IPFS,CLUSTER "cluster-ctl add need peer id" '
    test_must_fail rep-mgr-ctl peers add
'

test_expect_success IPFS,CLUSTER "cluster-ctl add invalid peer id" '
    test_must_fail rep-mgr-ctl peers add XXXinvalid-peerXXX
'

test_expect_success IPFS,CLUSTER "cluster-ctl rm needs peer id" '
    test_must_fail rep-mgr-ctl peers rm
'

test_expect_success IPFS,CLUSTER "cluster-ctl rm invalid peer id" '
    test_must_fail rep-mgr-ctl peers rm XXXinvalid-peerXXX
'

test_expect_success IPFS,CLUSTER "empty cluster-ctl status succeeds" '
    rep-mgr-ctl status
'

test_expect_success IPFS,CLUSTER "invalid CID status" '
    test_must_fail rep-mgr-ctl status XXXinvalid-CIDXXX
'

test_expect_success IPFS,CLUSTER "empty cluster_ctl recover should not fail" '
    rep-mgr-ctl recover
'

test_expect_success IPFS,CLUSTER "pin ls succeeds" '
    rep-mgr-ctl pin ls
'

test_expect_success IPFS,CLUSTER "pin ls on invalid CID fails" '
    test_must_fail rep-mgr-ctl pin ls XXXinvalid-CIDXXX
'

test_clean_ipfs
test_clean_cluster

test_done
