package main

import (
	"fmt"
	"github.com/brimstone/watchdock/docker"
)

func main() {
	fmt.Println("from main")
	fmt.Println(docker.Hello())
}
