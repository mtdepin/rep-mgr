#!/bin/sh

set -e
user=ipfs

if [ -n "$DOCKER_DEBUG" ]; then
   set -x
fi

if [ `id -u` -eq 0 ]; then
    echo "Changing user to $user"
    # ensure directories are writable
    su-exec "$user" test -w "${IPFS_CLUSTER_PATH}" || chown -R -- "$user" "${IPFS_CLUSTER_PATH}"
    exec su-exec "$user" "$0" $@
fi

# Only ipfs user can get here
rep-mgr-service --version

if [ -e "${IPFS_CLUSTER_PATH}/service.json" ]; then
    echo "Found IPFS cluster configuration at ${IPFS_CLUSTER_PATH}"
else
    echo "This container only runs rep-mgr-service. ipfs needs to be run separately!"
    echo "Initializing default configuration..."
    rep-mgr-service init --consensus "${IPFS_CLUSTER_CONSENSUS}" --datastore "${IPFS_CLUSTER_DATASTORE}"
fi

exec rep-mgr-service $@
