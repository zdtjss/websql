package utils

import (
	"fmt"
	"log"
	"testing"
	"time"
)

func TestPtr(t *testing.T) {

	// t1()
	// t2()
	// t3()
	// t4()
	t5()
}

func TestRandomInt64(t *testing.T) {
	for range 100 {
		time.Sleep(10 * time.Millisecond)
		id := RandomInt64()
		fmt.Println(id)
	}
}

func t1() {
	name := "tomcat"
	usr := &User{Id: "001", Name: &name}
	log.Printf("t1 id: %v, name: %v", &usr.Id, usr.Name)
}

func t2() {
	name := "tomcat"
	usr := &User{Id: "001", Name: &name}
	log.Printf("t2 id: %v, name: %v", &usr.Id, usr.Name)
	usr.Id = "mod"
	name = "nginx"
	usr.Name = &name
	log.Printf("t2 id: %v, name: %v", &usr.Id, usr.Name)
}

func t3() {
	name := "tomcat"
	usr2 := User{Id: "001", Name: &name}
	log.Printf("t3 id: %v, name: %v", &usr2.Id, usr2.Name)
}
func t4() {
	name := "tomcat"
	usr2 := User{Id: "001", Name: &name}
	log.Printf("t4 id: %v, name: %v", &usr2.Id, usr2.Name)

	usr2.Id = "mod"
	name = "nginx"
	usr2.Name = &name
	log.Printf("t4 id: %v, name: %v", &usr2.Id, usr2.Name)
}

func t5() {

	catName := "tom"
	cat := Cat{
		Id:   "tom1",
		Name: &catName,
	}
	cat2 := &Cat{
		Id:   "tom1",
		Name: &catName,
	}

	name := "tomcat"
	usr2 := User{Id: "001", Name: &name, Cat: cat, Cat2: cat2}
	log.Printf("t5 cid1: %v, cname1: %v,cid2: %v,cname2: %v, catName:%v", &usr2.Cat.Id, usr2.Cat.Name, &usr2.Cat2.Id, usr2.Cat2.Name, &catName)
	log.Printf("t5 cid1: %v, cname1: %v,cid2: %v,cname2: %v", usr2.Cat.Id, *usr2.Cat.Name, usr2.Cat2.Id, *usr2.Cat2.Name)

	t6(usr2, catName)

	catName = "tom2"
	cat.Id = "cm"
	*&cat.Name = &catName

	cat2.Id = "cm2"
	cat2.Name = &catName
	log.Printf("t5 cid1: %v, cname1: %v,cid2: %v,cname2: %v", &usr2.Cat.Id, usr2.Cat.Name, &usr2.Cat2.Id, usr2.Cat2.Name)
	log.Printf("t5  cid1: %v, cname1: %v,cid2: %v,cname2: %v, catName: %v", usr2.Cat.Id, *usr2.Cat.Name, usr2.Cat2.Id, *usr2.Cat2.Name, &catName)

	t7(&usr2, &catName)
}

func t6(usr2 User, catName string) {
	log.Printf("t6 cid1: %v, cname1: %v,cid2: %v,cname2: %v, catName:%v", &usr2.Cat.Id, usr2.Cat.Name, &usr2.Cat2.Id, usr2.Cat2.Name, &catName)
	log.Printf("t6 cid1: %v, cname1: %v,cid2: %v,cname2: %v", usr2.Cat.Id, *usr2.Cat.Name, usr2.Cat2.Id, *usr2.Cat2.Name)
}

func t7(usr2 *User, catName *string) {
	log.Printf("t7 cid1: %v, cname1: %v,cid2: %v,cname2: %v", &usr2.Cat.Id, usr2.Cat.Name, &usr2.Cat2.Id, usr2.Cat2.Name)
	log.Printf("t7  cid1: %v, cname1: %v,cid2: %v,cname2: %v, catName: %v", usr2.Cat.Id, *usr2.Cat.Name, usr2.Cat2.Id, *usr2.Cat2.Name, catName)
}

type User struct {
	Id   string
	Name *string
	Cat  Cat
	Cat2 *Cat
}

type Cat struct {
	Id   string
	Name *string
}
