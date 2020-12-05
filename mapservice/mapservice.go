// vi:set ai sm nu ts=4 sw=4 fileencoding=utf-8:
/*
########################################################################################
#  _______  _______  _______                ___       _______     _______              #
# (  ____ \(       )(  ___  )              /   )     / ___   )   / ___   )             #
# | (    \/| () () || (   ) |             / /) |     \/   )  |   \/   )  |             #
# | |      | || || || (___) |            / (_) (_        /   )       /   )             #
# | | ____ | |(_)| ||  ___  |           (____   _)     _/   /      _/   /              #
# | | \_  )| |   | || (   ) | Game           ) (      /   _/      /   _/               #
# | (___) || )   ( || )   ( | Master's       | |   _ (   (__/\ _ (   (__/\             #
# (_______)|/     \||/     \| Assistant      (_)  (_)\_______/(_)\_______/             #
#                                                                                      #
########################################################################################
*/
//
////////////////////////////////////////////////////////////////////////////////////////
//                                                                                    //
//                                     MapService                                     //
//                                                                                    //
// Inter-map communication service.  Transmits map updates to other maps and allows   //
// API callers to inject events to be sent to all maps.                               //
//                                                                                    //
// This is a re-implementation from scratch of the Python GMA game server (as         //
// originally implemented in the Mapper.MapService module), in the Go language, in    //
// the hopes that this will provide better performance than the Python version.  It   //
// was also done out of personal interest to explore Go design features for a server  //
// such as this one.                                                                  //
//                                                                                    //
////////////////////////////////////////////////////////////////////////////////////////

package mapservice

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"database/sql"
	"log"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)
//
// PROTOCOL_VERSION is the version of the client/server protocol this implementation uses.
// This is a manually-set value rather than getting set automatically by
// the set_version script, because it reflects what the code in here actually does,
// not whatever version number was set for the product (which is what this
// code is _expected_ to use). We have a unit tests that flags if this value
// isn't the same as what set_version is set to expect.
//
const PROTOCOL_VERSION = "332"

//
// Turn on DEBUGGING to get extra information logged during transactions with clients
//
const DEBUGGING = false
//
// We will terminate clients if they've been idle this many seconds and we have a full
// channel of messages trying to send to them
//
const ClientIdleTimeout = 180
//
// Number of messages which can be buffered in a client channel before we resort to
// more expensive queuing
//
const CommChannelBufferSize = 256

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//  __  __              ____ _ _            _   
// |  \/  | __ _ _ __  / ___| (_) ___ _ __ | |_ 
// | |\/| |/ _` | '_ \| |   | | |/ _ \ '_ \| __|
// | |  | | (_| | |_) | |___| | |  __/ | | | |_ 
// |_|  |_|\__,_| .__/ \____|_|_|\___|_| |_|\__|
//              |_|                             
//
// MapClient keeps the context around a single active client connection.
//
type MapClient struct {
    Connection          net.Conn        // this client's socket connection
    Scanner            *bufio.Scanner   // scanner to collect input lines
    ClientAddr          string          // IP address of client
    Service            *MapService      // pointer to the overall map service
    Auth               *Authenticator   // pointer to the authentication object
    Authenticated       bool            // true if connection is authenticated (or no authentication is supported)
    ReachedEOF          bool            // true if we need to shut down this connection
    AcceptedList        []string        // list of accepted messages for this client (nil to accept all)
    dice               *DieRoller       // random number generator ala rolling dice
    WriteOnly           bool            // is this client refusing to listen to incoming messages?
    IncomingDataType    string          // what multi-command event are we processing? or ""
    IncomingData        []string        // holding buffer for multi-command sequence of events
    LastPolo            int64           // last time we heard a POLO response
    UnauthenticatedPings int            // number of times we pinged this client withouth authentication
	CommChannel			chan string		// buffered channel for data to be sent to the client
	ReadyToClose        bool			// true if we're really finished with this connection now
	messageBacklogQueue []string		// holding area for backlog of messages waiting to get into channel
    lock                sync.RWMutex    // controls concurrent access to this structure between goroutines
}

func (c *MapClient) Username() string {
	if c.Auth == nil || !c.Authenticated {
		return "unknown"
	}
	return c.Auth.Username
}

//
// Authentication protocol
// -> OK <version> <challenge>
// <- AUTH <response> [<user> [<client>]]
// -> DENIED <message> 
// OR -> GRANTED <username>
// OR -> PRIV <message>
//
func (c *MapClient) AuthenticateUser() error {
	c.Authenticated = false
	if c.Auth == nil {
		return fmt.Errorf("No authentication method defined for this server.")
	}
	c.Auth.Reset()
	challenge, err := c.Auth.GenerateChallenge()
	if err != nil {
		return err
	}
	c.Send("OK", PROTOCOL_VERSION, challenge)
	for {
		event, err := c.NextEvent()
		if err != nil {
			c.Send("DENIED", "Unable to understand response")
			return err
		}
		switch event.EventType() {
			case "POLO": // ignore
			case "AUTH":
				if len(event.Fields) >= 3 {
					c.Auth.Username = event.Fields[2]
					user_password, ok := c.Service.PersonalPasswords[c.Auth.Username]
					if ok {
						c.Auth.SetSecret(user_password)	 // use personal password if one defined for that user
					}
				} else {
					c.Auth.Username = "<unknown>"
				}
				if len(event.Fields) >= 4 {
					c.Auth.Client = event.Fields[3]
				} else {
					c.Auth.Client = "<unknown>"
				}

				successful, err := c.Auth.ValidateResponse(event.Fields[1])
				if err != nil {
					log.Printf("[client %s] ERROR validating authentication response \"%s\": %v", c.ClientAddr, event.Fields[1], err)
					c.Send("DENIED", "Invalid AUTH command format")
					return err
				}
				if !successful {
					log.Printf("[client %s] ERROR validating authentication response \"%s\": login incorrect", c.ClientAddr, event.Fields[1])
					c.Send("DENIED", "Login incorrect")
					return fmt.Errorf("Login incorrect")
				}
				if c.Auth.GmMode {
					c.Auth.Username = "GM"
					c.Send("GRANTED", "GM")
					c.Authenticated = true
					log.Printf("[client %s] Access granted for GM", c.ClientAddr)
					return nil
				}

				c.Auth.Username = strings.ToLower(c.Auth.Username)
				if c.Auth.Username == "gm" {
					log.Printf("[client %s] Access denied to GM impersonator!", c.ClientAddr)
					c.Send("DENIED", "You are not the GM.")
					return fmt.Errorf("Login incorrect")
				}

				log.Printf("[client %s] Access granted for %s", c.ClientAddr, c.Auth.Username)
				c.Send("GRANTED", c.Auth.Username)
				c.Authenticated = true
				return nil

			default:
				c.Send("PRIV", "Not authorized for that operation until authenticated.")
		}
	}
}

//
// Client communications are arranged to minimize critical paths
// around access to shared data and to avoid service to the other
// clients to lock up if one client stops accepting input. To
// implement this, we have a buffered channel for each client. Our
// service code writes to that channel (abandoning clients which
// aren't responsive enough to avoid the channel filling up. In
// our overall architecture, clients shouldn't be in that position
// short of a serious problem, and it's better for a client to 
// reconnect when it can than to drop messages or back up a big
// backlog of messages here or (worse) block waiting for the 
// channel or socket to have available space to write into.
//
// A dedicated goroutine is started for each client which feeds
// all messages sent on the client's channel out to the network
// socket connected to the client app. When the server is done
// talking to a client, we don't shut down the socket right away,
// but rather send a stop signal of "■■■" on the channel. When the
// background feeder goroutine gets that signal, it closes the socket
// and terminates its own operation.
//

//
// Local client interface to the service Send method.
// If the client requested a restricted set of messages,
// we will filter those here.
//
// Send a discrete list of fields (command and parameters)
// to a single client
//
func (c *MapClient) Send(values ...string) {
	c._send_event(nil, values)
}

func (c *MapClient) SendRaw(data string) {
	c.sendToClientChannel(data)
}

//
// Same but with extra data lines needed for some
// block-data events (e.g., LS)
//
func (c *MapClient) SendWithExtraData(ev *MapEvent) {
	c._send_event(ev.MultiRawData, ev.Fields)
}

func (c *MapClient) _send_event(extra_data []string, values []string) {
	if c.AcceptedList != nil {
		ok_to_send := false
		for _, allowed_type := range c.AcceptedList {
			if values[0] == allowed_type {
				ok_to_send = true
				break
			}
		}
		if !ok_to_send {
			if DEBUGGING {
				log.Printf("[client %s] blocked sending %v", c.ClientAddr, values)
			}
			return
		}
	}
	message, err := PackageValues(values...)
	if err != nil {
		log.Printf("[client %s] ERROR packaging data to be transmitted: %v (%v)", c.ClientAddr, err, values)
	} else {
		if strings.ContainsAny(message, "\n\r") {
			log.Printf("[client %s] ERROR packaging data to be transmitted: message would contain newlines (%v)", c.ClientAddr, values)
		} else {
			c.sendToClientChannel(message)
		}
	}

	if extra_data != nil {
		for _, data := range extra_data {
			c.SendRaw(data)
		}
	}
}


//
// Send to all other clients (other than myself)
//
func (c *MapClient) SendToOthers(values ...string) {
	for _, aClient := range c.Service.AllClients() {
		if aClient.ClientAddr != c.ClientAddr {
			aClient.Send(values...)
		}
	}
}

//
// Same, but without preprocessing. We assume the values
// string is already correctly formed to ship out to
// clients.
//
func (c *MapClient) SendRawToOthers(values string) {
	for _, aClient := range c.Service.AllClients() {
		if aClient.ClientAddr != c.ClientAddr {
			aClient.SendRaw(values)
		}
	}
}

//
// Send a message to a client.
//
// For efficiency, we have a buffered channel that feeds each
// client, but it's possible that a channel could fill up if a
// client doesn't keep up by reading messages as fast as we
// send them. We don't want the server (or any goroutine serving
// other client requests) to block on the full channel, creating
// a denial-of-service condition, so we'll fall back to storing the
// extra messages in a local queue for that client.  This is more
// expensive and requires locking, so we don't do that for all
// messages--just in case our channel fills up, which should not
// happen often if the buffers are sized appropriately.
// However, we also don't want this backlog to go on too long either,
// so we will check to see if a client we're queuing up messages for
// hasn't responded to our pings for a while, and will eventually give
// up on it if it really does look like they're not able to keep up.
//
func (c *MapClient) sendToClientChannel(data string) {
	c.lock.RLock()
	queueing := c.messageBacklogQueue != nil
	c.lock.RUnlock()

	if queueing {
		// If we're already queueing messages, just add to the backlog
		c.queueMessage(data)
		return
	}

	select {
		case c.CommChannel <- data:
			if DEBUGGING {
				log.Printf("[client %s] chan<-%s", c.ClientAddr, data)
			}

		default:
			if time.Now().Unix() - c.LastPolo > ClientIdleTimeout {
				log.Printf("[client %s] TERMINATING CONNECTION TO DEAD/PAINFULLY SLOW CLIENT", c.ClientAddr)
				c.Close()
			} else {
				// queue this message up 
				c.queueMessage(data)
			}
	}
}

func (c *MapClient) queueMessage(data string) {
	if DEBUGGING {
		log.Printf("[client %s] queued %s", c.ClientAddr, data)
	}
	c.lock.Lock()
	c.messageBacklogQueue = append(c.messageBacklogQueue, data)
	c.lock.Unlock()
}

//
// Signal that we're done with the client.
// we'll keep the channel and socket open until we know
// we have sent the remaining messages out to the client
// (including, presumably, the relevant message to the client
// about why we're terminating them)
//
func (c *MapClient) Close() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[client %s] recovered from MapClient Close error: %v", c.ClientAddr, r)
		}
	}()
	c.ReachedEOF = true
	// try to signal the background feeder that we're finished and it's
	// safe to shut down the socket, if possible
	select {
		case c.CommChannel <- "■■■":
			if DEBUGGING {
				log.Printf("[client %s] Signaling client to stop", c.ClientAddr)
			}
		default:
			log.Printf("[client %s] Unable to send stop signal to client channel; shutting down socket as last resort.", c.ClientAddr)
			c.Connection.Close()
	}
}

//
// Wait for, parse, and return the next input event from a client
// Returns error if EOF or error is reached before the next non-null event
//
func (c *MapClient) NextEvent() (*MapEvent, error) {
	for c.Scanner.Scan() {
		t := strings.TrimSpace(c.Scanner.Text())
		if t == "" {
			continue	// ignore blank input lines
		}
		new_event, err := NewMapEvent(t, "", "")
		if err != nil {
			log.Printf("[client %s] Error in incoming event: %v", c.ClientAddr, err)
			continue
		}
		return new_event, nil
	}
	err := c.Scanner.Err()
	if err == nil {
		c.ReachedEOF = true
		return nil, fmt.Errorf("EOF while waiting for input from client")
	}
	return nil, err
}

//
// Send the list of all connected clients to a client
//
func (c *MapClient) ConnResponse() {
	c.Send("CONN")
	cksum := sha256.New()
	time_now := time.Now().Unix()
	count := 0

	for _, peer := range c.Service.AllClients() {
		is := strconv.Itoa(count)
		who := "peer"
		if c.ClientAddr == peer.ClientAddr {
			who = "you"
		}
		user := peer.Username()
		client := "unknown"
		auth := "0"
		wo := "0"
		if peer.Authenticated && peer.Auth != nil {
			client = peer.Auth.Client
			auth = "1"
		}
		if peer.WriteOnly {
			wo = "1"
		}
		active_sec := fmt.Sprintf("%d", time_now - peer.LastPolo)

		c.Send("CONN:", is, who, peer.ClientAddr, user, client, auth, "0", wo, active_sec)
		ckval, err := PackageValues(is, who, peer.ClientAddr, user, client, auth, "0", wo, active_sec)
		if err != nil {
			log.Printf("[client %s] WARNING: Unable to calculate checksum for line %d of /CONN response: %v", c.ClientAddr, count, err)
		} else {
			cksum.Write([]byte(ckval))
		}
		count++
	}
	c.Send("CONN.", strconv.Itoa(count), base64.StdEncoding.EncodeToString(cksum.Sum(nil)))
}

//
// Send messages to the client socket as they arrive
// from our channel.
//
func (c *MapClient) backgroundSender() {
	checkForBacklog := false

	if DEBUGGING {
		log.Printf("[client %s] launched backgroundSender", c.ClientAddr)
	}

FeedClient:
	for {
		select {
			case message, more := <-c.CommChannel:
				if !more || message == "■■■" {
					log.Printf("[client %s] Disconnecting", c.ClientAddr)
					c.Connection.Close()
					c.ReachedEOF = true
					break FeedClient
				} else {
					if DEBUGGING {
						log.Printf("[client %s] tx: %s", c.ClientAddr, message)
					}
					c.Connection.Write([]byte(message + "\n"))
				}
				if len(c.CommChannel) == 0 {
					checkForBacklog = true
				}
		}

		if checkForBacklog {
			// To avoid spinning here repeatedly checking the queue status, we will
			// only check after reading from the channel. This means we'll
			// set checkForBacklog when, and only when, we just drained the
			// channel, which will cause us to check the queue once to see if
			// we should re-fill the channel from it. (Other concurrent routines
			// won't be sending to the channel in the mean time if the queue
			// of backlogged messages will not be empty.)
			checkForBacklog = false
			c.lock.Lock()
			if c.messageBacklogQueue != nil {
				if len(c.messageBacklogQueue) > 0 {
drainBacklog:
					for {
						select {
							case c.CommChannel <- c.messageBacklogQueue[0]:
								if DEBUGGING {
									log.Printf("[client %s] unqueue %s", c.ClientAddr, c.messageBacklogQueue[0])
								}
								if len(c.messageBacklogQueue) > 1 {
									c.messageBacklogQueue = c.messageBacklogQueue[1:]
								} else {
									c.messageBacklogQueue = nil
									break drainBacklog
								}

							default:
								// no more will fit, stop here
								break drainBacklog
						}
					}
				} else {
					c.messageBacklogQueue = nil
				}
			}
			c.lock.Unlock()
		}
	}

	if DEBUGGING {
		log.Printf("[client %s] stopped backgroundSender", c.ClientAddr)
	}
	c.ReadyToClose = true
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//  __  __            ____                  _          
// |  \/  | __ _ _ __/ ___|  ___ _ ____   _(_) ___ ___ 
// | |\/| |/ _` | '_ \___ \ / _ \ '__\ \ / / |/ __/ _ \
// | |  | | (_| | |_) |__) |  __/ |   \ V /| | (_|  __/
// |_|  |_|\__,_| .__/____/ \___|_|    \_/ |_|\___\___|
//              |_|                                    
//
// MapService runs a single server instance. It encapsulates all of the
// state and context of the service.
//
type MapService struct {
    lock                sync.RWMutex            // controls concurrent access to this structure
    AcceptIncoming      bool                    // server is accepting new connections
    serverRunning       bool                    // if false, we're shutting down operations
    outstandingClients  sync.WaitGroup          // atomic semaphore counting connected clients
    IncomingListener    net.Listener            // incoming socket for new connections
    Database            *sql.DB                 // database interface for persistent storage
    PlayerGroupPass     []byte                  // authentication password shared amongst players
    GmPass              []byte                  // authentication password for the GM
    PersonalPasswords   map[string][]byte       // set of passwords for individual players
    Clients             map[string]*MapClient   // dictionary of connected clients by client address
    InitFile            string                  // name of initial greeting file
    EventHistory        map[string]*MapEvent    // game state as mapping of key to event
    ImageList           map[string]string       // dictionary of server locations for known images
    ChatHistory         []*MapEvent             // history of messages sent to chat channel
    IdByName            map[string]string       // dictionary of object IDs by creature name
    ClassById           map[string]string       // dictionary of object classes by ID
    PlayerDicePresets   map[string][]DicePreset // dictionary mapping username to personal die roll presets
    SaveNeeded          bool                    // have we made changes to the game state since the last save?
    StopChannel         chan int                // channel used to signal time for server to stop
}

//
// Get a list of clients the server is tracking
//
func (ms *MapService) AllClients() []*MapClient {
	// This allows us to lock the global client list for as
	// little time as possible, so a routine can then go off
	// and use the list independently. 
	ms.lock.RLock()
	clients := make([]*MapClient, len(ms.Clients))
	i := 0
	for _, aClient := range ms.Clients {
		clients[i] = aClient
		i++
	}
	ms.lock.RUnlock()
	return clients
}

//
// Run starts the MapService going, listening for incoming connections
// and for each one spawning a goroutine to handle the conversation with
// it from that point forward.
//
func (ms *MapService) Run() {
	var err error
	//
	// load all user presets into memory for quick recall later
	//
	if ms.Database != nil {
		ms.PlayerDicePresets, err = LoadDicePresets(ms.Database)
		if err != nil {
			log.Printf("Unable to preload dice presets! (%v)", err)
			ms.EmergencyStop()
			return
		}
		err = ms.LoadState()
		if err != nil {
			log.Printf("Unable to preload game state! (%v)", err)
			ms.EmergencyStop()
			return
		}
	}
	//
	// Initialize
	//
	ms.AcceptIncoming = true
	ms.serverRunning = true
	ms.IdByName = make(map[string]string)
	ms.ClassById = make(map[string]string)

	for ms.serverRunning {
		client, err := ms.IncomingListener.Accept()
		if err != nil {
			if !ms.serverRunning {
				// we got here because serverRunning flipped to false and the incoming
				// socket closed while we were waiting to accept a connection on it.
				// So in that case it's not really an unexpected condition at all.
				break
			}
			log.Printf("Error accepting incoming connection: %v", err)
		} else {
			go func () {
				ms.outstandingClients.Add(1)
				defer ms.outstandingClients.Done()
				ms.HandleClientConnection(client)
			}()
		}
	}
}

//
// Send a periodic "keep-alive" ping to all connected clients.
// returns true if there were any connected clients to send to.
//
func (ms *MapService) PingAll() bool {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("caught attempt to ping too late to client: %v", r)
		}
	}()

	i := 0
	for _, client := range ms.AllClients() {
		if !client.Authenticated {
			client.UnauthenticatedPings++
			if client.UnauthenticatedPings > 2 {
				log.Printf("[client %s] timeout waiting for successful authentication", client.ClientAddr)
				client.Send("DENIED", "No successful login made in time.")
				client.Close()
			}
		}
		client.Send("MARCO")
		i++
	}
	if i > 0 {
		log.Printf("Pinged %d client%s", i, plural(i))
	}
	return i > 0
}

//
// Initiate a shutdown. This will terminate the incoming connection loop
// and wait for any existing connections to finish what they were doing.
//
func (ms *MapService) EmergencyStop() {
	ms.StopChannel <- 1
}

func (ms *MapService) Shutdown() {
	ms.lock.Lock()
	ms.AcceptIncoming = false
	ms.serverRunning = false
	ms.lock.Unlock()

	log.Printf("MapService waiting for outstanding clients to exit...")
	ms.outstandingClients.Wait()
	log.Printf("Done. Proceeding to shut down...")
}

//
// Format a set of strings as a TCL list to conform to the protocol
// specification.
//
func PackageValues(values ...string) (string, error) {
	return ToTclString(values)
}

//
// Maintain the list of our client connections, for operations where we
// need to send messages to some or all of them.
//
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (ms *MapService) AddClient(newClient *MapClient) error {
	ms.lock.Lock()
	_, ok := ms.Clients[(*newClient).ClientAddr]
	if ok {
		ms.lock.Unlock()
		return fmt.Errorf("Already tracking a connection for client %v; unable to add another", newClient.ClientAddr)
	}
	ms.Clients[(*newClient).ClientAddr] = newClient
	log.Printf("Now %d connected client%s", len(ms.Clients), plural(len(ms.Clients)))
	ms.lock.Unlock()

	// notify everyone of the change
	ms.NotifyPeerChange(newClient.Username(), "joined")
	return nil
}

// Wait for pending output to drain on the network socket before
// fully closing it.

func (ms *MapService) WaitAndRemoveClient(oldClient *MapClient) {
	log.Printf("[client %s] Waiting to close socket", (*oldClient).ClientAddr)
	for !(*oldClient).ReadyToClose {
		time.Sleep(10 * time.Millisecond)
	}
	(*oldClient).Connection.Close()
	log.Printf("[client %s] Closed socket", (*oldClient).ClientAddr)
}

func (ms *MapService) RemoveClient(oldClient string) {
	ms.lock.RLock()
	oldClientObj, ok := ms.Clients[oldClient]
	ms.lock.RUnlock()
	if ok {
		go ms.WaitAndRemoveClient(oldClientObj)
		ms.lock.Lock()
		delete(ms.Clients, oldClient)
		ms.lock.Unlock()
		log.Printf("Now %d connected client%s", len(ms.Clients), plural(len(ms.Clients)))
	}
	// notify everyone of the change
	ms.NotifyPeerChange(oldClientObj.Username(), "left")
}

func (ms *MapService) NotifyPeerChange(username, action string) {
	for _, peer := range ms.AllClients() {
		if peer.Authenticated {
			peer.Send("//", username, action)
			peer.ConnResponse()
		}
	}
}

//
// HandleClientConnection is invoked as a coroutine for each incoming client
// connection, and handles the communication with that client throughout
// the life of the connection.
//
func (ms *MapService) HandleClientConnection(clientConnection net.Conn) {
	dieRoller, err := NewDieRoller()
	if err != nil {
		log.Printf("Internal error: Unable to create new die roller for client at %s: %v",
			clientConnection.RemoteAddr().String(), err)
		clientConnection.Close()
		return
	}

	thisClient := MapClient {
		Connection:    clientConnection,
		ClientAddr:    clientConnection.RemoteAddr().String(),
		Scanner:       bufio.NewScanner(clientConnection),
		Service:       ms,
		Authenticated: false,
		dice:          dieRoller,
		LastPolo:	   time.Now().Unix(),
		CommChannel:   make(chan string, CommChannelBufferSize),
	}
	log.Printf("Incoming connection from %s", thisClient.ClientAddr)
	defer ms.WaitAndRemoveClient(&thisClient)
	go thisClient.backgroundSender()
	sync_client := false

	//
	// Start by sending our greeting to the client
	//
	if !ms.AcceptIncoming {
		log.Printf("[client %s] DENIED access (server not accepting new connections at this time).", thisClient.ClientAddr)
		thisClient.Send("DENIED", "Server is not ready to accept connections. Try again later.")
		goto end_connection
	}

	err = ms.AddClient(&thisClient)
	if err != nil {
		thisClient.Send("DENIED", "Internal error setting up connection.")
		log.Printf("[client %s] ERROR adding client to list: %v", thisClient.ClientAddr, err)
		goto end_connection
	}

	//
	// Initial server greeting from InitFile
	//
	if ms.InitFile != "" {
		fp, err := os.Open(ms.InitFile)
		if err != nil {
			log.Printf("[client %s] ERROR opening %s: %v", thisClient.ClientAddr, ms.InitFile, err)
		} else {
			scanner := bufio.NewScanner(fp)
			for scanner.Scan() {
				init_text := scanner.Text()
				if init_text == "SYNC" {
					sync_client = true
				} else if len(init_text) >= 4 && init_text[0:4] == "LOAD" {
					log.Printf("Ignoring deprecated init-file directive: %s", init_text)
				} else {
					thisClient.SendRaw(scanner.Text())
				}
			}
			fp.Close()
		}
	}

	//
	// Authenticate the user if we have passwords set for the service
	//
	if ms.PlayerGroupPass != nil || ms.GmPass != nil {
		thisClient.Auth = &Authenticator{
			GmSecret: ms.GmPass,
			Secret:   ms.PlayerGroupPass,
			GmMode:   false,
		}
		err := thisClient.AuthenticateUser()
		if err != nil {
			log.Printf("[client %s] Dropping connection due to authentication error: %v", thisClient.ClientAddr, err)
			goto end_connection
		}
		ms.NotifyPeerChange(thisClient.Username(), "authenticated")
	} else {
		// proceed without authentication (since this server is not configured
		// to do authentication at all)
		thisClient.Authenticated = true		// vacuously
		thisClient.Send("OK", PROTOCOL_VERSION)
		ms.NotifyPeerChange(thisClient.Username(), "joined")
	}

	if sync_client {
		ms.Sync(&thisClient)
	}

	//
	// Read input events from the client and act upon them
	//
	for !thisClient.ReachedEOF {
		event, err := thisClient.NextEvent()
		if thisClient.ReachedEOF {
			break
		}
		if err != nil {
			log.Printf("[client %s] error reading input: %v", thisClient.ClientAddr, err)
			goto end_connection
		} else {
			// interpret the event
			log.Printf("[client %s] event %v; key %v", thisClient.ClientAddr, event.Fields, event.Key)
			ms.ExecuteAction(event, &thisClient)
		}
	}

	for thisClient.Scanner.Scan() {
		event := thisClient.Scanner.Text()
		log.Printf("[client %s] Received event [%s]", thisClient.ClientAddr, event)
	}

end_connection:
	ms.RemoveClient(thisClient.ClientAddr)
	thisClient.Close()
	if commError := thisClient.Scanner.Err(); commError != nil {
		log.Printf("[client %s] I/O Error on connection: %v", thisClient.ClientAddr, commError)
	}
	if DEBUGGING {
		log.Printf("[client %s] Exiting client handler", thisClient.ClientAddr)
	}
}

//
// The serivce maintains a sense of the current state of the game,
// in the form of a list of events received; each event has a "key"
// which controls resolution of obsolete events, i.e., when any
// event arrives, it overwrites any previous event with the same key.
//
// This way, a client can ask for the set of currently-stored events
// and apply them to its own state to be caught up to where the other
// clients are, without needlessly acting on old state information which
// was subsequently made obsolete.
//
var nextEventSequence int = 0

func (ms *MapService) UpdateState(event *MapEvent) {
	// Events with a blank key are ones we aren't going to bother
	// tracking here since they don't really change the state of the
	// game.
	if event.Key != "" {
		//
		// store this as the most recent with the given key.
		// we store the sequence number in each one so they can be
		// played back in the correct order, but store by key since
		// most of the time that's what we're doing (so the more
		// expensive operation of sorting by sequence number only
		// happens occasionally).
		//
		ms.lock.Lock()
		event.Sequence = nextEventSequence
		nextEventSequence++
		ms.EventHistory[event.Key] = event
		ms.SaveNeeded = true
		ms.lock.Unlock()
	}
}

//
// Act on the incoming event. Many will just be echoed to the other
// clients, but a few require special processing, which we'll do here.
//
func (ms *MapService) ExecuteAction(event *MapEvent, thisClient *MapClient) {
	switch event.EventType() {
		// Effectively a no-op. Ignore completely.
		case "MARCO":
			return

		// Also a no-op, but we're interested in how long it's been since
		// we received an answer to our keep-alive pings, so we'll record
		// this.
		case "POLO":
			thisClient.LastPolo = time.Now().Unix()
			return

		// Events not allowed to clients
		case "AC", "CONN", "CONN:", "CONN.", "DENIED", "GRANTED", "ROLL", "OK", "PRIV":
			thisClient.Send("//", "Clients not allowed to send this command", event.EventType())

		// Events simply relayed to all other clients
		case "//", "AI", "AI:", "AI.", "AV", "CLR@", "L", "M", "M?", "M@", "MARK":
			thisClient.SendToOthers(event.Fields...)

		// Events simply relayed, but restricted to GM only
		case "CO", "CS", "DSM", "I", "IL", "TB":
			if !thisClient.Authenticated || (thisClient.Auth != nil && !thisClient.Auth.GmMode) {
				log.Printf("[client %s] DENIED privileged command %v to non-GM user", thisClient.ClientAddr, event.Fields)
				thisClient.Send("PRIV", "You are not authorized to use the %v command", event.EventType())
				return
			}
			thisClient.SendToOthers(event.Fields...)

		// ACCEPT <message set>
		//
		// Restrict messages to this client to include only those in the set,
		// unless <message set> is *, in which case accept all messages.
		case "ACCEPT":
			if event.Fields[1] == "*" {
				thisClient.AcceptedList = nil
			} else {
				allowed, err := ParseTclList(event.Fields[1])
				if err != nil {
					log.Printf("[client %s] Error understanding ACCEPT command: %v", thisClient.ClientAddr, err)
				} else {
					thisClient.AcceptedList = allowed
				}
			}
			log.Printf("[client %s] accepting %v", thisClient.ClientAddr, thisClient.AcceptedList)

		// AI? <name> <size>
		//
		// Client requests definition of the given image by name and size.
		// If we know, we'll send an AI@ event to answer the question directly.
		// Otherwise, we'll forward on the question to the other clients to answer.
		case "AI?":
			ms.lock.RLock()
			server_location, ok := ms.ImageList[event.Fields[1] + "‖" + event.Fields[2]]
			ms.lock.RUnlock()
			if ok {
				thisClient.Send("AI@", event.Fields[1], event.Fields[2], server_location)
			} else {
				thisClient.SendToOthers(event.Fields...)
			}

		// AI@ <name> <size> <server_id>
		//
		// Declare that the image with the given <name> and <size>
		// may be found at the given <server_id>. If we receive this,
		// we remember that location so we can use it to answer subsequent
		// queries for that image.
		case "AI@":
			ms.lock.Lock()
			ms.ImageList[event.Fields[1] + "‖" + event.Fields[2]] = event.Fields[3]
			ms.SaveNeeded = true
			ms.lock.Unlock()
			thisClient.SendToOthers(event.Fields...)

		// AUTH <response> [<user> [<client>]]
		// It's a bit late for this one to arrive now.
		case "AUTH":
			thisClient.Send("//", "AUTH command after authentication step ignored.")

		// CC [*|<user> [<target> [<messageID>]]]
		//
		// Clear the chat history
		//  <user> is the name of the user (if not "") who initiated this action
		//  <target> is "" or (if >=0) the minimum message ID to remain after the clear,
		//  or (if < 0) indicates that -<target> most recent messages should be kept.
		//  <messageID> will be reassigned by the server here.
		case "CC":
			// fill out missing fields with default values
			if len(event.Fields) < 2 { event.Fields = append(event.Fields, "*") }
			if len(event.Fields) < 3 { event.Fields = append(event.Fields, "")  }
			if len(event.Fields) < 4 { event.Fields = append(event.Fields, "0") }

			if event.Fields[2] == "" {
				// delete entire history
				ms.lock.Lock()
				ms.ChatHistory = nil
				ms.SaveNeeded = true
				ms.lock.Unlock()
			} else {
				target, err := strconv.Atoi(event.Fields[2])
				if err != nil {
					thisClient.Send("//", fmt.Sprintf("CC command rejected; invalid target: %v", err))
					return
				}
				if target < 0 {
					// delete all but last -target elements
					ms.lock.Lock()
					if len(ms.ChatHistory) > -target {
						ms.ChatHistory = append([]*MapEvent(nil), ms.ChatHistory[len(ms.ChatHistory)+target:]...)
						ms.SaveNeeded = true
					}
					ms.lock.Unlock()
				} else {
					// delete all up to one with message ID target
					ms.lock.Lock()
					if len(ms.ChatHistory) > 0 {
						n := sort.Search(len(ms.ChatHistory), func(i int) bool {
							mid, err := ms.ChatHistory[i].MessageID()
							if err != nil {
								log.Printf("[client %s] CC: Error getting message ID from %v: %v", thisClient.ClientAddr, ms.ChatHistory[i], err)
								return false
							}
							return mid >= target
						})
						ms.ChatHistory = append([]*MapEvent(nil), ms.ChatHistory[n:]...)
						ms.SaveNeeded = true
					}
					ms.lock.Unlock()
				}
			}
			ms.lock.Lock()
			event.AssignMessageID()
			ms.ChatHistory = append(ms.ChatHistory, event)
			ms.SaveNeeded = true
			ms.lock.Unlock()

			// Now forward the CC command out to all our peers
			thisClient.SendToOthers(event.Fields...)

		// CLR <id>
		//
		// Delete all objects matching <id> from clients. <id> may be:
		//	*						all objects
		//  E*						all map elements
		//	M*						all monsters
		// 	P*						all players
		//  [<imagename>=]<name>	creature with the given <name>
		//  <id>					object with ID <id>
		case "CLR":
			switch event.Fields[1] {
				case "*":	// delete all objects
					ms.lock.Lock()
					ms.EventHistory = make(map[string]*MapEvent)
					nextEventSequence = 0
					ms.IdByName = make(map[string]string)
					ms.SaveNeeded = true
					ms.lock.Unlock()

				case "E*", "M*", "P*":	// delete tokens of the given type
					ms.lock.Lock()
					for key, ev := range ms.EventHistory {
						if ev.EventClass() == event.Fields[1][0:1] {
							delete(ms.EventHistory, key)
						}
					}
					ms.lock.Unlock()

				default: // delete creature token by name or any object by ID
					creature_name := strip_creature_base_name(event.Fields[1])
					ms.lock.RLock()
					target, ok := ms.IdByName[creature_name]
					ms.lock.RUnlock()
					if !ok {
						target = event.Fields[1]
					}
					ms.lock.Lock()
					for key, ev := range ms.EventHistory {
						if ev.ID == target {
							delete(ms.EventHistory, key)
						}
					}
					ms.lock.Unlock()
			}

			// Now forward the CLR command out to all our peers
			thisClient.SendToOthers(event.Fields...)

		// D <recipients> <die-expression>
		//
		// Roll the dice described by <die-expression> and then transmit the
		// result to the people in <recipients>. The latter may include
		// the following special recipients:
		//   @  send back to the client requesting this die-roll.
		//   *  send to all connected clients
		//   %  send privately to the GM, and ONLY the GM, regardless of any
		//      other values in <recipients>.
		//
		case "D":
			title, results, err := thisClient.dice.DoRoll(event.Fields[2])
			if err != nil {
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
				fmt.Sprintf("ERROR: die roll request not accepted: %v", err),
				NextMessageID())
				return
			}
			to_all := false
			to_gm := false
			to_list, err := ParseTclList(event.Fields[1])
			if err != nil {
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
				fmt.Sprintf("ERROR: die roll recipient list not understood: %v", err),
				NextMessageID())
				return
			}
			for _, recipient := range to_list {
				switch recipient {
					case "*":
						to_all = true
					case "%":
						to_gm = true
				}
			}


			for _, result := range results {
				var details []string
				for _, detail := range result.Details {
					formatted_detail, err := ToTclString([]string{detail.Type, detail.Value})
					if err != nil {
						log.Printf("Internal error formatting ROLL response: %v", err)
						return
					}
					details = append(details, formatted_detail)
				}
				formatted_detail_list, err := ToTclString(details)
				if err != nil {
					log.Printf("Internal error formatting ROLL response: %v", err)
					return
				}
				//
				// Create a chat channel event containing the die roll result
				//
				response_event, err := NewMapEventFromList("", []string{"ROLL", thisClient.Username(),
					event.Fields[1], title, strconv.Itoa(result.Result), formatted_detail_list,
					""}, "", "")
				if err != nil {
					log.Printf("Internal error creating ROLL event: %v", err)
					return
				}
				//
				// Add to the history of chat messages
				//
				ms.lock.Lock()
				response_event.AssignMessageID()
				ms.ChatHistory = append(ms.ChatHistory, response_event)
				ms.SaveNeeded = true
				ms.lock.Unlock()
				//
				// Send to recipients
				//
				if to_gm {
					//
					// ONLY to the GM's client(s)
					//
					for peerAddr, peer := range ms.Clients {
						if !peer.WriteOnly && peer.Authenticated {
							if peer.Username() == "GM" {
								peer.Send(response_event.Fields...)
							} else if peerAddr == thisClient.ClientAddr {
								ack_event, err := NewMapEventFromList("", []string{"ROLL",
									thisClient.Username(), event.Fields[1], title, "*",
									"{comment {Results sent to GM}}", ""}, "", "")
								if err != nil {
									log.Printf("Internal error creating ROLL ack event: %v", err)
									return
								}
								ms.lock.Lock()
								ack_event.AssignMessageID()
								ms.lock.Unlock()
								thisClient.Send(ack_event.Fields...)
							}
						}
					}
				} else {
					//
					// Send results openly to all (listed) peers 
					//
					for peerAddr, peer := range ms.Clients {
						if !peer.WriteOnly && peer.Authenticated {
							if !to_all && peerAddr != thisClient.ClientAddr {
								ok_to_send := false
								for _, recipient := range to_list {
									if peer.Username() == recipient {
										ok_to_send = true
										break
									}
								}
								if !ok_to_send {
									continue
								}
							}
							peer.Send(response_event.Fields...)
						}
					}
				}
			}

		//
		// DD <deflist>
		//
		// Define a personal set of die-roll presets. <deflist>
		// is a list of presets, each of which is a 3-tuple:
		//   <name> <description> <dice-spec>
		//
		case "DD":
			if !thisClient.Authenticated || thisClient.Auth == nil {
				log.Printf("[client %s] DD command failed: no username authenticated for user", thisClient.ClientAddr)
				return
			}
			new_set, err := NewDicePresetListFromString(event.Fields[1])
			if err != nil {
				log.Printf("[client %s] DD command failed: %v; new set %s", thisClient.ClientAddr, err, event.Fields[1])
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset not understood: %v", err),
					NextMessageID())
				return
			}
			if ms.Database == nil {
				log.Printf("[client %s] DD command failed (no open database)", thisClient.ClientAddr)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset could not be stored: the system administrator has not configured persistent storage."),
					NextMessageID())
				return
			}

			err = UpdateDicePresets(ms.Database, thisClient.Username(), new_set)
			if err != nil {
				log.Printf("[client %s] DD command failed to store: %v", thisClient.ClientAddr, err)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset could not be stored: %v", err),
					NextMessageID())
				return
			}
			ms.PlayerDicePresets[thisClient.Username()] = new_set
			ms.SaveNeeded = true
			ms.SendDicePresetsToOtherClients(thisClient, thisClient.Username())

		//
		// DD+ <deflist>
		//
		// Add a new set of die-roll presets to the existing
		// list for a user. 
		//
		case "DD+":
			if !thisClient.Authenticated || thisClient.Auth == nil {
				log.Printf("[client %s] DD+ command failed: no username authenticated for user", thisClient.ClientAddr)
				return
			}
			if ms.Database == nil {
				log.Printf("[client %s] DD+ command failed (no open database)", thisClient.ClientAddr)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset could not be stored: the system administrator has not configured persistent storage."),
					NextMessageID())
				return
			}
			new_set, err := NewDicePresetListFromString(event.Fields[1])
			if err != nil {
				log.Printf("[client %s] DD+ command failed: %v; new set %s", thisClient.ClientAddr, err, event.Fields[1])
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset not understood: %v", err),
					NextMessageID())
				return
			}
			old_set, ok := ms.PlayerDicePresets[thisClient.Username()]
			if ok {
				new_set = append(old_set, new_set...)
			}
			err = UpdateDicePresets(ms.Database, thisClient.Username(), new_set)
			if err != nil {
				log.Printf("[client %s] DD+ command failed to store: %v", thisClient.ClientAddr, err)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset could not be stored: %v", err),
					NextMessageID())
				return
			}
			ms.PlayerDicePresets[thisClient.Username()] = new_set
			ms.SaveNeeded = true
			ms.SendDicePresetsToOtherClients(thisClient, thisClient.Username())

		//
		// DD/ <regex>
		//
		// Remove all die-roll presets for the requesting user which match
		// the given regular expression.
		//
		case "DD/":
			if !thisClient.Authenticated || thisClient.Auth == nil {
				log.Printf("[client %s] DD/ command failed: no username authenticated for user", thisClient.ClientAddr)
				return
			}
			if ms.Database == nil {
				log.Printf("[client %s] DD/ command failed (no open database)", thisClient.ClientAddr)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll preset could not be stored: the system administrator has not configured persistent storage."),
					NextMessageID())
				return
			}
			old_set, ok := ms.PlayerDicePresets[thisClient.Username()]
			if !ok || len(old_set) == 0 {
				return // nothing to do in this case
			}
			pattern, err := regexp.Compile(event.Fields[1])
			if err != nil {
				log.Printf("[client %s] DD/ command failed on regex compilation: %v", thisClient.ClientAddr, err)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll filter regex not understood: %v", err),
					NextMessageID())
				return
			}

			var new_set []DicePreset
			for _, preset := range old_set {
				if !pattern.MatchString(preset.Name) {
					new_set = append(new_set, preset)
				}
			}

			err = UpdateDicePresets(ms.Database, thisClient.Username(), new_set)
			if err != nil {
				log.Printf("[client %s] DD/ command failed to store: %v", thisClient.ClientAddr, err)
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: die roll filter results could not be stored: %v", err),
					NextMessageID())
				return
			}
			ms.PlayerDicePresets[thisClient.Username()] = new_set
			ms.SaveNeeded = true
			ms.SendDicePresetsToOtherClients(thisClient, thisClient.Username())

		//
		// DR
		//
		// Request die-roll presets on file for this user.
		//
		case "DR":
			if !thisClient.Authenticated || thisClient.Auth == nil {
				log.Printf("[client %s] DR command failed: no username authenticated for user", thisClient.ClientAddr)
				return
			}

			old_set, ok := ms.PlayerDicePresets[thisClient.Username()]
			if !ok || len(old_set) == 0 {
				thisClient.Send("DD=")
				thisClient.Send("DD.", "0", "")
				return
			}

			ms.SendMyPresets(thisClient, thisClient.Username())

		//
		// LS
		// LS: <data>
		// LS. <count> <checksum>
		//
		// Load a data set (as if from a .map data file) describing all attributes
		// of a set of map elements and creature token.  Even though they are sent
		// as multiple commands over the client/server connections, we will store
		// them here as a single event with multiple lines in the raw portion and an
		// empty Fields list.
		//
		case "LS":
			if thisClient.IncomingData != nil {
				log.Printf("[client %s] WARNING: LS command received before previous one completed!", thisClient.ClientAddr)
				log.Printf("[client %s] WARNING: Abandoning %d element%s previously received!",
					thisClient.ClientAddr, len(thisClient.IncomingData),
					plural(len(thisClient.IncomingData)))
				thisClient.IncomingData = nil
			}
			thisClient.IncomingDataType = "LS"
			return // don't save this (incomplete) operation in the state history

		case "LS:":
			if thisClient.IncomingDataType == "" {
				log.Printf("[client %s] WARNING: LS: command received before LS command (ignored)", thisClient.ClientAddr)
				return
			}
			if thisClient.IncomingDataType != "LS" {
				log.Printf("[client %s] WARNING: LS: command received during %s command set (ignored)", thisClient.ClientAddr, thisClient.IncomingDataType)
				return
			}
			saved_data := event.Fields[1]
			thisClient.IncomingData = append(thisClient.IncomingData, saved_data)
			return // don't save this (incomplete) operation in the state history

		case "LS.":
			if thisClient.IncomingDataType == "" {
				log.Printf("[client %s] WARNING: LS. command received before LS command (ignored)", thisClient.ClientAddr)
				return
			}
			if thisClient.IncomingDataType != "LS" {
				log.Printf("[client %s] WARNING: LS. command received during %s command sequence (ignored)", thisClient.ClientAddr, thisClient.IncomingDataType)
				return
			}
			data_by_id := make(map[string][]string)
			expected_count, err := strconv.Atoi(event.Fields[1])
			if err != nil {
				log.Printf("[client %s] ERROR: LS. command count value couldn't be parsed: %v (LS sequence not accepted)", thisClient.ClientAddr, err)
				goto reject_LS
			}
			if len(thisClient.IncomingData) != expected_count {
				log.Printf("[client %s] ERROR: LS. command count value %d doesn't match expected count %d (LS sequence not accepted)", thisClient.ClientAddr, len(thisClient.IncomingData), expected_count)
				goto reject_LS
			}


			if len(event.Fields) < 3 || event.Fields[2] == "" {
				log.Printf("[client %s] WARNING: LS. command without checksum (won't validate)", thisClient.ClientAddr)
			} else {
				expected_checksum, err := base64.StdEncoding.DecodeString(event.Fields[2])
				if err != nil {
					log.Printf("[client %s] WARNING: LS. command checksum value couldn't be parsed: %v (LS sequence not accepted)", thisClient.ClientAddr, err)
					goto reject_LS
				}
				cksum := sha256.New()
				for _, x := range thisClient.IncomingData {
					log.Printf("[client %s] adding \"%s\" to checksum", thisClient.ClientAddr, x)
					cksum.Write([]byte(x))
				}
				if !bytesEqual(expected_checksum, cksum.Sum(nil)) {
					log.Printf("[client %s] ERROR: LS. command checksum mismatch (LS sequence not accepted)", thisClient.ClientAddr)
					log.Printf("[client %s] calculated: %v", thisClient.ClientAddr, cksum.Sum(nil))
					log.Printf("[client %s] expected:   %v", thisClient.ClientAddr, expected_checksum)
					goto reject_LS
				}
			}
			//
			// run through the list of objects sent in the LS command,
			// rearranging them from the random order they're allowed to arrive
			// per the protocol spec into a new set of events that each describe
			// a single object.
			//

			thisClient.SendToOthers("LS")
			for _, item_text := range thisClient.IncomingData {
				item, err := ParseTclList(item_text)
				if err != nil {
					log.Printf("[client %s] ERROR: LS object format error in %s: %v; sequence rejected", thisClient.ClientAddr, item_text, err)
					goto reject_LS
				}

				if len(item) == 0 {
					continue
				}
				if len(item) < 2 {
					log.Printf("[client %s] ERROR: LS object format error in %s; sequence rejected", thisClient.ClientAddr, item_text)
					goto reject_LS
				}
				thisClient.SendToOthers("LS:", item_text)
				switch item[0] {
					case "M", "P":
						attrs := strings.SplitN(item[1], ":", 2)
						if len(attrs) != 2 {
							log.Printf("[client %s] ERROR: LS object format error in %s; sequence rejected", thisClient.ClientAddr, item[1])
							goto reject_LS
						}
						obj_list, ok := data_by_id[attrs[1]]
						if !ok {
							obj_list = []string{item_text}
						} else {
							obj_list = append(obj_list, item_text)
						}
						data_by_id[attrs[1]] = obj_list
						ms.lock.Lock()
						ms.ClassById[attrs[1]] = item[0]
						ms.SaveNeeded = true
						ms.lock.Unlock()
						if attrs[0] == "NAME" {
							if len(item) < 3 {
								log.Printf("[client %s] ERROR: LS object format error in %s; sequence rejected", thisClient.ClientAddr, item_text)
								goto reject_LS
							}
							ms.lock.Lock()
							ms.IdByName[strip_creature_base_name(item[2])] = attrs[1]
							ms.SaveNeeded = true
							ms.lock.Unlock()
						}
					case "F":
						data_by_id[item[1]] = []string{item_text}
						ms.lock.Lock()
						ms.ClassById[item[1]] = ""
						ms.SaveNeeded = true
						ms.lock.Unlock()

					default:
						attrs := strings.SplitN(item[0], ":", 2)
						if len(attrs) != 2 {
							log.Printf("[client %s] ERROR: LS object format error in %s; sequence rejected", thisClient.ClientAddr, item[0])
							goto reject_LS
						}
						old_list, ok := data_by_id[attrs[1]]
						if !ok {
							old_list = []string{item_text}
						} else {
							old_list = append(old_list, item_text)
						}
						data_by_id[attrs[1]] = old_list
						ms.lock.Lock()
						ms.ClassById[attrs[1]] = "E"
						ms.SaveNeeded = true
						ms.lock.Unlock()
				}
			}
			thisClient.SendToOthers(event.Fields...)
			//
			// repackage by object
			//
			for obj_id, definition := range data_by_id {
				cksum := sha256.New()
				var elements []string
				for _, element := range definition {
					cksum.Write([]byte(element))
					elements = append(elements, "LS: {" + element + "}")
				}
				elements = append(elements, fmt.Sprintf("LS. %d %s",
					len(elements), base64.StdEncoding.EncodeToString(cksum.Sum(nil))))
				ms.lock.RLock()
				obj_class, ok := ms.ClassById[obj_id]
				ms.lock.RUnlock()
				if !ok {
					obj_class = ""
				}
				new_event, err := NewMapEvent("LS", obj_id, obj_class)
				if err != nil {
					log.Printf("[client %s] ERROR packaging LS data for object %s: %v",
						thisClient.ClientAddr, obj_id, err)
					return
				}
				new_event.MultiRawData = elements
				ms.UpdateState(new_event)
			}

reject_LS:
			thisClient.IncomingDataType = ""
			thisClient.IncomingData = nil
			return // don't save the original event to our history (we already saved the repackaged ones)

		//
		// NO
		// NO+
		// 
		// Set client to write-only mode. NO+ is an obsolete variation
		// of this command which we will now treat as a synonym for NO.
		//
		case "NO", "NO+":
			thisClient.WriteOnly = true

		//
		// OA <id> <kvlist>
		//
		// Update a set of arbitrary attributes for the given object
		// by <id> (which may be the unique identifier for any object
		// (i.e., a UUID), or "@<name>" for a named creature token).
		//
		// <kvlist> is a list with an even number of elements, alternating
		// between the name of an object attribute and its new value.
		//
		case "OA":
			target := ""
			name := ""
			if event.Fields[1] == "" || event.Fields[1] == "@" {
				log.Printf("[client %s] OA command rejected (empty ID field)", thisClient.ClientAddr)
				return
			}
			kvlist, err := ParseTclList(event.Fields[2])
			if err != nil {
				log.Printf("[client %s] OA command: cannot parse kvlist: %v",
					thisClient.ClientAddr, err)
				return
			}
			if len(kvlist) % 2 != 0 {
				log.Printf("[client %s] OA command: kvlist has non-even number of elements: %d",
					thisClient.ClientAddr, len(kvlist))
				return
			}

			if event.Fields[1][0:1] == "@" {
				var ok bool

				name = strip_creature_base_name(event.Fields[1][1:])
				ms.lock.RLock()
				target, ok = ms.IdByName[name]
				ms.lock.RUnlock()
				if ok {
					ms.lock.RLock()
					known_class, ok := ms.ClassById[target]
					ms.lock.RUnlock()
					if ok {
						event.Class = known_class
					}
					event.ID = target
				} else {
					target = ""
					event.ID = ""
					log.Printf("[client %s] OA command: setting attribute for %s: unknown object name (attempting best try)", thisClient.ClientAddr, event.Fields[1])
				}
			} else {
				target = event.Fields[1]
				ms.lock.RLock()
				known_class, ok := ms.ClassById[target]
				ms.lock.RUnlock()
				if ok {
					event.Class = known_class
				}
				event.ID = target
			}

			// if we're changing the object's NAME attribute, we'll have to
			// change the mapping of name to ID now.
			for i := 0; i < len(kvlist)-1; i+= 2 {
				if kvlist[i] == "NAME" {
					ms.lock.Lock()
					if target != "" {
						if name != "" {
							_, ok := ms.IdByName[name]
							if ok {
								delete(ms.IdByName, name)
							}
						}
						ms.IdByName[kvlist[i+1]] = target
					}
					ms.SaveNeeded = true
					ms.lock.Unlock()
					break
				}
			}
			thisClient.SendToOthers(event.Fields...)

		//
		// OA+ <id> <key> <valuelist>
		// OA- <id> <key> <valuelist>
		//
		// Assuming that the object with the given <id> has an attribute
		// <key> whose value is a list of values, add (OA+) or remove (OA-) the
		// list of values to/from that attribute.
		//
		case "OA+", "OA-":
			target := ""
			name := ""
			if event.Fields[1] == "" || event.Fields[1] == "@" {
				log.Printf("[client %s] %s command rejected (empty ID field)",
					thisClient.ClientAddr, event.EventType())
				return
			}

			if event.Fields[1][0:1] == "@" {
				var ok bool
				name = strip_creature_base_name(event.Fields[1][1:])
				ms.lock.RLock()
				target, ok = ms.IdByName[name]
				ms.lock.RUnlock()
				if ok {
					ms.lock.RLock()
					known_class, ok := ms.ClassById[target]
					ms.lock.RUnlock()
					if ok {
						event.Class = known_class
					}
					event.ID = target
				} else {
					target = ""
					event.ID = ""
					log.Printf("[client %s] %s command: setting attribute for %s: unknown object name (attempting best try)",
						thisClient.ClientAddr, event.EventType(), event.Fields[1])
				}
			} else {
				target = event.Fields[1]
				ms.lock.RLock()
				known_class, ok := ms.ClassById[target]
				ms.lock.RUnlock()
				if ok {
					event.Class = known_class
				}
				event.ID = target
			}
			log.Printf("%v", target)
			thisClient.SendToOthers(event.Fields...)

		//
		// PS <id> <color> <name> <area> <size> player|monster <x> <y> <reach>
		//
		// Place someone (a creature token) on the map.
		//
		case "PS":
			ms.lock.Lock()
			ms.IdByName[event.Fields[3]] = event.Fields[1]
			ms.SaveNeeded = true
			ms.lock.Unlock()
			thisClient.SendToOthers(event.Fields...)

		//
		// SYNC [CHAT [<target>]]
		//
		// Synchronize the client with the current game state by replaying
		// all of the saved events we've been tracking.
		//
		// In the case of SYNC CHAT, rather than replaying the events, we replay
		// the saved chat messages. If <target> is supplied, only the messages
		// with IDs greater than <target> are sent. If <target> is negative, then
		// only the most recent |<target>| messages are sent.
		//
		// The events are stored in EventHistory as a map of key->*MapEvent
		// (and ChatHistory for chat events as a linear slice of *MapEvent)
		// 
		// The ChatHistory list is stored in ascending message ID order so we 
		// can simply re-transmit its events by finding our starting point
		// and going from there. However, the EventHistory map needs to be
		// sorted first. We store it by key to make updates more efficient
		// since the more expensive sorting operation only needs to be 
		// performed on the relatively rare SYNC command.
		//
		case "SYNC":
			if len(event.Fields) == 1 {
				// SYNC
				// Send the stored events in our history
				//
				ms.Sync(thisClient)
			} else {
				if event.Fields[1] == "CHAT" {
					// SYNC CHAT [target]
					ms.lock.RLock()
					start := 0
					if len(event.Fields) > 2 {
						target, err := strconv.Atoi(event.Fields[2])
						if err != nil {
							ms.lock.RUnlock()
							log.Printf("[client %s] SYNC CHAT target value not understood: %v", thisClient.ClientAddr, err)
							return
						}
						if target < 0 {
							// send the most recent |target| messages
							start = len(ms.ChatHistory) + target
							if start < 0 {
								start = 0
							}
						} else {
							// send everything from message #target+1 to the end
							start = sort.Search(len(ms.ChatHistory), func (i int) bool {
								numeric_id, err := ms.ChatHistory[i].MessageID()
								if err != nil { return false }
								return numeric_id > target
							})
						}
					}
					for _, message := range ms.ChatHistory[start:] {
						if message.CanSendTo(thisClient.Username()) {
							thisClient.Send(message.Fields...)
						}
					}
					ms.lock.RUnlock()
				} else {
					log.Printf("[client %s] SYNC command not understood", thisClient.ClientAddr)
				}
			}
			return // don't record the SYNC in the history

		//
		// TO <sender> <recipientlist> <message> [<messageID>]
		//
		// Send a chat message to the list of recipients (as with the D
		// command). The <messageID> is ignored when provided by a client
		// but one is assigned by the server as it relays the message.
		// We will also replace the <sender> value with the actual sender's
		// name.
		//
		case "TO":
			if len(event.Fields) == 4 {
				event.Fields = append(event.Fields, "")
			} else if len(event.Fields) != 5 {
				log.Printf("[client %s] Rejected malformed TO event %v", thisClient.ClientAddr, event.Fields)
				return
			}
			to_all := false
			to_list, err := ParseTclList(event.Fields[2])
			if err != nil {
				thisClient.Send("TO", thisClient.Username(), thisClient.Username(),
					fmt.Sprintf("ERROR: recipient list not understood: %v", err),
					NextMessageID())
				return
			}
			for _, recipient := range to_list {
				if recipient == "*" {
					to_all = true
				}
			}
			event.Fields[1] = thisClient.Username()
			ms.lock.Lock()
			event.AssignMessageID()
			ms.ChatHistory = append(ms.ChatHistory, event)
			ms.SaveNeeded = true
			ms.lock.Unlock()
			if to_all {
				thisClient.SendToOthers(event.Fields...)
			} else {
				for _, peer := range ms.Clients {
					if peer.WriteOnly || !peer.Authenticated || peer.ClientAddr == thisClient.ClientAddr {
						continue
					}
					ok := false
					for _, recipient := range to_list {
						if recipient == peer.Username() {
							ok = true
							break
						}
					}
					if !ok {
						continue
					}
					peer.Send(event.Fields...)
				}
			}
			thisClient.Send(event.Fields...)

		//
		// /CONN
		//
		// Request a list of connected users.
		//
		case "/CONN":
			thisClient.ConnResponse()
	}
	//
	// Add this event to the tracked game state
	//
	ms.UpdateState(event)
}

func (ms *MapService) Sync(thisClient *MapClient) {
	//
	// pull our list of events from the map so they can be
	// sorted
	ms.lock.RLock()
	events_to_sync := make(MapEventList, len(ms.EventHistory))
	i := 0
	for _, event := range ms.EventHistory {
		events_to_sync[i] = event
		i++
	}
	ms.lock.RUnlock()
	//
	// sort events by sequence and send them to the client
	// 
	sort.Sort(events_to_sync)
	thisClient.Send("//", "DUMP OF CURRENT GAME STATE FOLLOWS")
	thisClient.Send("CLR", "*")
	for _, event := range events_to_sync {
		if event.MultiRawData != nil {
			thisClient.SendWithExtraData(event)
		} else {
			thisClient.Send(event.Fields...)
		}
	}
	thisClient.Send("//", "END OF STATE DUMP")
}


//
// Send player's die-roll presets to all clients connected with
// the same username as a given connection, but not that connection
// itself. This is used when a user has multiple clients connected
// and changes their presets on one, so their others stay in sync
// with the current set of presets.
//
func (ms *MapService) SendDicePresetsToOtherClients(mainClient *MapClient, username string) {
	for _, peer := range ms.AllClients() {
		if peer.ClientAddr != mainClient.ClientAddr && peer.Username() == username {
			ms.SendMyPresets(peer, username)
		}
	}
}

//
// Send die-roll presets to a logged-in user's connection.
//
func (ms *MapService) SendMyPresets(thisClient *MapClient, username string) {
	var count int

	thisClient.Send("DD=")
	cksum := sha256.New()
	presets, ok := ms.PlayerDicePresets[username]
	if ok {
		for i, preset := range presets {
			thisClient.Send("DD:", strconv.Itoa(i), preset.Name, preset.Description, preset.RollSpec)
			chkdata, err := PackageValues(strconv.Itoa(i), preset.Name, preset.Description, preset.RollSpec)
			if err != nil {
				log.Printf("WARNING: falied to package DD: data for checksum: %v", err)
				// we will continue to complete the operation in this case, however.
			} else {
				cksum.Write([]byte(chkdata))
			}
		}
		count = len(presets)
	} else {
		count = 0
	}

	thisClient.Send("DD.", strconv.Itoa(count), base64.StdEncoding.EncodeToString(cksum.Sum(nil)))
}

//
// Creature names may be <name> or <image>=<name>. Either way,
// we just want the <name> part.
//
func strip_creature_base_name(name string) string {
	return name[strings.Index(name, "=")+1:]
}

//
// Save current game state to the database
//
// Database Schema
//  ________________       ________________
// | events         |     | extradata      |
// |----------------|     |----------------|
// | eventid    PAi |---->| extraid    PAi |
// | rawdata      s |     | eventid      i |
// | sequence     i |     | datarow      s |
// | key          s |     |________________|
// | class        s |
// | objid        s |
// |________________|
//  ________________
// | chats          |
// |----------------|
// | rawdata      s |
// | msgid        s |
// |________________|
//  ________________
// | images         |
// |----------------|
// | name         s |
// | zoom         s |
// | location     s |
// |________________|
//  ________________
// | idbyname       |
// |----------------|
// | name         s |
// | objid        s |
// |________________|
//  ________________
// | classbyid      |
// |----------------|
// | objid        s |
// | class        s |
// |________________|
//

// P=primary key
// A=auto-increment
// i=integer
// s=string

func (ms *MapService) LoadState() error {
	var err error
	var result, subresult *sql.Rows
	var event *MapEvent
	var actual_msgid int

	if ms.Database == nil {
		log.Printf("LoadState: no database open")
		return fmt.Errorf("LoadState: no database open")
	}
	ms.lock.Lock()
	ms.EventHistory = make(map[string]*MapEvent)
	result, err = ms.Database.Query(`
		select eventid, rawdata, sequence, key, class, objid 
		from events`)
	if err != nil {
		log.Printf("LoadState: error loading from events table: %v", err)
		goto load_err
	}
	for result.Next() {
		var eventid  int64
		var rawdata  string
		var sequence int64
		var key      string
		var class    string
		var objid    string
		var extra    string
		err = result.Scan(&eventid, &rawdata, &sequence, &key, &class, &objid)
		if err != nil {
			log.Printf("LoadState: error scanning results from events table: %v", err)
			goto load_err
		}
		event, err = NewMapEvent(rawdata, objid, class)
		if err != nil {
			log.Printf("LoadState: error constructing new map event from \"%s\" (id %v, class %v): %v", rawdata, objid, class, err)
			goto load_err
		}
		event.Sequence = int(sequence)
		if event.Key != key {
			fmt.Printf("Warning: Loaded event #%d (seq %d) has key %s but we think it should be %s", eventid, sequence, key, event.Key)
		}
		subresult, err = ms.Database.Query(`
			select datarow from extradata
				where extradata.eventid = ?
				order by extraid
		`, eventid)
		if err != nil {
			log.Printf("LoadState: error querying extradata for event id %v: %v", eventid, err)
			goto load_err
		}
		for subresult.Next() {
			err = subresult.Scan(&extra)
			if err != nil {
				log.Printf("LoadState: error scanning extradata value for event id %v: %v", eventid, err)
				goto load_err
			}
			event.MultiRawData = append(event.MultiRawData, extra)
		}
		subresult.Close()
		ms.EventHistory[event.Key] = event
		if nextEventSequence <= event.Sequence {
			nextEventSequence = event.Sequence+1
		}
	}
	result.Close()

	ms.ChatHistory = nil
	result, err = ms.Database.Query(`select rawdata, msgid from chats`)
	if err != nil {
		log.Printf("LoadState: error querying chats table: %v", err)
		goto load_err
	}
	for result.Next() {
		var rawdata  string
		var msgid    int
		err = result.Scan(&rawdata, &msgid)
		if err != nil {
			log.Printf("LoadState: error scanning result of chats table query: %v", err)
			goto load_err
		}
		event, err = NewMapEvent(rawdata, "", "")
		if err != nil {
			log.Printf("LoadState: error creating new map event for \"%s\": %v", rawdata, err)
			goto load_err
		}
		actual_msgid, err = event.MessageID()
		if err != nil {
			log.Printf("Warning: skipping restored chat message %s with no apparent message ID", rawdata)
			continue
		}
		if actual_msgid != msgid {
			log.Printf("Warning: restored chat message %s with message ID %d but database says it should be %d",
				rawdata, actual_msgid, msgid)
		}
		ms.ChatHistory = append(ms.ChatHistory, event)
		AdvanceMessageId(actual_msgid)
	}
	result.Close()

	ms.ImageList = make(map[string]string)
	result, err = ms.Database.Query(`select name, zoom, location from images`)
	if err != nil {
		log.Printf("LoadState: error querying images table: %v", err)
		goto load_err
	}
	for result.Next() {
		var name string
		var zoom string
		var location string
		err = result.Scan(&name, &zoom, &location)
		if err != nil {
			log.Printf("LoadState: error scanning image table: %v", err)
			goto load_err
		}
		ms.ImageList[name + "‖" + zoom] = location
	}
	result.Close()

	ms.IdByName = make(map[string]string)
	result, err = ms.Database.Query(`select name, objid from idbyname`)
	if err != nil {
		log.Printf("LoadState: error querying idbyname table: %v", err)
		goto load_err
	}
	for result.Next() {
		var name string
		var objid string
		err = result.Scan(&name, &objid)
		if err != nil {
			log.Printf("LoadState: error scanning idbyname: %v", err)
			goto load_err
		}
		ms.IdByName[name] = objid
	}
	result.Close()

	ms.ClassById = make(map[string]string)
	result, err = ms.Database.Query(`select objid, class from classbyid`)
	if err != nil {
		log.Printf("LoadState: error querying classbyid table: %v", err)
		goto load_err
	}
	for result.Next() {
		var class string
		var objid string
		err = result.Scan(&objid, &class)
		if err != nil {
			log.Printf("LoadState: error scanning classbyid: %v", err)
			goto load_err
		}
		ms.ClassById[objid] = class
	}
	result.Close()

	ms.lock.Unlock()
	return nil

load_err:
	ms.lock.Unlock()
	return fmt.Errorf("Error reading from game state database (%v)", err)
}

func (ms *MapService) SaveState() error {
	var err error
	var tx *sql.Tx
	var event, chat *MapEvent
	var rawdata, extra string
	var res sql.Result
	var eventid int64
	var msgid int

	if ms.Database == nil {
		return fmt.Errorf("SaveState: no database open")
	}
	if !ms.SaveNeeded {
		log.Printf("Game state does not need to be saved.")
		return nil
	}

	tx, err = ms.Database.Begin()
	if err != nil {
		return fmt.Errorf("SaveState: Unable to start transaction: %v", err)
	}

	if _, err = tx.Exec(`
		delete from events;
		delete from extradata;
		delete from chats;
		delete from images;
		delete from idbyname;
		delete from classbyid;
	`); err != nil { goto bail_out }

	ms.lock.RLock()
	for _, event = range ms.EventHistory {
		rawdata, err = event.RawEventText()
		if err != nil { goto save_err }
		res, err = tx.Exec(`insert into events (rawdata, sequence, key, class, objid)
			values (?, ?, ?, ?, ?)`,
			rawdata, event.Sequence, event.Key, event.Class, event.ID)
		if err != nil { goto save_err }
		eventid, err = res.LastInsertId()
		if err != nil { goto save_err }
		for _, extra = range event.MultiRawData {
			if _, err = tx.Exec(`insert into extradata (eventid, datarow) values (?, ?)`,
				eventid, extra); err != nil {
				goto save_err
			}
		}
	}
	for _, chat = range ms.ChatHistory {
		msgid, err = chat.MessageID()
		if err != nil { goto save_err }
		rawdata, err = chat.RawEventText()
		if err != nil { goto save_err }
		if _, err = tx.Exec(`insert into chats (rawdata, msgid) values (?, ?)`,
			rawdata, msgid); err != nil {
			goto save_err
		}
	}

	for k, location := range ms.ImageList {
		parts := strings.SplitN(k, "‖", 2)
		if len(parts) != 2 {
			log.Printf("ImageList entry has invalid key \"%s\"", k)
			continue
		}
		_, err = tx.Exec(`insert into images (name, zoom, location) values (?, ?, ?)`, parts[0], parts[1], location)
		if err != nil { goto save_err }
	}

	for k, v := range ms.IdByName {
		_, err = tx.Exec(`insert into idbyname (name, objid) values (?, ?)`, k, v)
		if err != nil { goto save_err }
	}

	for k, v := range ms.ClassById {
		_, err = tx.Exec(`insert into classbyid (objid, class) values (?, ?)`, k, v)
		if err != nil { goto save_err }
	}

	ms.lock.RUnlock()

	if err = tx.Commit(); err != nil {
		goto bail_out
	}

	ms.lock.Lock()
	ms.SaveNeeded = false
	ms.lock.Unlock()
	return nil

save_err:
	ms.lock.RUnlock()

bail_out:
	if rberr := tx.Rollback(); rberr != nil {
		return fmt.Errorf("Error writing to game state database (%v); further, failed to rollback database transaction (%v)!", err, rberr)
	}
	return fmt.Errorf("Error writing to game state database (%v)", err)
}

//
// Dump the current game state to the logfile
//
func (ms *MapService) DumpState() {
	ms.lock.RLock()
	log.Printf("Dump of current game state:")
	log.Printf("SEQUENCE CLS ID------------------------------ DATA-----------------------------")
	for _, event := range ms.EventHistory {
		rawdata, err := event.RawEventText()
		if err != nil {
			rawdata = fmt.Sprintf("**ERROR** %v", err)
		}
		log.Printf("%8d %3s %-32s %s", event.Sequence, event.Class, event.ID, rawdata)
	}
	log.Printf("IMAGE-ID------------ SIZE FILE-------------------------------------------------")
	for key, image := range ms.ImageList {
		parts := strings.SplitN(key, "‖", 2)
		if len(parts) != 2 {
			log.Printf("**INVALID** %13s %s", key, image)
		} else {
			log.Printf("%20s %4s %s", parts[0], parts[1], image)
		}
	}
	log.Printf("ID------------------------------ CLS")
	for id, class := range ms.ClassById {
		log.Printf("%-32s %s", id, class)
	}
	log.Printf("ID------------------------------ NAME")
	for name, id := range ms.IdByName {
		log.Printf("%-32s %s", name, id)
	}
	log.Printf("Next sequence: %d", nextEventSequence)
	log.Printf("Save needed?   %v", ms.SaveNeeded)

	log.Printf("ADDRESS--------- USER----------- CLIENT--------- AUTH?")
	for _, client := range ms.Clients {
		client_type := "<unknown>"
		if client.Auth != nil {
			client_type = client.Auth.Client
		}
		log.Printf("%-16s %-15s %-15s %v", client.ClientAddr, client.Username(), client_type, client.Authenticated)
	}
	log.Printf("******************************** END ******************************************")
	ms.lock.RUnlock()
}
// @[00]@| GMA 4.2.2
// @[01]@|
// @[10]@| Copyright © 1992–2020 by Steven L. Willoughby
// @[11]@| (AKA Software Alchemy), Aloha, Oregon, USA. All Rights Reserved.
// @[12]@| Distributed under the terms and conditions of the BSD-3-Clause
// @[13]@| License as described in the accompanying LICENSE file distributed
// @[14]@| with GMA.
// @[15]@|
// @[20]@| Redistribution and use in source and binary forms, with or without
// @[21]@| modification, are permitted provided that the following conditions
// @[22]@| are met:
// @[23]@| 1. Redistributions of source code must retain the above copyright
// @[24]@|    notice, this list of conditions and the following disclaimer.
// @[25]@| 2. Redistributions in binary form must reproduce the above copy-
// @[26]@|    right notice, this list of conditions and the following dis-
// @[27]@|    claimer in the documentation and/or other materials provided
// @[28]@|    with the distribution.
// @[29]@| 3. Neither the name of the copyright holder nor the names of its
// @[30]@|    contributors may be used to endorse or promote products derived
// @[31]@|    from this software without specific prior written permission.
// @[32]@|
// @[33]@| THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
// @[34]@| CONTRIBUTORS “AS IS” AND ANY EXPRESS OR IMPLIED WARRANTIES,
// @[35]@| INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
// @[36]@| MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// @[37]@| DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS
// @[38]@| BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY,
// @[39]@| OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
// @[40]@| PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
// @[41]@| PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// @[42]@| THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR
// @[43]@| TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF
// @[44]@| THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// @[45]@| SUCH DAMAGE.
// @[46]@|
// @[50]@| This software is not intended for any use or application in which
// @[51]@| the safety of lives or property would be at risk due to failure or
// @[52]@| defect of the software.
