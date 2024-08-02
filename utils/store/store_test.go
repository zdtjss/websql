package store

import (
	"fmt"
	"go-web/config"
	"testing"
)

func TestGetItem(t *testing.T) {

	Add("abc", UserPower{UserId: config.AdminId})

	var userPower UserPower

	Get("abc", &userPower)

	fmt.Println(userPower.UserId)
}

func TestGetItem2(t *testing.T) {

	setItem("abc", UserPower{UserId: config.AdminId})

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
	// *&power = &vala
	power.UserId = "100"
}

type UserPower struct {
	UserId string
}
