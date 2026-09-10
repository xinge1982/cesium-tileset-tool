package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

var configPath = flag.String("c", "config.yaml", "config file path")
var logFile = flag.Bool("logfile", false, "log into file")

var GitCommit string
var BuildDate string

const Version string = "2.0"

type MyFormatter struct {
}

type Runner interface {
	Init([]string) error
	Run() error
	Name() string
}

func main() {
	subCmds := []Runner{
		NewFeatureTileSetBuildCommand(),
	}
	if len(os.Args) > 1 {
		for _, cmd := range subCmds {
			if strings.EqualFold(os.Args[1], cmd.Name()) {
				err := cmd.Init(os.Args[2:])
				if err != nil {
					log.Error(err)
					return
				}
				err = cmd.Run()
				if err != nil {
					log.Error(err)
				}
				return
			}
		}
	}

	flag.Parse()
}

func (m *MyFormatter) Format(entry *log.Entry) ([]byte, error) {
	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	timestamp := entry.Time.Local().Format("2006-01-02 15:04:05")
	var newLog string
	newLog = fmt.Sprintf("%s\t%s\t%s\n", timestamp, entry.Level, entry.Message)

	b.WriteString(newLog)
	return b.Bytes(), nil
}
