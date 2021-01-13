# BCHD-monitor
A monitoring client for [BCHD](https://github.com/gcash/bchd) BitcoinCash node.

## Features
- watch for BlockHeight falling behind other nodes or arriving too late
- check for gRPC connection getting lost more often than
  other nodes (and do a traceroute)
- notify node operators via: Email, Telegram or Pushover

## Building from source

### Requirements
```
Go >= 1.13
```

### Installation
1. Rename the `config.example.yaml` file in the root to `config.yaml` and
customize your settings (BCHD address, email/notification settings, ...)

2. In the project root directory, just run:
```
./scripts/build.sh
./bin/bchd-monitor monitor
```

### Running tests
In the project root directory, just run:
```
./scripts/test.sh
```

## Contact
Follow me on [Twitter](https://twitter.com/ekliptor) and [Memo](https://memo.cash/profile/1JFKA1CabVyX98qPRAUQBL9NhoTnXZr5Zm).