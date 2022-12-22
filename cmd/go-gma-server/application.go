package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MadScienceZone/go-gma/v5/mapper"
	"github.com/lestrrat-go/strftime"
)

//
// Application holds the global settings and other context for the application
// generally.
//
type Application struct {
	// Logger is whatever device or file we're writing logs to.
	Logger *log.Logger

	// If DebugLevel is 0, no extra debugging output will be logged.
	// Otherwise, increasing levels of output are generated for
	// increasing values of DebugLevel.
	DebugLevel int

	// Endpoint is the "[host]:port" string which specifies where our
	// incoming socket is listening.
	Endpoint string

	// If not empty, this gives the filename from which we are to read in
	// the initial client command set.
	InitFile string

	clientPreamble struct {
		syncData bool
		lastRead time.Time
		preamble []string
		postAuth []string
		postReady []string
	}

	// If not empty, we require authentication, with passwords taken
	// from this file.
	PasswordFile string

	// How often to save internal state to disk.
	SaveInterval time.Duration

	// Pathname for database file.
	DatabaseName string

}

//
// Debug logs messages conditionally based on the currently set
// debug level. It acts just like fmt.Println as far as formatting
// its arguments.
//
func (a *Application) Debug(level int, message ...any) {
	if a != nil && a.Logger != nil && a.DebugLevel >= level {
		a.Logger.Println(message...)
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
func (a *Application) Debugf(level int, format string, args ...any) {
	if a != nil && a.Logger != nil && a.DebugLevel >= level {
		a.Logger.Printf(format, args...)
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
	var saveInterval = flag.String("save-interval", "10m", "Save internal state this often")
	var sqlDbName = flag.String("sqlite", "", "Specify filename for sqlite database to use")
	flag.Parse()

	if *logFile == "" {
		a.Logger = nil
	} else {
		a.Logger = log.Default()
		if *logFile != "-" {
			path, err := a.FancyFileName(*logFile)
			if err != nil {
				return fmt.Errorf("unable to understand log file path \"%s\": %v", *logFile, err)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("unable to open log file: %v", err)
			} else {
				a.Logger.SetOutput(f)
			}
		}
	}

	if *initFile != "" {
		a.InitFile = *initFile
		a.Logf("reading client initial command set from \"%s\"", a.InitFile)
		p1, p2, p3, e := a.ClientPreamble()
		if e != nil {
			a.Logf("error reading init file \"%s\": %v", a.InitFile, e)
			os.Exit(1)
		}
		a.Logf("preamble: %v", p1)
		a.Logf("preamble (post-auth): %v", p2)
		a.Logf("preamble: (post-ready): %v", p3)
	}

	if *passFile != "" {
		a.PasswordFile = *passFile
		a.Logf("authentication enabled via \"%s\"", a.PasswordFile)
	} else {
		a.Log("WARNING: authentication not enabled!")
	}

	if *endPoint != "" {
		a.Endpoint = *endPoint
		a.Logf("configured to listen on \"%s\"", a.Endpoint)
	} else {
		return fmt.Errorf("non-empty tcp [host]:port value required")
	}

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

	if *sqlDbName == "" {
		return fmt.Errorf("database name is required")
	}
	a.DatabaseName = *sqlDbName
	a.Logf("using database \"%s\" to store internal state", a.DatabaseName)

	return nil
}

//
// FancyFileName expands tokens found in the path string to allow the user
// to specify dynamically-named files at runtime. If there's a problem with
// the formatting, an error is returned along with the original path.
//
// The tokens which may appear in the path include the following
// (note that all of these are modified as appropriate to the locale's
// national conventions and language):
//    %A   full weekday name
//    %a   abbreviated weekday name
//    %B   full month name
//    %b   abbreviated month name
//    %C   zero-padded two-digit year 00-99
//    %c   time and date
//    %d   day of month as number 01-31 (zero padded)
//    %e   day of month as number  1-31 (space padded)
//    %F   == %Y-%m-%d
//    %H   hour as number 00-23 (zero padded)
//    %h   abbreviated month name (same as %b)
//    %I   hour as number 01-12 (zero padded)
//    %j   day of year as number 001-366
//    %k   hour as number  0-23 (space padded)
//    %L   milliseconds as number 000-999
//    %l   hour as number  1-12 (space padded)
//    %M   minute as number 00-59
//    %m   month as number 01-12
//    %P   process ID
//    %p   AM or PM
//    %R   == %H:%M
//    %r   == %I:%M:%S %p
//    %S   second as number 00-60
//    %s   Unix timestamp as a number
//    %T   == %H:%M:%S
//    %U   week of the year as number 00-53 (Sunday as first day of week)
//    %u   weekday as number (1=Monday .. 7=Sunday)
//    %V   week of the year as number 00-53 (Monday as first day of week)
//    %v   == %e-%b-%Y
//    %W   week of the year as number 00-53 (Monday as first day of week)
//    %w   weekday as number (0=Sunday .. 6=Saturday)
//    %X   time
//    %x   date
//    %Y   full year
//    %y   two-digit year (00-99)
//    %Z   time zone name
//    %z   time zone offset from UTC
//    %µ   microseconds as number 000-999
//    %%   literal % character
//
func (a *Application) FancyFileName(path string) (string, error) {
	ss := strftime.NewSpecificationSet()

	if err := ss.Delete('n'); err != nil {
		return path, err
	}
	if err := ss.Delete('t'); err != nil {
		return path, err
	}
	if err := ss.Delete('D'); err != nil {
		return path, err
	}
	if err := ss.Set('P', strftime.Verbatim(strconv.Itoa(os.Getpid()))); err != nil {
		return path, err
	}

	return strftime.Format(path, time.Now(),
		strftime.WithSpecificationSet(ss),
		strftime.WithUnixSeconds('s'),
		strftime.WithMilliseconds('L'),
		strftime.WithMicroseconds('µ'),
	)
}

// ClientPreamble returns a copy of the client preamble data
// as three slices of strings, representing the initial data
// to be sent, data after authentication, and data after
// the client negotiation is complete. If the initialization
// file has changed since we last read it, we read and parse
// that data first, caching it for subsequent calls.
//
// This is not thread-safe.
func (a *Application) ClientPreamble() ([]string, []string, []string, error) {
	if a.InitFile == "" {
		return nil, nil, nil, nil
	}

	fileInfo, err := os.Stat(a.InitFile)
	if err != nil {
		return nil, nil, nil, err
	}

	if a.clientPreamble.lastRead.IsZero() || fileInfo.ModTime().After(a.clientPreamble.lastRead) {
		f, err := os.Open(a.InitFile)
		if err != nil {
			return nil, nil, nil, err
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
					return nil, nil, nil, fmt.Errorf("invalid command \"%v\" in init file %s", scanner.Text(), a.InitFile)
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
							return nil, nil, nil, err
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
					return nil, nil, nil, err
				}
				break
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, nil, nil, err
		}
	}

	return a.clientPreamble.preamble, a.clientPreamble.postAuth, a.clientPreamble.postReady, nil
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

