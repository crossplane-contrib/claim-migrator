# claim-migrator

This utility is EXPERIMENTAL. Use with Caution.

Migrate Crossplane Claims from one namespace to another.

## Usage

Let's say we have a claim in the `src-ns` namespace:

```shell
$ kubectl get claim  -A
NAMESPACE   NAME                               SYNCED   READY   CONNECTION-SECRET   AGE
src-ns      tenant.k8s.example.com/dev-teams   True     True                        10s
```

To migrate this claim to `dest-ns` run:

```shell
./claim-migrator migrate -n src-ns --dest-namespace dest-ns tenant.k8s.example.com/dev-teams
```

Confirm the Claim has been migrated:

```shell
kubectl get claim -A
NAMESPACE   NAME                               SYNCED   READY   CONNECTION-SECRET   AGE
dest-ns     tenant.k8s.example.com/dev-teams   True     True                        36s
```

## Building

```shell
go build
```
