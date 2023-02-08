package logutils

import "log"

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
