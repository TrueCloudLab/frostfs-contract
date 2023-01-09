<p align="center">
<img src="./.github/logo.svg" width="500px" alt="FrostFS">
</p>
<p align="center">
  <a href="https://frostfs.info">FrostFS</a> related smart contracts.
</p>

---

# Overview

FrostFS-Contract contains all FrostFS related contracts written for
[neo-go](https://github.com/nspcc-dev/neo-go) compiler. These contracts
are deployed both in the mainchain and the sidechain.

Mainchain contracts:

- neofs
- processing

Sidechain contracts:

- alphabet
- audit
- balance
- container
- neofsid
- netmap
- nns
- proxy
- reputation
- subnet

# Getting started 

## Prerequisites

To compile smart contracts you need:

-   [neo-go](https://github.com/nspcc-dev/neo-go) >= 0.99.2

## Compilation

To build and compile smart contract, run `make all` command. Compiled contracts
`*_contract.nef` and manifest `config.json` files are placed in the 
corresponding directories. 

```
$ make all
/home/user/go/bin/cli contract compile -i alphabet -c alphabet/config.yml -m alphabet/config.json -o alphabet/alphabet_contract.nef
/home/user/go/bin/cli contract compile -i audit -c audit/config.yml -m audit/config.json -o audit/audit_contract.nef
/home/user/go/bin/cli contract compile -i balance -c balance/config.yml -m balance/config.json -o balance/balance_contract.nef
/home/user/go/bin/cli contract compile -i container -c container/config.yml -m container/config.json -o container/container_contract.nef
/home/user/go/bin/cli contract compile -i neofsid -c neofsid/config.yml -m neofsid/config.json -o neofsid/neofsid_contract.nef
/home/user/go/bin/cli contract compile -i netmap -c netmap/config.yml -m netmap/config.json -o netmap/netmap_contract.nef
/home/user/go/bin/cli contract compile -i proxy -c proxy/config.yml -m proxy/config.json -o proxy/proxy_contract.nef
/home/user/go/bin/cli contract compile -i reputation -c reputation/config.yml -m reputation/config.json -o reputation/reputation_contract.nef
/home/user/go/bin/cli contract compile -i subnet -c subnet/config.yml -m subnet/config.json -o subnet/subnet_contract.nef
/home/user/go/bin/cli contract compile -i nns -c nns/config.yml -m nns/config.json -o nns/nns_contract.nef
/home/user/go/bin/cli contract compile -i neofs -c neofs/config.yml -m neofs/config.json -o neofs/neofs_contract.nef
/home/user/go/bin/cli contract compile -i processing -c processing/config.yml -m processing/config.json -o processing/processing_contract.nef
```

You can specify path to the `neo-go` binary with `NEOGO` environment variable:

```
$ NEOGO=/home/user/neo-go/bin/neo-go make all
```

Remove compiled files with `make clean` or `make mr_proper` command.

# Testing
Smartcontract tests reside in `tests/` directory. To execute test suite
after applying changes, simply run `make test`.
```
$ make test
ok      github.com/TrueCloudLab/frostfs-contract/tests       0.462s
```

# FrostFS API compatibility

| neofs-contract version |                                                                                           supported FrostFS API versions                                                                                           |
|:----------------------:|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|
|         v0.9.x         |                                    [v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)                                    |
|        v0.10.x         |                                    [v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0)                                    |
|        v0.11.x         | [v2.7.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.7.0), [v2.8.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.8.0), [v2.9.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.9.0) |
|        v0.12.x         |                                                                      [v2.10.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.10.0)                                                                      |
|        v0.13.x         |                                                                      [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0)                                                                      |
|        v0.14.x         |                                                                      [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0)                                                                      |
|        v0.15.x         |                                  [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0), [v2.12.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.12.0)                                  |
|        v0.15.x         |                                  [v2.11.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.11.0), [v2.12.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.12.0)                                  |
|        v0.16.x         |                                                                      [v2.14.0](https://github.com/nspcc-dev/neofs-api/releases/tag/v2.14.0)                                                                      |


# License

This project is licensed under the GPLv3 License - see the 
[LICENSE.md](LICENSE.md) file for details
