# `rep-mgr-follow`

> A tool to run IPFS Cluster follower peers

`rep-mgr-follow` allows to setup and run IPFS Cluster follower peers.

Follower peers can join collaborative clusters to track content in the
cluster. Follower peers do not have permissions to modify the cluster pinset
or access endpoints from other follower peers.

`rep-mgr-follow` allows to run several peers at the same time (each
joining a different cluster) and it is intended to be a very easy to use
application with a minimal feature set. In order to run a fully-featured peer
(follower or not), use `rep-mgr-service`.

### Usage

The `rep-mgr-follow` command is always followed by the cluster name
that we wish to work with. Full usage information can be obtained by running:

```
$ rep-mgr-follow --help
$ rep-mgr-follow --help
$ rep-mgr-follow <clusterName> --help
$ rep-mgr-follow <clusterName> info --help
$ rep-mgr-follow <clusterName> init --help
$ rep-mgr-follow <clusterName> run --help
$ rep-mgr-follow <clusterName> list --help
```

For more information, please check the [Documentation](https://ipfscluster.io/documentation), in particular the [`rep-mgr-follow` section](https://ipfscluster.io/documentation/rep-mgr-follow).


