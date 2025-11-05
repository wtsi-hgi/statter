# statter

[![CI](https://github.com/wtsi-hgi/statter/actions/workflows/go-checks.yml/badge.svg)](https://github.com/wtsi-hgi/statter/actions)

--
    import "github.com/wtsi-hgi/statter/client"

A simple program to get inodes for paths; useful for when the filesystem might
be unreliable and you want to be able to provide a timeout for the stat call.

## Installation

```bash
go install github.com/wtsi-hgi/statter@latest
```

## Usage

The program needs reads length (little endian, uint16) prefixed strings from
stdin and prints inodes (little endian, uint64) to stdout.

Any error is printed to stderr and the process is stopped with exit code 1.

You can specify the `-timeout` flag to change the state timeout from the default
`1s`.