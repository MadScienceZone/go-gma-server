/*
########################################################################################
#  _______  _______  _______                ___       ______       __    _______       #
# (  ____ \(       )(  ___  )              /   )     / ___  \     /  \  / ___   )      #
# | (    \/| () () || (   ) |             / /) |     \/   \  \    \/) ) \/   )  |      #
# | |      | || || || (___) |            / (_) (_       ___) /      | |     /   )      #
# | | ____ | |(_)| ||  ___  |           (____   _)     (___ (       | |   _/   /       #
# | | \_  )| |   | || (   ) | Game           ) (           ) \      | |  /   _/        #
# | (___) || )   ( || )   ( | Master's       | |   _ /\___/  / _  __) (_(   (__/\      #
# (_______)|/     \||/     \| Assistant      (_)  (_)\______/ (_) \____/\_______/      #
#                                                                                      #
########################################################################################
*/

package mapservice

import (
	"log"
	"net"
	"strings"

	"github.com/MadScienceZone/go-gma/v5/auth"
	"github.com/MadScienceZone/go-gma/v5/mapper"
)

//
// Server-side analogue to the go-gma/v4/mapper/mapclient.go functions.
// These manage the server's connections to all of its clients.
//
// EXPERIMENTAL CODE
//
// THIS PACKAGE IS STILL A WORK IN PROGRESS	and has not been
// completely tested yet. Although GMA generally is a stable
// product, this module of it is new, and is now.
//

//
// Connection describes a connection to a client.
//
type Connection struct {
	// Context context.Context
	// Endpoint string

	// Client identity information is held here, even if
	// we are not requiring client authentication.
	Authenticator *auth.Authenticator

	// What messages does this client wish to receive?
	Subscriptions map[ServerMessage]bool

	clientConn mapper.MapConnection

	// Not sure if these should be here
	Logger         *log.Logger
	DebuggingLevel uint
}

//
// Log debugging info at the given level.
//
func (c *Connection) debug(level input, msg string) {
	if c.DebuggingLevel >= level && c.Logger != nil {
		for i, line := range strings.Split(msg, "\n") {
			if line != "" {
				c.Logger.Printf("DEBUG%d.%02d: %s", level, i, line)
			}
		}
	}
}

//
// Close terminates the connection to the client.
//
func (c *Connection) Close() {
	c.debug(1, "Close()")
	c.clientConn.Close()
}

//
// ServeForever listens for incoming connections and dispatches
// client session goroutines for each as they arrive.
//
func ServeForever(endpoint string) error {
	incoming, err := net.Listen("tcp", endpoint)
	if err != nil {
		return err
	}
	for {
		client, err := incoming.Accept()
		if err != nil {
			// TODO
		}
		// go handle the client
		// client.RemoteAddr().String()
		// client.Read([]byte) (int, error)
		// client.Write([]byte) (int, error)
	}
}
