[![Go Report Card](https://goreportcard.com/badge/github.com/APTrust/exchange)](https://goreportcard.com/report/github.com/APTrust/exchange)

# Exchange

Exchange is a suite of back-end services that handle bag ingest, restoration and deletion for APTrust, along with ingest, replication and restoration for DPN. Each of these services is in turn composed of a number of micro services that represent a single step in the overall process of a larger service. The micro services allow the larger services to recover from failure without having to re-do large amounts of work.

Exchange is a rewrite of APTrust's original bagman code, which is functionally complete, well tested and proven, but also messy and hard to maintain. Exchange aims to replace bagman with an equally complete system that meets the following criteria:

* Code is divided into logical packages, is clear and easy to maintain and extend
* Code relies on the fewest possible external systems (i.e. remove NSQ)
* Code uses Amazon's Elastic File System, instead of EBS (i.g. remove volume manager)
* Code provides a simple, extensible REST client (instead of the messy Fluctus client)
* Code provides a straight-forward configuration system (currently a mess in bagman)
* Code provides a simple, consistent context manager for both cron jobs an microservices (also a mess in bagman)
* Code does not rely on any AWS services, other than S3 and Glacier

The first of these goals is the most important. The code must be clear and maintainable for the long term.

The existing bagman code will continue to run until this code is complete.

## Unit Testing

```
go test github.com/APTrust/exchange/...
```

## Integration Testing

To run integration tests, you'll need the following:

- A copy of the Pharos repo
- Environment variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY set to values that can access APTrust's test buckets
- Environment variables PHAROS_API_USER=system@aptrust.org and PHAROS_API_KEY=c3958c7b09e40af1d065020484dafa9b2a35cea0

Once you have all that, simple run `./scripts/process_items.sh` to run the integration tests.

## TODO

Most of the TODOs are embedded in the code, and you can find them with this:

```
grep -r TODO . --include=*.go
```

This is the list of global TODOs (not related to a specific package).

* Aim for ~100% test coverage. Run the above test command with the -cover option to see coverage.
