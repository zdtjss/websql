package logutils

import (
	"log"
	"os"
)

func init() {
	logFile, err := os.OpenFile("./websql.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		log.Fatalf("create log file err %+v", err)
	}
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}
