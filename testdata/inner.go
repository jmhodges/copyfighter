package main

import "net/http"

type someInt interface {
	Bang()
}
type bar struct {
	baz int
}

type other struct {
	quux int64
	srv  *http.Server
	si   someInt
}

func main() {
	type foo string
}

type Foo http.Client

func CallsFoo(f Foo) {

}

func (f Foo) OnOtherToo(o other) {

}

func (o other) OnStruct() {

}
func (o other) OnStruct2() {

}

func (o *other) OnPtr() {

}
func (o *other) OnPtr2() {

}
func (o *other) OnPtr3() {

}

// config is 72 bytes on a 64-bit word size: two strings, two int64s, and a
// slice. It is the only struct in this package wider than the default limit.
type config struct {
	name    string
	addr    string
	timeout int64
	retries int64
	tags    []string
}

func Configure(c config) {

}

func (c config) Validate() {

}
