# ⛔ DEPRECATED
The server is moving into the [go-gma](https://github.com/MadScienceZone/go-gma) project
as of the v5.0.0 release of the latter.

[![Coverage Status](https://coveralls.io/repos/github/MadScienceZone/go-gma-server/badge.svg?branch=main)](https://coveralls.io/github/MadScienceZone/go-gma-server?branch=main)
![GitHub](https://img.shields.io/github/license/MadScienceZone/go-gma-server)
# go-gma-server
Go port of the GMA mapper service.

This is a work in progress. It represents an expanded and reworked
version of my initial stab at a Go port of the original Python server,
now that the newer protocol and supporting API have been better defined.

# Previous unversioned work
This is a personal side project, and the Go implementation was done in spare time
over a week or so, so I'm not saying this is a thoroughly designed and tested
commercial-grade product, but rather it does demonstrate something I created
using Go that is simple enough to look at as an isolated sample, yet still
complex enough to involve multiple coroutines, mutexes, database integration,
network I/O, a very simple authentication mechanism, etc.

In the man directory are manual page entries for the server and relevant 
internal routines which provide well-defined supporting functions used by the
server. (In the original Python implementation, these functions are part of
a library common to several related tools which include the game server and
various game clients that connect to it.)

The protocol implemented by the server for communication with its clients
is documented in the mapper(6) client manual page, toward the end of that
document.

## Documentation
The full [GMA manual](https://www.madscience.zone/gma/gma.pdf) includes
notes on the usage of the server and clients (although this will need to
be updated to come into line with innovations introduced by this
implementation).

Relevant manual pages (included in the appendices of the full manual)
include mapper(6) which describes the network protocol implemented by this
server and the Go API modules documented on [pkg.go.dev](https://pkg.go.dev/github.com/MadScienceZone/go-gma/v4).

## Versioning
Once the project is ready for production use, it will be synchronized
with at least the major version number of all compatible modules of GMA.
Until then, it will use its own pre-release version number.

## Author
Steve Willoughby [steve@madscience.zone](mailto:steve@madscience.zone)

## Legal Notice
GMA uses trademarks and/or copyrights owned by Paizo Inc., used under Paizo's 
Community Use Policy ([paizo.com/communityuse]()). We are expressly prohibited from 
charging you to use or access this content. GMA is not published, endorsed, or 
specifically approved by Paizo. For more information about Paizo Inc. and Paizo 
products, visit [paizo.com]().
