package main

import "log"

func ErrorCheck(e error, m string) {
	if e != nil {
		log.Fatal(m)
	}
}
