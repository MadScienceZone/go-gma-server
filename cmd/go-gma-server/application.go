package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MadScienceZone/go-gma/v5/auth"
	"github.com/MadScienceZone/go-gma/v5/mapper"
	"github.com/MadScienceZone/go-gma/v5/util"
	"golang.org/x/exp/slices"
)

type DebugFlags uint64

const (
	DebugAuth DebugFlags = 1 << iota
	DebugDB
	DebugEvents
	DebugIO
	DebugInit
	DebugMisc
	DebugAll DebugFlags = 0xffffffff
)

//
// DebugFlagNameSlice returns a list of debug option names
// from the given DebugFlags value.
//
func DebugFlagNameSlice(flags DebugFlags) []string {
	if flags == 0 {
		return nil
	}
	if flags == DebugAll {
		return []string{"all"}
	}

	var list []string
	for _, f := range []struct {
		bits DebugFlags
		name string
	}{
		{bits: DebugAuth, name: "auth"},
		{bits: DebugDB, name: "db"},
		{bits: DebugEvents, name: "events"},
		{bits: DebugIO, name: "i/o"},
		{bits: DebugInit, name: "init"},
		{bits: DebugMisc, name: "misc"},
	} {
		if (flags & f.bits) != 0 {
			list = append(list, f.name)
		}
	}
	return list
}

//
// DebugFlagNames returns a single string representation of
// the debugging flags (topics) stored in the DebugFlags
// value passed in.
//
func DebugFlagNames(flags DebugFlags) string {
	list := DebugFlagNameSlice(flags)
	if list == nil {
		return "<none>"
	}
	return "<" + strings.Join(list, ",") + ">"
}

//
// NamedDebugFlags takes a comma-separated list of
// debug flag (topic) names, or a list of individual
// names, or both, and returns the DebugFlags
// value which includes all of them.
//
// If "none" appears in the list, it cancels all previous
// values seen, but subsequent names will add their values
// to the list.
//
func NamedDebugFlags(names ...string) (DebugFlags, error) {
	var d DebugFlags
	var err error
	for _, name := range names {
		for _, flag := range strings.Split(name, ",") {
			switch flag {
			case "none":
				d = 0
			case "all":
				d = DebugAll
			case "auth":
				d |= DebugAuth
			case "db":
				d |= DebugDB
			case "events":
				d |= DebugEvents
			case "I/O", "i/o", "io":
				d |= DebugIO
			case "init":
				d |= DebugInit
			case "misc":
				d |= DebugMisc
			default:
				err = fmt.Errorf("No such -debug flag: \"%s\"", flag)
				// but keep processing the rest
			}
		}
	}
	return d, err
}

//
// Application holds the global settings and other context for the application
// generally.
//
type Application struct {
	// Logger is whatever device or file we're writing logs to.
	Logger *log.Logger

	// If DebugLevel is 0, no extra debugging output will be logged.
	// Otherwise, it gives a set of debugging topics to report.
	DebugLevel DebugFlags

	// Endpoint is the "[host]:port" string which specifies where our
	// incoming socket is listening.
	Endpoint string

	// If not empty, this gives the filename from which we are to read in
	// the initial client command set.
	InitFile string

	clientPreamble struct {
		syncData  bool
		preamble  []string
		postAuth  []string
		postReady []string
		lock      sync.RWMutex
	}

	// If not empty, we require authentication, with passwords taken
	// from this file.
	PasswordFile string
	clientAuth   struct {
		groupPassword     []byte
		gmPassword        []byte
		personalPasswords map[string][]byte
		lock              sync.RWMutex
	}

	// Pathname for database file.
	DatabaseName string
	sqldb        *sql.DB

	Clients            []*mapper.ClientConnection
	clientLock         sync.RWMutex
	MessageIDGenerator chan int
}

func (a *Application) AddClient(c *mapper.ClientConnection) {
	if c == nil || a == nil {
		return
	}
	a.Debug(DebugIO, "acquiring write lock on client list")
	a.clientLock.Lock()
	defer func() {
		a.Debug(DebugIO, "releasing write lock on client list")
		a.clientLock.Unlock()
	}()
	a.Debugf(DebugIO, "write lock granted; proceeding to add client %s", c.IdTag())
	a.Clients = append(a.Clients, c)
}

func (a *Application) RemoveClient(c *mapper.ClientConnection) {
	if c == nil || a == nil {
		return
	}
	a.Debug(DebugIO, "acquiring write lock on client list")
	a.clientLock.Lock()
	defer func() {
		a.Debug(DebugIO, "releasing write lock on client list")
		a.clientLock.Unlock()
	}()
	a.Debug(DebugIO, "write lock granted; proceeding to drop client")
	pos := slices.Index[*mapper.ClientConnection](a.Clients, c)
	if pos < 0 {
		a.Logf("client %v not found in server's client list, so can't delete it more", c.IdTag())
		return
	}
	a.Clients[pos] = nil
	a.Clients = slices.Delete[[]*mapper.ClientConnection, *mapper.ClientConnection](a.Clients, pos, pos+1)
	a.Debugf(DebugIO, "removed client %s", c.IdTag())
}

func (a *Application) GetClients() []*mapper.ClientConnection {
	if a == nil {
		return nil
	}
	a.Debug(DebugIO, "acquiring read lock on client list")
	a.clientLock.RLock()
	defer func() {
		a.Debug(DebugIO, "releasing read lock on client list")
		a.clientLock.RUnlock()
	}()
	a.Debug(DebugIO, "read lock granted; proceeding to get client list")
	return a.Clients
}

//
// Debug logs messages conditionally based on the currently set
// debug level. It acts just like fmt.Println as far as formatting
// its arguments.
//
func (a *Application) Debug(level DebugFlags, message ...any) {
	if a != nil && a.Logger != nil && (a.DebugLevel&level) != 0 {
		var dmessage []any
		dmessage = append(dmessage, DebugFlagNames(level))
		dmessage = append(dmessage, message...)
		a.Logger.Println(dmessage...)
	}
}

//
// Log logs messages to the application's logger.
// It acts just like fmt.Println as far as formatting
// its arguments.
//
func (a *Application) Log(message ...any) {
	if a != nil && a.Logger != nil {
		a.Logger.Println(message...)
	}
}

//
// Logf logs messages to the application's logger.
// It acts just like fmt.Printf as far as formatting
// its arguments.
//
func (a *Application) Logf(format string, args ...any) {
	if a != nil && a.Logger != nil {
		a.Logger.Printf(format, args...)
	}
}

//
// Debugf works like Debug, but takes a format string and argument
// list just like fmt.Printf does.
//
func (a *Application) Debugf(level DebugFlags, format string, args ...any) {
	if a != nil && a.Logger != nil && (a.DebugLevel&level) != 0 {
		a.Logger.Printf(DebugFlagNames(level)+" "+format, args...)
	}
}

//
// GetAppOptions configures the application by reading command-line options.
//
func (a *Application) GetAppOptions() error {

	var initFile = flag.String("init-file", "", "Load initial client commands from named file path")
	var logFile = flag.String("log-file", "-", "Write log to given pathname (stderr if '-'); special % tokens allowed in path")
	var passFile = flag.String("password-file", "", "Require authentication with named password file")
	var endPoint = flag.String("endpoint", ":2323", "Incoming connection endpoint ([host]:port)")
	//	var saveInterval = flag.String("save-interval", "10m", "Save internal state this often")
	var sqlDbName = flag.String("sqlite", "", "Specify filename for sqlite database to use")
	var debugFlags = flag.String("debug", "", "List the debugging trace types to enable")
	flag.Parse()

	if *debugFlags != "" {
		a.DebugLevel, _ = NamedDebugFlags(*debugFlags)
		a.Debugf(DebugInit, "debugging flags set to %#v%s", a.DebugLevel, DebugFlagNames(a.DebugLevel))
	}

	if *logFile == "" {
		a.Logger = nil
	} else {
		a.Logger = log.Default()
		if *logFile != "-" {
			path, err := util.FancyFileName(*logFile, nil)
			if err != nil {
				return fmt.Errorf("unable to understand log file path \"%s\": %v", *logFile, err)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("unable to open log file: %v", err)
			} else {
				a.Logger.SetOutput(f)
			}
			a.Debugf(DebugInit, "Logging to %v", path)
		}
	}

	if *initFile != "" {
		a.InitFile = *initFile
		a.Logf("reading client initial command set from \"%s\"", a.InitFile)
		if err := a.refreshClientPreamble(); err != nil {
			a.Logf("error reading init file \"%s\": %v", a.InitFile, err)
			return err
		}
	}

	if *passFile != "" {
		a.PasswordFile = *passFile
		a.Logf("authentication enabled via \"%s\"", a.PasswordFile)
		if err := a.refreshAuthenticator(); err != nil {
			a.Logf("unable to set up authentication: %v", err)
			return err
		}
	} else {
		a.Log("WARNING: authentication not enabled!")
	}

	if *endPoint != "" {
		a.Endpoint = *endPoint
		a.Logf("configured to listen on \"%s\"", a.Endpoint)
	} else {
		return fmt.Errorf("non-empty tcp [host]:port value required")
	}

	/*
		if *saveInterval == "" {
			a.SaveInterval = 10 * time.Minute
			a.Logf("defaulting state save interval to 10 minutes")
		} else {
			d, err := time.ParseDuration(*saveInterval)
			if err != nil {
				return fmt.Errorf("invalid save-time interval: %v", err)
			}
			a.SaveInterval = d
			a.Logf("saving state to disk every %v", a.SaveInterval)
		}
	*/

	if *sqlDbName == "" {
		return fmt.Errorf("database name is required")
	}
	a.DatabaseName = *sqlDbName
	a.Logf("using database \"%s\" to store internal state", a.DatabaseName)

	return nil
}

// refreshClientPreamble updates the application's set of
// preamble data lists.
func (a *Application) refreshClientPreamble() error {
	if a.InitFile == "" {
		return nil
	}

	a.Debug(DebugInit, "acquiring a write lock on the preamble data")
	a.clientPreamble.lock.Lock()
	defer func() {
		a.Debug(DebugInit, "releasing write lock on preamble data")
		a.clientPreamble.lock.Unlock()
	}()
	a.Debug(DebugInit, "acquired write lock; proceeding")

	f, err := os.Open(a.InitFile)
	if err != nil {
		return err
	}
	defer f.Close()

	recordPattern := regexp.MustCompile("^(\\w+)\\s+({.*)")
	continuationPattern := regexp.MustCompile("^\\s+")
	endOfRecordPattern := regexp.MustCompile("^}")
	commandPattern := regexp.MustCompile("^(\\w+)\\s*$")

	a.clientPreamble.preamble = nil
	a.clientPreamble.postAuth = nil
	a.clientPreamble.postReady = nil
	a.clientPreamble.syncData = false
	currentPreamble := &a.clientPreamble.preamble

	scanner := bufio.NewScanner(f)
outerScan:
	for scanner.Scan() {
	rescan:
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		if strings.HasPrefix(scanner.Text(), "//") {
			*currentPreamble = append(*currentPreamble, scanner.Text())
			continue
		}
		if f := commandPattern.FindStringSubmatch(scanner.Text()); f != nil {
			// dataless command f[1]
			switch f[1] {
			case "AUTH":
				currentPreamble = &a.clientPreamble.postAuth
			case "READY":
				currentPreamble = &a.clientPreamble.postReady
			case "SYNC":
				a.clientPreamble.syncData = true
			default:
				return fmt.Errorf("invalid command \"%v\" in init file %s", scanner.Text(), a.InitFile)
			}
		} else if f := recordPattern.FindStringSubmatch(scanner.Text()); f != nil {
			// start of record type f[1] with start of JSON string f[2]
			// collect rest of string
			var dataPacket strings.Builder
			dataPacket.WriteString(f[2])

			for scanner.Scan() {
				if continuationPattern.MatchString(scanner.Text()) {
					dataPacket.WriteString(scanner.Text())
				} else {
					if endOfRecordPattern.MatchString(scanner.Text()) {
						dataPacket.WriteString(scanner.Text())
					}
					if err := commitInitCommand(f[1], dataPacket, currentPreamble); err != nil {
						return err
					}
					if !endOfRecordPattern.MatchString(scanner.Text()) {
						// We already read into next record
						goto rescan
					} else {
						continue outerScan
					}
				}
			}
			// We reached EOF while scanning with a command in progress
			if err := commitInitCommand(f[1], dataPacket, currentPreamble); err != nil {
				return err
			}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if (a.DebugLevel & DebugInit) != 0 {
		a.Debugf(DebugInit, "client initial commands from %v", a.InitFile)
		a.Debugf(DebugInit, "client sync: %v", a.clientPreamble.syncData)

		for i, p := range a.clientPreamble.preamble {
			a.Debugf(DebugInit, "client preamble #%d: %s", i, p)
		}
		for i, p := range a.clientPreamble.postAuth {
			a.Debugf(DebugInit, "client post-auth #%d: %s", i, p)
		}
		for i, p := range a.clientPreamble.postReady {
			a.Debugf(DebugInit, "client post-ready #%d: %s", i, p)
		}
	}

	return nil
}

func commitInitCommand(cmd string, src strings.Builder, dst *[]string) error {
	var b []byte
	var err error

	s := []byte(src.String())

	switch cmd {
	case "AI":
		var data mapper.AddImageMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "AI?":
		var data mapper.QueryImageMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "AV":
		var data mapper.AdjustViewMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "CC":
		var data mapper.ClearChatMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "CLR":
		var data mapper.ClearMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "CLR@":
		var data mapper.ClearFromMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "CO":
		var data mapper.CombatModeMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "CS":
		var data mapper.UpdateClockMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "DD=":
		var data mapper.UpdateDicePresetsMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "DSM":
		var data mapper.UpdateStatusMarkerMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "I":
		var data mapper.UpdateTurnMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "IL":
		var data mapper.UpdateInitiativeMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "L":
		var data mapper.LoadFromMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-ARC":
		var data mapper.LoadArcObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-CIRC":
		var data mapper.LoadCircleObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-LINE":
		var data mapper.LoadLineObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-POLY":
		var data mapper.LoadPolygonObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-RECT":
		var data mapper.LoadRectangleObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-SAOE":
		var data mapper.LoadSpellAreaOfEffectObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-TEXT":
		var data mapper.LoadTextObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "LS-TILE":
		var data mapper.LoadTileObjectMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "MARK":
		var data mapper.MarkMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "OA":
		var data mapper.UpdateObjAttributesMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "OA+":
		var data mapper.AddObjAttributesMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "OA-":
		var data mapper.RemoveObjAttributesMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "PROGRESS":
		var data mapper.UpdateProgressMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "AC", "PS":
		var data mapper.PlaceSomeoneMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "ROLL":
		var data mapper.RollResultMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "TB":
		var data mapper.ToolbarMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "TO":
		var data mapper.ChatMessageMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "UPDATES":
		var data mapper.UpdateVersionsMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	case "WORLD":
		var data mapper.WorldMessagePayload
		if err = json.Unmarshal(s, &data); err == nil {
			b, err = json.Marshal(data)
		}

	default:
		return fmt.Errorf("invalid command %v in initialization file", cmd)
	}

	if err == nil {
		*dst = append(*dst, fmt.Sprintf("%s %s", cmd, string(b)))
	}
	return err
}

func (a *Application) GetPersonalCredentials(user string) []byte {
	a.Debug(DebugAuth, "acquiring a read lock on the password data")
	a.clientAuth.lock.RLock()
	defer func() {
		a.Debug(DebugAuth, "releasing read lock on password data")
		a.clientAuth.lock.RUnlock()
	}()
	a.Debug(DebugAuth, "acquired read lock; proceeding")
	secret, ok := a.clientAuth.personalPasswords[user]
	if !ok {
		return nil
	}
	return secret
}

func (a *Application) newClientAuthenticator(user string) (*auth.Authenticator, error) {
	if a.PasswordFile == "" {
		return nil, nil
	}

	a.Debug(DebugAuth, "acquiring a read lock on the password data")
	a.clientAuth.lock.RLock()
	defer func() {
		a.Debug(DebugAuth, "releasing read lock on password data")
		a.clientAuth.lock.RUnlock()
	}()
	a.Debug(DebugAuth, "acquired read lock; proceeding")

	cauth := &auth.Authenticator{
		Secret:   a.clientAuth.groupPassword,
		GmSecret: a.clientAuth.gmPassword,
	}

	if user != "" {
		personalPass, ok := a.clientAuth.personalPasswords[user]
		if ok {
			cauth.SetSecret(personalPass)
			a.Debugf(DebugAuth, "using personal password for %s", user)
		} else {
			a.Debugf(DebugAuth, "no personal password found for %s, using group password", user)
		}
	}

	return cauth, nil
}

func (a *Application) refreshAuthenticator() error {
	if a.PasswordFile == "" {
		return nil
	}

	a.Debug(DebugInit, "acquiring a write lock on the password data")
	a.clientAuth.lock.Lock()
	defer func() {
		a.Debug(DebugInit, "releasing write lock on password data")
		a.clientAuth.lock.Unlock()
	}()
	a.Debug(DebugInit, "acquired write lock; proceeding")

	fp, err := os.Open(a.PasswordFile)
	if err != nil {
		a.Logf("unable to open password file \"%s\": %v", a.PasswordFile, err)
		return err
	}
	defer func() {
		if err := fp.Close(); err != nil {
			a.Logf("error closing %s: %v", a.PasswordFile, err)
		}
	}()

	a.clientAuth.groupPassword = []byte{}
	a.clientAuth.gmPassword = []byte{}
	a.clientAuth.personalPasswords = make(map[string][]byte)

	scanner := bufio.NewScanner(fp)
	if scanner.Scan() {
		// first line is the group password
		a.clientAuth.groupPassword = scanner.Bytes()
		a.Debug(DebugInit, "set group password")
		if scanner.Scan() {
			// next line, if any, is the gm-specific password
			a.clientAuth.gmPassword = scanner.Bytes()
			a.Debug(DebugInit, "set GM password")

			// following lines are <user>:<password> for individual passwords
			line := 3
			for scanner.Scan() {
				pp := strings.SplitN(scanner.Text(), ":", 2)
				if len(pp) != 2 {
					a.Logf("WARNING: %s, line %d: ignoring personal password: missing delimiter", a.PasswordFile, line)
				} else {
					a.clientAuth.personalPasswords[pp[0]] = []byte(pp[1])
					a.Debugf(DebugInit, "set personal password for %s", pp[0])
				}
				line++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		a.Logf("error reading %s: %v", a.PasswordFile, err)
		return err
	}

	return nil
}

func (a *Application) GetPreamble() ([]string, []string, []string, bool) {
	if a == nil {
		return nil, nil, nil, false
	}
	a.Debug(DebugIO, "acquiring read lock on preamble data")
	a.clientPreamble.lock.RLock()
	a.Debug(DebugIO, "read lock granted; continuing")
	defer func() {
		a.Debug(DebugIO, "releasing read lock on preamble data")
		a.clientPreamble.lock.RUnlock()
	}()

	return a.clientPreamble.preamble, a.clientPreamble.postAuth, a.clientPreamble.postReady, a.clientPreamble.syncData
}

func (a *Application) HandleServerMessage(payload mapper.MessagePayload, requester *mapper.ClientConnection) {
	a.Logf("Received %T %v", payload, payload)
	switch p := payload.(type) {
	case mapper.AddImageMessagePayload:
		for _, instance := range p.Sizes {
			if instance.ImageData != nil && len(instance.ImageData) > 0 {
				a.Logf("not storing image \"%s\"@%v (inline image data not supported)", p.Name, instance.Zoom)
				continue
			}
			if err := a.StoreImageData(p.Name, mapper.ImageInstance{
				Zoom:        instance.Zoom,
				IsLocalFile: instance.IsLocalFile,
				File:        instance.File,
			}); err != nil {
				a.Logf("error storing image data for \"%s\"@%v: %v", p.Name, instance.Zoom, err)
			}
		}
		if err := a.SendToAllExcept(requester, mapper.AddImage, p); err != nil {
			a.Logf("error sending on AddImage to peer systems: %v", err)
		}

	case mapper.QueryImageMessagePayload:
		imgData, err := a.QueryImageData(mapper.ImageDefinition{Name: p.Name})
		if err != nil {
			a.Logf("unable to answer QueryImage (%v)", err)
			if err := a.SendToAllExcept(requester, mapper.QueryImage, p); err != nil {
				a.Logf("error sending QueryImage on to peers, as well: %v", err)
			}
			return
		}

		// Now that we have all the answers we know about, figure out
		// which we can answer directly and which ones we'll need to
		// call in help for.
		var answers mapper.AddImageMessagePayload
		var questions mapper.QueryImageMessagePayload

		answers.Name = p.Name
		questions.Name = p.Name
		for _, askedFor := range p.Sizes {
			// do we know the answer to this one?
			askOthers := true
			for _, found := range imgData.Sizes {
				if found.Zoom == askedFor.Zoom {
					// yes!
					answers.Sizes = append(answers.Sizes, mapper.ImageInstance{
						Zoom:        found.Zoom,
						IsLocalFile: found.IsLocalFile,
						File:        found.File,
					})
					askOthers = false
					break
				}
			}
			if askOthers {
				// we didn't find it in the database, ask if anyone else knows...
				questions.Sizes = append(questions.Sizes, mapper.ImageInstance{
					Zoom: askedFor.Zoom,
				})
			}
		}

		if len(answers.Sizes) > 0 {
			if err := requester.Conn.Send(mapper.AddImage, answers); err != nil {
				a.Logf("error sending QueryImage answer to requester: %v", err)
			}
		}

		if len(questions.Sizes) > 0 {
			if err := a.SendToAllExcept(requester, mapper.QueryImage, questions); err != nil {
				a.Logf("error asking QueryImage query out to other peers: %v", err)
			}
		}

	case mapper.ClearChatMessagePayload:
		if requester != nil && requester.Auth != nil {
			p.RequestedBy = requester.Auth.Username
		}
		p.MessageID = <-a.MessageIDGenerator
		a.SendToAllExcept(requester, mapper.ClearChat, p)
		if err := a.ClearChatHistory(p.Target); err != nil {
			a.Logf("error clearing chat history (target=%d): %v", p.Target, err)
		}
		if err := a.AddToChatHistory(p.MessageID, mapper.ClearChat, p); err != nil {
			a.Logf("unable to add ClearChat event to chat history: %v", err)
		}

	case mapper.RollDiceMessagePayload:
		if requester.Auth == nil {
			a.Logf("refusing to accept die roll from unauthenticated user")
			requester.Conn.Send(mapper.ChatMessage, mapper.ChatMessageMessagePayload{
				ChatCommon: mapper.ChatCommon{
					MessageID: <-a.MessageIDGenerator,
				},
				Text: "I can't accept your die roll request. I don't know who you even are.",
			})
			return
		}

		label, results, err := requester.D.DoRoll(p.RollSpec)
		if err != nil {
			requester.Conn.Send(mapper.ChatMessage, mapper.ChatMessageMessagePayload{
				ChatCommon: mapper.ChatCommon{
					MessageID: <-a.MessageIDGenerator,
				},
				Text: fmt.Sprintf("Unable to understand your die-roll request: %v", err),
			})
			return
		}
		var genericParts []string
		for _, part := range strings.Split(label, "‖") {
			if pos := strings.IndexRune(part, '≡'); pos >= 0 {
				genericParts = append(genericParts, part[:pos])
			} else {
				genericParts = append(genericParts, part)
			}
		}
		genericLabel := strings.Join(genericParts, ", ")

		response := mapper.RollResultMessagePayload{
			ChatCommon: mapper.ChatCommon{
				Recipients: p.Recipients,
				ToAll:      p.ToAll,
				ToGM:       p.ToGM,
			},
			Title: label,
		}
		for _, r := range results {
			response.MessageID = <-a.MessageIDGenerator
			response.Result = r

			if err := a.AddToChatHistory(response.MessageID, mapper.RollResult, response); err != nil {
				a.Logf("unable to add RollResult event to chat history: %v", err)
			}

			for _, peer := range a.GetClients() {
				if p.ToGM {
					if peer == requester {
						peer.Conn.Send(mapper.ChatMessage, mapper.ChatMessageMessagePayload{
							ChatCommon: mapper.ChatCommon{
								MessageID: <-a.MessageIDGenerator,
							},
							Text: "(die-roll result sent to GM)",
						})
					}
					if peer.Auth == nil || !peer.Auth.GmMode {
						a.Debugf(DebugIO, "sending to GM and %v isn't the GM (skipped)", peer.IdTag())
						continue
					}
				} else if !p.ToAll {
					if peer.Auth == nil || peer.Auth.Username == "" {
						a.Debugf(DebugIO, "sending to explicit list but we don't know who %v is (skipped)", peer.IdTag())
						continue
					}
					if peer.Auth.Username != requester.Auth.Username && slices.Index[string](p.Recipients, peer.Auth.Username) < 0 {
						a.Debugf(DebugIO, "sending to explicit list but user \"%s\" (from %v) isn't on the list (skipped)", peer.Auth.Username, peer.IdTag())
						continue
					}
				}

				if peer.Features.DiceColorBoxes {
					response.Title = label
				} else {
					response.Title = genericLabel
				}

				if err := peer.Conn.Send(mapper.RollResult, response); err != nil {
					a.Logf("error sending die-roll result %v to %v: %v", response, peer.IdTag(), err)
				}
			}
		}

	case mapper.SyncChatMessagePayload:
		if err := a.QueryChatHistory(p.Target, requester); err != nil {
			a.Logf("error syncing chat history (target=%d): %v", p.Target, err)
		}

	case mapper.DefineDicePresetsMessagePayload:
		if requester.Auth == nil {
			a.Logf("Unable to store die-roll preset for unauthenticated user")
			return
		}

		target := requester.Auth.Username
		if p.For != "" {
			if requester.Auth.GmMode {
				target = p.For
				a.Debugf(DebugIO, "GM requests storage of die-roll presets for %s", target)
			} else {
				a.Logf("non-GM request to change die-roll presets for %s ignored", p.For)
			}
		}

		if err := a.StoreDicePresets(target, p.Presets, true); err != nil {
			a.Logf("error storing die-roll preset: %v", err)
		}
		if err := a.SendDicePresets(target); err != nil {
			a.Logf("error sending die-roll presets after changing them: %v", err)
		}

	case mapper.AddDicePresetsMessagePayload:
		if requester.Auth == nil {
			a.Logf("Unable to store die-roll preset for unauthenticated user")
			return
		}

		target := requester.Auth.Username
		if p.For != "" {
			if requester.Auth.GmMode {
				target = p.For
				a.Debugf(DebugIO, "GM requests add to die-roll presets for %s", target)
			} else {
				a.Logf("non-GM request to add to die-roll presets for %s ignored", p.For)
			}
		}

		if err := a.StoreDicePresets(target, p.Presets, false); err != nil {
			a.Logf("error adding to die-roll preset: %v", err)
		}
		if err := a.SendDicePresets(target); err != nil {
			a.Logf("error sending die-roll presets after changing them: %v", err)
		}

	case mapper.FilterDicePresetsMessagePayload:
		if requester.Auth == nil {
			a.Logf("Unable to filter die-roll preset for unauthenticated user")
			return
		}

		target := requester.Auth.Username
		if p.For != "" {
			if requester.Auth.GmMode {
				target = p.For
				a.Debugf(DebugIO, "GM requests filter of die-roll presets for %s", target)
			} else {
				a.Logf("non-GM request to filter die-roll presets for %s ignored", p.For)
			}
		}

		if err := a.FilterDicePresets(target, p); err != nil {
			a.Logf("error filtering die-roll preset for %s with /%s/: %v", target, p.Filter, err)
		}
		if err := a.SendDicePresets(target); err != nil {
			a.Logf("error sending die-roll presets after filtering them: %v", err)
		}

	case mapper.QueryDicePresetsMessagePayload:
		if requester.Auth == nil {
			a.Logf("Unable to query die-roll preset for unauthenticated user")
			return
		}

		target := requester.Auth.Username
		if p.For != "" {
			if requester.Auth.GmMode {
				target = p.For
				a.Debugf(DebugIO, "GM requests die-roll presets for %s", target)
			} else {
				a.Logf("non-GM request to get die-roll presets for %s ignored", p.For)
			}
		}
		if err := a.SendDicePresets(target); err != nil {
			a.Logf("error sending die-roll presets: %v", err)
		}

	case mapper.ChatMessageMessagePayload:
		if requester.Auth == nil {
			a.Logf("refusing to pass on chat message from unauthenticated user")
			_ = requester.Conn.Send(mapper.ChatMessage, mapper.ChatMessageMessagePayload{
				ChatCommon: mapper.ChatCommon{
					MessageID: <-a.MessageIDGenerator,
				},
				Text: "I can't accept that chat message since I don't know who you even are.",
			})
			return
		}

		p.Sender = requester.Auth.Username
		p.MessageID = <-a.MessageIDGenerator

		if err := a.AddToChatHistory(p.MessageID, mapper.ChatMessage, p); err != nil {
			a.Logf("unable to add ChatMessage event to chat history: %v", err)
		}

		for _, peer := range a.GetClients() {
			if p.ToGM {
				if peer.Auth == nil || (!peer.Auth.GmMode && peer.Auth.Username != requester.Auth.Username) {
					a.Debugf(DebugIO, "sending to GM and %v isn't the GM (skipped)", peer.IdTag())
					continue
				}
			} else if !p.ToAll {
				if peer.Auth == nil || peer.Auth.Username == "" {
					a.Debugf(DebugIO, "sending to explicit list but we don't know who %v is (skipped)", peer.IdTag())
					continue
				}
				if peer.Auth.Username != requester.Auth.Username && slices.Index[string](p.Recipients, peer.Auth.Username) < 0 {
					a.Debugf(DebugIO, "sending to explicit list but user \"%s\" (from %v) isn't on the list (skipped)", peer.Auth.Username, peer.IdTag())
					continue
				}
			}

			if err := peer.Conn.Send(mapper.ChatMessage, p); err != nil {
				a.Logf("error sending message %v to %v: %v", p, peer.IdTag(), err)
			}
		}

	case mapper.QueryPeersMessagePayload:
		var peers mapper.UpdatePeerListMessagePayload
		for _, peer := range a.GetClients() {
			thisPeer := mapper.Peer{
				Addr:     peer.Address,
				LastPolo: time.Since(peer.LastPoloTime).Seconds(),
				IsMe:     peer == requester,
			}
			if peer.Auth != nil {
				thisPeer.User = peer.Auth.Username
				thisPeer.Client = peer.Auth.Client
				thisPeer.IsAuthenticated = peer.Auth.Username != ""
			}
			peers.PeerList = append(peers.PeerList, thisPeer)
		}
		if err := requester.Conn.Send(mapper.UpdatePeerList, peers); err != nil {
			a.Logf("error sending peer list: $v", err)
		}

	// These commands are passed on to our peers with no further action required.
	case mapper.MarkMessagePayload,
		mapper.UpdateProgressMessagePayload:
		a.SendToAllExcept(requester, payload.MessageType(), payload)

	// These commands are passed on to our peers and remembered for later sync operations.
	// TODO effect on state
	case mapper.AdjustViewMessagePayload, mapper.ClearMessagePayload, mapper.ClearFromMessagePayload,
		mapper.LoadFromMessagePayload,
		mapper.LoadArcObjectMessagePayload,
		mapper.LoadCircleObjectMessagePayload,
		mapper.LoadLineObjectMessagePayload,
		mapper.LoadPolygonObjectMessagePayload,
		mapper.LoadRectangleObjectMessagePayload,
		mapper.LoadSpellAreaOfEffectObjectMessagePayload,
		mapper.LoadTextObjectMessagePayload,
		mapper.LoadTileObjectMessagePayload,
		mapper.AddObjAttributesMessagePayload,
		mapper.RemoveObjAttributesMessagePayload,
		mapper.UpdateObjAttributesMessagePayload,
		mapper.PlaceSomeoneMessagePayload:
		a.SendToAllExcept(requester, payload.MessageType(), payload)

	// TODO as above but they are privileged
	case mapper.CombatModeMessagePayload, mapper.UpdateStatusMarkerMessagePayload,
		mapper.UpdateTurnMessagePayload, mapper.UpdateInitiativeMessagePayload,
		mapper.UpdateClockMessagePayload, mapper.ToolbarMessagePayload:
		if requester == nil || requester.Auth == nil {
			a.Logf("refusing to execute privileged command %v for unauthenticated user", p.MessageType())
			requester.Conn.Send(mapper.Priv, mapper.PrivMessagePayload{
				Command: p.RawMessage(),
				Reason:  "You are not the GM. You might not even be real.",
			})
			return
		}
		if !requester.Auth.GmMode {
			a.Logf("refusing to execute privileged command %v %v for non-GM user %s", p.MessageType(), p, requester.Auth.Username)
			requester.Conn.Send(mapper.Priv, mapper.PrivMessagePayload{
				Command: p.RawMessage(),
				Reason:  "You are not the GM.",
			})
			return
		}
		a.SendToAllExcept(requester, payload.MessageType(), payload)

	case mapper.SyncMessagePayload:
		//TODO
	}
}

func (a *Application) SendToAllExcept(c *mapper.ClientConnection, cmd mapper.ServerMessage, data any) error {
	if c == nil {
		a.Debugf(DebugIO, "sending %v %v to all clients", cmd, data)
	} else {
		a.Debugf(DebugIO, "sending %v %v to all clients except %v", cmd, data, c.IdTag())
	}
	var reportedError error

	for _, peer := range a.GetClients() {
		a.Debugf(DebugIO, "peer %v", peer.IdTag())
		if c == nil || peer != c {
			a.Debugf(DebugIO, "-> %v %v %v", peer.IdTag(), cmd, data)
			if err := peer.Conn.Send(cmd, data); err != nil {
				a.Logf("error sending %v to client %v: %v", data, peer.IdTag(), err)
				reportedError = err
			}
		}
	}
	return reportedError
}

func (a *Application) SendToAll(cmd mapper.ServerMessage, data any) error {
	return a.SendToAllExcept(nil, cmd, data)
}
