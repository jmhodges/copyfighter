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
