package logger

import (
	"log"
	"os"
)

func init() {
	logFile, err := os.OpenFile("./websql.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		log.Printf("创建日志文件失败，将使用标准输出: %+v", err)
		return
	}
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func PrintErr(err error) {
	if err != nil {
		log.Println(err)
	}
}

func PrintErrf(format string, err error, msg ...any) {
	if err == nil {
		return
	}
	if len(msg) == 0 {
		log.Printf(format+" err : %s\n", err.Error())

	} else {
		msg = append(msg, err.Error())
		log.Printf(format+" err : %s\n", msg...)
	}
}

func PanicErr(err error) {
	if err != nil {
		log.Panicln(err)
	}
}

func PanicErrf(format string, err error, msg ...any) {
	if err == nil {
		return
	}
	if len(msg) == 0 {
		log.Panicf(format+" err : %s \n", err.Error())
	} else {
		msg = append(msg, err.Error())
		log.Panicf(format+" err : %s\n", msg...)
	}
}