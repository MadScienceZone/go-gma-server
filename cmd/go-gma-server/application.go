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

	"github.com/MadScienceZone/go-gma/v5/auth"
	"github.com/MadScienceZone/go-gma/v5/mapper"
	"github.com/MadScienceZone/go-gma/v5/util"
)

type DebugFlags uint64

const (
	DebugAuth DebugFlags = 1 << iota
	DebugEvents
	DebugIO
	DebugInit
	DebugMisc
	DebugAll DebugFlags = 0xffffffff
)

func DebugFlagNames(flags DebugFlags) string {
	if flags == 0 {
		return "<none>"
	}
	if flags == DebugAll {
		return "<all>"
	}

	var list []string
	for _, f := range []struct {
		bits DebugFlags
		name string
	}{
		{bits: DebugAuth, name: "auth"},
		{bits: DebugEvents, name: "events"},
		{bits: DebugIO, name: "i/o"},
		{bits: DebugInit, name: "init"},
		{bits: DebugMisc, name: "misc"},
	} {
		if (flags & f.bits) != 0 {
			list = append(list, f.name)
		}
	}

	return "<" + strings.Join(list, ",") + ">"
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
		for _, flag := range strings.Split(*debugFlags, ",") {
			switch flag {
			case "none":
				a.DebugLevel = 0
			case "all":
				a.DebugLevel = DebugAll
			case "auth":
				a.DebugLevel = DebugAuth
			case "events":
				a.DebugLevel = DebugEvents
			case "I/O", "i/o", "io":
				a.DebugLevel = DebugIO
			case "init":
				a.DebugLevel |= DebugInit
			case "misc":
				a.DebugLevel |= DebugMisc
			default:
				return fmt.Errorf("No such -debug flag: \"%s\"", flag)
			}
		}
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

func (a *Application) HandleServerMessage(payload mapper.MessagePayload) {
	a.Logf("Received %T %v", payload, payload)
	switch p := packet.(type) {
	case mapper.AddImageMessagePayload:
	}

}
