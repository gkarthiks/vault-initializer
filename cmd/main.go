package main

import (
	log "github.com/sirupsen/logrus"
	"os"
	"vault-initializer/utility"
)

var (
	avail           bool
	secSharesStr    string
	secThresholdStr string
	err             error
)

func init() {
	appMode, avail := os.LookupEnv("APP_MODE")
	if !avail {
		appMode = "debug"
		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
		log.SetLevel(log.DebugLevel)
	} else if appMode == "production" {
		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
		log.SetLevel(log.InfoLevel)
	} else {
		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
		log.SetLevel(log.DebugLevel)
	}

	vaultInitConfigMap, avail := os.LookupEnv("INIT_CONFIG_MAP")
	if !avail {
		log.Panic("The initialization config map is not specified")
	} else {
		log.Debugf("The initialization config map is specified as %s", vaultInitConfigMap)
	}

	utility.ParseInitConfigData(vaultInitConfigMap)

}

func main() {
	doneCh := make(chan bool)
	go func() {
		utility.StartRoutine()
	}()
	<-doneCh
}
