# Changelog

See [RELEASE](./RELEASE.md) for workflow instructions.

## UNRELEASED

### Upgrade information

### Highlights

### Features

* [#4845](https://github.com/spacemeshos/go-spacemesh/pull/4845) API to fetche opened connections.

> grpcurl -plaintext 127.0.0.1:9093 spacemesh.v1.AdminService.PeerInfoStream

```json
{
  "id": "12D3KooWEcgADBR4zHirw7YAt6nUtCNhbPWkB2fnHAemnG5cGf2n",
  "connections": [
    {
      "address": "/ip4/46.4.81.145/tcp/5001",
      "uptime": "1359.186975782s",
      "outbound": true
    }
  ]
}
{
  "id": "12D3KooWHK5m83sNj2eNMJMGAngcS9gBja27ho83t79Q2CD4iRjQ",
  "connections": [
    {
      "address": "/ip4/34.86.244.124/tcp/5000",
      "uptime": "1108.414456262s",
      "outbound": true
    }
  ],
  "tags": [
    "bootnode"
  ]
}
```

### Improvements

* [#4882](https://github.com/spacemeshos/go-spacemesh/pull/4882) Increase cache size and parametrize datastore.
* [#4887](https://github.com/spacemeshos/go-spacemesh/pull/4887) Fixed crashes on API call.