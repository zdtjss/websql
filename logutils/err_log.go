package logutils

import "log"

func Println(err error) {
	if err != nil {
		log.Println(err)
	}
}

func Printf(format string, err ...any) {
	if len(err) == 0 || (len(err) == 1 && err[0] == nil) {
		return
	} else {
		log.Printf(format, err...)
	}
}

func Panicln(err error) {
	if err != nil {
		log.Panicln(err)
	}
}

func Panicf(format string, err ...any) {
	if len(err) == 0 || (len(err) == 1 && err[0] == nil) {
		return
	} else {
		log.Panicf(format, err...)

	}
}
