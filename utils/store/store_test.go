package store

import (
	"fmt"
	"testing"
)

func TestGetItem(t *testing.T) {

	StoreItem("abc", UserPower{UserId: 1})

	var userPower UserPower

	GetItem("abc", &userPower)

	fmt.Println(userPower.UserId)
}

func TestGetItem2(t *testing.T) {

	setItem("abc", UserPower{UserId: 1})

	var userPower = new(UserPower)

	getItem("abc", userPower)

	fmt.Println(userPower.UserId)
}

var powers = make(map[string]UserPower, 10)

func setItem(key string, power UserPower) {
	powers[key] = power
}

func getItem(key string, power *UserPower) {
	// val := powers[key]
	// *&power = &val
	(*&power).UserId = 100
}

type UserPower struct {
	UserId uint64
}
