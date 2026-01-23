# statter

[![CI](https://github.com/wtsi-hgi/statter/actions/workflows/go-checks.yml/badge.svg)](https://github.com/wtsi-hgi/statter/actions)

--
    import "github.com/wtsi-hgi/statter/client"

A simple program to get either stat information for paths, or to perform a
directory walk and retrieve directory entry information; useful for when the
filesystem might be unreliable and you want to be able to provide a timeout for
the stat call, or when escalated privelages are required and a small attack
surface is wanted.

## Installation

```bash
go install github.com/wtsi-hgi/statter@latest
```

## Usage

There are client libraries for convenient access to both the `stat` and `walk`
functionality.

`client.CreateStatter` can be used to get both a `os.Lstat` like function, that
can be given paths to stat, and a 'Head' function that will read the first byte
of a file.

`client.WalkPath` can be used to walk a directory, the results of which will be
passed to the given callbacks.